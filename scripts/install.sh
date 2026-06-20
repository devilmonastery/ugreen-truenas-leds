#!/bin/bash
set -e

BIN=truenas-leds
CONFIG=config.example.yaml
SERVICE=truenas-leds.service

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd -- "$script_dir/.." && pwd)

# Install binary
install -m 755 "$repo_root/bin/$BIN" /usr/local/bin/$BIN

# Install config
target_config_dir=/etc/truenas-leds
mkdir -p "$target_config_dir"
install -m 644 "$repo_root/$CONFIG" "$target_config_dir/config.yaml"

# Install systemd service
install -m 644 "$repo_root/packaging/systemd/$SERVICE" /etc/systemd/system/$SERVICE

# Reload systemd and restart service
systemctl daemon-reload
systemctl enable --now $SERVICE
systemctl restart $SERVICE

echo "Install complete. Service status:"
systemctl status $SERVICE --no-pager