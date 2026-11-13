// Package desktopbus connects an elevated app to its invoking user's session bus.
package desktopbus

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const helperArgument = "--desktop-bus-socket"

func UserID() int {
	uid := os.Getuid()
	if uid == 0 {
		s := os.Getenv("PKEXEC_UID")
		if s == "" {
			s = os.Getenv("SUDO_UID")
		}
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			uid = n
		}
	}
	return uid
}
func Address() string {
	uid := UserID()
	if uid > 0 {
		return fmt.Sprintf("unix:path=/run/user/%d/bus", uid)
	}
	return os.Getenv("DBUS_SESSION_BUS_ADDRESS")
}

// RunHelper handles the private socket-opening mode before application startup.
// The child has already dropped to the desktop UID. It never accesses WireGuard.
func RunHelper(args []string) (handled bool, err error) {
	if len(args) < 2 || args[1] != helperArgument {
		return false, nil
	}
	if len(args) != 3 {
		return true, fmt.Errorf("invalid desktop bus helper arguments")
	}
	controlFile := os.NewFile(3, "desktop-bus-control")
	if controlFile == nil {
		return true, fmt.Errorf("missing helper control socket")
	}
	defer controlFile.Close()
	control, err := net.FileConn(controlFile)
	if err != nil {
		return true, err
	}
	defer control.Close()
	socket, err := net.DialTimeout("unix", args[2], 3*time.Second)
	if err != nil {
		return true, err
	}
	defer socket.Close()
	// D-Bus receives the sender's Unix credentials with this initial byte.
	// Send it before handing the socket to the elevated parent.
	if _, err = socket.Write([]byte{0}); err != nil {
		return true, err
	}
	file, err := socket.(*net.UnixConn).File()
	if err != nil {
		return true, err
	}
	defer file.Close()
	_, _, err = control.(*net.UnixConn).WriteMsgUnix([]byte{1}, unix.UnixRights(int(file.Fd())), nil)
	return true, err
}

func Connect(address string) (*dbus.Conn, error) {
	if address == "" {
		return nil, fmt.Errorf("desktop session bus unavailable")
	}
	uid := UserID()
	if os.Geteuid() != 0 || uid == 0 {
		return dbus.Connect(address)
	}
	// A session daemon rejects root's socket credentials. A short-lived child opens
	// the socket as the invoking user and passes the descriptor back to this process.
	// No credentials are changed on the GTK thread or any other parent thread.
	expected := fmt.Sprintf("unix:path=/run/user/%d/bus", uid)
	if address != expected {
		return nil, fmt.Errorf("unexpected desktop bus address")
	}
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return nil, err
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return nil, err
	}
	conn, err := openAsUser(strings.TrimPrefix(address, "unix:path="), uint32(uid), uint32(gid))
	if err != nil {
		return nil, err
	}
	bus, err := dbus.NewConn(&initialByteSent{Conn: conn})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err = bus.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(uid))}); err != nil {
		bus.Close()
		return nil, err
	}
	if err = bus.Hello(); err != nil {
		bus.Close()
		return nil, err
	}
	return bus, nil
}

func openAsUser(path string, uid, gid uint32) (net.Conn, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parentFile := os.NewFile(uintptr(fds[0]), "desktop-bus-parent")
	defer parentFile.Close()
	childFile := os.NewFile(uintptr(fds[1]), "desktop-bus-child")
	defer childFile.Close()
	parent, err := net.FileConn(parentFile)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, helperArgument, path)
	cmd.ExtraFiles = []*os.File{childFile}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, NoSetGroups: os.Geteuid() != 0}}
	// Helper startup needs no desktop environment, session bus, or Go settings.
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	// Only the child should retain the write end, so startup failures produce EOF.
	childFile.Close()
	parent.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, oob := make([]byte, 1), make([]byte, unix.CmsgSpace(4))
	n, oobn, _, _, readErr := parent.(*net.UnixConn).ReadMsgUnix(data, oob)
	if readErr != nil {
		cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("desktop bus helper: %w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("desktop bus helper: %w: %s", waitErr, stderr.String())
	}
	if n != 1 {
		return nil, fmt.Errorf("desktop bus helper returned no socket")
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		rights, e := unix.ParseUnixRights(&message)
		if e != nil {
			return nil, e
		}
		if len(rights) != 1 {
			for _, fd := range rights {
				unix.Close(fd)
			}
			return nil, fmt.Errorf("invalid helper socket descriptors")
		}
		file := os.NewFile(uintptr(rights[0]), "desktop-session-bus")
		socket, e := net.FileConn(file)
		file.Close()
		return socket, e
	}
	return nil, fmt.Errorf("desktop bus helper returned no descriptor")
}

// The helper already sent the credential-bearing NUL byte. Consume the
// authentication library's duplicate locally, then forward all subsequent data.
type initialByteSent struct {
	net.Conn
	skipped bool
}

func (c *initialByteSent) Write(data []byte) (int, error) {
	if !c.skipped {
		if len(data) != 1 || data[0] != 0 {
			return 0, fmt.Errorf("unexpected initial D-Bus authentication byte")
		}
		c.skipped = true
		return 1, nil
	}
	return c.Conn.Write(data)
}
