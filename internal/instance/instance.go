// Package instance forwards repeated launches even without a shared session bus.
package instance

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Instance struct {
	lock     *os.File
	listener net.Listener
}

// Open returns the primary instance, or nil after notifying an existing one.
// dir must be inside a trusted runtime directory belonging to the current user.
func Open(dir string, activate func()) (*Instance, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "activate.sock")
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, err
		}
		// The primary may have acquired the lock just before opening its socket.
		deadline := time.Now().Add(2 * time.Second)
		for {
			conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil, nil
			}
			if time.Now().After(deadline) {
				return nil, err
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Only the lock owner may remove a socket left behind by a crash.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		lock.Close()
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		lock.Close()
		return nil, err
	}
	i := &Instance{lock: lock, listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
			activate()
		}
	}()
	return i, nil
}

func (i *Instance) Close() {
	i.listener.Close()
	i.lock.Close()
}
