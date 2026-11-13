package gui

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"golang.org/x/sys/unix"
	"wireguard-linux-gui/internal/desktopbus"
)

func (t *Tunnels) importTunnels() {
	if t.closed || t.importBusy || t.operationBusy {
		return
	}
	t.importBusy = true
	t.actions["import"].SetEnabled(false)
	finish := func() {
		t.importBusy = false
		if !t.closed {
			t.actions["import"].SetEnabled(!t.operationBusy)
		}
	}
	d := gtk.NewFileChooserNative("Import WireGuard tunnels", &t.window.Window, gtk.FileChooserActionOpen, "Import", "Cancel")
	filter := gtk.NewFileFilter()
	filter.SetName("WireGuard configurations (*.conf, *.zip)")
	filter.AddPattern("*.conf")
	filter.AddPattern("*.zip")
	d.AddFilter(filter)
	d.SetSelectMultiple(true)
	d.ConnectResponse(func(response int) {
		defer d.Destroy()
		if response != int(gtk.ResponseAccept) {
			finish()
			return
		}
		files := d.Files()
		if files.NItems() > maxImportFiles {
			finish()
			ShowError(errors.New("select at most 128 files"))
			return
		}
		paths := make([]string, 0, files.NItems())
		for i := uint(0); i < files.NItems(); i++ {
			paths = append(paths, files.Item(i).Cast().(*gio.File).Path())
		}
		go func() {
			batch, err := prepareImports(paths)
			glib.IdleAdd(func() {
				if t.closed {
					return
				}
				if err != nil {
					finish()
					ShowError(err)
					return
				}
				commit := func() {
					if t.closed {
						return
					}
					go func() {
						err := commitImports(TunnelsPath, batch, true)
						glib.IdleAdd(func() {
							finish()
							if !t.closed {
								ShowError(err)
								ShowError(t.ScanTunnels())
							}
						})
					}()
				}
				hooks := []string{}
				for _, tunnel := range batch {
					if hasShellHooks(tunnel.data) {
						hooks = append(hooks, tunnel.name)
					}
				}
				if len(hooks) != 0 {
					prompt := adw.NewMessageDialog(&t.window.Window, "Trust configuration commands?", "These configurations contain shell hooks that run as root when activated or deactivated: "+strings.Join(hooks, ", ")+". Only import them if you trust their source. Review the commands in the source files before confirming.")
					prompt.AddResponse("cancel", "Cancel")
					prompt.AddResponse("import", "Trust and Import")
					prompt.SetCloseResponse("cancel")
					prompt.SetResponseAppearance("import", adw.ResponseDestructive)
					prompt.ConnectResponse(func(response string) {
						prompt.Destroy()
						if response == "import" {
							commit()
						} else {
							finish()
						}
					})
					prompt.Present()
				} else {
					commit()
				}
			})
		}()
	})
	d.Show()
}

const (
	maxConfigBytes    = 4 << 20
	maxImportBytes    = 32 << 20
	maxArchiveBytes   = 64 << 20
	maxImportFiles    = 128
	maxArchiveEntries = 256
)

type importedTunnel struct {
	name string
	data []byte
}

// Match wg-quick's case-insensitive Interface section and hook keys, including
// repeated sections/keys and comments. Do not rely on an INI parser's dialect.
func hasShellHooks(data []byte) bool {
	inInterface := false
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		key, _, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if strings.HasPrefix(key, "[") {
			inInterface = strings.EqualFold(key, "[Interface]")
		}
		if inInterface {
			switch strings.ToLower(key) {
			case "preup", "postup", "predown", "postdown":
				return true
			}
		}
	}
	return false
}

func openImportSource(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("import source must be a regular file")
	}
	return f, nil
}

func readConfig(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err == nil && len(data) > maxConfigBytes {
		err = errors.New("configuration exceeds 4 MiB")
	}
	if err == nil && bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("configuration contains a NUL byte")
	}
	return data, err
}

func prepareImports(paths []string) ([]importedTunnel, error) {
	if len(paths) > maxImportFiles {
		return nil, errors.New("import exceeds 128 files")
	}
	batch := []importedTunnel{}
	names := map[string]bool{}
	total := 0
	add := func(name string, reader io.Reader) error {
		if !strings.HasSuffix(name, ".conf") || !validTunnelName(strings.TrimSuffix(name, ".conf")) {
			return fmt.Errorf("invalid tunnel name: %s", name)
		}
		if names[name] {
			return fmt.Errorf("duplicate tunnel: %s", name)
		}
		if len(batch) >= maxImportFiles {
			return errors.New("import exceeds 128 configurations")
		}
		data, err := readConfig(reader)
		if err != nil {
			return err
		}
		total += len(data)
		if total > maxImportBytes {
			return errors.New("import exceeds 32 MiB total")
		}
		names[name] = true
		batch = append(batch, importedTunnel{name, data})
		return nil
	}
	for _, path := range paths {
		if err := func() error {
			if path == "" {
				return errors.New("select a local file")
			}
			file, err := openImportSource(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if strings.HasSuffix(path, ".zip") {
				info, err := file.Stat()
				if err != nil {
					return err
				}
				if info.Size() > maxArchiveBytes {
					return errors.New("archive exceeds 64 MiB")
				}
				archive, err := zip.NewReader(file, info.Size())
				if err != nil {
					return err
				}
				if len(archive.File) > maxArchiveEntries {
					return errors.New("archive exceeds 256 entries")
				}
				for _, entry := range archive.File {
					if entry.FileInfo().IsDir() || !strings.HasSuffix(entry.Name, ".conf") {
						continue
					}
					if entry.UncompressedSize64 > maxConfigBytes {
						return errors.New("configuration exceeds 4 MiB")
					}
					reader, err := entry.Open()
					if err != nil {
						return err
					}
					err = add(filepath.Base(entry.Name), reader)
					reader.Close()
					if err != nil {
						return err
					}
				}
				return nil
			}
			if !strings.HasSuffix(path, ".conf") {
				return errors.New("select a .conf or .zip file")
			}
			return add(filepath.Base(path), file)
		}(); err != nil {
			return nil, err
		}
	}
	return batch, nil
}

func commitImports(dir string, batch []importedTunnel, trustHooks bool) (err error) {
	for _, tunnel := range batch {
		if hasShellHooks(tunnel.data) && !trustHooks {
			return errors.New("configuration shell hooks require explicit trust")
		}
	}
	created := []string{}
	defer func() {
		if err != nil {
			for _, path := range created {
				os.Remove(path)
			}
		}
	}()
	for _, tunnel := range batch {
		if err = writeImported(dir, tunnel.name, tunnel.data); err != nil {
			return err
		}
		created = append(created, filepath.Join(dir, tunnel.name))
	}
	return nil
}

func importFile(path string) error {
	batch, err := prepareImports([]string{path})
	if err != nil {
		return err
	}
	return commitImports(TunnelsPath, batch, false)
}
func parseZipArchive(path, dir string) error {
	batch, err := prepareImports([]string{path})
	if err != nil {
		return err
	}
	return commitImports(dir, batch, false)
}
func writeImported(dir, name string, data []byte) error {
	if !strings.HasSuffix(name, ".conf") || !validTunnelName(strings.TrimSuffix(name, ".conf")) {
		return fmt.Errorf("invalid tunnel name: %s", name)
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	err = errors.Join(err, file.Close())
	if err != nil {
		os.Remove(path)
	}
	return err
}
func (t *Tunnels) exportTunnels() {
	d := gtk.NewFileChooserNative("Export WireGuard tunnels", &t.window.Window, gtk.FileChooserActionSave, "Export", "Cancel")
	d.SetCurrentName("wg_tunnels_" + time.Now().Format("2006-01-02") + ".zip")
	d.ConnectResponse(func(response int) {
		defer d.Destroy()
		if response != int(gtk.ResponseAccept) {
			return
		}
		if file := d.File(); file != nil {
			path := file.Path()
			go func() { ShowError(exportArchive(TunnelsPath, path)) }()
		}
	})
	d.Show()
}
func exportArchive(dir, path string) (err error) {
	if path == "" {
		return errors.New("select a local destination")
	}
	// Pin the parent directory and replace its directory entry, never follow
	// the destination symlink or truncate a pre-existing inode/hard link.
	parent, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	temporary := ".wireguard-export-" + rand.Text()
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(parent, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	archive := zip.NewWriter(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		source, e := os.Open(filepath.Join(dir, entry.Name()))
		if e != nil {
			return e
		}
		header := &zip.FileHeader{Name: entry.Name(), Method: zip.Deflate}
		header.SetMode(0600)
		writer, e := archive.CreateHeader(header)
		if e != nil {
			source.Close()
			return e
		}
		_, e = io.Copy(writer, source)
		e = errors.Join(e, source.Close())
		if e != nil {
			return e
		}
	}
	if err = archive.Close(); err != nil {
		return err
	}
	// The invoking desktop user must be able to read their private-key backup.
	if uid := desktopbus.UserID(); os.Geteuid() == 0 && uid > 0 {
		if err = file.Chown(uid, -1); err != nil {
			return err
		}
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return unix.Renameat(parent, temporary, parent, filepath.Base(path))
}
