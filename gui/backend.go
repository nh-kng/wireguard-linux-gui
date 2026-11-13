package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/ungerik/go-dry"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/settings"
)

const tunnelCommandTimeout = 2 * time.Minute

// Bound diagnostic output even for verbose hooks. Kill the entire process group
// on timeout/cancellation so descendants cannot hold the output pipes open.
type commandOutput struct{ buffer bytes.Buffer }

func (b *commandOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := (64 << 10) - b.buffer.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.buffer.Write(p)
	}
	return n, nil
}
func runTunnelCommand(ctx context.Context, command string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, tunnelCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var output commandOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return output.buffer.Bytes(), err
}

var runWGQuick = func(ctx context.Context, action, name string) ([]byte, error) {
	return runTunnelCommand(ctx, "wg-quick", action, name)
}

type tunnelBackend struct {
	client      wireguardClient
	settings    *settings.Settings
	reconnected bool
	changed     bool
	ctx         context.Context
}

// Synchronous entry points are used by unit and explicitly invoked namespace
// tests. Production UI actions use startTunnelOperation below.
func (t *Tunnels) ToggleTunnel(row *gtk.ListBoxRow, name string) error {
	if row != nil && name == "" {
		name = row.Name()
	}
	defer t.RefreshStatus()
	backend := tunnelBackend{client: wgc, settings: Settings, reconnected: t.reconnectedRememberedServers, ctx: context.Background()}
	err := backend.toggle(name)
	return errors.Join(err, backend.save())
}
func (t *Tunnels) deactivateAll() error {
	defer t.RefreshStatus()
	backend := tunnelBackend{client: wgc, settings: Settings, reconnected: t.reconnectedRememberedServers, ctx: context.Background()}
	err := backend.deactivateAll()
	return errors.Join(err, backend.save())
}
func (b *tunnelBackend) save() error {
	if b.changed {
		return b.settings.Save()
	}
	return nil
}

var policyPath = "/usr/share/polkit-1/actions/wireguard-linux-gui.policy"

func (b *tunnelBackend) toggle(name string) error {
	if !validTunnelName(name) {
		return errors.New("invalid tunnel name")
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	list, err := b.client.Devices()
	if err != nil {
		return err
	}
	activeNames := deviceNames(list)
	// Disconnect from the given tunnel, then query the resulting actual state.
	dc := b.disconnectTunnel

	// disconnects from all tunnels before connecting to a new one
	// when the multipleTunnels option is disabled
	if b.settings.MultipleTunnels {
		d, err := b.client.Device(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if !errors.Is(err, os.ErrNotExist) {
			if err := dc(d); err != nil {
				return err
			}
		}

	} else {
		for _, d := range list {
			if err := dc(d); err != nil {
				return err
			}
		}
	}

	// Do not reconnect after a disconnect action.
	if dry.StringListContains(activeNames, name) {
		return nil
	}

	// connect to a tunnel
	output, err := runWGQuick(b.ctx, "up", name)
	if err != nil {
		err = fmt.Errorf("wg-quick up %s: %w\n%s", name, err, output)
		wlog("ERROR", err.Error())
		return err
	}

	if b.settings.RememberConnectedServers {
		if b.settings.ConnectedServers == nil {
			b.settings.ConnectedServers = map[string]int64{}
		}
		b.settings.ConnectedServers[name] = time.Now().Unix()
		b.changed = true
	}

	if err := wlog("INFO", "Connected to "+name); err != nil {
		return err
	}

	return nil
}

func (t *Tunnels) SetPasswordless() error {
	// not installed
	if !dry.FileExists(policyPath) {
		return nil
	}

	content, err := ioutil.ReadFile(policyPath)
	if err != nil {
		return err
	}

	changed := false
	if Settings.Passwordless && bytes.Contains(content, []byte("<allow_any>auth_admin</allow_any>")) {
		content = bytes.Replace(content, []byte("<allow_any>auth_admin</allow_any>"), []byte("<allow_any>yes</allow_any>"), 1)
		content = bytes.Replace(content, []byte("<allow_inactive>auth_admin</allow_inactive>"), []byte("<allow_inactive>yes</allow_inactive>"), 1)
		content = bytes.Replace(content, []byte("<allow_active>auth_admin_keep</allow_active>"), []byte("<allow_active>yes</allow_active>"), 1)
		changed = true
	} else if !Settings.Passwordless && bytes.Contains(content, []byte("<allow_any>yes</allow_any>")) {
		content = bytes.Replace(content, []byte("<allow_any>yes</allow_any>"), []byte("<allow_any>auth_admin</allow_any>"), 1)
		content = bytes.Replace(content, []byte("<allow_inactive>yes</allow_inactive>"), []byte("<allow_inactive>auth_admin</allow_inactive>"), 1)
		content = bytes.Replace(content, []byte("<allow_active>yes</allow_active>"), []byte("<allow_active>auth_admin_keep</allow_active>"), 1)
		changed = true
	}

	if changed {
		if err := ioutil.WriteFile(policyPath, content, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (b *tunnelBackend) disconnectTunnel(d *wgtypes.Device) error {
	output, err := runWGQuick(b.ctx, "down", d.Name)
	if err != nil {
		err = fmt.Errorf("wg-quick down %s: %w\n%s", d.Name, err, output)
		wlog("ERROR", err.Error())
		return err
	}

	if b.settings.RememberConnectedServers && b.reconnected {
		if _, remembered := b.settings.ConnectedServers[d.Name]; remembered {
			delete(b.settings.ConnectedServers, d.Name)
			b.changed = true
		}
	}

	return wlog("INFO", "Disconnected from "+d.Name)
}

func (b *tunnelBackend) deactivateAll() error {
	ds, err := b.client.Devices()
	if err != nil {
		return err
	}
	for _, d := range ds {
		if err = b.disconnectTunnel(d); err != nil {
			return err
		}
	}
	return nil
}
