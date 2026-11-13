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
