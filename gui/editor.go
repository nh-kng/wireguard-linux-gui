package gui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

var tunnelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_=+.-]{1,15}$`)

func validTunnelName(name string) bool {
	return tunnelNamePattern.MatchString(name) && name != "." && name != ".."
}
func (t *Tunnels) openEditor(name string) error {
	text := ""
	if name == "" {
		key, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return err
		}
		text = "[Interface]\nPrivateKey = " + key.String() + "\n\n[Peer]\nPublicKey = \nAllowedIPs = \nEndpoint = \n"
	} else {
		data, err := os.ReadFile(filepath.Join(TunnelsPath, name+".conf"))
		if err != nil {
			return err
		}
		text = string(data)
	}
	t.editingName = name
	object[*gtk.Entry](t.builder, "editor_name").SetText(name)
	publicKey := ""
	if cfg, err := ini.Load([]byte(text)); err == nil {
		if key, e := wgtypes.ParseKey(cfg.Section("Interface").Key("PrivateKey").String()); e == nil {
			publicKey = key.PublicKey().String()
		}
	}
	object[*gtk.Entry](t.builder, "editor_publickey").SetText(publicKey)
	object[*gtk.TextView](t.builder, "editor_text").Buffer().SetText(text)
	t.modified = false
	title := "Edit Tunnel"
	if name == "" {
		title = "Add Empty Tunnel"
	}
	t.editor.SetTitle(title)
	t.editor.Present()
	object[*gtk.Entry](t.builder, "editor_name").GrabFocus()
	return nil
}
func (t *Tunnels) cancelEditor() {
	if t.modified {
		message(&t.editor.Window, "Discard changes?", "Unsaved tunnel changes will be lost.", func() { t.modified = false; t.editor.SetVisible(false) })
	} else {
		t.editor.SetVisible(false)
	}
}
func (t *Tunnels) saveEditor() error {
	if t.operationBusy {
		return errors.New("wait for the tunnel operation to finish")
	}
	name := strings.TrimSpace(object[*gtk.Entry](t.builder, "editor_name").Text())
	if !validTunnelName(name) {
		return errors.New("use a tunnel name of 1–15 letters, numbers, or _ = + . - characters")
	}
	path := filepath.Join(TunnelsPath, name+".conf")
	if name != t.editingName {
		if _, err := os.Stat(path); err == nil {
			return errors.New("a tunnel with that name already exists")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	buffer := object[*gtk.TextView](t.builder, "editor_text").Buffer()
	data := buffer.Text(buffer.StartIter(), buffer.EndIter(), false)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	if t.editingName != "" && name != t.editingName {
		if err := os.Remove(filepath.Join(TunnelsPath, t.editingName+".conf")); err != nil {
			return err
		}
	}
	t.lastSelected = name
	t.modified = false
	t.editor.SetVisible(false)
	return t.ScanTunnels()
}
func (t *Tunnels) removeSelected() {
	row := t.list.SelectedRow()
	if row == nil {
		return
	}
	name := row.Name()
	message(&t.window.Window, "Remove tunnel?", "Remove "+name+"? Active interfaces continue running.", func() {
		if t.operationBusy {
			ShowError(errors.New("wait for the tunnel operation to finish"))
			return
		}
		if err := os.Remove(filepath.Join(TunnelsPath, name+".conf")); err != nil {
			ShowError(err)
			return
		}
		ShowError(t.ScanTunnels())
	})
}
