package gui

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ungerik/go-dry"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/dustin/go-humanize"
	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/rs/zerolog/log"
	"golang.zx2c4.com/wireguard/wgctrl"
	"gopkg.in/ini.v1"
	"wireguard-linux-gui/gui/countries"
	"wireguard-linux-gui/gui/endpoints"
	"wireguard-linux-gui/gui/get"
	"wireguard-linux-gui/internal/statuspoll"
	"wireguard-linux-gui/settings"
)

var (
	Connected = false
	Settings  = &settings.Settings{}
)

func init() {
	if err := Settings.Init(); err != nil {
		log.Error().Err(err).Msg("Error on settings init")
	}
}

type Tunnels struct {
	Interface struct {
		Status     *gtk.Label
		PublicKey  *gtk.Label
		ListenPort *gtk.Label
		Addresses  *gtk.Label
		DNS        *gtk.Label
	}

	Peer struct {
		PublicKey       *gtk.Label
		AllowedIPs      *gtk.Label
		Endpoint        *gtk.Label
		LatestHandshake *gtk.Label
		Transfer        *gtk.Label
	}

	Settings struct {
		MultipleTunnels          *gtk.CheckButton
		StartOnTray              *gtk.CheckButton
		CheckUpdates             *gtk.CheckButton
		CountryFlags             *gtk.CheckButton
		RememberConnectedServers *gtk.CheckButton
		Passwordless             *gtk.CheckButton
	}

	ButtonChangeState *gtk.Button
	icons             map[string]*gtk.Image
	statusPoller      *statuspoll.Poller[[]*wgtypes.Device]
	statusTimer       glib.SourceHandle
	devices           []*wgtypes.Device
	statusAvailable   bool

	editingName                  string
	selectedPublicKey            string
	selectedListenPort           string
	lastSelected                 string
	reconnectedRememberedServers bool

	grayIcon  *gtk.Image
	greenIcon *gtk.Image

	menuNetworks           *gtk.MenuItem
	menuDeactivate         *gtk.MenuItem
	menuStatus             *gtk.MenuItem
	tunnelsTrayMenuItems   map[string]*gtk.CheckMenuItem
	tunnelsTrayMenuHandles map[string]glib.SignalHandle
}

func (t *Tunnels) Create() error {
	t.icons = map[string]*gtk.Image{}
	t.tunnelsTrayMenuItems = make(map[string]*gtk.CheckMenuItem)
	t.tunnelsTrayMenuHandles = make(map[string]glib.SignalHandle)

	var err error
	if err := t.createStatusMenu(); err != nil {
		return err
	}

	t.grayIcon, err = loadStatusIcon(IconPath + "dot-gray.png")
	if err != nil {
		return err
	}

	t.greenIcon, err = loadStatusIcon(IconPath + "dot-green.png")
	if err != nil {
		return err
	}

	tl, err := get.ListBox("tunnel_list")
	if err != nil {
		return err
	}

	window.Connect("key-press-event", func(win *gtk.ApplicationWindow, ev *gdk.Event) {
		keyEvent := &gdk.EventKey{ev}

		if keyEvent.KeyVal() == gdk.KEY_F5 {
			wlog("INFO", "Scanning tunnels from keyboard command (F5)")
			if err := t.ScanTunnels(); err != nil {
				wlog("ERROR", err.Error())
			}
		}
	})

	t.Interface.Status, err = get.Label("label_interface_status")
	if err != nil {
		return err
	}

	t.Interface.PublicKey, err = get.Label("label_interface_public_key")
	if err != nil {
		return err
	}

	t.Interface.ListenPort, err = get.Label("label_interface_listen_port")
	if err != nil {
		return err
	}

	t.Interface.Addresses, err = get.Label("label_interface_addresses")
	if err != nil {
		return err
	}

	t.Interface.DNS, err = get.Label("label_interface_dns_servers")
	if err != nil {
		return err
	}

	t.Peer.PublicKey, err = get.Label("label_peer_public_key")
	if err != nil {
		return err
	}

	t.Peer.AllowedIPs, err = get.Label("label_peer_allowed_ips")
	if err != nil {
		return err
	}

	t.Peer.Endpoint, err = get.Label("label_peer_endpoint")
	if err != nil {
		return err
	}

	t.Peer.LatestHandshake, err = get.Label("label_peer_latest_handshake")
	if err != nil {
		return err
	}

	t.Peer.Transfer, err = get.Label("label_peer_transfer")
	if err != nil {
		return err
	}

	t.ButtonChangeState, err = get.Button("button_change_state")
	if err != nil {
		return err
	}

	t.ButtonChangeState.Connect("clicked", func() {
		if err = t.ToggleTunnel(tl.GetSelectedRow(), ""); err != nil {
			ShowError(window, err)
		}
	})

	// boxPeers, err := get.Box("box_peers")
	// if err != nil {
	// 	return err
	// }

	tl.Connect("row-selected", func(l *gtk.ListBox, row *gtk.ListBoxRow) {
		t.UpdateRow(row)
		t.applySelectionState(row)
	})

	// button: add tunnel
	btnAddTunnel, err := get.Button("button_add_tunnel")
	if err != nil {
		return err
	}

	btnAddTunnel.Connect("clicked", func() {
		err := func() error {
			log.Print("btn add tunnel")
			dialog, err := gtk.FileChooserNativeDialogNew("wireguard-linux-gui - Choose tunnel files (*.conf, *.zip)", window, gtk.FILE_CHOOSER_ACTION_OPEN, "OK", "Cancel")
			if err != nil {
				return err
			}
			defer dialog.Destroy()

			// filter *.conf and *.zip files
			filter, err := gtk.FileFilterNew()
			if err != nil {
				return err
			}
			filter.AddPattern("*.conf")
			filter.AddPattern("*.zip")
			filter.SetName("*.conf / *.zip")

			dialog.AddFilter(filter)
			dialog.SetSelectMultiple(true)

			res := dialog.Run()
			if gtk.ResponseType(res) == gtk.RESPONSE_ACCEPT {
				list, err := dialog.GetFilenames()
				if err != nil {
					return err
				}
				for _, fname := range list {
					// if file is zip archive
					if strings.HasSuffix(fname, ".zip") {
						err := parseZipArchive(fname)
						if err != nil {
							return err
						}
						continue
					}

					data, err := os.ReadFile(fname)
					if err != nil {
						return err
					}

					err = os.WriteFile(filepath.Join(TunnelsPath, filepath.Base(fname)), data, 666)
					if err != nil {
						return err
					}
				}

				if err := t.ScanTunnels(); err != nil {
					return err
				}
			}

			return nil
		}()

		if err != nil {
			ShowError(window, err, "add tunnel error")
		}
	})

	// button: delete tunnel
	btnDelTunnel, err := get.Button("button_del_tunnel")
	if err != nil {
		return err
	}

	btnDelTunnel.Connect("clicked", func() {
		err := func() error {
			row := tl.GetSelectedRow()
			if row == nil {
				return errors.New("No row selected.")
			}

			name, err := row.GetName()
			if err != nil {
				return err
			}

			ext := ""
			if !strings.HasSuffix(name, ".conf") {
				ext = ".conf"
			}

			path := filepath.Join(TunnelsPath, name+ext)

			d := gtk.MessageDialogNew(window, gtk.DIALOG_MODAL, gtk.MESSAGE_QUESTION, gtk.BUTTONS_YES_NO, "Do you really want to delete "+name+"?")
			defer d.Destroy()

			res := d.Run()
			if res == gtk.RESPONSE_YES {
				if err := os.Remove(path); err != nil {
					return err
				}

				if err := t.ScanTunnels(); err != nil {
					return err
				}

				return nil
			}

			return nil
		}()

		if err != nil {
			ShowError(window, err, "tunnel delete error")
		}
	})

	// button: zip tunnels
	btnZipTunnels, err := get.Button("button_zip_tunnel")
	if err != nil {
		return err
	}

	btnZipTunnels.Connect("clicked", func() {
		err := func() error {
			d, err := gtk.FileChooserDialogNewWith2Buttons("wireguard-linux-gui - zip tunnels -> Save zip file", window, gtk.FILE_CHOOSER_ACTION_SAVE, "Cancel", gtk.RESPONSE_CANCEL, "Save", gtk.RESPONSE_ACCEPT)
			if err != nil {
				panic(err)
			}
			defer d.Destroy()

			d.SetDoOverwriteConfirmation(true)
			t := time.Now()
			d.SetCurrentName(fmt.Sprint("wg_tunnels_", t.Day(), "_", t.Month(), "_", t.Year(), ".zip"))

			res := d.Run()
			if res == gtk.RESPONSE_ACCEPT {
				dest := d.GetFilename()
				base := strings.TrimSuffix(filepath.Base(dest), filepath.Ext(dest))

				zf, err := os.Create(dest)
				if err != nil {
					return err
				}
				defer zf.Close()

				archive := zip.NewWriter(zf)
				defer archive.Close()

				return filepath.Walk(TunnelsPath, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}

					header, err := zip.FileInfoHeader(info)
					if err != nil {
						return err
					}

					header.Name = filepath.Join(base, strings.TrimPrefix(path, TunnelsPath))

					if info.IsDir() {
						header.Name += "/"
					} else {
						header.Method = zip.Deflate
						header.SetMode(0777)
					}

					log.Debug().Interface("header", header).Interface("info", info).Msg("walk")
					writer, err := archive.CreateHeader(header)
					if err != nil {
						return err
					}

					if info.IsDir() {
						return nil
					}

					f, err := os.Open(path)
					if err != nil {
						return err
					}

					defer f.Close()
					if _, err = io.Copy(writer, f); err != nil {
						return err
					}

					return nil
				})
			}

			return nil
		}()

		if err != nil {
			ShowError(window, err, "tunnel delete error")
		}
	})

	// stores a modified state for the editor
	modified := false
	editorWindow.Connect("hide", func() {
		if err := t.ScanTunnels(); err != nil {
			ShowError(window, err, "scan tunnel after closing editor window error")
		}

		modified = false
	})

	ne, err := get.Entry("editor_name")
	if err != nil {
		return err
	}
	ne.Connect("changed", func() {
		modified = true
	})

	et, err := get.TextView("editor_text")
	if err != nil {
		return err
	}

	etb, err := et.GetBuffer()
	if err != nil {
		return err
	}

	etb.Connect("changed", func() {
		modified = true
	})

	// button: edit tunnel
	btnEditTunnel, err := get.Button("button_edit_tunnel")
	if err != nil {
		return err
	}

	btnEditTunnel.Connect("clicked", func() {
		modified = false

		err := func() error {
			row := tl.GetSelectedRow()
			if row == nil {
				return nil
			}

			name, err := row.GetName()
			if err != nil {
				return err
			}

			ext := ""
			if !strings.HasSuffix(name, ".conf") {
				ext = ".conf"
			}

			t.editingName = name
			path := filepath.Join(TunnelsPath, name+ext)
			log.Print(path)

			ew, err := createEditor(name, true)
			if err != nil {
				return err
			}

			et, err := get.TextView("editor_text")
			if err != nil {
				return err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			conf := string(data)

			// gets public key to use in the PublicKey entry
			re := regexp.MustCompile(`(?m)PublicKey[\s]+=[\s]+(.*)`)
			m := re.FindStringSubmatch(conf)
			if len(m) >= 2 {
				epk, err := get.Entry("editor_publickey")
				if err != nil {
					return err
				}

				epk.SetText(m[1])
			}

			ttt, err := gtk.TextTagTableNew()
			if err != nil {
				return err
			}
			tb, err := gtk.TextBufferNew(ttt)
			if err != nil {
				return err
			}

			tb.SetText(conf)
			et.SetBuffer(tb)

			ew.ShowAll()

			return nil
		}()

		if err != nil {
			ShowError(window, err, "edit tunnel error")
		}
	})

	// button: editor's cancel
	btnEditorCancel, err := get.Button("editor_button_cancel")
	if err != nil {
		return err
	}

	btnEditorCancel.Connect("clicked", func() {
		if !modified {
			editorWindow.Close()
			return
		}

		err := func() error {
			d := gtk.MessageDialogNew(editorWindow, gtk.DIALOG_MODAL, gtk.MESSAGE_QUESTION, gtk.BUTTONS_YES_NO, "Do you want to cancel any modification done to the tunnel?")
			defer d.Destroy()

			res := d.Run()
			if res == gtk.RESPONSE_YES {
				modified = false
				editorWindow.Close()
				return nil
			}

			return nil
		}()

		if err != nil {
			ShowError(window, err, "cancel tunnel error")
		}
	})

	// button: editor's save
	btnEditorSave, err := get.Button("editor_button_save")
	if err != nil {
		return err
	}

	btnEditorSave.Connect("clicked", func() {
		err := func() error {
			name := t.editingName
			path := filepath.Join(TunnelsPath, name+".conf")

			// get the new tunnel name
			nameEntry, err := get.Entry("editor_name")
			if err != nil {
				return err
			}

			newName, err := nameEntry.GetText()
			if err != nil {
				return err
			}
			newName = strings.TrimSpace(newName)

			if !regexp.MustCompile(`^[a-zA-Z0-9_=+.-]{1,15}$`).MatchString(newName) || newName == "." || newName == ".." {
				return errors.New("Use a tunnel name of 1–15 letters, numbers, or _ = + . - characters.")
			}
			if name != newName {
				newPath := filepath.Join(TunnelsPath, newName+".conf")
				if _, err := os.Stat(newPath); err == nil {
					return errors.New("A tunnel with that name already exists.")
				} else if !os.IsNotExist(err) {
					return err
				}
				if name != "" {
					if err := os.Rename(path, newPath); err != nil {
						return err
					}
				}
				path = newPath
			}

			// get the tunnels' edited text through editor_text's buffer
			etxt, err := get.TextView("editor_text")
			if err != nil {
				return err
			}

			b, err := etxt.GetBuffer()
			if err != nil {
				return err
			}

			data, err := b.GetText(b.GetStartIter(), b.GetEndIter(), false)
			if err != nil {
				return err
			}

			// write changes
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				return err
			}

			t.lastSelected = newName
			modified = false
			if err := t.ScanTunnels(); err != nil {
				return err
			}

			editorWindow.Close()

			return nil
		}()

		if err != nil {
			ShowError(window, err, "save tunnel error")
		}
	})

	//////////////
	// Settings

	// button: settings save
	btnSettingsSave, err := get.Button("button_settings_save")
	if err != nil {
		return err
	}

	btnSettingsSave.Connect("clicked", func() {
		prevCountryFlags := Settings.CountryFlags

		err := func() error {
			t.ToSettings()
			Settings.Save()
			settingsWindow.Close()

			return nil
		}()

		if err != nil {
			ShowError(window, err, "settings save error")
		}

		// only download country database when previously disabled and now enabled
		if !prevCountryFlags && Settings.CountryFlags {
			go func() {
				err := countries.OpenDatabase()
				if err != nil {
					ShowError(window, err, "download country database error")
				}
			}()
		}

		if err := t.SetPasswordless(); err != nil {
			ShowError(window, err, "set passwordless error")
		}

		if err := t.ScanTunnels(); err != nil {
			ShowError(window, err, "scan tunnels error")
		}
	})

	// button: settings cancel
	btnSettingsCancel, err := get.Button("button_settings_cancel")
	if err != nil {
		return err
	}

	btnSettingsCancel.Connect("clicked", func() {
		err := func() error {
			Settings.Init()
			settingsWindow.Close()

			return nil
		}()

		if err != nil {
			ShowError(window, err, "settings cancel error")
		}
	})

	if err := t.FromSettings(); err != nil {
		return err
	}

	if err := t.ScanTunnels(); err != nil {
		return err
	}

	// GTK widgets and AppIndicator must only be accessed on the GTK thread.
	t.statusPoller = statuspoll.New(func() ([]*wgtypes.Device, error) {
		// Use a separate client so UI actions and polling never share a socket.
		client, err := wgctrl.New()
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.Devices()
	}, func(f func()) { glib.IdleAdd(f) }, func(ds []*wgtypes.Device, err error) {
		t.devices = ds
		t.applyStatus(ds, err)
		t.scheduleStatusRefresh()
	})
	window.Connect("show", func() { t.RefreshStatus() })
	window.Connect("hide", func() { t.scheduleStatusRefresh() })
	application.Connect("shutdown", func() {
		t.cancelStatusTimer()
		t.statusPoller.Close()
	})

	// Restore only missing profiles. Toggling an existing interface would stop it.
	if Settings.RememberConnectedServers {
		ds, err := wgc.Devices()
		if err != nil {
			log.Error().Err(err).Msg("skipping remembered tunnels: WireGuard status unavailable")
		}
		active := deviceNames(ds)
		// With a single tunnel allowed, preserve any externally activated profile.
		if err == nil && (Settings.MultipleTunnels || len(active) == 0) {
			var remembered []string
			for name := range Settings.ConnectedServers {
				remembered = append(remembered, name)
			}
			sort.Slice(remembered, func(i, j int) bool {
				a, b := remembered[i], remembered[j]
				if Settings.ConnectedServers[a] == Settings.ConnectedServers[b] {
					return a < b
				}
				return Settings.ConnectedServers[a] > Settings.ConnectedServers[b]
			})
			for _, name := range remembered {
				if dry.StringListContains(active, name) {
					continue
				}
				if err := t.ToggleTunnel(nil, name); err != nil {
					log.Error().Err(err).Str("tunnel", name).Msg("error restoring tunnel")
				}
				if !Settings.MultipleTunnels {
					break
				}
			}
		}
	}
	t.reconnectedRememberedServers = true
	if err := t.createLayoutMenus(); err != nil {
		return err
	}
	t.RefreshStatus()
	return nil
}

func (t *Tunnels) ToSettings() {
	Settings.MultipleTunnels = t.Settings.MultipleTunnels.GetActive()
	Settings.StartOnTray = t.Settings.StartOnTray.GetActive()
	Settings.CheckUpdates = t.Settings.CheckUpdates.GetActive()
	Settings.CountryFlags = t.Settings.CountryFlags.GetActive()
	Settings.RememberConnectedServers = t.Settings.RememberConnectedServers.GetActive()
	Settings.Passwordless = t.Settings.Passwordless.GetActive()
}

func (t *Tunnels) FromSettings() error {
	log.Debug().Interface("settings", Settings).Msg("tunnel.FromSettings()")
	var err error
	// checkbox: multiple tunnels
	t.Settings.MultipleTunnels, err = get.CheckButton("settings_multiple_active_tunnels")
	if err != nil {
		return err
	}
	t.Settings.MultipleTunnels.SetActive(Settings.MultipleTunnels)

	// checkbox: start on tray
	t.Settings.StartOnTray, err = get.CheckButton("settings_start_tray")
	if err != nil {
		return err
	}
	t.Settings.StartOnTray.SetActive(Settings.StartOnTray)

	// checkbox: check updates
	t.Settings.CheckUpdates, err = get.CheckButton("settings_check_updates")
	if err != nil {
		return err
	}
	t.Settings.CheckUpdates.SetActive(Settings.CheckUpdates)

	// checkbox: country flags
	t.Settings.CountryFlags, err = get.CheckButton("settings_country_flags")
	if err != nil {
		return err
	}
	t.Settings.CountryFlags.SetActive(Settings.CountryFlags)

	// checkbox: remember connected servers
	t.Settings.RememberConnectedServers, err = get.CheckButton("settings_remember_connected")
	if err != nil {
		return err
	}
	t.Settings.RememberConnectedServers.SetActive(Settings.RememberConnectedServers)

	// checkbox: passwordless
	t.Settings.Passwordless, err = get.CheckButton("settings_polkit_policy")
	if err != nil {
		return err
	}
	t.Settings.Passwordless.SetActive(Settings.Passwordless)

	return nil
}

func (t *Tunnels) UpdateRow(row *gtk.ListBoxRow) {
	if row == nil {
		return
	}
	err := func() error {
		ds := t.devices

		id, err := row.GetName()
		if err != nil {
			return err
		}

		cfg, err := ini.Load(TunnelsPath + id + ".conf")
		if err != nil {
			return err
		}

		t.lastSelected = id
		t.UnknownLabels()

		peersec := cfg.Section("Peer")
		insec := cfg.Section("Interface")
		t.selectedPublicKey = "—"
		if key, err := wgtypes.ParseKey(insec.Key("PrivateKey").String()); err == nil {
			t.selectedPublicKey = key.PublicKey().String()
		}
		t.selectedListenPort = insec.Key("ListenPort").String()
		if t.selectedListenPort == "" {
			t.selectedListenPort = "Automatic"
		}

		nameLabel, _ := get.Label("label_interface_name")
		nameLabel.SetText(id)
		keepalive, _ := get.Label("label_peer_keepalive")
		value := peersec.Key("PersistentKeepalive").String()
		if value == "" || value == "0" {
			value = "Off"
		} else {
			value += " seconds"
		}
		keepalive.SetText(value)
		t.Interface.Addresses.SetText(insec.Key("Address").String())
		t.setInterfaceState("Inactive")
		t.Interface.DNS.SetText(insec.Key("DNS").String())

		t.ButtonChangeState.SetLabel("Activate")
		t.Interface.PublicKey.SetText(t.selectedPublicKey)
		t.Interface.ListenPort.SetText(t.selectedListenPort)

		t.Peer.AllowedIPs.SetText(peersec.Key("AllowedIPs").String())
		t.Peer.PublicKey.SetText(peersec.Key("PublicKey").String())
		t.Peer.Endpoint.SetText(peersec.Key("Endpoint").String())

		if t.statusAvailable {
			t.applyStatus(ds, nil)
		} else {
			t.setInterfaceState("Unknown")
		}

		return nil
	}()

	if err != nil {
		log.Error().Err(err).Msg("row activated")
		ShowError(window, err)
	}
}

func (t *Tunnels) ScanTunnels() error {
	var err error
	var configList []string
	list, err := dry.ListDirFiles(TunnelsPath)
	if err != nil {
		// showError(err)
		return err
	}
	configByName := map[string]string{}

	for _, fileName := range list {
		if !strings.HasSuffix(fileName, ".conf") {
			continue
		}

		name := strings.TrimSuffix(fileName, ".conf")
		configByName[name] = TunnelsPath + fileName

		configList = append(configList, name)
	}

	tl, err := get.ListBox("tunnel_list")
	if err != nil {
		return err
	}

	for {
		row := tl.GetRowAtIndex(0)
		if row == nil {
			break
		}

		row.Destroy()
	}

	sort.Slice(configList, func(a, b int) bool {
		return configList[a] < configList[b]
	})

	activeNames := t.ActiveDeviceName()
	t.icons = make(map[string]*gtk.Image)
	for _, item := range t.tunnelsTrayMenuItems {
		item.Destroy()
	}
	t.tunnelsTrayMenuItems = make(map[string]*gtk.CheckMenuItem)
	t.tunnelsTrayMenuHandles = make(map[string]glib.SignalHandle)

	tunnelsMenu := indicator.GetMenu()
	menuIndex := 4

	for _, name := range configList {
		row, err := gtk.ListBoxRowNew()
		if err != nil {
			return err
		}
		row.SetName(name)
		row.SetMarginStart(0)
		row.SetMarginEnd(0)

		menuItem, err := gtk.CheckMenuItemNewWithLabel(name)
		if err != nil {
			return err
		}
		tunnelsMenu.Add(menuItem)
		tunnelsMenu.ReorderChild(menuItem, menuIndex)
		menuIndex++
		tunnelName := name // this new variable is necessary - https://go.dev/blog/loopvar-preview
		handle := menuItem.Connect("toggled", func() {
			err := t.ToggleTunnel(nil, tunnelName)
			if err != nil {
				wlog("ERROR", "connect tunnel error: "+err.Error())
			}
		})
		t.tunnelsTrayMenuItems[name] = menuItem
		t.tunnelsTrayMenuHandles[name] = handle

		var img *gtk.Image

		if dry.StringListContains(activeNames, name) {
			green, err := gtk.ImageNewFromPixbuf(t.greenIcon.GetPixbuf())
			if err != nil {
				return err
			}
			img = green
			menuItem.HandlerBlock(handle)
			menuItem.SetActive(true)
			menuItem.HandlerUnblock(handle)
		} else {
			gray, err := gtk.ImageNewFromPixbuf(t.grayIcon.GetPixbuf())
			if err != nil {
				return err
			}
			img = gray
			menuItem.HandlerBlock(handle)
			menuItem.SetActive(false)
			menuItem.HandlerUnblock(handle)
		}

		t.icons[name] = img

		img.SetVAlign(gtk.ALIGN_CENTER)
		img.SetHAlign(gtk.ALIGN_START)
		img.SetSizeRequest(10, 10)
		img.SetVExpand(false)
		img.SetHExpand(false)

		var country *gtk.Image
		if Settings.CountryFlags {
			cfg, err := ini.Load(configByName[name])
			if err != nil {
				return err
			}

			peersec := cfg.Section("Peer")

			nothing, err := loadEmbeddedImage("./flags/IDK.png")
			if err != nil {
				return err
			}
			country = nothing
			country.SetVAlign(gtk.ALIGN_CENTER)
			country.SetHAlign(gtk.ALIGN_START)
			country.SetSizeRequest(10, 10)
			country.SetVExpand(false)
			country.SetHExpand(false)

			go func() {
				err := func() error {
					endpoint := strings.Split(peersec.Key("Endpoint").String(), ":")[0]
					log.Debug().Str("endpoint", endpoint).Msg("checking country flag for tunnel")

					ip := ""
					switch endpoints.Categorize(endpoint) {
					case endpoints.IPV4, endpoints.IPV6:
						ip = endpoint
					case endpoints.FQDN:
						ips, err := net.LookupIP(endpoint)
						if err != nil {
							log.Error().Err(err).Str("endpoint", endpoint).Msg("dns lookup error")
							return err
						}

						if len(ips) > 0 {
							ip = ips[0].String()
						} else {
							return errors.New("endpoint \"" + endpoint + "\" coudln't find ip from dns")
						}
					default:
						return errors.New("endpoint \"" + endpoint + "\" is invalid")
					}

					log.Debug().Str("endpoint", endpoint).Str("ip", ip).Msg("found ip for tunnel endpoint")

					countryCode, err := countries.Find(ip)
					if err != nil {
						log.Error().Err(err).Msg("find country error")
						return err
					}

					log.Debug().Str("country", countryCode).Str("ip", ip).Msg("found country for tunnel")

					flag, err := loadEmbeddedImage("./flags/" + countryCode + ".png")
					if err != nil {
						log.Error().Err(err).Str("country", countryCode).Str("ip", ip).Msg("error loading embedded image")
						return err
					}

					// prevents crash when deleting servers
					glib.IdleAdd(func() {
						if country != nil && country.GetPixbuf() != nil {
							country.SetFromPixbuf(flag.GetPixbuf())
						}
					})
					return nil
				}()
				if err != nil {
					log.Error().Err(err).Msg("country flag error")
					wlog("ERROR", "country flag error: "+err.Error())
				}
			}()
		}

		label, err := gtk.LabelNew(name)
		if err != nil {
			return err
		}
		label.SetHAlign(gtk.ALIGN_START)

		label.SetMarginStart(4)
		label.SetMarginEnd(4)
		label.SetMarginTop(4)
		label.SetMarginBottom(4)

		box, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 4)
		if err != nil {
			return err
		}

		box.Add(img)

		if Settings.CountryFlags {
			box.Add(country)
		}

		box.Add(label)

		row.SetName(name)
		row.Add(box)
		row.ShowAll()
		tl.Insert(row, -1)

		if name == t.lastSelected {
			tl.SelectRow(row)
			t.UpdateRow(row)
		}
	}

	if tl.GetSelectedRow() == nil {
		row := tl.GetRowAtIndex(0)
		if row != nil {
			tl.SelectRow(row)
			t.UpdateRow(row)
		}
	}

	if export, err := get.Button("button_zip_tunnel"); err == nil {
		export.SetSensitive(len(configList) > 0)
	}
	indicator.GetMenu().ShowAll()
	t.menuNetworks.SetVisible(Connected)
	t.menuDeactivate.SetVisible(Connected)
	if t.Interface.Status != nil {
		t.RefreshStatus()
	}

	return nil
}

func (t *Tunnels) UnknownLabels() {
	t.ButtonChangeState.SetLabel("Activate")
	for _, label := range []*gtk.Label{t.Interface.Addresses, t.Interface.Status, t.Interface.DNS, t.Interface.PublicKey, t.Interface.ListenPort, t.Peer.AllowedIPs, t.Peer.PublicKey, t.Peer.Endpoint, t.Peer.LatestHandshake, t.Peer.Transfer} {
		label.SetText("—")
	}
}

func deviceNames(ds []*wgtypes.Device) []string {
	names := make([]string, 0, len(ds))
	for _, d := range ds {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}

func (t *Tunnels) ActiveDeviceName() []string {
	return deviceNames(t.devices)
}

// RefreshStatus requests a background snapshot of live WireGuard interfaces.
// Call only from the GTK main thread. An activated interface does not imply a
// successful peer handshake.
func (t *Tunnels) RefreshStatus() {
	if t.statusPoller == nil {
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
	interval := uint(5000)
	if window.IsVisible() {
		interval = 1000
	}
	t.statusTimer = glib.TimeoutAdd(interval, func() bool {
		t.statusTimer = 0
		t.RefreshStatus()
		return false
	})
}

func (t *Tunnels) applyStatus(ds []*wgtypes.Device, err error) {
	t.statusAvailable = err == nil
	if err != nil {
		Connected = false
		t.menuNetworks.Hide()
		t.menuDeactivate.Hide()
		t.menuStatus.SetLabel("Status: Unavailable")
		indicator.SetIconFull("wireguard_off", "WireGuard status unavailable")
		indicator.SetTitle("WireGuard status unavailable")
		t.setInterfaceState("Unknown")
		t.ButtonChangeState.SetSensitive(false)
		for _, item := range t.tunnelsTrayMenuItems {
			item.SetSensitive(false)
		}
		return
	}
	names := deviceNames(ds)
	Connected = len(names) > 0
	summary, icon := "No active tunnels", "wireguard_off"
	if Connected {
		summary, icon = "Active tunnels: "+strings.Join(names, ", "), "wg_connected"
	}
	state := "Inactive"
	if Connected {
		state = "Active"
	}
	t.menuStatus.SetLabel("Status: " + state)
	t.menuDeactivate.SetLabel("Deactivate")
	if len(names) > 1 {
		t.menuDeactivate.SetLabel("Deactivate All")
	}
	t.menuNetworks.SetVisible(Connected)
	t.menuDeactivate.SetVisible(Connected)
	var networks []string
	for _, d := range ds {
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
	t.menuNetworks.SetLabel("Networks: " + strings.Join(networks, ", "))
	indicator.SetIconFull(icon, summary)
	indicator.SetTitle("wireguard-linux-gui — " + summary)
	for name, img := range t.icons {
		if dry.StringListContains(names, name) {
			img.SetFromPixbuf(t.greenIcon.GetPixbuf())
		} else {
			img.SetFromPixbuf(t.grayIcon.GetPixbuf())
		}
	}
	for name, item := range t.tunnelsTrayMenuItems {
		item.SetSensitive(true)
		item.HandlerBlock(t.tunnelsTrayMenuHandles[name])
		item.SetActive(dry.StringListContains(names, name))
		item.HandlerUnblock(t.tunnelsTrayMenuHandles[name])
	}
	tl, err := get.ListBox("tunnel_list")
	if err != nil {
		return
	}
	row := tl.GetSelectedRow()
	t.applySelectionState(row)
	if row == nil {
		return
	}
	name, err := row.GetName()
	if err != nil {
		return
	}
	t.setInterfaceState("Inactive")
	t.Interface.PublicKey.SetText(t.selectedPublicKey)
	t.Interface.ListenPort.SetText(t.selectedListenPort)
	t.ButtonChangeState.SetLabel("Activate")
	t.Peer.LatestHandshake.SetText("—")
	t.Peer.Transfer.SetText("—")
	for _, d := range ds {
		if d.Name != name {
			continue
		}
		t.setInterfaceState("Active")
		t.ButtonChangeState.SetLabel("Deactivate")
		t.Interface.PublicKey.SetText(d.PublicKey.String())
		t.Interface.ListenPort.SetText(strconv.Itoa(d.ListenPort))
		for _, p := range d.Peers {
			hs := "Never"
			if !p.LastHandshakeTime.IsZero() {
				hs = humanize.Time(p.LastHandshakeTime)
			}
			t.Peer.LatestHandshake.SetText(hs)
			t.Peer.Transfer.SetText(humanize.Bytes(uint64(p.ReceiveBytes)) + " received, " + humanize.Bytes(uint64(p.TransmitBytes)) + " sent")
		}
	}
}

func (t *Tunnels) ToggleTunnel(row *gtk.ListBoxRow, name string) error {
	defer t.RefreshStatus()
	list, err := wgc.Devices()
	if err != nil {
		return err
	}

	if row == nil && name != "" {
		if tl, err := get.ListBox("tunnel_list"); err == nil {
			for i := uint(0); ; i++ {
				r := tl.GetRowAtIndex(int(i))
				if r == nil {
					break
				}
				if rname, _ := r.GetName(); rname == name {
					row = r
					break
				}
			}
		}
	}

	activeNames := deviceNames(list)
	if row != nil && name == "" {
		// conf name
		name, err = row.GetName()
		if err != nil {
			return err
		}
	}

	// https://github.com/UnnoTed/wireguird/issues/11#issuecomment-1332047191
	if len(name) >= 16 {
		return errors.New("Tunnel's file name is too long (" + strconv.Itoa(len(name)) + "), max length: 15")
	}

	if name == "" {
		return errors.New("No tunnel selected")
	}

	// Disconnect from the given tunnel, then query the resulting actual state.
	dc := func(d *wgtypes.Device) error {
		c := exec.Command("wg-quick", "down", d.Name)
		output, err := c.CombinedOutput()
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
	c := exec.Command("wg-quick", "up", name)
	output, err := c.CombinedOutput()
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
	if !dry.FileExists("/usr/share/polkit-1/actions/wireguard-linux-gui.policy") {
		return nil
	}

	content, err := ioutil.ReadFile("/usr/share/polkit-1/actions/wireguard-linux-gui.policy")
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
		if err := ioutil.WriteFile("/usr/share/polkit-1/actions/wireguard-linux-gui.policy", content, 0644); err != nil {
			return err
		}
	}

	return nil
}

func wlog(t string, text string) error {
	wlogs, err := get.ListBox("wireguard_logs")
	if err != nil {
		return err
	}

	l, err := gtk.LabelNew("")
	if err != nil {
		return err
	}

	l.SetText("[" + time.Now().Format("02/Jan/06 15:04:05 MST") + "][" + t + "]: " + text)
	l.SetHExpand(true)
	l.SetHAlign(gtk.ALIGN_START)

	glib.IdleAdd(func() {
		l.Show()
		wlogs.Add(l)
	})

	return nil
}

func parseZipArchive(target string) error {
	rc, err := zip.OpenReader(target)
	if err != nil {
		return err
	}
	defer rc.Close()

	for _, f := range rc.File {
		// parse only .conf files from zip archive
		if !f.FileInfo().IsDir() && strings.HasSuffix(f.FileInfo().Name(), ".conf") {
			fr, err := f.Open()
			if err != nil {
				return err
			}
			defer fr.Close()

			data, err := io.ReadAll(fr)
			if err != nil {
				return err
			}

			err = os.WriteFile(filepath.Join(TunnelsPath, f.FileInfo().Name()), data, 666)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
