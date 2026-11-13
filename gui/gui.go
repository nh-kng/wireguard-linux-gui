package gui

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/rs/zerolog/log"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/internal/statuspoll"
	"wireguard-linux-gui/internal/tray"
	"wireguard-linux-gui/settings"
)

const (
	Version = "1.2.0"
	Repo    = "https://github.com/UnnoTed/wireguird"
)

var (
	TunnelsPath = "/etc/wireguard/"
	Settings    = &settings.Settings{}
	wgc         wireguardClient
	current     *Tunnels
)

type wireguardClient interface {
	Devices() ([]*wgtypes.Device, error)
	Device(string) (*wgtypes.Device, error)
	Close() error
}

type Tunnels struct {
	sessionAddress                        string
	app                                   *adw.Application
	builder                               *gtk.Builder
	window                                *adw.ApplicationWindow
	preferences                           *adw.Window
	editor, logs                          *adw.Window
	list                                  *gtk.ListBox
	actions                               map[string]*gio.SimpleAction
	switches                              map[string]*adw.SwitchRow
	fields                                map[string]*gtk.Label
	icons                                 map[string]*gtk.Image
	tray                                  *tray.Tray
	assets                                fs.FS
	flags                                 map[string]*gtk.Image
	flagGeneration                        uint64
	preferencesPrompt                     *adw.MessageDialog
	iconDir                               string
	statusPoller                          *statuspoll.Poller[[]*wgtypes.Device]
	statusTimer                           glib.SourceHandle
	devices                               []*wgtypes.Device
	statusAvailable                       bool
	editingName, lastSelected             string
	selectedPublicKey, selectedListenPort string
	reconnectedRememberedServers          bool
	syncingSettings                       bool
	modified                              bool
	closed                                bool
}

func object[T any](b *gtk.Builder, id string) T {
	obj := b.GetObject(id)
	if obj == nil {
		panic("missing UI object: " + id)
	}
	v, ok := obj.Cast().(T)
	if !ok {
		panic(fmt.Sprintf("UI object %s has unexpected type %T", id, obj.Cast()))
	}
	return v
}
func Create(app *adw.Application, b *gtk.Builder, assets fs.FS, sessionAddress string) (*Tunnels, error) {
	t := &Tunnels{assets: assets, sessionAddress: sessionAddress, app: app, builder: b, actions: map[string]*gio.SimpleAction{}, switches: map[string]*adw.SwitchRow{}, fields: map[string]*gtk.Label{}, icons: map[string]*gtk.Image{}}
	t.window = object[*adw.ApplicationWindow](b, "main_window")
	t.window.SetApplication(&app.Application)
	t.preferences = object[*adw.Window](b, "settings_window")
	t.editor = object[*adw.Window](b, "editor_window")
	t.logs = object[*adw.Window](b, "log_window")
	t.preferences.SetTransientFor(&t.window.Window)
	t.editor.SetTransientFor(&t.window.Window)
	t.logs.SetTransientFor(&t.window.Window)
	t.preferences.SetApplication(&app.Application)
	t.editor.SetApplication(&app.Application)
	t.logs.SetApplication(&app.Application)
	t.list = object[*gtk.ListBox](b, "tunnel_list")
	for _, id := range []string{"interface_status", "interface_public_key", "interface_listen_port", "interface_addresses", "interface_dns_servers", "peer_public_key", "peer_allowed_ips", "peer_endpoint", "peer_keepalive", "peer_latest_handshake", "peer_transfer"} {
		t.fields[id] = object[*gtk.Label](b, "label_"+id)
	}
	for _, id := range []string{"multiple_active_tunnels", "start_tray", "check_updates", "remember_connected", "country_flags", "polkit_policy"} {
		t.switches[id] = object[*adw.SwitchRow](b, "settings_"+id)
	}
	t.fromSettings()
	current = t
	var err error
	wgc, err = wgctrl.New()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(TunnelsPath, 0700); err != nil {
		wgc.Close()
		return nil, err
	}
	stopAppearance := t.followAppearance()
	t.bindActions()
	for id, row := range t.switches {
		row.NotifyProperty("active", func() {
			if !t.syncingSettings {
				t.actions["preferences-save"].SetEnabled(t.preferencesDirty())
				if id == "country_flags" {
					t.refreshCountryFlags(row.Active())
				}
			}
		})
	}
	t.list.ConnectRowSelected(func(row *gtk.ListBoxRow) { t.UpdateRow(row) })
	object[*gtk.Entry](b, "editor_name").ConnectChanged(func() { t.modified = true })
	object[*gtk.TextView](b, "editor_text").Buffer().ConnectChanged(func() { t.modified = true })
	t.preferences.ConnectCloseRequest(func() bool { t.closePreferences(); return true })
	t.editor.ConnectCloseRequest(func() bool { t.cancelEditor(); return true })
	t.window.ConnectCloseRequest(func() bool {
		if t.tray.Available() {
			t.window.SetVisible(false)
		} else {
			t.app.Quit()
		}
		return true
	})
	t.window.ConnectShow(func() { t.RefreshStatus() })
	t.window.ConnectHide(func() { t.scheduleStatusRefresh() })
	app.ConnectShutdown(func() {
		t.closed = true
		stopAppearance()
		t.cancelStatusTimer()
		if t.statusPoller != nil {
			t.statusPoller.Close()
		}
		t.tray.Close()
		wgc.Close()
		if t.iconDir != "" {
			os.RemoveAll(t.iconDir)
		}
	})
	t.iconDir, err = os.MkdirTemp("", "wireguard-linux-gui-icons-")
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(t.iconDir, 0755); err != nil {
		return nil, err
	}
	for _, name := range []string{"wireguard-inactive-symbolic", "wireguard-active-symbolic"} {
		data, e := fs.ReadFile(assets, "Icon/"+name+".svg")
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(t.iconDir, name+".svg"), data, 0644); e != nil {
			return nil, e
		}
	}
	t.tray, err = tray.New(t.sessionAddress, "wireguard-inactive-symbolic", t.iconDir, func(f func()) { glib.IdleAdd(f) }, t.Present, func(available bool) {
		if !available {
			t.Present()
		}
	})
	if err != nil {
		log.Warn().Err(err).Msg("tray unavailable; showing window")
	}
	if err = t.ScanTunnels(); err != nil {
		return nil, err
	}
	t.statusPoller = statuspoll.New(func() ([]*wgtypes.Device, error) {
		client, e := wgctrl.New()
		if e != nil {
			return nil, e
		}
		defer client.Close()
		return client.Devices()
	}, func(f func()) { glib.IdleAdd(f) }, func(ds []*wgtypes.Device, e error) { t.devices = ds; t.applyStatus(ds, e); t.scheduleStatusRefresh() })
	t.restoreConnections()
	if Settings.CheckUpdates {
		glib.TimeoutAdd(60000, func() bool { go updateCheck(); return false })
	}
	if !Settings.StartOnTray || !t.tray.Available() {
		t.Present()
	}
	t.RefreshStatus()
	return t, nil
}
func (t *Tunnels) Present() {
	if !t.closed {
		t.window.Present()
		t.RefreshStatus()
	}
}
func (t *Tunnels) label(id, text string) { t.fields[id].SetText(text) }
func ShowError(err error) {
	if err == nil {
		return
	}
	glib.IdleAdd(func() {
		if current == nil || current.closed {
			return
		}
		wlog("ERROR", err.Error())
		message(&current.window.Window, "Error", err.Error(), nil)
	})
}
func message(parent *gtk.Window, title, body string, confirm func()) {
	d := adw.NewMessageDialog(parent, title, body)
	if confirm == nil {
		d.AddResponse("close", "Close")
		d.SetCloseResponse("close")
	} else {
		d.AddResponse("cancel", "Cancel")
		d.AddResponse("confirm", "Confirm")
		d.SetCloseResponse("cancel")
		d.SetResponseAppearance("confirm", adw.ResponseDestructive)
	}
	d.ConnectResponse(func(response string) {
		d.Destroy()
		if response == "confirm" && confirm != nil {
			confirm()
		}
	})
	d.Present()
}
func wlog(level, text string) error {
	glib.IdleAdd(func() {
		if current == nil || current.closed {
			return
		}
		b := object[*gtk.TextView](current.builder, "log_text").Buffer()
		b.Insert(b.EndIter(), "["+time.Now().Format(time.RFC3339)+"]["+level+"] "+text+"\n")
	})
	return nil
}
func updateCheck() {
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/UnnoTed/wireguird/releases")
	if err != nil {
		wlog("ERROR", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&releases) == nil && len(releases) > 0 && releases[0].TagName != "v"+Version {
		glib.IdleAdd(func() {
			if current != nil && !current.closed {
				message(&current.window.Window, "Upstream update available", releases[0].TagName+" (original wireguird project)", nil)
			}
		})
	}
}
