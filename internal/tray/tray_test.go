package tray

import (
	"bufio"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

type fakeWatcher struct{ registered chan string }

func (w *fakeWatcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	w.registered <- service
	return nil
}
func privateBus(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon unavailable")
	}
	cmd := exec.Command(binary, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("missing private bus address")
	}
	return scanner.Text()
}
func TestTrayProtocolAndWatcherRestart(t *testing.T) {
	address := privateBus(t)
	host, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	w := &fakeWatcher{make(chan string, 8)}
	if err = host.Export(w, "/StatusNotifierWatcher", watcher); err != nil {
		t.Fatal(err)
	}
	_, err = prop.Export(host, "/StatusNotifierWatcher", prop.Map{watcher: {"IsStatusNotifierHostRegistered": {Value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = host.RequestName(watcher, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	var clicks atomic.Int32
	item, err := New(address, "wireguard-inactive-symbolic", "/tmp/icons", func(f func()) { f() }, func() { clicks.Add(1) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer item.Close()
	await := func() {
		t.Helper()
		select {
		case <-w.registered:
		case <-time.After(3 * time.Second):
			t.Fatal("watcher registration timed out")
		}
	}
	await()
	if !item.Available() {
		t.Fatal("tray not available")
	}
	item.Update("wireguard-active-symbolic", "Active", []Item{{Label: "test", Visible: true, Enabled: true, Checkable: true, Checked: true, Activate: func() { clicks.Add(1) }}, {Label: "disabled", Visible: true}})
	iconObject := host.Object(item.conn.Names()[0], itemPath)
	for name, want := range map[string]string{"IconName": "wireguard-active-symbolic", "IconThemePath": "/tmp/icons"} {
		value, err := iconObject.GetProperty(itemInterface + "." + name)
		if err != nil || value.Value() != want {
			t.Fatalf("%s = %v, %v; want %q", name, value, err, want)
		}
	}
	obj := host.Object(item.conn.Names()[0], menuPath)
	var revision uint32
	var root layout
	if err = obj.Call(menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &root); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || len(root.Children) != 2 {
		t.Fatalf("layout: %d %+v", revision, root)
	}
	var checked dbus.Variant
	if err = obj.Call(menuInterface+".GetProperty", 0, int32(1), "toggle-state").Store(&checked); err != nil {
		t.Fatal(err)
	}
	if checked.Value() != int32(1) {
		t.Fatalf("checkmark: %v", checked)
	}
	for _, id := range []int32{1, 2} {
		if err = obj.Call(menuInterface+".Event", 0, id, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
			t.Fatal(err)
		}
	}
	if clicks.Load() != 1 {
		t.Fatalf("disabled action activated: %d", clicks.Load())
	}
	item.Update("wireguard-active-symbolic", "Active", []Item{{Label: "other", Visible: true, Enabled: true, Activate: func() { clicks.Add(10) }}, {Label: "test", Visible: true, Enabled: true, Activate: func() { clicks.Add(1) }}})
	if err = obj.Call(menuInterface+".Event", 0, int32(1), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	if clicks.Load() != 2 {
		t.Fatal("menu IDs changed after reordering")
	}
	item.Update("wireguard-active-symbolic", "Active", []Item{{Label: "other", Visible: true, Enabled: true, Activate: func() { clicks.Add(10) }}})
	if err = obj.Call(menuInterface+".Event", 0, int32(1), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	if clicks.Load() != 2 {
		t.Fatal("stale menu event activated another action")
	}
	if _, err = host.ReleaseName(watcher); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for item.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if item.Available() {
		t.Fatal("watcher disappearance not observed")
	}
	if _, err = host.RequestName(watcher, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	await()
	deadline = time.Now().Add(3 * time.Second)
	for !item.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !item.Available() {
		t.Fatal("watcher restart not observed")
	}
	item.conn.Close()
	deadline = time.Now().Add(3 * time.Second)
	for item.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if item.Available() {
		t.Fatal("session bus disconnect not observed")
	}
	item.Update("/tmp/icon.png", "Disconnected", nil)
	item.Close()
}
