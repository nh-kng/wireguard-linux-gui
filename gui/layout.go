package gui

import (
	"sort"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/gui/get"
)

// Keep all entry points wired to the same actions as the window controls.
func clickButton(id string) {
	if b, err := get.Button(id); err == nil && b.GetSensitive() {
		b.Clicked()
	}
}

func addMenuAction(menu *gtk.Menu, title string, action func(), controls ...*gtk.Button) error {
	item, err := gtk.MenuItemNewWithLabel(title)
	if err != nil {
		return err
	}
	item.Connect("activate", action)
	for _, control := range controls {
		control := control
		sync := func() { item.SetSensitive(control.GetSensitive()) }
		sync()
		control.Connect("notify::sensitive", sync)
	}
	menu.Append(item)
	return nil
}

func menuSeparator(menu *gtk.Menu) error {
	item, err := gtk.SeparatorMenuItemNew()
	if err != nil {
		return err
	}
	menu.Append(item)
	return nil
}

func showAbout() {
	d := gtk.MessageDialogNew(window, gtk.DIALOG_MODAL, gtk.MESSAGE_INFO, gtk.BUTTONS_CLOSE, "WireGuard Linux GUI\nVersion %s\nA native GTK client for WireGuard on Linux.", Version)
	d.SetTitle("About WireGuard")
	d.Run()
	d.Destroy()
}

func (t *Tunnels) createStatusMenu() error {
	menu := indicator.GetMenu()
	var err error
	t.menuStatus, err = gtk.MenuItemNewWithLabel("Status: Reading…")
	if err != nil {
		return err
	}
	t.menuStatus.SetSensitive(false)
	menu.Append(t.menuStatus)
	t.menuNetworks, err = gtk.MenuItemNewWithLabel("Networks: None")
	if err != nil {
		return err
	}
	t.menuNetworks.SetSensitive(false)
	t.menuNetworks.SetNoShowAll(true)
	menu.Append(t.menuNetworks)
	t.menuDeactivate, err = gtk.MenuItemNewWithLabel("Deactivate")
	if err != nil {
		return err
	}
	t.menuDeactivate.SetNoShowAll(true)
	menu.Append(t.menuDeactivate)
	t.menuDeactivate.Connect("activate", func() {
		// Snapshot the devices because toggling requests a new background status.
		for _, d := range t.devices {
			if err := t.ToggleTunnel(nil, d.Name); err != nil {
				ShowError(window, err)
				return
			}
		}
	})
	if err := menuSeparator(menu); err != nil {
		return err
	}
	if err := menuSeparator(menu); err != nil {
		return err
	}
	for _, action := range []struct {
		title string
		run   func()
	}{
		{"Manage Tunnels", func() { application.Activate() }},
		{"Import Tunnel(s) from File…", func() { application.Activate(); clickButton("button_add_tunnel") }},
	} {
		if err := addMenuAction(menu, action.title, action.run); err != nil {
			return err
		}
	}
	if err := menuSeparator(menu); err != nil {
		return err
	}
	if err := addMenuAction(menu, "About WireGuard", showAbout); err != nil {
		return err
	}
	if err := addMenuAction(menu, "Quit WireGuard", func() { application.Quit() }); err != nil {
		return err
	}
	menu.ShowAll()
	return nil
}

func (t *Tunnels) createLayoutMenus() error {
	editorWindow.SetTransientFor(window)
	settingsWindow.SetTransientFor(window)
	logs, err := get.Window("log_window")
	if err != nil {
		return err
	}
	logs.SetTransientFor(window)
	logs.HideOnDelete()
	showLog := func() { logs.ShowAll(); logs.Present() }
	importTunnel := func() { clickButton("button_add_tunnel") }
	exportTunnels := func() { clickButton("button_zip_tunnel") }
	exportControl, err := get.Button("button_zip_tunnel")
	if err != nil {
		return err
	}
	actions, err := gtk.MenuNew()
	if err != nil {
		return err
	}
	for _, a := range []struct {
		title string
		run   func()
	}{
		{"View Log", showLog}, {"Export Tunnels to Zip…", exportTunnels},
		{"Settings…", func() { settingsWindow.ShowAll(); settingsWindow.Present() }},
	} {
		var controls []*gtk.Button
		if a.title == "Export Tunnels to Zip…" {
			controls = append(controls, exportControl)
		}
		if err := addMenuAction(actions, a.title, a.run, controls...); err != nil {
			return err
		}
	}
	actions.ShowAll()
	actionButton, err := get.MenuButton("menu")
	if err != nil {
		return err
	}
	actionButton.SetPopup(actions)
	add, err := gtk.MenuNew()
	if err != nil {
		return err
	}
	addEmpty := func() {
		if err := t.addEmptyTunnel(); err != nil {
			ShowError(window, err)
		}
	}
	if err := addMenuAction(add, "Add Empty Tunnel…", addEmpty); err != nil {
		return err
	}
	if err := addMenuAction(add, "Import Tunnel(s) from File…", importTunnel); err != nil {
		return err
	}
	add.ShowAll()
	addButton, err := get.MenuButton("add_menu")
	if err != nil {
		return err
	}
	addButton.SetPopup(add)
	bar, err := gtk.MenuBarNew()
	if err != nil {
		return err
	}
	type menuAction struct {
		title  string
		run    func()
		button string
	}
	sections := []struct {
		name  string
		items []menuAction
	}{
		{"WireGuard", []menuAction{{"About WireGuard", showAbout, ""}, {"Settings…", func() { settingsWindow.ShowAll(); settingsWindow.Present() }, ""}, {"Quit WireGuard", func() { application.Quit() }, ""}}},
		{"File", []menuAction{{"Add Empty Tunnel…", addEmpty, ""}, {"Import Tunnel(s) from File…", importTunnel, ""}, {"Export Tunnels to Zip…", exportTunnels, "button_zip_tunnel"}, {"Close Window", func() { window.Hide() }, ""}}},
		{"Tunnel", []menuAction{{"Activate", func() { clickButton("button_change_state") }, "button_change_state"}, {"Edit…", func() { clickButton("button_edit_tunnel") }, "button_edit_tunnel"}, {"Remove Tunnel", func() { clickButton("button_del_tunnel") }, "button_del_tunnel"}}},
		{"Window", []menuAction{{"Manage Tunnels", func() { application.Activate() }, ""}, {"View Log", showLog, ""}}},
	}
	for _, section := range sections {
		if section.name == "Tunnel" {
			if err := appendEditMenu(bar); err != nil {
				return err
			}
		}
		parent, err := gtk.MenuItemNewWithLabel(section.name)
		if err != nil {
			return err
		}
		menu, err := gtk.MenuNew()
		if err != nil {
			return err
		}
		for _, a := range section.items {
			a := a // Go 1.21: callbacks must keep their own action.
			item, err := gtk.MenuItemNewWithLabel(a.title)
			if err != nil {
				return err
			}
			item.Connect("activate", a.run)
			menu.Append(item)
			if a.button != "" {
				control, err := get.Button(a.button)
				if err != nil {
					return err
				}
				syncState := func() {
					item.SetSensitive(control.GetSensitive())
					if a.button == "button_change_state" {
						text, _ := control.GetLabel()
						item.SetLabel(text)
					}
				}
				syncState()
				control.Connect("notify::sensitive", syncState)
				control.Connect("notify::label", syncState)
			}
		}
		parent.SetSubmenu(menu)
		bar.Append(parent)
	}
	box, err := get.Box("application_menu")
	if err != nil {
		return err
	}
	box.Add(bar)
	return nil
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

func (t *Tunnels) applySelectionState(row *gtk.ListBoxRow) {
	selected := row != nil
	if !selected {
		t.UnknownLabels()
		t.setInterfaceState("—")
		if label, err := get.Label("label_interface_name"); err == nil {
			label.SetText("Select a tunnel")
		}
		if label, err := get.Label("label_peer_keepalive"); err == nil {
			label.SetText("—")
		}
	}
	for _, id := range []string{"button_edit_tunnel", "button_del_tunnel"} {
		if b, err := get.Button(id); err == nil {
			b.SetSensitive(selected)
		}
	}
	t.ButtonChangeState.SetSensitive(selected && t.statusAvailable)
}

func (t *Tunnels) setInterfaceState(state string) {
	t.Interface.Status.SetText(state)
	icon, err := get.Image("interface_status_icon")
	if err != nil {
		return
	}
	image := t.grayIcon
	if state == "Active" {
		image = t.greenIcon
	}
	if image != nil {
		icon.SetFromPixbuf(image.GetPixbuf())
	}
}

func (t *Tunnels) addEmptyTunnel() error {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return err
	}
	t.editingName = ""
	name, err := get.Entry("editor_name")
	if err != nil {
		return err
	}
	name.SetText("")
	publicKey, err := get.Entry("editor_publickey")
	if err != nil {
		return err
	}
	publicKey.SetText(key.PublicKey().String())
	text, err := get.TextView("editor_text")
	if err != nil {
		return err
	}
	buffer, err := text.GetBuffer()
	if err != nil {
		return err
	}
	buffer.SetText("[Interface]\nPrivateKey = " + key.String() + "\n\n[Peer]\nPublicKey = \nAllowedIPs = \nEndpoint = \n")
	editorWindow.SetTitle("Add Empty Tunnel")
	editorWindow.ShowAll()
	editorWindow.Present()
	name.GrabFocus()
	return nil
}

func loadStatusIcon(path string) (*gtk.Image, error) {
	image, err := loadEmbeddedImage(path)
	if err != nil {
		return nil, err
	}
	pixbuf, err := image.GetPixbuf().ScaleSimple(10, 10, gdk.INTERP_BILINEAR)
	if err != nil {
		return nil, err
	}
	image.SetFromPixbuf(pixbuf)
	return image, nil
}

func appendEditMenu(bar *gtk.MenuBar) error {
	parent, err := gtk.MenuItemNewWithLabel("Edit")
	if err != nil {
		return err
	}
	menu, err := gtk.MenuNew()
	if err != nil {
		return err
	}
	for _, action := range []struct{ title, signal string }{{"Cut", "cut-clipboard"}, {"Copy", "copy-clipboard"}, {"Paste", "paste-clipboard"}, {"Select All", "select-all"}} {
		action := action
		item, err := gtk.MenuItemNewWithLabel(action.title)
		if err != nil {
			return err
		}
		item.Connect("activate", func() {
			focus, err := window.GetFocus()
			if err != nil {
				return
			}
			switch widget := focus.(type) {
			case *gtk.Label:
				if action.signal == "select-all" {
					widget.SelectRegion(0, -1)
				} else if action.signal == "copy-clipboard" {
					widget.Emit(action.signal, glib.TYPE_NONE)
				}
			case *gtk.Entry:
				if action.signal == "select-all" {
					widget.SelectRegion(0, -1)
				} else {
					widget.Emit(action.signal, glib.TYPE_NONE)
				}
			case *gtk.TextView:
				if action.signal == "select-all" {
					buffer, err := widget.GetBuffer()
					if err == nil {
						buffer.SelectRange(buffer.GetStartIter(), buffer.GetEndIter())
					}
				} else {
					widget.Emit(action.signal, glib.TYPE_NONE)
				}
			}
		})
		menu.Connect("show", func() {
			focus, _ := window.GetFocus()
			sensitive := false
			switch focus.(type) {
			case *gtk.Entry, *gtk.TextView:
				sensitive = true
			case *gtk.Label:
				sensitive = action.signal == "copy-clipboard" || action.signal == "select-all"
			}
			item.SetSensitive(sensitive)
		})
		menu.Append(item)
	}
	parent.SetSubmenu(menu)
	bar.Append(parent)
	return nil
}
