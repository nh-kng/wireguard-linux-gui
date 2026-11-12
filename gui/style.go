package gui

// Stylesheet follows the compact, system-control appearance of WireGuard for
// Windows. Scope the rules to our windows so GTK dialogs keep their own theme.
const Stylesheet = `
.wireguard {
  font-family: "Segoe UI", "Noto Sans", sans-serif;
  font-size: 13px;
  color: #202020;
  background-color: #f0f0f0;
}
.wireguard headerbar {
  min-height: 32px;
  padding: 4px 8px;
  background-image: none;
  background-color: #f0f0f0;
  border-bottom: 1px solid #c8c8c8;
  box-shadow: none;
}
.wireguard headerbar .title { font-weight: normal; }
.wireguard headerbar .subtitle { font-size: 11px; color: #555555; }
.wireguard .navigation { padding: 10px 12px 0; }
.wireguard button {
  min-height: 24px;
  min-width: 16px;
  padding: 2px 10px;
  border: 1px solid #adadad;
  border-radius: 2px;
  background-image: none;
  background-color: #e8e8e8;
  color: #202020;
  box-shadow: none;
  text-shadow: none;
}
.wireguard button:hover { background-color: #e5f1fb; border-color: #0078d7; }
.wireguard button:active { background-color: #cce4f7; border-color: #005a9e; }
.wireguard button:checked { background-color: #ffffff; border-color: #0078d7; }
.wireguard button:focus { outline: 1px dotted #202020; outline-offset: -4px; }
.wireguard button:disabled { color: #707070; background-color: #f0f0f0; border-color: #cccccc; }
.wireguard .tunnel-scroll { border: 1px solid #adadad; background-color: #ffffff; }
.wireguard .tunnel-list { background-color: #ffffff; color: #202020; }
.wireguard .tunnel-list row { padding: 2px 6px; border: none; }
.wireguard .tunnel-list row:hover { background-color: #e5f3ff; }
.wireguard .tunnel-list row:selected { background-color: #cce8ff; color: #202020; }
.wireguard .tunnel-list row:selected:focus { background-color: #0078d7; color: #ffffff; }
.wireguard .details-group { margin-bottom: 10px; }
.wireguard .details-group > border { border: 1px solid #c0c0c0; border-radius: 0; }
.wireguard .details-group > label { padding: 0 5px; color: #202020; }
.wireguard viewport { background-color: transparent; }
.wireguard entry, .wireguard textview, .wireguard textview text {
  background-color: #ffffff;
  color: #202020;
}
.wireguard entry { border: 1px solid #adadad; border-radius: 0; box-shadow: none; }
.wireguard entry:focus { border-color: #0078d7; }
.wireguard .logs { background-color: #ffffff; color: #202020; font-family: monospace; font-size: 12px; }
.wireguard .logs row { padding: 3px 6px; }
`
