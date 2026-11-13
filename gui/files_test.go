package gui

import (
	"archive/zip"
	"os"
	"path/filepath"
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
