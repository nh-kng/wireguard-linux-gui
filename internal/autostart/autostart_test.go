package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if handled, err := RunHelper(os.Args); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestConfigDir(t *testing.T) {
	for _, test := range []struct {
		home, xdg, want string
		bad             bool
	}{
		{"/home/test", "", "/home/test/.config", false},
		{"/home/test", "/custom/config", "/custom/config", false},
		{"/home/test", "relative", "/home/test/.config", false},
		{"", "", "", true},
	} {
		got, err := ConfigDir(test.home, test.xdg)
		if (err != nil) != test.bad || got != test.want {
			t.Fatalf("%+v: got %q, %v", test, got, err)
		}
	}
}

func TestStoreLifecycle(t *testing.T) {
	store := Store{ConfigDir: filepath.Join(t.TempDir(), "config")}
	snapshot, err := store.Read()
	if err != nil || snapshot.Exists || snapshot.Enabled {
		t.Fatalf("default: %+v, %v", snapshot, err)
	}
	for _, enabled := range []bool{true, true, false, false, true} {
		if err := store.Write(Desired(enabled)); err != nil {
			t.Fatal(err)
		}
		got, err := store.Read()
		if err != nil || !got.Exists || got.Enabled != enabled {
			t.Fatalf("state: %+v, %v", got, err)
		}
		for _, key := range []string{"Type=Application", "Name=WireGuard", "Terminal=false", "TryExec=" + Binary, "Exec=" + Binary + " --desktop-launch"} {
			if !strings.Contains(string(got.Data), key+"\n") {
				t.Fatalf("missing %s", key)
			}
		}
		if strings.Contains(string(got.Data), "Hidden=true") == enabled {
			t.Fatal("incorrect Hidden override")
		}
	}
	info, err := os.Stat(filepath.Join(store.ConfigDir, "autostart"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions: %v %v", info, err)
	}
}

func TestExternalEntryAndFailure(t *testing.T) {
	store := Store{ConfigDir: t.TempDir()}
	original := Snapshot{Exists: true, Data: []byte("[Desktop Entry]\nHidden=true\n"), Mode: 0600}
	if err := store.Write(original); err != nil {
		t.Fatal(err)
	}
	got, err := store.Read()
	if err != nil || got.Enabled {
		t.Fatalf("minimal Hidden entry: %+v %v", got, err)
	}
	path, _ := store.path()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil {
		t.Fatal("directory accepted as entry")
	}
	if err := store.Write(Desired(true)); err == nil {
		t.Fatal("rename onto directory succeeded")
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files leaked: %v %v", files, err)
	}
	if err := (Store{ConfigDir: "relative"}).Write(Desired(true)); err == nil {
		t.Fatal("relative config accepted")
	}
}

func TestCommitRollback(t *testing.T) {
	saveErr := errors.New("settings save failed")
	for _, exists := range []bool{false, true} {
		store := Store{ConfigDir: t.TempDir()}
		if exists {
			if err := store.Write(Snapshot{Exists: true, Data: []byte(entry + "Hidden=true\n# custom contents\n"), Mode: 0600}); err != nil {
				t.Fatal(err)
			}
		}
		before, err := store.Read()
		if err != nil {
			t.Fatal(err)
		}
		err = Commit(store, true, func() error {
			got, err := store.Read()
			if err != nil || !got.Enabled {
				t.Fatalf("entry not enabled before save: %v %v", got, err)
			}
			return saveErr
		})
		if !errors.Is(err, saveErr) {
			t.Fatalf("save error lost: %v", err)
		}
		after, err := store.Read()
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("rollback: before %+v after %+v err %v", before, after, err)
		}
	}
}

type failingManager struct {
	snapshot  Snapshot
	readErr   error
	writes    int
	failWrite int
}

func (m *failingManager) Read() (Snapshot, error) { return m.snapshot, m.readErr }
func (m *failingManager) Write(s Snapshot) error {
	m.writes++
	if m.writes == m.failWrite {
		return errors.New("write failed")
	}
	m.snapshot = s
	return nil
}

func TestCommitFailures(t *testing.T) {
	for _, stage := range []string{"read", "write", "rollback", "unchanged", "success"} {
		t.Run(stage, func(t *testing.T) {
			m := &failingManager{}
			called := false
			switch stage {
			case "read":
				m.readErr = errors.New("read failed")
			case "write":
				m.failWrite = 1
			case "rollback":
				m.failWrite = 2
			case "unchanged":
				m.snapshot = Desired(true)
			}
			err := Commit(m, true, func() error {
				called = true
				if stage == "rollback" {
					return errors.New("save failed")
				}
				return nil
			})
			if stage == "success" || stage == "unchanged" {
				if err != nil || !called {
					t.Fatalf("commit: %v", err)
				}
				if stage == "unchanged" && m.writes != 0 {
					t.Fatal("unchanged entry overwritten")
				}
			} else if err == nil {
				t.Fatal("failure hidden")
			}
			if (stage == "read" || stage == "write") && called {
				t.Fatal("saved after autostart failure")
			}
			if stage == "rollback" && (!strings.Contains(err.Error(), "restore login startup") || !strings.Contains(err.Error(), "save failed")) {
				t.Fatalf("rollback error lost: %v", err)
			}
		})
	}
}

func TestLaunch(t *testing.T) {
	for _, code := range []int{0, 126, 127} {
		calls := 0
		handled, got := Launch([]string{Binary, "--desktop-launch"}, 1000, "/home/test", "/custom config", func(args []string) int {
			calls++
			want := []string{"--disable-internal-agent", Binary, "--desktop-config-dir", "/custom config"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("launch args: %q", args)
			}
			return code
		})
		if !handled || got != code || calls != 1 {
			t.Fatalf("result: %v %d calls %d", handled, got, calls)
		}
	}
	for _, uid := range []int{0, 1000} {
		handled, code := Launch([]string{Binary, "--desktop-launch"}, uid, "", "", func([]string) int { t.Fatal("invalid launch invoked pkexec"); return 0 })
		if !handled || code != 1 {
			t.Fatal("invalid launch accepted")
		}
	}
	handled, _ := Launch([]string{Binary}, 1000, "", "", nil)
	if handled {
		t.Fatal("normal startup intercepted")
	}
}

func TestLaunchContext(t *testing.T) {
	dir, args, err := Context([]string{Binary, "--desktop-config-dir", "/custom config", "--other"})
	if err != nil || dir != "/custom config" || !reflect.DeepEqual(args, []string{Binary, "--other"}) {
		t.Fatalf("context: %q %q %v", dir, args, err)
	}
	for _, args := range [][]string{{Binary, "--desktop-config-dir"}, {Binary, "--desktop-config-dir", ""}, {Binary, "--desktop-config-dir", "/one", "--desktop-config-dir", "/two"}} {
		if _, _, err := Context(args); err == nil {
			t.Fatalf("invalid context accepted: %q", args)
		}
	}
	if _, err := NewClient(0, ""); err == nil {
		t.Fatal("missing desktop identity accepted")
	}
}

func TestClientSubprocess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root desktop test user")
	}
	client, err := NewClient(os.Getuid(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(Desired(true)); err != nil {
		t.Fatal(err)
	}
	got, err := client.Read()
	if err != nil || !got.Enabled {
		t.Fatalf("helper response: %+v %v", got, err)
	}
	if err := client.Write(Snapshot{}); err != nil {
		t.Fatal(err)
	}
}

func TestSystemEntryOverrideAndRollback(t *testing.T) {
	system := Store{ConfigDir: t.TempDir()}
	if err := system.Write(Desired(true)); err != nil {
		t.Fatal(err)
	}
	local := Store{ConfigDir: t.TempDir(), SystemDirs: []string{system.ConfigDir}}
	got, err := local.Read()
	if err != nil || !got.Enabled || got.Exists {
		t.Fatalf("inherited entry: %+v %v", got, err)
	}
	saveErr := errors.New("settings save failed")
	if err := Commit(local, false, func() error { return saveErr }); !errors.Is(err, saveErr) {
		t.Fatal(err)
	}
	got, err = local.Read()
	if err != nil || !got.Enabled || got.Exists {
		t.Fatalf("inherited entry not restored: %+v %v", got, err)
	}
	if err := Commit(local, false, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	got, err = local.Read()
	if err != nil || got.Enabled || !got.Exists {
		t.Fatalf("system entry not suppressed: %+v %v", got, err)
	}
	inherited, err := system.Read()
	if err != nil || !inherited.Enabled {
		t.Fatalf("system entry modified: %+v %v", inherited, err)
	}
}

func TestDefaultEnabledAndSavedChoicePreserved(t *testing.T) {
	store := Store{ConfigDir: t.TempDir(), SystemDirs: []string{}}
	if err := EnsureDefault(store); err != nil {
		t.Fatal(err)
	}
	first, err := store.Read()
	if err != nil || !first.Exists || !first.Enabled {
		t.Fatalf("first launch: %+v %v", first, err)
	}
	if err := EnsureDefault(store); err != nil {
		t.Fatal(err)
	}
	again, err := store.Read()
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("enabled entry changed: %+v %v", again, err)
	}
	if err := store.Write(Desired(false)); err != nil {
		t.Fatal(err)
	}
	disabled, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefault(store); err != nil {
		t.Fatal(err)
	}
	again, err = store.Read()
	if err != nil || !reflect.DeepEqual(disabled, again) {
		t.Fatalf("saved off choice changed: %+v %v", again, err)
	}
}

func TestDefaultPreservesSystemEntry(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		system := Store{ConfigDir: t.TempDir()}
		if err := system.Write(Desired(enabled)); err != nil {
			t.Fatal(err)
		}
		local := Store{ConfigDir: t.TempDir(), SystemDirs: []string{system.ConfigDir}}
		if err := EnsureDefault(local); err != nil {
			t.Fatal(err)
		}
		got, err := local.Read()
		if err != nil || got.Exists || !got.Inherited || got.Enabled != enabled {
			t.Fatalf("system choice changed: %+v %v", got, err)
		}
	}
}

func TestDefaultFailures(t *testing.T) {
	readErr := errors.New("configuration unreadable")
	m := &failingManager{readErr: readErr}
	if err := EnsureDefault(m); !errors.Is(err, readErr) || m.writes != 0 {
		t.Fatalf("read failure: %v, writes %d", err, m.writes)
	}
	m = &failingManager{failWrite: 1}
	if err := EnsureDefault(m); err == nil {
		t.Fatal("write failure hidden")
	}
}
