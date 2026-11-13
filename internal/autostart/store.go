// Package autostart manages the invoking desktop user's XDG login entry.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/ini.v1"
)

const Binary = "/usr/bin/wireguard-linux-gui"
const Filename = "wireguard-linux-gui.desktop"

const entry = "[Desktop Entry]\nType=Application\nName=WireGuard\nTerminal=false\nTryExec=" + Binary + "\nExec=" + Binary + " --desktop-launch\n"

// ConfigDir ignores relative XDG paths, as required by the base directory spec.
func ConfigDir(home, xdg string) (string, error) {
	if filepath.IsAbs(xdg) {
		return filepath.Clean(xdg), nil
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("desktop user's home directory is unavailable")
	}
	return filepath.Join(home, ".config"), nil
}

// Snapshot retains exact contents and permissions for preference-save rollback.
type Snapshot struct {
	Exists bool
	// Inherited marks a system entry when no per-user override exists.
	Inherited bool
	Data      []byte
	Mode      os.FileMode
	Enabled   bool
}

type Store struct {
	ConfigDir string
	// Nil uses the standard system directory. Tests may supply isolated entries.
	SystemDirs []string
}

func (s Store) path() (string, error) {
	if !filepath.IsAbs(s.ConfigDir) {
		return "", errors.New("configuration directory must be absolute")
	}
	return filepath.Join(s.ConfigDir, "autostart", Filename), nil
}

func (s Store) Read() (Snapshot, error) {
	path, err := s.path()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := readEntry(path)
	if err != nil || snapshot.Exists {
		return snapshot, err
	}
	dirs := s.SystemDirs
	if dirs == nil {
		dirs = []string{"/etc/xdg"}
	}
	for _, dir := range dirs {
		inherited, err := readEntry(filepath.Join(dir, "autostart", Filename))
		if err != nil {
			return Snapshot{}, err
		}
		if inherited.Exists {
			// Rollback removes our override instead of copying a system entry into
			// the user's configuration directory.
			return Snapshot{Inherited: true, Enabled: inherited.Enabled}, nil
		}
	}
	return Snapshot{}, nil
}

func readEntry(path string) (Snapshot, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, errors.New("autostart entry is not a regular file")
	}
	if info.Size() > 1<<20 {
		return Snapshot{}, errors.New("autostart entry exceeds 1 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	file, err := ini.Load(data)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read autostart entry: %w", err)
	}
	section := file.Section("Desktop Entry")
	hasHidden := section.HasKey("Hidden")
	hidden, err := section.Key("Hidden").Bool()
	if hasHidden && err != nil {
		return Snapshot{}, fmt.Errorf("invalid Hidden value: %w", err)
	}
	enabled := !hidden && section.Key("Type").String() == "Application" && section.Key("Exec").String() != ""
	return Snapshot{Exists: true, Data: data, Mode: info.Mode().Perm(), Enabled: enabled}, nil
}

func Desired(enabled bool) Snapshot {
	data := entry
	if !enabled {
		data += "Hidden=true\n"
	}
	return Snapshot{Exists: true, Data: []byte(data), Mode: 0644, Enabled: enabled}
}

func (s Store) Write(snapshot Snapshot) error {
	path, err := s.path()
	if err != nil {
		return err
	}
	if !snapshot.Exists {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".wireguard-autostart-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(snapshot.Mode.Perm()); err == nil {
		_, err = file.Write(snapshot.Data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

type Manager interface {
	Read() (Snapshot, error)
	Write(Snapshot) error
}

// EnsureDefault enables login startup on first launch. An existing user or
// system entry is an explicit configuration and must never be overwritten.
func EnsureDefault(manager Manager) error {
	snapshot, err := manager.Read()
	if err != nil {
		return err
	}
	if snapshot.Exists || snapshot.Inherited {
		return nil
	}
	return manager.Write(Desired(true))
}

// Commit restores the entry if saving other preferences fails.
// It only writes when the enabled state changes, preserving external edits.
func Commit(manager Manager, enabled bool, save func() error) error {
	before, err := manager.Read()
	if err != nil {
		return err
	}
	if before.Enabled == enabled {
		return save()
	}
	if err = manager.Write(Desired(enabled)); err != nil {
		return err
	}
	if err = save(); err != nil {
		if rollback := manager.Write(before); rollback != nil {
			return errors.Join(err, fmt.Errorf("restore login startup: %w", rollback))
		}
		return err
	}
	return nil
}
