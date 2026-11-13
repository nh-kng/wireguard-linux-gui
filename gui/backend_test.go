package gui

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/settings"
)

type fakeWireGuard struct {
	active map[string]bool
	err    error
}

func (f *fakeWireGuard) Devices() ([]*wgtypes.Device, error) {
	if f.err != nil {
		return nil, f.err
	}
	var ds []*wgtypes.Device
	for _, name := range []string{"one", "two", "three"} {
		if f.active[name] {
			ds = append(ds, &wgtypes.Device{Name: name})
		}
	}
	return ds, nil
}
func (f *fakeWireGuard) Device(name string) (*wgtypes.Device, error) {
	if f.active[name] {
		return &wgtypes.Device{Name: name}, nil
	}
	return nil, os.ErrNotExist
}
func (f *fakeWireGuard) Close() error { return nil }
func TestTunnelActions(t *testing.T) {
	for _, test := range []struct {
		name       string
		multiple   bool
		active     []string
		selected   string
		deactivate bool
		want       []string
	}{
		{name: "activate", selected: "one", want: []string{"up one"}},
		{name: "disconnect does not reconnect", active: []string{"one"}, selected: "one", want: []string{"down one"}},
		{name: "single mode switches", active: []string{"one", "two"}, selected: "three", want: []string{"down one", "down two", "up three"}},
		{name: "multiple mode preserves others", multiple: true, active: []string{"one"}, selected: "two", want: []string{"up two"}},
		{name: "deactivate all never toggles back on", active: []string{"one", "two"}, deactivate: true, want: []string{"down one", "down two"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldClient, oldQuick, oldSettings := wgc, runWGQuick, Settings
			defer func() { wgc, runWGQuick, Settings = oldClient, oldQuick, oldSettings }()
			f := &fakeWireGuard{active: map[string]bool{}}
			for _, name := range test.active {
				f.active[name] = true
			}
			wgc = f
			Settings = &settings.Settings{MultipleTunnels: test.multiple}
			var calls []string
			runWGQuick = func(action, name string) ([]byte, error) {
				calls = append(calls, action+" "+name)
				f.active[name] = action == "up"
				return nil, nil
			}
			controller := &Tunnels{}
			var err error
			if test.deactivate {
				err = controller.deactivateAll()
			} else {
				err = controller.ToggleTunnel(nil, test.selected)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, test.want) {
				t.Fatalf("commands %v; want %v", calls, test.want)
			}
		})
	}
}
func TestUnavailableStatusCannotActivate(t *testing.T) {
	oldClient, oldQuick := wgc, runWGQuick
	defer func() { wgc, runWGQuick = oldClient, oldQuick }()
	wgc = &fakeWireGuard{err: errors.New("status unavailable")}
	runWGQuick = func(action, name string) ([]byte, error) { t.Fatal("ran command without status"); return nil, nil }
	if (&Tunnels{}).ToggleTunnel(nil, "one") == nil {
		t.Fatal("missing error")
	}
}
