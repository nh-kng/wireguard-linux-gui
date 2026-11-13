package desktopbus

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// OpenURI asks the invoking user's desktop to open a link in its default app.
// Using the authenticated session connection avoids launching a browser as root.
func OpenURI(ctx context.Context, address, uri string) error {
	conn, err := Connect(address)
	if err != nil {
		return fmt.Errorf("open link: %w", err)
	}
	defer conn.Close()
	options := []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	}
	if err = conn.AddMatchSignalContext(ctx, options...); err != nil {
		return fmt.Errorf("listen for link response: %w", err)
	}
	responses := make(chan *dbus.Signal, 8)
	conn.Signal(responses)
	defer conn.RemoveSignal(responses)
	portal := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	return requestURI(ctx, portal, responses, uri)
}

type uriPortal interface {
	CallWithContext(context.Context, string, dbus.Flags, ...interface{}) *dbus.Call
}

func requestURI(ctx context.Context, portal uriPortal, responses <-chan *dbus.Signal, uri string) error {
	var handle dbus.ObjectPath
	err := portal.CallWithContext(ctx, "org.freedesktop.portal.OpenURI.OpenURI", 0,
		"", uri, map[string]dbus.Variant{}).Store(&handle)
	if err != nil {
		return fmt.Errorf("open link: %w", err)
	}
	// Keep the connection alive until the desktop finishes the request.
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("open link: %w", ctx.Err())
		case signal, ok := <-responses:
			if !ok {
				return fmt.Errorf("open link: desktop connection closed")
			}
			if signal == nil || signal.Path != handle || signal.Name != "org.freedesktop.portal.Request.Response" {
				continue
			}
			var response uint32
			var results map[string]dbus.Variant
			if err := dbus.Store(signal.Body, &response, &results); err != nil {
				return fmt.Errorf("open link response: %w", err)
			}
			if response == 0 || response == 1 { // Success or user cancellation.
				return nil
			}
			return fmt.Errorf("desktop could not open link (response %d)", response)
		}
	}
}
