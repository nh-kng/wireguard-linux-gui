package main

import (
	"os"
	"testing"
)

// These early entrypoint paths must exit before GTK or WireGuard initialization.
func TestInvalidDesktopLaunchContext(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	for _, args := range [][]string{
		{"wireguard-linux-gui", "--desktop-config-dir"},
		{"wireguard-linux-gui", "--desktop-launch", "unexpected"},
	} {
		os.Args = args
		if code := run(); code != 1 {
			t.Fatalf("invalid arguments %q returned %d", args, code)
		}
	}
}
