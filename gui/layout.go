package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	glibv2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/rs/zerolog/log"
	"wireguard-linux-gui/internal/autostart"
	"wireguard-linux-gui/internal/tray"
	"wireguard-linux-gui/settings"
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
		"log": func() { t.logs.Present() }, "preferences": t.openPreferences,
		"about":              t.openAbout,
		"preferences-cancel": t.cancelPreferences, "preferences-save": t.commitPreferences,
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
	prefKey := gtk.NewEventControllerKey()
	prefKey.ConnectKeyPressed(func(value, code uint, state gdk.ModifierType) bool {
		if value == gdk.KEY_Escape {
			t.closePreferences()
			return true
		}
		return false
	})
	t.preferences.AddController(prefKey)
	t.actions["preferences-save"].SetEnabled(false)
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
func preferenceValues(s *settings.Settings) map[string]bool {
	return map[string]bool{
		"multiple_active_tunnels": s.MultipleTunnels,
		"start_tray":              s.StartOnTray,
		"check_updates":           s.CheckUpdates,
		"remember_connected":      s.RememberConnectedServers,
		"country_flags":           s.CountryFlags,
		"polkit_policy":           s.Passwordless,
	}
}

const loginStartupSubtitle = "Open WireGuard when you sign in. Administrator authentication may be required."

func (t *Tunnels) reloadLoginStartup() {
	t.loginStartupAvailable = false
	t.loginStartupEnabled = false
	reason := LoginStartupError
	if LoginStartup != nil {
		snapshot, err := LoginStartup.Read()
		if err != nil {
			reason = err.Error()
		} else {
			t.loginStartupAvailable = true
			t.loginStartupEnabled = snapshot.Enabled
		}
	}
	row := t.switches["start_login"]
	row.SetSensitive(t.loginStartupAvailable)
	if t.loginStartupAvailable {
		row.SetSubtitle(loginStartupSubtitle)
	} else {
		if reason == "" {
			reason = "No non-root desktop user is available."
		}
		row.SetSubtitle("Start at login is unavailable: " + reason)
	}
}

func (t *Tunnels) fromSettings() {
	t.syncingSettings = true
	defer func() { t.syncingSettings = false }()
	for id, v := range preferenceValues(Settings) {
		t.switches[id].SetActive(v)
	}
	t.switches["start_login"].SetActive(t.loginStartupEnabled)
}
func (t *Tunnels) preferencesDirty() bool {
	if t.loginStartupAvailable && t.switches["start_login"].Active() != t.loginStartupEnabled {
		return true
	}
	for id, v := range preferenceValues(Settings) {
		if t.switches[id].Active() != v {
			return true
		}
	}
	return false
}
func (t *Tunnels) openPreferences() {
	if !t.preferences.Visible() {
		t.reloadLoginStartup()
		t.fromSettings()
		t.actions["preferences-save"].SetEnabled(false)
	}
	t.preferences.Present()
}
func (t *Tunnels) cancelPreferences() {
	t.fromSettings()
	t.preferences.SetVisible(false)
	t.actions["preferences-save"].SetEnabled(false)
	t.refreshCountryFlags(Settings.CountryFlags)
}
func (t *Tunnels) commitPreferences() {
	if err := t.saveSettings(); err != nil {
		message(&t.preferences.Window, "Could not save preferences", err.Error(), nil)
		return
	}
	t.actions["preferences-save"].SetEnabled(false)
	t.preferences.SetVisible(false)
	t.refreshCountryFlags(Settings.CountryFlags)
}
func (t *Tunnels) closePreferences() {
	if !t.preferencesDirty() {
		t.cancelPreferences()
		return
	}
	if t.preferencesPrompt != nil {
		t.preferencesPrompt.Present()
		return
	}
	d := adw.NewMessageDialog(&t.preferences.Window, "Save changes?", "Your preferences have unsaved changes.")
	t.preferencesPrompt = d
	d.AddResponse("cancel", "Keep Editing")
	d.AddResponse("discard", "Discard")
	d.AddResponse("save", "Save")
	d.SetCloseResponse("cancel")
	d.SetDefaultResponse("save")
	d.SetResponseAppearance("discard", adw.ResponseDestructive)
	d.SetResponseAppearance("save", adw.ResponseSuggested)
	d.ConnectResponse(func(response string) {
		t.preferencesPrompt = nil
		d.Destroy()
		switch response {
		case "discard":
			t.cancelPreferences()
		case "save":
			t.commitPreferences()
		}
	})
	d.Present()
}
func (t *Tunnels) saveSettings() error {
	enabled := t.switches["start_login"].Active()
	if !t.loginStartupAvailable || enabled == t.loginStartupEnabled {
		return t.saveGeneralSettings()
	}
	if err := autostart.Commit(LoginStartup, enabled, t.saveGeneralSettings); err != nil {
		return err
	}
	t.loginStartupEnabled = enabled
	return nil
}

func (t *Tunnels) saveGeneralSettings() error {
	previous := *Settings
	Settings.MultipleTunnels = t.switches["multiple_active_tunnels"].Active()
	Settings.StartOnTray = t.switches["start_tray"].Active()
	Settings.CheckUpdates = t.switches["check_updates"].Active()
	Settings.RememberConnectedServers = t.switches["remember_connected"].Active()
	Settings.CountryFlags = t.switches["country_flags"].Active()
	Settings.Passwordless = t.switches["polkit_policy"].Active()
	policyChanged := previous.Passwordless != Settings.Passwordless
	if policyChanged {
		if err := t.SetPasswordless(); err != nil {
			*Settings = previous
			return err
		}
	}
	if err := Settings.Save(); err != nil {
		*Settings = previous
		if policyChanged {
			err = errors.Join(err, t.SetPasswordless())
		}
		return err
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
	networksByTunnel := make(map[string]string)
	if active && t.statusAvailable {
		for _, d := range t.devices {
			networks := []string{}
			for _, p := range d.Peers {
				for _, ip := range p.AllowedIPs {
					networks = append(networks, ip.String())
				}
			}
			networks = uniqueNetworks(networks)
			if len(networks) == 0 {
				networks = []string{"None"}
			}
			networksByTunnel[d.Name] = strings.Join(networks, ", ")
		}
		label := "Deactivate"
		if len(names) > 1 {
			label = "Deactivate All"
		}
		items = append(items, tray.Item{Key: "deactivate", Label: label, Visible: true, Enabled: true, Activate: func() {
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
		label := name
		if networks, ok := networksByTunnel[name]; ok {
			label += " — " + networks
		}
		items = append(items, tray.Item{Key: "tunnel:" + name, Label: label, Enabled: t.statusAvailable, Visible: true, Checkable: true, Checked: contains(names, name), Activate: func() { ShowError(t.ToggleTunnel(nil, name)) }})
	}
	if t.list.RowAtIndex(0) != nil {
		items = append(items, tray.Item{Separator: true, Visible: true})
	}
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
