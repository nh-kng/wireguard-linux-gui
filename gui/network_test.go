package gui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"wireguard-linux-gui/settings"
)

// Requires an explicitly requested private network namespace. Never runs on the host network.
func TestWireGuardNamespace(t *testing.T) {
	if os.Getenv("WG_NET_TEST") != "1" {
		t.Skip("set WG_NET_TEST=1 inside a root network namespace")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root required")
	}
	host, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if host == self {
		t.Fatal("refusing to test in host network namespace")
	}
	client, err := wgctrl.New()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	oldClient, oldQuick, oldSettings, oldPath := wgc, runWGQuick, Settings, TunnelsPath
	defer func() { wgc, runWGQuick, Settings, TunnelsPath = oldClient, oldQuick, oldSettings, oldPath }()
	wgc = client
	TunnelsPath = t.TempDir() + "/"
	Settings = &settings.Settings{MultipleTunnels: true}
	runWGQuick = func(ctx context.Context, action, name string) ([]byte, error) {
		return runTunnelCommand(ctx, "wg-quick", action, filepath.Join(TunnelsPath, name+".conf"))
	}
	for _, name := range []string{"wg-ui-one", "wg-ui-two"} {
		key, e := wgtypes.GeneratePrivateKey()
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(TunnelsPath, name+".conf"), []byte("[Interface]\nPrivateKey = "+key.String()+"\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	controller := &Tunnels{}
	defer controller.deactivateAll()
	if err = controller.ToggleTunnel(nil, "wg-ui-one"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Device("wg-ui-one"); err != nil {
		t.Fatal("activation:", err)
	}
	if err = controller.ToggleTunnel(nil, "wg-ui-one"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Device("wg-ui-one"); err == nil {
		t.Fatal("deactivation left interface active")
	}
	// An interface activated externally must be seen by the same live status query.
	if out, e := runWGQuick(context.Background(), "up", "wg-ui-one"); e != nil {
		t.Fatalf("external activation: %v %s", e, out)
	}
	ds, err := client.Devices()
	if err != nil || !contains(deviceNames(ds), "wg-ui-one") {
		t.Fatalf("external state: %v", err)
	}
	if err = controller.ToggleTunnel(nil, "wg-ui-two"); err != nil {
		t.Fatal(err)
	}
	if err = controller.deactivateAll(); err != nil {
		t.Fatal(err)
	}
	ds, err = client.Devices()
	if err != nil || len(ds) != 0 {
		t.Fatalf("deactivate all: %v %v", deviceNames(ds), err)
	}
}
