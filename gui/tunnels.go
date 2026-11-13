package gui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/dustin/go-humanize"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

func (t *Tunnels) ScanTunnels() error {
	entries, err := os.ReadDir(TunnelsPath)
	if err != nil {
		return err
	}
	for {
		row := t.list.RowAtIndex(0)
		if row == nil {
			break
		}
		t.list.Remove(row)
	}
	t.icons = map[string]*gtk.Image{}
	t.flags = map[string]*gtk.Image{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".conf")
		row := gtk.NewListBoxRow()
		row.SetName(name)
		box := gtk.NewBox(gtk.OrientationHorizontal, 8)
		box.SetMarginStart(6)
		box.SetMarginEnd(6)
		box.SetMarginTop(4)
		box.SetMarginBottom(4)
		icon := gtk.NewImageFromIconName("object-select-symbolic")
		icon.SetPixelSize(16)
		icon.SetVAlign(gtk.AlignCenter)
		icon.SetVisible(t.statusAvailable && contains(deviceNames(t.devices), name))
		icon.SetTooltipText("Active tunnel")
		t.icons[name] = icon
		box.Append(icon)
		flag := gtk.NewImage()
		flag.SetPixelSize(20)
		flag.SetVAlign(gtk.AlignCenter)
		flag.SetVisible(false)
		t.flags[name] = flag
		box.Append(flag)
		label := gtk.NewLabel(name)
		label.SetXAlign(0)
		box.Append(label)
		row.SetChild(box)
		t.list.Append(row)
		if name == t.lastSelected {
			t.list.SelectRow(row)
		}
	}
	if t.list.SelectedRow() == nil {
		t.list.SelectRow(t.list.RowAtIndex(0))
	}
	t.actions["export"].SetEnabled(len(t.icons) > 0)
	enabled := Settings.CountryFlags
	if t.preferences.Visible() {
		enabled = t.switches["country_flags"].Active()
	}
	t.refreshCountryFlags(enabled)
	t.UpdateRow(t.list.SelectedRow())
	t.updateTray()
	t.RefreshStatus()
	return nil
}
func (t *Tunnels) UpdateRow(row *gtk.ListBoxRow) {
	selected := row != nil
	for _, id := range []string{"edit", "remove"} {
		t.actions[id].SetEnabled(selected)
	}
	t.actions["toggle"].SetEnabled(selected && t.statusAvailable)
	for _, label := range t.fields {
		label.SetText("—")
	}
	heading := object[*adw.ActionRow](t.builder, "profile_heading")
	if !selected {
		object[*gtk.Button](t.builder, "button_change_state").SetLabel("Activate")
		heading.SetTitle("Select a tunnel")
		return
	}
	name := row.Name()
	t.lastSelected = name
	heading.SetTitle(name)
	cfg, err := ini.Load(TunnelsPath + name + ".conf")
	if err != nil {
		ShowError(err)
		return
	}
	in, peer := cfg.Section("Interface"), cfg.Section("Peer")
	t.selectedPublicKey = "—"
	if key, e := wgtypes.ParseKey(in.Key("PrivateKey").String()); e == nil {
		t.selectedPublicKey = key.PublicKey().String()
	}
	t.selectedListenPort = in.Key("ListenPort").String()
	if t.selectedListenPort == "" {
		t.selectedListenPort = "Automatic"
	}
	t.label("interface_addresses", in.Key("Address").String())
	t.label("interface_dns_servers", in.Key("DNS").String())
	t.label("peer_allowed_ips", peer.Key("AllowedIPs").String())
	t.label("peer_public_key", peer.Key("PublicKey").String())
	t.label("peer_endpoint", peer.Key("Endpoint").String())
	keepalive := peer.Key("PersistentKeepalive").String()
	if keepalive == "" || keepalive == "0" {
		keepalive = "Off"
	} else {
		keepalive += " seconds"
	}
	t.label("peer_keepalive", keepalive)
	t.renderStatus()
}
func deviceNames(ds []*wgtypes.Device) []string {
	names := make([]string, 0, len(ds))
	for _, d := range ds {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}
func (t *Tunnels) applyStatus(ds []*wgtypes.Device, err error) {
	if err != nil {
		ds = nil
	}
	t.devices = ds
	t.statusAvailable = err == nil
	activeNames := deviceNames(ds)
	for name, icon := range t.icons {
		icon.SetVisible(t.statusAvailable && contains(activeNames, name))
	}
	t.actions["toggle"].SetEnabled(t.list.SelectedRow() != nil && t.statusAvailable)
	t.renderStatus()
	t.updateTray()
}
func (t *Tunnels) renderStatus() {
	if t.list.SelectedRow() == nil {
		return
	}
	button := object[*gtk.Button](t.builder, "button_change_state")
	button.SetLabel("Activate")
	t.label("interface_public_key", t.selectedPublicKey)
	t.label("interface_listen_port", t.selectedListenPort)
	t.label("peer_latest_handshake", "—")
	t.label("peer_transfer", "—")
	if !t.statusAvailable {
		t.label("interface_status", "Unknown")
		return
	}
	t.label("interface_status", "Inactive")
	for _, d := range t.devices {
		if d.Name != t.list.SelectedRow().Name() {
			continue
		}
		t.label("interface_status", "Active")
		button.SetLabel("Deactivate")
		t.label("interface_public_key", d.PublicKey.String())
		t.label("interface_listen_port", strconv.Itoa(d.ListenPort))
		for _, p := range d.Peers {
			hs := "Never"
			if !p.LastHandshakeTime.IsZero() {
				hs = humanize.Time(p.LastHandshakeTime)
			}
			t.label("peer_latest_handshake", hs)
			t.label("peer_transfer", fmt.Sprintf("%s received, %s sent", humanize.Bytes(uint64(p.ReceiveBytes)), humanize.Bytes(uint64(p.TransmitBytes))))
		}
	}
}
