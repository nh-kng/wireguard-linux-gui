package gui

import (
	"context"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	glibv2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"gopkg.in/ini.v1"
	"wireguard-linux-gui/gui/countries"
)

// Invalidate pending lookups before hiding flags. A lookup from an older
// preference or tunnel list must never make a hidden flag visible again.
func (t *Tunnels) refreshCountryFlags(enabled bool) {
	t.flagGeneration++
	generation := t.flagGeneration
	for _, flag := range t.flags {
		flag.SetVisible(false)
		flag.Clear()
	}
	if !enabled || len(t.flags) == 0 {
		return
	}
	for name, flag := range t.flags {
		cfg, err := ini.Load(filepath.Join(TunnelsPath, name+".conf"))
		if err != nil {
			continue
		}
		endpoint := cfg.Section("Peer").Key("Endpoint").String()
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil {
			continue
		}
		go func() {
			if err := countries.OpenDatabase(); err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return
			}
			code, err := countries.Find(ips[0].String())
			if err != nil {
				return
			}
			data, err := fs.ReadFile(t.assets, "flags/"+strings.ToUpper(code)+".png")
			if err != nil {
				return
			}
			glib.IdleAdd(func() {
				if t.closed || t.flagGeneration != generation {
					return
				}
				texture, err := gdk.NewTextureFromBytes(glibv2.NewBytesWithGo(data))
				if err == nil {
					flag.SetFromPaintable(texture)
					flag.SetVisible(true)
				}
			})
		}()
	}
}
