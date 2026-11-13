package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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
			runWGQuick = func(ctx context.Context, action, name string) ([]byte, error) {
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
	runWGQuick = func(ctx context.Context, action, name string) ([]byte, error) {
		t.Fatal("ran command without status")
		return nil, nil
	}
	if (&Tunnels{}).ToggleTunnel(nil, "one") == nil {
		t.Fatal("missing error")
	}
}

func TestTunnelCommandCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runTunnelCommand(ctx, "/bin/sh", "-c", "sleep 30 & wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("descendants held command open after cancellation")
	}
}

func TestTunnelCommandOutputLimit(t *testing.T) {
	data, err := runTunnelCommand(context.Background(), "/bin/sh", "-c", "printf '%070000d' 0")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 64<<10 {
		t.Fatalf("unbounded output: %d", len(data))
	}
}

func TestRestoreRejectsInvalidNamesAndPreservesActiveTunnels(t *testing.T) {
	oldQuick := runWGQuick
	defer func() { runWGQuick = oldQuick }()
	dir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(dir, name+".conf"), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeWireGuard{active: map[string]bool{"one": true}}
	snapshot := &settings.Settings{MultipleTunnels: true, RememberConnectedServers: true, ConnectedServers: map[string]int64{"../bad": 3, "one": 2, "two": 1}}
	var calls []string
	runWGQuick = func(ctx context.Context, action, name string) ([]byte, error) {
		calls = append(calls, action+" "+name)
		client.active[name] = action == "up"
		return nil, nil
	}
	backend := tunnelBackend{client: client, settings: snapshot, ctx: context.Background()}
	if err := backend.restore(dir); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"up two"}) {
		t.Fatalf("restore commands: %v", calls)
	}
	if !client.active["one"] || !client.active["two"] {
		t.Fatalf("wrong final state: %v", client.active)
	}
}

func TestCancelledOrInvalidToggleRunsNoCommands(t *testing.T) {
	oldQuick := runWGQuick
	defer func() { runWGQuick = oldQuick }()
	runWGQuick = func(context.Context, string, string) ([]byte, error) { t.Fatal("unexpected command"); return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := tunnelBackend{client: &fakeWireGuard{}, settings: &settings.Settings{}, ctx: ctx}
	if err := backend.toggle("one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	backend.ctx = context.Background()
	if err := backend.toggle("../bad.conf"); err == nil {
		t.Fatal("invalid tunnel name accepted")
	}
}
