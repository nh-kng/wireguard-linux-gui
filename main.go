//go:generate go run ./gui/get/generator/generator.go -target=./gui/get
//go:generate go run github.com/UnnoTed/fileb0x fileb0x.toml
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/UnnoTed/go-appindicator"
	"github.com/UnnoTed/horizontal"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/rs/zerolog/log"
	"wireguard-linux-gui/gui"
	"wireguard-linux-gui/gui/countries"
	"wireguard-linux-gui/internal/instance"
	"wireguard-linux-gui/static"
)

var iconTmpDir string
var win *gtk.ApplicationWindow

func main() {
	os.Exit(run())
}

func run() int {
	if os.Geteuid() != 0 {
		fmt.Println("wireguard-linux-gui requires root to access /etc/wireguard and wg-quick.")
		fmt.Println("Run: pkexec wireguard-linux-gui")
		return 1
	}

	defer countries.CloseDatabase()

	log.Logger = log.Output(horizontal.ConsoleWriter{Out: os.Stderr})
	log.Info().Uint("major", gtk.GetMajorVersion()).Uint("minor", gtk.GetMinorVersion()).Uint("micro", gtk.GetMicroVersion()).Msg("GTK Version")

	if err := gui.Settings.Load(); err != nil {
		log.Error().Err(err).Msg("error initial settings load")
	}

	const appID = "com.wireguard.linuxgui"
	application, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	if err != nil {
		log.Error().Err(err).Msg("error creating application")
		return 1
	}

	// pkexec may start us without the desktop's session bus. Keep a local
	// singleton as well as GtkApplication's D-Bus activation support.
	session := sha256.Sum256([]byte(os.Getenv("DISPLAY") + "\x00" + os.Getenv("WAYLAND_DISPLAY")))
	owner, err := instance.Open(filepath.Join("/run/wireguard-linux-gui", fmt.Sprintf("%x", session[:8])), func() {
		glib.IdleAdd(func() { application.Activate() })
	})
	if err != nil {
		log.Error().Err(err).Msg("error activating application")
		return 1
	}
	if owner == nil {
		return 0
	}
	defer owner.Close()

	iconTmpDir, err = os.MkdirTemp("", "wireguard-linux-gui-icons-")
	if err != nil {
		log.Error().Err(err).Msg("error creating icon directory")
		return 1
	}
	defer os.RemoveAll(iconTmpDir)
	// The desktop shell reads these public icons as the unprivileged user.
	if err := os.Chmod(iconTmpDir, 0755); err != nil {
		log.Error().Err(err).Msg("error making tray icons readable")
		return 1
	}

	application.Connect("activate", func() {
		if win != nil {
			win.ShowAll()
			win.Present()
			return
		}
		if err := createWindow(application); err != nil {
			log.Error().Err(err).Msg("create window error")
			application.Quit()
		}
	})

	return application.Run(os.Args)
}

func createTray(application *gtk.Application) (*appindicator.Indicator, error) {
	menu, err := gtk.MenuNew()
	if err != nil {
		return nil, err
	}

	iconsDir := "/usr/share/wireguard-linux-gui/Icon/"
	if _, err := os.Stat(iconsDir); os.IsNotExist(err) {
		iconsDir = iconTmpDir
		icons := []string{"wireguard_off", "wg_connected"}
		for _, icon := range icons {
			iconBytes, err := static.ReadFile("./Icon/" + icon + ".png")
			if err != nil {
				log.Error().Err(err).Str("icon", icon).Msg("Error reading embedded icon")
				return nil, err
			}

			iconPath := filepath.Join(iconTmpDir, icon+".png")
			if err := os.WriteFile(iconPath, iconBytes, 0644); err != nil {
				log.Error().Err(err).Str("icon", icon).Msg("Failed to write temp icon")
				return nil, err
			}
		}
	}

	indicator := appindicator.New(application.GetApplicationID(), "wireguard_off", appindicator.CategoryApplicationStatus)
	indicator.SetIconThemePath(iconsDir)
	indicator.SetTitle("wireguard-linux-gui")
	// indicator.SetLabel("wireguard-linux-gui", "")
	indicator.SetStatus(appindicator.StatusActive)
	indicator.SetMenu(menu)

	return indicator, nil
}

func createWindow(application *gtk.Application) error {
	data, err := static.ReadFile("wireguard-linux-gui.glade")
	if err != nil {
		log.Error().Err(err).Msg("cant read wireguard-linux-gui.glade")
		return err
	}

	b, err := gtk.BuilderNew()
	if err != nil {
		return err
	}
	if err := b.AddFromString(string(data)); err != nil {
		log.Error().Err(err).Msg("error getting main glade file")
		return err
	}

	wobj, err := b.GetObject("main_window")
	if err != nil {
		log.Error().Err(err).Msg("error getting main_window")
		return err
	}

	var ok bool
	win, ok = wobj.(*gtk.ApplicationWindow)
	if !ok {
		panic("not window")
	}
	application.AddWindow(win)

	indicator, err := createTray(application)
	if err != nil {
		log.Error().Err(err).Msg("create tray error")
		return err
	}

	if err := gui.Create(application, b, win, indicator); err != nil {
		log.Error().Err(err).Msg("error gui setup")
		return err
	}

	if !gui.Settings.StartOnTray {
		win.ShowAll()
	}

	win.SetTitle("Manage WireGuard Tunnels")

	return nil
}
