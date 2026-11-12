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

Work from a local checkout on Linux. Install Go and the native build and runtime
dependencies on Debian/Ubuntu:

```sh
sudo apt update
sudo apt install build-essential pkg-config wireguard-tools libgtk-3-dev libayatana-appindicator3-dev golang-go resolvconf
```

The application's Go version requirement is declared in [go.mod](go.mod).
GoReleaser may require a newer Go toolchain when installed from source.

GTK 3 pulls in the GLib development files. Ayatana AppIndicator provides the
system tray integration. These native libraries are required because the GUI
uses CGO.

## Build and run

Run from the repository root:

```sh
go generate ./...
go build -o wireguard-linux-gui .
sudo ./wireguard-linux-gui
```

Generation creates the GTK builder helpers and embeds the interface and icons.
Rerun it after changing the Glade file or embedded assets. The generated
`static/ab0x.go` is ignored by Git.

Root access is needed to read and manage tunnel configurations. Installed desktop
launchers use `pkexec`.

## Check your changes

Format changed Go files with `gofmt`, then generate assets and run the tests:

```sh
go generate ./...
go test ./...
go build -o wireguard-linux-gui .
```

The existing settings and endpoint tests can also run without GTK dependencies:

```sh
go test ./settings ./gui/endpoints
```

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

If the build reports missing `gio-2.0`, `glib-2.0`, or `gobject-2.0`, install
`libgtk-3-dev`, which depends on `libglib2.0-dev`. If it reports missing
`ayatana-appindicator3-0.1`, install `libayatana-appindicator3-dev`.

Verify that `pkg-config` can find all required libraries:

```sh
pkg-config --modversion gio-2.0 glib-2.0 gobject-2.0 gtk+-3.0 ayatana-appindicator3-0.1
```

Then rerun the build or package task.

## Submit a pull request

Create a branch for your change and keep the pull request focused on one problem.
Describe the problem, the resulting behavior, and how you verified it. Link any
related issue and add or update tests when behavior changes. Update documentation
when setup or usage changes, and report any checks you could not run.
