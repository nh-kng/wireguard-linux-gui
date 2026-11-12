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

## Getting started

Use a Linux desktop with GTK 3, Ayatana AppIndicator, `wireguard-tools`, and
`resolvconf`. To build from a checkout, follow the
[build instructions](CONTRIBUTING.md#build-and-run).

Launch an installed copy from your desktop application menu. The launcher uses
`pkexec` to request root access, which is needed to read and manage tunnel
configurations in `/etc/wireguard`.

Import a WireGuard `.conf` or `.zip` file using Add Tunnel, select the tunnel,
and activate it. Use the Log tab to inspect connection activity.

Local package builds produce `.deb`, `.rpm`, and `.tar.gz` files; see
[packaging instructions](CONTRIBUTING.md#build-packages).

Historical wireguird releases and the release check still point to the original
upstream repository. They are not releases of this renamed project.

## Help and contributing

Open an issue in this repository for bugs or feature requests. See
[CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing, build
troubleshooting, and guidance on issues and pull requests.

## License

[MIT](LICENSE).
