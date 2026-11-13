# Third-party notices

The application source remains MIT-licensed. It links the following generated
bindings, whose source files are licensed under MPL-2.0:

- [gotk4/pkg](https://github.com/diamondburned/gotk4/tree/d44ab4b5b24e/pkg),
  revision `d44ab4b5b24e` (the Cairo binding files retain their MIT notices).
- [gotk4-adwaita/pkg](https://github.com/diamondburned/gotk4-adwaita/tree/f81761ef0e0d20148d0f02fc85c782b0c8c9b230/pkg),
  revision `f81761ef0e0d20148d0f02fc85c782b0c8c9b230`.

Copies of the binding licenses are in `licenses/`. Source for the exact versions
is available from the links above and through `go mod download` using `go.mod`.
The AGPL generators are not part of the application build or runtime.

GTK4 and Libadwaita are dynamically linked system libraries distributed by the
operating system under their own licenses. Other Go dependency license notices
remain available in their source distributions referenced by `go.mod`.

The tray's `Icon/wireguard-*-symbolic.svg` files use the WireGuard brand icon
from [CoreUI Icons](https://github.com/coreui/coreui-icons/blob/master/svg/brand/cib-wireguard.svg).
The outer circle was removed, retaining the original dragon paths; symbolic
theme colors and inactive opacity were added.
CoreUI's brand icons are distributed under CC0-1.0; the upstream license is
in `licenses/coreui-icons-LICENSE.txt`. WireGuard is a trademark of its owner.
