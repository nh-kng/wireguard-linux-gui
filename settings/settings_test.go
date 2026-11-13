package settings

import (
	"os"
	"testing"
)

func TestSettingsRename(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	legacy := []byte(`{"MultipleTunnels":true,"ConnectedServers":{"office":123}}`)
	if err := os.WriteFile(legacyFilePath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if !s.MultipleTunnels || s.ConnectedServers["office"] != 123 {
		t.Fatalf("legacy settings were not preserved: %+v", s)
	}
	s.MultipleTunnels = false
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err := saved.Load(); err != nil {
		t.Fatal(err)
	}
	if saved.MultipleTunnels || saved.ConnectedServers["office"] != 123 {
		t.Fatalf("new settings must take precedence: %+v", saved)
	}
	data, err := os.ReadFile(legacyFilePath)
	if err != nil || string(data) != string(legacy) {
		t.Fatalf("legacy file changed: %q, %v", data, err)
	}
}

func TestCountryFlagsPersistence(t *testing.T) {
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	// Older files without the preference keep flags disabled.
	if err := os.WriteFile(FilePath, []byte(`{"ConnectedServers":{"office":123}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.CountryFlags {
		t.Fatal("flags must default to disabled")
	}
	for _, enabled := range []bool{true, false} {
		s.CountryFlags = enabled
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		var saved Settings
		if err := saved.Load(); err != nil {
			t.Fatal(err)
		}
		if saved.CountryFlags != enabled || saved.ConnectedServers["office"] != 123 {
			t.Fatalf("flag preference or remembered connections changed: %+v", saved)
		}
	}
}

func TestSavePreservesPreviousSettingsOnFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	// A failed atomic replacement must leave the existing destination untouched
	// and remove the temporary file rather than reporting a successful save.
	if err := os.Mkdir(FilePath, 0700); err != nil {
		t.Fatal(err)
	}
	marker := FilePath + "/keep"
	if err := os.WriteFile(marker, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (&Settings{}).Save(); err == nil {
		t.Fatal("save over directory succeeded")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "previous" {
		t.Fatalf("previous destination changed: %q %v", data, err)
	}
	files, err := os.ReadDir(".")
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary file leaked: %v %v", files, err)
	}
}

func TestSavePreservesRestrictivePermissions(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(FilePath, []byte(`{"Debug":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (&Settings{Debug: true}).Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(FilePath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("settings permissions changed: %v %v", info, err)
	}
	var saved Settings
	if err := saved.Load(); err != nil || !saved.Debug {
		t.Fatalf("saved settings: %+v %v", saved, err)
	}
}
