package gui

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func (t *Tunnels) importTunnels() {
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
			return
		}
		files := d.Files()
		for i := uint(0); i < files.NItems(); i++ {
			file := files.Item(i).Cast().(*gio.File)
			if err := importFile(file.Path()); err != nil {
				ShowError(err)
				ShowError(t.ScanTunnels())
				return
			}
		}
		ShowError(t.ScanTunnels())
	})
	d.Show()
}
func importFile(path string) error {
	if path == "" {
		return errors.New("select a local file")
	}
	if strings.HasSuffix(path, ".zip") {
		return parseZipArchive(path, TunnelsPath)
	}
	if !strings.HasSuffix(path, ".conf") {
		return errors.New("select a .conf or .zip file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return writeImported(TunnelsPath, filepath.Base(path), data)
}
func writeImported(dir, name string, data []byte) error {
	if !validTunnelName(strings.TrimSuffix(name, ".conf")) {
		return fmt.Errorf("invalid tunnel name: %s", name)
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	return file.Close()
}
func parseZipArchive(path, dir string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !strings.HasSuffix(file.Name, ".conf") {
			continue
		}
		reader, e := file.Open()
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(reader, 4<<20+1))
		reader.Close()
		if e != nil {
			return e
		}
		if len(data) > 4<<20 {
			return errors.New("configuration exceeds 4 MiB")
		}
		if e = writeImported(dir, filepath.Base(file.Name), data); e != nil {
			return e
		}
	}
	return nil
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
			ShowError(exportArchive(TunnelsPath, file.Path()))
		}
	})
	d.Show()
}
func exportArchive(dir, path string) (err error) {
	if path == "" {
		return errors.New("select a local destination")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	defer func() {
		if e := file.Close(); err == nil {
			err = e
		}
	}()
	archive := zip.NewWriter(file)
	defer func() {
		if e := archive.Close(); err == nil {
			err = e
		}
	}()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			return e
		}
		header := &zip.FileHeader{Name: entry.Name(), Method: zip.Deflate}
		header.SetMode(0600)
		writer, e := archive.CreateHeader(header)
		if e != nil {
			return e
		}
		if _, e = writer.Write(data); e != nil {
			return e
		}
	}
	return nil
}
