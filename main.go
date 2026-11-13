package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	glibv2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/rs/zerolog/log"
	"wireguard-linux-gui/gui"
	"wireguard-linux-gui/internal/desktopbus"
	"wireguard-linux-gui/internal/instance"
)

func main() { os.Exit(run()) }
func run() int {
	if handled, err := desktopbus.RunHelper(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Run: pkexec wireguard-linux-gui (root is required for /etc/wireguard and wg-quick)")
		return 1
	}
	if err := gui.Settings.Init(); err != nil {
		log.Error().Err(err).Msg("load settings")
	}
	if address := desktopbus.Address(); address != "" {
		os.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	}
	glibv2.SetApplicationName("WireGuard")
	app := adw.NewApplication("com.wireguard.linuxgui", gio.ApplicationNonUnique)
	session := sha256.Sum256([]byte(os.Getenv("DISPLAY") + "\x00" + os.Getenv("WAYLAND_DISPLAY")))
	owner, err := instance.Open(filepath.Join("/run/wireguard-linux-gui", fmt.Sprintf("%x", session[:8])), func() { glib.IdleAdd(func() { app.Activate() }) })
	if err != nil {
		log.Error().Err(err).Msg("activate application")
		return 1
	}
	if owner == nil {
		return 0
	}
	defer owner.Close()
	var controller *gui.Tunnels
	failed := false
	app.ConnectActivate(func() {
		if controller != nil {
			controller.Present()
			return
		}
		data, err := assets.ReadFile("gui/window.ui")
		if err != nil {
			log.Error().Err(err).Msg("read interface")
			failed = true
			app.Quit()
			return
		}
		builder := gtk.NewBuilder()
		if err = builder.AddFromString(string(data)); err != nil {
			log.Error().Err(err).Msg("load interface")
			failed = true
			app.Quit()
			return
		}
		controller, err = gui.Create(app, builder, assets, desktopbus.Address())
		if err != nil {
			log.Error().Err(err).Msg("create interface")
			failed = true
			app.Quit()
		}
	})
	result := app.Run(os.Args)
	if failed {
		return 1
	}
	return result
}
