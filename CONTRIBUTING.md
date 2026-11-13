# Contributing to wireguard-linux-gui

Bug reports, fixes, interface improvements, and documentation updates are welcome.
See the [README](README.md) for the project overview and usage.

## Report bugs and request features

Search existing issues before opening a new one. For a bug report, include:

- Your Linux distribution, desktop environment, and application version or commit.
- Steps to reproduce, expected behavior, and actual behavior.
- Relevant logs or screenshots. For build failures, include the command, error
  output, and `go version`.

Remove private keys, preshared keys, and other secrets from configurations and
logs before sharing them. For a feature request, describe the use case and the
behavior you would like to see.

## Development setup

Work from a local checkout on Linux. Install Go and any missing native build and
runtime dependencies on Debian/Ubuntu. This is a one-time setup; skip it when
the dependencies are already installed:

```sh
sudo apt update
sudo apt install build-essential pkg-config wireguard-tools libgtk-4-dev libadwaita-1-dev libgirepository1.0-dev resolvconf
```

Install Go 1.24 or newer separately; Ubuntu 24.04's default `golang-go` package
is too old. The requirement is declared in [go.mod](go.mod).
GoReleaser may require a newer Go toolchain when installed from source.

GTK4 and Libadwaita provide the native interface. GObject introspection is
required by the Go bindings. The tray uses D-Bus directly and needs no GTK3
or AppIndicator library. An elevated launch opens its desktop-bus sockets through
a short-lived child running as the invoking user; WireGuard operations remain
in the privileged process. Builds use CGO and require Go 1.24 or newer.

## Build and run

Run from the repository root:

```sh
go build -ldflags "-X wireguard-linux-gui/gui.BuildDate=$(date -u +%Y-%m-%d)" -o wireguard-linux-gui .
sudo ./wireguard-linux-gui
```

The GTK4 Builder XML and icons are embedded by `go build`; no generation step
is needed. Edit `gui/window.ui` for layout changes.

The linker flag stamps the UTC build date shown below the About dialog's version.
Without this flag, the date is shown as `unknown`. Release packages stamp it automatically.

Root access is needed to read and manage tunnel configurations. Installed desktop
launchers use `pkexec`.

## Install locally for desktop testing

First choose **Quit** from the tray menu of every running copy. Closing the
window only hides it, so reopening would otherwise activate the old process.
For a quick test without installing, use `sudo ./wireguard-linux-gui` after
building as described above.

To test launching the built app from Applications, run these commands from the
repository root. They replace the installed binary and desktop integration:

```sh
sudo install -Dm755 ./wireguard-linux-gui /usr/bin/wireguard-linux-gui
sudo install -Dm644 deb/usr/share/applications/wireguard-linux-gui.desktop /usr/share/applications/wireguard-linux-gui.desktop
sudo install -Dm644 deb/usr/share/polkit-1/actions/wireguard-linux-gui.policy /usr/share/polkit-1/actions/wireguard-linux-gui.policy
sudo mkdir -p /usr/share/wireguard-linux-gui
sudo cp -a Icon /usr/share/wireguard-linux-gui/
```

Launch **WireGuard** from Applications, then check:

- Closing the window and launching again shows the existing window with one
  tray icon. The tray's **Manage Tunnels** action also reopens it.
- Activating or deactivating a profile updates its status and tray checkmark
  immediately.
- Changes made outside the app, for example with `wg-quick`, appear within
  approximately one second while the window is open or five seconds while hidden.
- Reopening the window requests fresh status immediately.
- Closing the window or choosing **Quit** leaves active tunnels running.

### Verify login startup manually

These checks require an explicitly requested GUI test and an installed copy with
the updated desktop launcher; unit tests do not launch a desktop session.

- With no existing login entry, launch the app and confirm **Preferences →
  General → Start at login** is enabled and the user entry exists. Disable it and
  save, then restart the app; it must stay disabled. Existing system entries,
  including disabled ones, must be preserved.
- Open Preferences, toggle **Start at login**, then Cancel or Discard. Confirm
  the login entry is unchanged. Save the toggle and reopen Preferences to confirm
  it reflects the entry on disk, including edits made outside the app.
- Sign out and back in with startup enabled. Accept administrator authentication
  and confirm one app instance appears. Repeat while canceling authentication;
  the app should not start or retry, and the preference should stay enabled.
- Check both settings of **Start in the system tray**. Without a tray host, the
  window must remain accessible. Reopening from Applications should reuse the
  existing instance. Remembered tunnels should follow their existing preference.
- Disable login startup and save, then sign out and back in; the app should stay
  closed. Confirm the entry contains `Hidden=true`.
- Launch from a desktop session with an absolute custom `XDG_CONFIG_HOME` and
  confirm the entry appears there, owned by the desktop user. A direct `sudo`
  launch should use the invoking user's default config directory. A root-only
  launch should show the login preference disabled with an explanation.
- Simulate an unwritable configuration directory or settings destination in an
  isolated test setup. Saving should show an error and leave Preferences open;
  a later settings-save failure must restore the previous login entry.

### Rebuild and reinstall after changes

Choose **Quit** from the running app's tray menu, then run from the repository
root:

```sh
go build -ldflags "-X wireguard-linux-gui/gui.BuildDate=$(date -u +%Y-%m-%d)" -o wireguard-linux-gui . &&
sudo install -Dm755 ./wireguard-linux-gui /usr/bin/wireguard-linux-gui &&
sudo install -Dm644 deb/usr/share/applications/wireguard-linux-gui.desktop /usr/share/applications/wireguard-linux-gui.desktop
```

The `&&` operators ensure installation only continues if the preceding step
succeeds. Reopen **WireGuard** from Applications afterward.

The commands also refresh the desktop launcher's display name. Repeat the
PolicyKit policy or icon installation commands above when those files change.
You do not need to rerun `apt install` for dependencies
that are already installed or rebuild a `.deb` package for this local workflow.

For package testing, build the packages below and install the chosen `.deb` with
`sudo apt install /absolute/path/to/package.deb` instead of copying files manually.

### Remove a local development installation

Choose **Quit** from the app's tray menu first; closing the window only hides it.
If you installed using the manual commands above, remove the installed files:

```sh
sudo rm -f /usr/bin/wireguard-linux-gui
sudo rm -f /usr/share/applications/wireguard-linux-gui.desktop
sudo rm -f /usr/share/polkit-1/actions/wireguard-linux-gui.policy
sudo rm -rf /usr/share/wireguard-linux-gui
```

If you installed a `.deb` package instead, use `sudo apt remove wireguard-linux-gui`
in place of those commands.

For either installation method, remove the login startup entry as your regular
desktop user, without `sudo`:

```sh
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/autostart/wireguard-linux-gui.desktop"
```

This removes only the GUI app and its desktop integration. WireGuard tools,
tunnel configurations, and active VPN connections remain intact. The repository
and its locally built binary also remain.

## Check your changes

Format changed Go files with `gofmt`, then run the tests:

```sh
go test ./...
go build -o wireguard-linux-gui .
```

The settings, endpoint, instance, and status polling tests can run without GTK
dependencies:

```sh
go test -race ./settings ./gui/endpoints ./internal/instance ./internal/statuspoll ./internal/tray ./internal/desktopbus
```

Run the isolated native UI integration test from a graphical session:

```sh
dbus-run-session -- env WG_GUI_TEST=1 go test ./gui -run TestNativeUI -v
```

It uses temporary configuration and settings directories and does not activate
real tunnels or alter the installed PolicyKit policy. The tray and desktop-bus tests use a
private D-Bus daemon to check menus, watcher restarts, and socket handoff. Real `wg-quick`
activation can also be tested safely in a private network namespace:

```sh
go test -c -o /tmp/wireguard-gui-test ./gui
sudo unshare --net -- env WG_NET_TEST=1 /tmp/wireguard-gui-test -test.run TestWireGuardNamespace -test.v
```

This test refuses to run in the host network namespace and uses temporary
configuration files. It does not change existing VPN interfaces or routes.

For interface changes, launch the application in a graphical session and check
the affected controls. Include screenshots in the pull request when useful.

## Build packages

Install the native build dependencies above, then install GoReleaser v2:

```sh
go install github.com/goreleaser/goreleaser/v2@latest
```

Use its installation directory directly so the command works even when it is
absent from `PATH`:

```sh
goreleaser_bin="$(go env GOBIN)"
if [ -z "$goreleaser_bin" ]; then
  goreleaser_bin="$(go env GOPATH)/bin"
fi
"$goreleaser_bin/goreleaser" release --snapshot --clean
```

This builds Linux amd64 `.deb`, `.rpm`, and `.tar.gz` artifacts in `dist/`
without publishing. `--clean` clears previous contents of `dist/`. Packages
include the desktop launcher, PolicyKit action, and icons. Configuration lives
in [.goreleaser.yaml](.goreleaser.yaml).

In VS Code, use **Tasks: Run Task** to run these tasks in order:

1. **Install native build dependencies (Ubuntu/Debian)** — prompts for your sudo
   password in the terminal.
2. **Install GoReleaser** — requires Go and internet access.
3. **Build Debian package** — creates all three artifact formats in `dist/`.

The [release workflow](.github/workflows/release.yml) runs GoReleaser when a tag
is pushed. Use snapshot mode for local package validation.

## Troubleshoot missing native dependencies

Verify native dependencies with:

```sh
pkg-config --modversion gtk4 libadwaita-1 gobject-introspection-1.0
```

Install `libgtk-4-dev`, `libadwaita-1-dev`, and `libgirepository1.0-dev` if a
library is missing. The first build compiles large generated CGO bindings and
can take several minutes. Use `go build -v -o wireguard-linux-gui .` to show
progress. Pin the bindings in `go.mod`; newer branches may require newer native
libraries than Ubuntu 24.04 provides.

### Remove obsolete GTK3 build dependencies

Install and verify the GTK4 dependencies first, then inspect a simulation:

```sh
apt-get -s remove libgtk-3-dev libayatana-appindicator3-dev libayatana-ido3-dev libayatana-indicator3-dev
sudo apt-get remove libgtk-3-dev libayatana-appindicator3-dev libayatana-ido3-dev libayatana-indicator3-dev
```

Retain GTK3 runtime libraries and the GNOME AppIndicator extension used by other
applications. Only remove additional orphan packages when they were installed
for this project and are not required by the new build or other software.
Do not run unrestricted `apt autoremove` as part of this migration.

## Submit a pull request

Create a branch for your change and keep the pull request focused on one problem.
Describe the problem, the resulting behavior, and how you verified it. Link any
related issue and add or update tests when behavior changes. Update documentation
when setup or usage changes, and report any checks you could not run.
