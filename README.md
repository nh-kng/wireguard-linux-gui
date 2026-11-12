# wireguard-linux-gui

A Linux GTK client for [WireGuard](https://www.wireguard.com/), based on
[wireguird](https://github.com/UnnoTed/wireguird).

The interface follows the [WireGuard Windows client](https://github.com/WireGuard/wireguard-windows):
compact Tunnels and Log tabs, a tunnel list with an Add Tunnel toolbar, and
bordered Interface and Peer groups with activation and editing controls.

![wireguard-linux-gui interface with example tunnels](preview.png)

- Lists tunnel configurations from `/etc/wireguard` and controls them with `wg-quick`.
- Imports `.conf` and `.zip` files and exports tunnels to zip.
- Includes a system tray menu, optional country flags, and remembered connections.
- Preserves settings from `wireguird.settings` when the new settings file is absent.
  Saving preferences writes `wireguard-linux-gui.settings`.

## Build and run

Install the build and runtime dependencies on Debian/Ubuntu:

```sh
sudo apt install wireguard-tools libgtk-3-dev libayatana-appindicator3-dev golang-go resolvconf
```

From this checkout:

```sh
go generate ./...
go build -o wireguard-linux-gui .
sudo ./wireguard-linux-gui
```

The binary embeds the interface and icons. Root access is needed to read and
manage tunnel configurations. Installed desktop launchers use `pkexec`.

## Packages

```sh
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser release --snapshot --clean
```

GoReleaser builds `wireguard-linux-gui` archives, Debian packages, and RPM packages,
including the renamed desktop launcher, PolicyKit action, and icons.

Historical wireguird releases and the release check still point to the original
upstream repository. They are not releases of this renamed project.
