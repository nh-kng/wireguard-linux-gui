#!/bin/sh
set -eu
go build -ldflags "-X wireguard-linux-gui/gui.BuildDate=$(date -u +%Y-%m-%d)" -o wireguard-linux-gui .
exec sudo ./wireguard-linux-gui
