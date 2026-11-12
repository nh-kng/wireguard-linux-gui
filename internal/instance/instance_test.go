package instance

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSecondProcessActivatesPrimary(t *testing.T) {
	if dir := os.Getenv("WG_TEST_INSTANCE_DIR"); dir != "" {
		i, err := Open(dir, func() {})
		if err != nil {
			t.Fatal(err)
		}
		if i != nil {
			i.Close()
			t.Fatal("second process became primary")
		}
		return
	}
	dir := t.TempDir()
	activated := make(chan struct{}, 1)
	primary, err := Open(dir, func() { activated <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecondProcessActivatesPrimary$")
	cmd.Env = append(os.Environ(), "WG_TEST_INSTANCE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("secondary: %v\n%s", err, out)
	}
	select {
	case <-activated:
	case <-time.After(3 * time.Second):
		t.Fatal("primary was not activated")
	}
}

func TestRestartRecoversStaleSocket(t *testing.T) {
	dir := t.TempDir()
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "activate.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	for n := 0; n < 2; n++ {
		i, err := Open(dir, func() {})
		if err != nil {
			t.Fatal(err)
		}
		if i == nil {
			t.Fatal("expected primary after shutdown")
		}
		i.Close()
	}
}

func TestConcurrentLaunchesHaveOnePrimary(t *testing.T) {
	dir := t.TempDir()
	const count = 8
	var wg sync.WaitGroup
	results := make(chan *Instance, count)
	errors := make(chan error, count)
	for n := 0; n < count; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i, err := Open(dir, func() {})
			results <- i
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	primaries := 0
	for i := range results {
		if i != nil {
			primaries++
			i.Close()
		}
	}
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	if primaries != 1 {
		t.Fatalf("got %d primaries, want 1", primaries)
	}
}
