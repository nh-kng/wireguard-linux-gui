package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	glibv2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/rs/zerolog/log"
	"wireguard-linux-gui/gui/countries"
	"wireguard-linux-gui/internal/tray"
)

func (t *Tunnels) bindActions() {
	functions := map[string]func(){
		"manage": t.Present, "quit": func() { t.app.Quit() }, "refresh": func() { ShowError(t.ScanTunnels()) },
		"add": func() { ShowError(t.openEditor("")) }, "edit": func() {
			if row := t.list.SelectedRow(); row != nil {
				ShowError(t.openEditor(row.Name()))
			}
		},
		"toggle": func() { ShowError(t.ToggleTunnel(t.list.SelectedRow(), "")) },
		"remove": t.removeSelected, "import": t.importTunnels, "export": t.exportTunnels,
		"log": func() { t.logs.Present() }, "preferences": func() { t.fromSettings(); t.preferences.Present() },
		"about": func() {
			message(&t.window.Window, "WireGuard Linux GUI", "Version "+Version+"\nA native GTK4/Libadwaita client for WireGuard.\nMIT license; bindings use MPL-2.0.", nil)
		},
		"editor-cancel": t.cancelEditor, "editor-save": func() { ShowError(t.saveEditor()) },
	}
	for name, f := range functions {
		f := f
		a := gio.NewSimpleAction(name, nil)
		a.ConnectActivate(func(_ *glibv2.Variant) { f() })
		t.app.AddAction(a)
		t.actions[name] = a
	}
	t.app.SetAccelsForAction("app.refresh", []string{"F5"})
	t.app.SetAccelsForAction("app.quit", []string{"<Control>q"})
	t.app.SetAccelsForAction("app.import", []string{"<Control>o"})
	t.app.SetAccelsForAction("app.preferences", []string{"<Control>comma"})
	key := gtk.NewEventControllerKey()
	key.ConnectKeyPressed(func(value, code uint, state gdk.ModifierType) bool {
		if value == gdk.KEY_Escape {
			t.cancelEditor()
			return true
		}
		return false
	})
	t.editor.AddController(key)
}
func uniqueNetworks(networks []string) []string {
	sort.Strings(networks)
	result := networks[:0]
	for _, n := range networks {
		if len(result) == 0 || result[len(result)-1] != n {
			result = append(result, n)
		}
	}
	return result
}
func (t *Tunnels) fromSettings() {
	t.syncingSettings = true
	defer func() { t.syncingSettings = false }()
	for id, v := range map[string]bool{"multiple_active_tunnels": Settings.MultipleTunnels, "start_tray": Settings.StartOnTray, "check_updates": Settings.CheckUpdates, "country_flags": Settings.CountryFlags, "remember_connected": Settings.RememberConnectedServers, "polkit_policy": Settings.Passwordless} {
		t.switches[id].SetActive(v)
	}
}
func (t *Tunnels) saveSettings() error {
	previous := *Settings
	Settings.MultipleTunnels = t.switches["multiple_active_tunnels"].Active()
	Settings.StartOnTray = t.switches["start_tray"].Active()
	Settings.CheckUpdates = t.switches["check_updates"].Active()
	Settings.CountryFlags = t.switches["country_flags"].Active()
	Settings.RememberConnectedServers = t.switches["remember_connected"].Active()
	Settings.Passwordless = t.switches["polkit_policy"].Active()
	policyChanged := previous.Passwordless != Settings.Passwordless
	if policyChanged {
		if err := t.SetPasswordless(); err != nil {
			*Settings = previous
			t.fromSettings()
			return err
		}
	}
	if err := Settings.Save(); err != nil {
		*Settings = previous
		t.fromSettings()
		if policyChanged {
			err = errors.Join(err, t.SetPasswordless())
		}
		return err
	}
	if !previous.CountryFlags && Settings.CountryFlags {
		go func() {
			if err := countries.OpenDatabase(); err != nil {
				ShowError(err)
			}
		}()
	}
	if previous.CountryFlags != Settings.CountryFlags {
		return t.ScanTunnels()
	}
	return nil
}
func (t *Tunnels) restoreConnections() {
	defer func() { t.reconnectedRememberedServers = true }()
	if !Settings.RememberConnectedServers {
		return
	}
	ds, err := wgc.Devices()
	if err != nil {
		log.Error().Err(err).Msg("skip remembered tunnels")
		return
	}
	active := deviceNames(ds)
	if !Settings.MultipleTunnels && len(active) > 0 {
		return
	}
	names := []string{}
	for name := range Settings.ConnectedServers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if Settings.ConnectedServers[a] == Settings.ConnectedServers[b] {
			return a < b
		}
		return Settings.ConnectedServers[a] > Settings.ConnectedServers[b]
	})
	for _, name := range names {
		if contains(active, name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(TunnelsPath, name+".conf")); err != nil {
			continue
		}
		ShowError(t.ToggleTunnel(nil, name))
		if !Settings.MultipleTunnels {
			break
		}
	}
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (t *Tunnels) updateTray() {
	names := deviceNames(t.devices)
	active := len(names) > 0
	state := "Inactive"
	if active {
		state = "Active"
	}
	if !t.statusAvailable {
		state = "Unavailable"
	}
	items := []tray.Item{{Key: "status", Label: "Status: " + state, Visible: true}}
	if active && t.statusAvailable {
		networks := []string{}
		for _, d := range t.devices {
			for _, p := range d.Peers {
				for _, ip := range p.AllowedIPs {
					networks = append(networks, ip.String())
				}
			}
		}
		networks = uniqueNetworks(networks)
		if len(networks) == 0 {
			networks = []string{"None"}
		}
		label := "Deactivate"
		if len(names) > 1 {
			label = "Deactivate All"
		}
		items = append(items, tray.Item{Key: "networks", Label: "Networks: " + strings.Join(networks, ", "), Visible: true}, tray.Item{Key: "deactivate", Label: label, Visible: true, Enabled: true, Activate: func() {
			ShowError(t.deactivateAll())
		}})
	}
	items = append(items, tray.Item{Separator: true, Visible: true})
	for i := 0; ; i++ {
		row := t.list.RowAtIndex(i)
		if row == nil {
			break
		}
		name := row.Name()
		items = append(items, tray.Item{Key: "tunnel:" + name, Label: name, Enabled: t.statusAvailable, Visible: true, Checkable: true, Checked: contains(names, name), Activate: func() { ShowError(t.ToggleTunnel(nil, name)) }})
	}
	items = append(items, tray.Item{Separator: true, Visible: true})
	for _, a := range []struct{ label, action string }{{"Manage Tunnels", "manage"}, {"Import Tunnel(s) from File…", "import"}, {"About WireGuard", "about"}, {"Quit WireGuard", "quit"}} {
		action := a.action
		items = append(items, tray.Item{Key: "action:" + action, Label: a.label, Enabled: true, Visible: true, Activate: func() {
			if action == "import" {
				t.Present()
			}
			t.actions[action].Activate(nil)
		}})
	}
	icon := "wireguard-inactive-symbolic"
	if active && t.statusAvailable {
		icon = "wireguard-active-symbolic"
	}
	t.tray.Update(icon, fmt.Sprintf("WireGuard — %s", state), items)
}
func (t *Tunnels) RefreshStatus() {
	if t.statusPoller == nil || t.closed {
		return
	}
	t.cancelStatusTimer()
	t.statusPoller.Refresh()
}
func (t *Tunnels) cancelStatusTimer() {
	if t.statusTimer != 0 {
		glib.SourceRemove(t.statusTimer)
		t.statusTimer = 0
	}
}
func (t *Tunnels) scheduleStatusRefresh() {
	t.cancelStatusTimer()
	if t.closed {
		return
	}
	interval := uint(5000)
	if t.window.Visible() {
		interval = 1000
	}
	t.statusTimer = glib.TimeoutAdd(interval, func() bool { t.statusTimer = 0; t.RefreshStatus(); return false })
}
