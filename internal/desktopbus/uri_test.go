package desktopbus

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

type fakeURIPortal struct {
	t   *testing.T
	err error
}

func (p fakeURIPortal) CallWithContext(_ context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	p.t.Helper()
	want := []interface{}{"", "https://example.com", map[string]dbus.Variant{}}
	if method != "org.freedesktop.portal.OpenURI.OpenURI" || flags != 0 || !reflect.DeepEqual(args, want) {
		p.t.Fatalf("unexpected portal request: %s %v %v", method, flags, args)
	}
	return &dbus.Call{Err: p.err, Body: []interface{}{dbus.ObjectPath("/request/link")}}
}

func TestRequestURIResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      []interface{}
		wantError bool
	}{
		{"success", []interface{}{uint32(0), map[string]dbus.Variant{}}, false},
		{"cancelled", []interface{}{uint32(1), map[string]dbus.Variant{}}, false},
		{"failed", []interface{}{uint32(2), map[string]dbus.Variant{}}, true},
		{"malformed", []interface{}{"invalid"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := make(chan *dbus.Signal, 2)
			responses <- &dbus.Signal{Path: "/request/unrelated", Name: "org.freedesktop.portal.Request.Response"}
			responses <- &dbus.Signal{Path: "/request/link", Name: "org.freedesktop.portal.Request.Response", Body: tc.body}
			err := requestURI(context.Background(), fakeURIPortal{t: t}, responses, "https://example.com")
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestRequestURIFailures(t *testing.T) {
	want := errors.New("portal unavailable")
	err := requestURI(context.Background(), fakeURIPortal{t: t, err: want}, nil, "https://example.com")
	if !errors.Is(err, want) {
		t.Fatalf("lost portal error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := requestURI(ctx, fakeURIPortal{t: t}, nil, "https://example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	closed := make(chan *dbus.Signal)
	close(closed)
	if err := requestURI(context.Background(), fakeURIPortal{t: t}, closed, "https://example.com"); err == nil {
		t.Fatal("missing error for closed connection")
	}
}
