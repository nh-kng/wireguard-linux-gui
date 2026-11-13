# wireguard-linux-gui

A Linux GTK client for [WireGuard](https://www.wireguard.com/), based on
[wireguird](https://github.com/UnnoTed/wireguird).

The interface uses GTK4 and Libadwaita, with a native GNOME header bar,
a tunnel sidebar, rounded Interface and Peer cards, and a preferences window.
It follows system-supported light/dark appearance and accent colors without a
bundled theme. The baseline is Ubuntu 24.04 (GTK 4.14 and Libadwaita 1.5).

![wireguard-linux-gui interface with example tunnels](preview.png)

- Lists tunnel configurations from `/etc/wireguard` and controls them with `wg-quick`.
- Imports `.conf` and `.zip` files and exports tunnels to zip.
- Includes a system tray menu and remembers connected tunnels by default.
- Allows multiple active tunnels by default; disable this in Preferences if needed.
- Preserves settings from `wireguird.settings` when the new settings file is absent.
  Saving preferences writes `wireguard-linux-gui.settings`.

## Getting started

Use a Linux desktop with GTK 4.14+, Libadwaita 1.5+, `wireguard-tools`, and
`resolvconf`. To build from a checkout, follow the
[build instructions](CONTRIBUTING.md#build-and-run).
To install a local build and test it from Applications, follow the
[desktop testing instructions](CONTRIBUTING.md#install-locally-for-desktop-testing).

Launch an installed copy from your desktop application menu. The launcher uses
`pkexec` to request root access, which is needed to read and manage tunnel
configurations in `/etc/wireguard`.

Use **+** to add an empty tunnel or import WireGuard `.conf` and `.zip` files.
Select a tunnel to inspect its Interface and Peer fields, then activate it.
Use **−** to remove it and the header menu for View Log, export, and preferences. The Edit
button sits below the details panel.

Preferences has **Cancel** and **Save** in its title bar. Country flags
preview immediately when toggled; **Cancel** restores the saved preference.
Closing Preferences with unsaved changes offers **Keep Editing**, **Discard**,
or **Save**. Country flags use the endpoint's IP location and download a country
database when first enabled.

**Start at login** is enabled automatically on the first launch for your desktop
user when no login entry exists. Change it under **Preferences → General** and
choose **Save**. An existing choice to disable startup stays disabled. This applies
only to your user account. The normal administrator authentication prompt may appear at login;
canceling it skips that launch. To stop launching at login, turn the preference
off and save. **Start in the system tray** separately controls whether the main
window appears; without an available tray host, the window is shown. Remembered
tunnels reconnect after the app starts when **Remember connected tunnels** is on.
This preference does not enable a VPN service at system boot.

The login entry is stored as `wireguard-linux-gui.desktop` under
`$XDG_CONFIG_HOME/autostart` (normally `~/.config/autostart`). Disabling startup
writes a `Hidden=true` entry. Launching through the desktop menu preserves a custom
`XDG_CONFIG_HOME`; direct `sudo` launches use the invoking user's `~/.config`.
The preference is unavailable when no non-root desktop user can be identified.

Closing the window keeps the app in the system tray when a StatusNotifier host
is available (Ubuntu includes one). Without a host, the window stays accessible
and closing it exits the app. Launching it again or
choosing **Manage Tunnels** in the tray brings back the existing window. Choose **Quit WireGuard**
to exit the app; active tunnels continue running independently.

The tray uses the WireGuard dragon as a symbolic icon: dimmed when inactive,
and the panel’s normal foreground color when active, following its light/dark theme.

The tray icon, tunnel checkmarks, and profile status refresh from live WireGuard
interfaces every second while the window is open and every five seconds while
hidden. Reopening the window or activating/deactivating a tunnel refreshes
immediately. Status queries run in the background and include changes made with
`wg-quick` or systemd. The tray menu shows **Status**, active **Networks**, and **Deactivate**, followed
by tunnel checkmarks, management and import actions, About, and Quit. Networks
and Deactivate are hidden when inactive. Activation is disabled when status
is unavailable; tunnel controls are disabled when no profile is selected.
An active interface does not guarantee that a peer is reachable; check the
latest handshake and transfer counters for peer activity.

Local package builds produce `.deb`, `.rpm`, and `.tar.gz` files; see
[packaging instructions](CONTRIBUTING.md#build-packages).

Historical wireguird releases and the release check still point to the original
upstream repository. They are not releases of this renamed project.

## Help and contributing

Open an issue in this repository for bugs or feature requests. See
[CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing, build
troubleshooting, and guidance on issues and pull requests.

## License

[MIT](LICENSE). See [third-party notices](THIRD_PARTY_NOTICES.md) for the
MPL-2.0 GTK4/Libadwaita bindings and their source links.
