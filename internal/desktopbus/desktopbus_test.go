package desktopbus

import (
	"bufio"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestMain(m *testing.M) {
	if handled, err := RunHelper(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestHelperDescriptorAuthenticatesOnPrivateBus(t *testing.T) {
	binary, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon unavailable")
	}
	daemon := exec.Command(binary, "--session", "--nofork", "--print-address=1")
	out, err := daemon.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { daemon.Process.Kill(); daemon.Wait() }()
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("missing bus address")
	}
	path := strings.Split(strings.TrimPrefix(scanner.Text(), "unix:path="), ",")[0]
	socket, err := openAsUser(path, uint32(os.Getuid()), uint32(os.Getgid()))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dbus.NewConn(&initialByteSent{Conn: socket})
	if err != nil {
		socket.Close()
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}); err != nil {
		t.Fatal(err)
	}
	if err = conn.Hello(); err != nil {
		t.Fatal(err)
	}
	var uid uint32
	if err = conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixUser", 0, conn.Names()[0]).Store(&uid); err != nil {
		t.Fatal(err)
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("bus saw UID %d; expected %d", uid, os.Getuid())
	}
}
func TestHelperFailure(t *testing.T) {
	if conn, err := openAsUser("/nonexistent/wireguard-gui-bus", uint32(os.Getuid()), uint32(os.Getgid())); err == nil {
		conn.Close()
		t.Fatal("missing socket accepted")
	}
}
func TestHelperArgumentValidation(t *testing.T) {
	if handled, err := RunHelper([]string{"gui"}); handled || err != nil {
		t.Fatal("normal startup treated as helper")
	}
	if handled, err := RunHelper([]string{"gui", helperArgument}); !handled || err == nil {
		t.Fatal("invalid helper arguments accepted")
	}
}
func TestInvokingUserAddress(t *testing.T) {
	// Root uses the explicitly supplied invoking user, while ordinary users retain their UID.
	t.Setenv("PKEXEC_UID", "12345")
	expected := os.Getuid()
	if expected == 0 {
		expected = 12345
	}
	if UserID() != expected || Address() != fmt.Sprintf("unix:path=/run/user/%d/bus", expected) {
		t.Fatalf("unexpected session identity: %d %s", UserID(), Address())
	}
}

func TestHelperSendsCredentialByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bus")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	raw, err := listener.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var socketErr error
	if err = raw.Control(func(fd uintptr) { socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1) }); err != nil {
		t.Fatal(err)
	}
	if socketErr != nil {
		t.Fatal(socketErr)
	}
	conn, err := openAsUser(path, uint32(os.Getuid()), uint32(os.Getgid()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	data, oob := make([]byte, 1), make([]byte, unix.CmsgSpace(unix.SizeofUcred))
	n, oobn, _, _, err := peer.ReadMsgUnix(data, oob)
	if err != nil || n != 1 || data[0] != 0 {
		t.Fatalf("credential byte: %d %v", n, err)
	}
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		creds, e := unix.ParseUnixCredentials(&message)
		if e != nil {
			continue
		}
		if creds.Uid != uint32(os.Getuid()) || creds.Pid == int32(os.Getpid()) {
			t.Fatalf("credentials were not sent by the helper: %+v", creds)
		}
		if err := syscall.Kill(int(creds.Pid), 0); err != nil {
			t.Fatalf("desktop services cannot inspect the live helper: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(int(creds.Pid), 0); err != syscall.ESRCH {
			t.Fatalf("helper was not reaped when its connection closed: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("repeated close: %v", err)
		}
		return
	}
	t.Fatal("credential byte had no Unix credentials")
}
