package gui

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
	"wireguard-linux-gui/internal/desktopbus"
)

// Use the desktop portal rather than root's private settings after pkexec.
// Native Libadwaita still supplies all colors and platform-specific accents.
func (t *Tunnels) followAppearance() func() {
	conn, err := desktopbus.Connect(t.sessionAddress)
	if err != nil {
		log.Warn().Err(err).Msg("connect to desktop appearance settings")
		return func() {}
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	if err = conn.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.portal.Desktop"), dbus.WithMatchInterface("org.freedesktop.portal.Settings"), dbus.WithMatchMember("SettingChanged")); err != nil {
		log.Warn().Err(err).Msg("watch desktop appearance settings")
		conn.Close()
		return func() {}
	}
	apply := func(value dbus.Variant) {
		raw := value.Value()
		for {
			v, ok := raw.(dbus.Variant)
			if !ok {
				break
			}
			raw = v.Value()
		}
		scheme, ok := raw.(uint32)
		if !ok {
			return
		}
		glib.IdleAdd(func() {
			if t.closed {
				return
			}
			style := adw.ColorSchemeDefault
			// The portal supplies the invoking user's explicit choice. Force it
			// so Libadwaita cannot override it with the elevated user's settings.
			if scheme == 1 {
				style = adw.ColorSchemeForceDark
			} else if scheme == 2 {
				style = adw.ColorSchemeForceLight
			}
			adw.StyleManagerGetDefault().SetColorScheme(style)
		})
	}
	go func() {
		var value dbus.Variant
		if err := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").Call("org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&value); err == nil {
			apply(value)
		} else {
			log.Warn().Err(err).Msg("read desktop appearance settings")
		}
		for signal := range signals {
			if len(signal.Body) == 3 && signal.Body[0] == "org.freedesktop.appearance" && signal.Body[1] == "color-scheme" {
				if v, ok := signal.Body[2].(dbus.Variant); ok {
					apply(v)
				}
			}
		}
	}()
	return func() { conn.Close() }
}
