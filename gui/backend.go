package gui

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/rs/zerolog/log"
	"github.com/ungerik/go-dry"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var runWGQuick = func(action, name string) ([]byte, error) {
	return exec.Command("wg-quick", action, name).CombinedOutput()
}

var policyPath = "/usr/share/polkit-1/actions/wireguard-linux-gui.policy"

func (t *Tunnels) ToggleTunnel(row *gtk.ListBoxRow, name string) error {
	defer t.RefreshStatus()
	list, err := wgc.Devices()
	if err != nil {
		return err
	}

	if row == nil && name != "" {
		if tl := t.list; tl != nil {
			for i := uint(0); ; i++ {
				r := tl.RowAtIndex(int(i))
				if r == nil {
					break
				}
				if rname := r.Name(); rname == name {
					row = r
					break
				}
			}
		}
	}

	activeNames := deviceNames(list)
	if row != nil && name == "" {
		// conf name
		name = row.Name()
	}

	// https://github.com/UnnoTed/wireguird/issues/11#issuecomment-1332047191
	if len(name) >= 16 {
		return errors.New("Tunnel's file name is too long (" + strconv.Itoa(len(name)) + "), max length: 15")
	}

	if name == "" {
		return errors.New("No tunnel selected")
	}

	// Disconnect from the given tunnel, then query the resulting actual state.
	dc := t.disconnectTunnel

	// disconnects from all tunnels before connecting to a new one
	// when the multipleTunnels option is disabled
	if Settings.MultipleTunnels {
		log.Info().Str("name", name).Msg("NAME")
		d, err := wgc.Device(name)
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
	output, err := runWGQuick("up", name)
	if err != nil {
		err = fmt.Errorf("wg-quick up %s: %w\n%s", name, err, output)
		wlog("ERROR", err.Error())
		return err
	}

	if Settings.RememberConnectedServers {
		// using time because order may be important
		if Settings.ConnectedServers == nil {
			Settings.ConnectedServers = map[string]int64{}
		}

		Settings.ConnectedServers[name] = time.Now().Unix()
		if err := Settings.Save(); err != nil {
			log.Error().Err(err).Msg("Error saving settings on connect")
		}
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

func (t *Tunnels) disconnectTunnel(d *wgtypes.Device) error {
	output, err := runWGQuick("down", d.Name)
	if err != nil {
		err = fmt.Errorf("wg-quick down %s: %w\n%s", d.Name, err, output)
		wlog("ERROR", err.Error())
		return err
	}

	if Settings.RememberConnectedServers && Settings.ConnectedServers != nil && len(Settings.ConnectedServers) > 0 && t.reconnectedRememberedServers {
		delete(Settings.ConnectedServers, d.Name)
		if err := Settings.Save(); err != nil {
			log.Error().Err(err).Msg("Error saving settings on disconnect")
			wlog("INFO", "Error saving settings on disconnect: "+err.Error())
		}
	}

	return wlog("INFO", "Disconnected from "+d.Name)
}

func (t *Tunnels) deactivateAll() error {
	defer t.RefreshStatus()
	ds, err := wgc.Devices()
	if err != nil {
		return err
	}
	for _, d := range ds {
		if err = t.disconnectTunnel(d); err != nil {
			return err
		}
	}
	return nil
}
