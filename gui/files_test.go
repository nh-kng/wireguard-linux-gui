package gui

import (
	"archive/zip"
	"bytes"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveRoundTrip(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	config := []byte("[Interface]\nPrivateKey = private-test-key\n")
	if err := writeImported(source, "test.conf", config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ignore.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if err := exportArchive(source, archive); err != nil {
		t.Fatal(err)
	}
	if err := parseZipArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "test.conf"))
	if err != nil || string(got) != string(config) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dest, "test.conf"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions: %v %v", info, err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 {
		t.Fatalf("unexpected files: %v", entries)
	}
	if err := parseZipArchive(archive, dest); err == nil {
		t.Fatal("duplicate import overwrote existing config")
	}
}
func TestInvalidImportNames(t *testing.T) {
	for _, name := range []string{"../outside.conf", "..conf", "too-long-interface-name.conf", ".conf", "bad name.conf"} {
		if err := writeImported(t.TempDir(), name, []byte("secret")); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
func TestZipPathsStayInsideDestination(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "nested.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, _ := w.Create("../../safe.conf")
	entry.Write([]byte("test"))
	w.Close()
	f.Close()
	dir := t.TempDir()
	if err := parseZipArchive(archive, dir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "safe.conf")); err != nil || string(data) != "test" {
		t.Fatalf("flattened zip: %s %v", data, err)
	}
}

func TestExportPreservesLinkTargets(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(fmt.Sprintf("hard=%t", hard), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			destination := filepath.Join(dir, "backup.zip")
			if err := os.WriteFile(target, []byte("keep me"), 0644); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if hard {
				link = os.Link
			}
			if err := link(target, destination); err != nil {
				t.Fatal(err)
			}
			if err := exportArchive(t.TempDir(), destination); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "keep me" {
				t.Fatalf("target changed: %q %v", data, err)
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatalf("target permissions changed: %v %v", info, err)
			}
			archive, err := zip.OpenReader(destination)
			if err != nil {
				t.Fatal(err)
			}
			archive.Close()
			info, err = os.Stat(destination)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("archive permissions: %v %v", info, err)
			}
		})
	}
}

func TestFailedExportPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "backup.zip")
	if err := os.WriteFile(destination, []byte("previous backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exportArchive(filepath.Join(dir, "missing"), destination); err == nil {
		t.Fatal("missing source accepted")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "previous backup" {
		t.Fatalf("backup changed: %q %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %v %v", entries, err)
	}
}

func TestImportedHooksRequireTrust(t *testing.T) {
	for _, hook := range []string{"PreUp", "postup", "PREDOWN", "PostDown"} {
		t.Run(hook, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "test.conf")
			data := []byte("[Peer]\nEndpoint=example.org:1234\n[INTERFACE] # comment\n\t" + hook + " = echo test # comment\n")
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			batch, err := prepareImports([]string{source})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := commitImports(dir, batch, false); err == nil {
				t.Fatal("hooks accepted without trust")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("untrusted import wrote files")
			}
			// Approval must apply to the inspected bytes, even if the original
			// file changes while the confirmation dialog is open.
			if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := commitImports(dir, batch, true); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "test.conf"))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("imported different bytes: %q %v", got, err)
			}
		})
	}
	if hasShellHooks([]byte("[Interface]\n# PostUp=ignored\n[Peer]\nPostUp=not-an-interface-hook\n")) {
		t.Fatal("comments or Peer keys treated as hooks")
	}
}

func TestImportRejectsUnsafeSources(t *testing.T) {
	for _, kind := range []string{"oversized", "nul", "fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.conf")
			switch kind {
			case "oversized":
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(maxConfigBytes + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "nul":
				if err := os.WriteFile(path, []byte("[Interface]\nPre\x00Up=echo test\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("/dev/zero", path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := prepareImports([]string{path}); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}

func testImportArchive(t *testing.T, count, size int, suffix string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(f)
	data := bytes.Repeat([]byte("x"), size)
	for i := 0; i < count; i++ {
		entry, err := archive.Create(fmt.Sprintf("t%d%s", i, suffix))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportArchiveBudgets(t *testing.T) {
	for _, test := range []struct {
		name         string
		count, size  int
		suffix, want string
	}{
		{"configuration count", maxImportFiles + 1, 1, ".conf", "128 configurations"},
		{"all entries", maxArchiveEntries + 1, 0, ".txt", "256 entries"},
		{"expanded total", maxImportBytes/maxConfigBytes + 1, maxConfigBytes, ".conf", "32 MiB"},
		{"single configuration", 1, maxConfigBytes + 1, ".conf", "4 MiB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := testImportArchive(t, test.count, test.size, test.suffix)
			dir := t.TempDir()
			if err := parseZipArchive(path, dir); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s limit, got %v", test.want, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("over-budget archive left imported files")
			}
		})
	}
}

func TestImportRollbackPreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.conf"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	batch := []importedTunnel{{"new.conf", []byte("new")}, {"existing.conf", []byte("replacement")}}
	if err := commitImports(dir, batch, false); err == nil {
		t.Fatal("duplicate destination accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.conf")); !os.IsNotExist(err) {
		t.Fatalf("partial import remains: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "existing.conf"))
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing config changed: %q %v", data, err)
	}
}

func TestImportSelectionAndArchiveSizeLimits(t *testing.T) {
	if _, err := prepareImports(make([]string, maxImportFiles+1)); err == nil {
		t.Fatal("over-limit selection accepted")
	}
	path := filepath.Join(t.TempDir(), "large.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxArchiveBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := prepareImports([]string{path}); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("expected archive size limit, got %v", err)
	}
}

func TestImportAggregateLimitAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	paths := []string{}
	data := bytes.Repeat([]byte("x"), maxConfigBytes)
	for i := 0; i <= maxImportBytes/maxConfigBytes; i++ {
		path := filepath.Join(dir, fmt.Sprintf("t%d.conf", i))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if _, err := prepareImports(paths); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("expected selection-wide limit, got %v", err)
	}
}
