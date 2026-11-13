package gui

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"golang.zx2c4.com/wireguard/wgctrl"
)

// Called only on the GTK thread. A single in-flight operation keeps tunnel
// changes serialized; the worker owns its client and settings snapshot.
func (t *Tunnels) startTunnelOperation(kind, name string) {
	if t.closed || t.operationBusy {
		return
	}
	snapshot := *Settings
	snapshot.ConnectedServers = maps.Clone(Settings.ConnectedServers)
	ctx, cancel := context.WithCancel(context.Background())
	t.operationCancel = cancel
	done := make(chan struct{})
	t.operationDone = done
	t.operationBusy = true
	t.syncOperationActions()
	t.updateTray()
	reconnected, dir := t.reconnectedRememberedServers, TunnelsPath
	go func() {
		defer cancel()
		defer close(done)
		backend := tunnelBackend{settings: &snapshot, ctx: ctx, reconnected: reconnected}
		client, err := wgctrl.New()
		if err == nil {
			backend.client = client
			switch kind {
			case "toggle":
				err = backend.toggle(name)
			case "deactivate":
				err = backend.deactivateAll()
			case "restore":
				err = backend.restore(dir)
			}
			client.Close()
		}
		glib.IdleAdd(func() {
			if t.closed {
				return
			}
			t.operationBusy = false
			t.operationCancel = nil
			if kind == "restore" {
				t.reconnectedRememberedServers = true
			}
			// Preferences and settings are read/written only on this thread.
			if backend.changed && Settings.RememberConnectedServers {
				Settings.ConnectedServers = snapshot.ConnectedServers
				err = errors.Join(err, Settings.Save())
			}
			ShowError(err)
			t.syncOperationActions()
			t.updateTray()
			t.RefreshStatus()
		})
	}()
}

func (t *Tunnels) syncOperationActions() {
	selected := t.list.SelectedRow() != nil
	t.actions["import"].SetEnabled(!t.operationBusy && !t.importBusy)
	for _, id := range []string{"add", "editor-save"} {
		t.actions[id].SetEnabled(!t.operationBusy)
	}
	for _, id := range []string{"edit", "remove"} {
		t.actions[id].SetEnabled(selected && !t.operationBusy)
	}
	t.actions["toggle"].SetEnabled(selected && t.statusAvailable && !t.operationBusy)
	t.actions["preferences-save"].SetEnabled(!t.operationBusy && t.preferencesDirty())
	t.renderStatus()
}

func (b *tunnelBackend) restore(dir string) error {
	if !b.settings.RememberConnectedServers {
		return nil
	}
	ds, err := b.client.Devices()
	if err != nil {
		return err
	}
	active := deviceNames(ds)
	if !b.settings.MultipleTunnels && len(active) > 0 {
		return nil
	}
	names := []string{}
	for name := range b.settings.ConnectedServers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, c := names[i], names[j]
		if b.settings.ConnectedServers[a] == b.settings.ConnectedServers[c] {
			return a < c
		}
		return b.settings.ConnectedServers[a] > b.settings.ConnectedServers[c]
	})
	var result error
	for _, name := range names {
		if err := b.ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		if !validTunnelName(name) || contains(active, name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name+".conf")); err != nil {
			continue
		}
		result = errors.Join(result, b.toggle(name))
		if !b.settings.MultipleTunnels {
			break
		}
	}
	return result
}
