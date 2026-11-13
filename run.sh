#!/bin/sh
set -eu
go build -o wireguard-linux-gui .
exec sudo ./wireguard-linux-gui
