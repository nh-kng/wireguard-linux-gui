package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/settings"
)

// Run with WG_GUI_TEST=1 under a graphical session. No real tunnel is activated.
func TestNativeUI(t *testing.T) {
	if os.Getenv("WG_GUI_TEST") != "1" {
		t.Skip("set WG_GUI_TEST=1 in a graphical session")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "gui/window.ui"))
	if err != nil {
		t.Fatal(err)
	}
	oldPath, oldSettings, oldCurrent, oldPolicy := TunnelsPath, Settings, current, policyPath
	t.Cleanup(func() { TunnelsPath, Settings, current, policyPath = oldPath, oldSettings, oldCurrent, oldPolicy })
	t.Chdir(t.TempDir())
	TunnelsPath = filepath.Join(t.TempDir(), "configs") + "/"
	Settings = &settings.Settings{}
	policyPath = filepath.Join(t.TempDir(), "policy.xml")
	app := adw.NewApplication("com.wireguard.linuxgui.test", gio.ApplicationNonUnique)
	var scenarioErr error
	app.ConnectActivate(func() {
		defer app.Quit()
		scenarioErr = func() error {
			b := gtk.NewBuilder()
			if err := b.AddFromString(string(data)); err != nil {
				return err
			}
			controller, err := Create(app, b, os.DirFS(root), os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
			if err != nil {
				return err
			}
			if controller.actions["toggle"].Enabled() || controller.actions["edit"].Enabled() {
				return errors.New("empty selection actions enabled")
			}
			if err = controller.openEditor(""); err != nil {
				return err
			}
			name := object[*gtk.Entry](b, "editor_name")
			name.SetText("ui-test")
			if !controller.editor.ActivateAction("app.editor-save", nil) {
				return errors.New("editor cannot resolve application actions")
			}
			if controller.list.SelectedRow() == nil || controller.list.SelectedRow().Name() != "ui-test" {
				return errors.New("saved profile not selected")
			}
			info, err := os.Stat(filepath.Join(TunnelsPath, "ui-test.conf"))
			if err != nil || info.Mode().Perm() != 0600 {
				return fmt.Errorf("config permissions: %v %v", info, err)
			}
			if err = controller.openEditor("ui-test"); err != nil {
				return err
			}
			name.SetText("renamed")
			if err = controller.saveEditor(); err != nil {
				return err
			}
			if _, err = os.Stat(filepath.Join(TunnelsPath, "ui-test.conf")); !os.IsNotExist(err) {
				return errors.New("rename retained old file")
			}
			if err = controller.openEditor(""); err != nil {
				return err
			}
			name.SetText("renamed")
			if controller.saveEditor() == nil {
				return errors.New("duplicate editor save accepted")
			}
			controller.modified = true
			controller.cancelEditor()
			if err = respondToDialog("cancel"); err != nil {
				return err
			}
			if !controller.editor.Visible() || !controller.modified {
				return errors.New("canceling discard lost editor state")
			}
			controller.editor.Close()
			if err = respondToDialog("confirm"); err != nil {
				return err
			}
			if controller.editor.Visible() || controller.modified {
				return errors.New("confirmed discard did not hide editor")
			}
			controller.applyStatus([]*wgtypes.Device{{Name: "renamed", ListenPort: 51820, Peers: []wgtypes.Peer{{LastHandshakeTime: time.Now(), ReceiveBytes: 1024, TransmitBytes: 2048}}}}, nil)
			if !controller.actions["toggle"].Enabled() || controller.fields["interface_status"].Text() != "Active" {
				return errors.New("live status not rendered")
			}
			if controller.fields["peer_transfer"].Text() == "—" {
				return errors.New("transfer counters missing")
			}
			controller.applyStatus(nil, errors.New("unavailable"))
			if controller.actions["toggle"].Enabled() || controller.fields["interface_status"].Text() != "Unknown" {
				return errors.New("unavailable status not rendered")
			}
			controller.actions["preferences"].Activate(nil)
			controller.switches["multiple_active_tunnels"].SetActive(true)
			loaded := settings.Settings{}
			if err = loaded.Load(); err != nil || !loaded.MultipleTunnels {
				return fmt.Errorf("preferences not auto-saved: %v", err)
			}
			if !controller.preferences.Visible() {
				return errors.New("auto-save closed preferences")
			}
			controller.switches["multiple_active_tunnels"].SetActive(false)
			if err = loaded.Load(); err != nil || loaded.MultipleTunnels {
				return fmt.Errorf("preference toggle off not auto-saved: %v", err)
			}
			controller.preferences.Close()
			controller.actions["preferences"].Activate(nil)
			if controller.switches["multiple_active_tunnels"].Active() {
				return errors.New("reopening preferences lost saved state")
			}
			style := adw.StyleManagerGetDefault()
			style.SetColorScheme(adw.ColorSchemeForceDark)
			if !style.Dark() {
				return errors.New("dark style not applied")
			}
			style.SetColorScheme(adw.ColorSchemeForceLight)
			if style.Dark() {
				return errors.New("light style not applied")
			}
			style.SetColorScheme(adw.ColorSchemeDefault)
			controller.removeSelected()
			if err = respondToDialog("cancel"); err != nil {
				return err
			}
			if _, err = os.Stat(filepath.Join(TunnelsPath, "renamed.conf")); err != nil {
				return errors.New("canceling removal deleted config")
			}
			controller.removeSelected()
			if err = respondToDialog("confirm"); err != nil {
				return err
			}
			if controller.list.SelectedRow() != nil || controller.actions["edit"].Enabled() {
				return errors.New("removed profile stayed selected")
			}
			controller.actions["log"].Activate(nil)
			wlog("TEST", "native GTK4 smoke test")
			// Consume deferred GTK callbacks, then confirm the no-host close path exits.
			controller.window.Close()
			return nil
		}()
	})
	app.Run([]string{"wireguard-ui-test"})
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
}

func respondToDialog(response string) error {
	windows := gtk.WindowGetToplevels()
	for i := uint(0); i < windows.NItems(); i++ {
		if dialog, ok := windows.Item(i).Cast().(*adw.MessageDialog); ok && dialog.Visible() {
			dialog.Response(response)
			return nil
		}
	}
	return errors.New("confirmation dialog missing")
}
