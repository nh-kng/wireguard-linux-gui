package gui

import (
	"context"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"wireguard-linux-gui/internal/desktopbus"
)

// BuildDate is supplied at link time by local and release build commands.
var BuildDate = "unknown"

func (t *Tunnels) openAbout() {
	// Reuse the bundled tray artwork even when running from a checkout.
	theme := gtk.IconThemeGetForDisplay(t.window.Window.Widget.Display())
	if !theme.HasIcon("wireguard-active-symbolic") {
		theme.AddSearchPath(t.iconDir)
	}

	dialog := adw.NewAboutDialog()
	dialog.ConnectActivateLink(func(uri string) bool {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			ShowError(desktopbus.OpenURI(ctx, t.sessionAddress, uri))
		}()
		return true
	})
	dialog.SetApplicationName("WireGuard Linux GUI")
	dialog.SetApplicationIcon("wireguard-active-symbolic")
	dialog.SetVersion(Version)
	addBuildDate(&dialog.Widget)
	dialog.SetComments("A native GTK4/Libadwaita client for WireGuard, based on wireguird.")
	dialog.SetWebsite("https://github.com/nh-kng/wireguard-linux-gui")
	dialog.SetIssueURL("https://github.com/nh-kng/wireguard-linux-gui/issues")
	dialog.SetDevelopers([]string{"nh-kng https://github.com/nh-kng", "UnnoTed https://github.com/UnnoTed"})
	dialog.SetCopyright("© 2020 UnnoTed")
	dialog.SetLicenseType(gtk.LicenseMITX11)
	dialog.AddLink("Original wireguird project", "https://github.com/UnnoTed/wireguird")
	dialog.AddLink("Third-party notices", "https://github.com/nh-kng/wireguard-linux-gui/blob/master/THIRD_PARTY_NOTICES.md")
	dialog.AddLegalSection("GTK4 and Libadwaita Go bindings", "", gtk.LicenseMPL20, "")
	dialog.AddAcknowledgementSection("WireGuard icon", []string{"CoreUI Icons https://github.com/coreui/coreui-icons"})

	// The About action is also available from the tray while the window is hidden.
	t.Present()
	dialog.Present(t.window)
}

func addBuildDate(widget *gtk.Widget) bool {
	// AdwAboutDialog has no public slot below the version. Find its styled
	// version chip rather than relying on a fixed internal widget hierarchy.
	if widget.HasCSSClass("app-version") {
		if parent, ok := widget.Parent().(*gtk.Box); ok {
			date := BuildDate
			if stamp, err := time.Parse(time.RFC3339, date); err == nil {
				date = stamp.UTC().Format("2006-01-02")
			}
			label := gtk.NewLabel("Build date: " + date)
			label.AddCSSClass("caption")
			label.AddCSSClass("dim-label")
			label.SetHAlign(gtk.AlignCenter)
			label.SetSelectable(true)
			parent.InsertChildAfter(label, widget)
			return true
		}
	}
	for child := widget.FirstChild(); child != nil; child = gtk.BaseWidget(child).NextSibling() {
		if addBuildDate(gtk.BaseWidget(child)) {
			return true
		}
	}
	return false
}
