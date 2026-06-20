#!/bin/bash
set -e

BIN=truenas-leds
CONFIG=config.yaml
SERVICE=truenas-leds.service

# Install binary
install -m 755 "$BIN" /usr/local/bin/$BIN

# Install config
target_config_dir=/etc/truenas-leds
mkdir -p "$target_config_dir"
install -m 644 "$CONFIG" "$target_config_dir/config.yaml"

# Install systemd service
install -m 644 "$SERVICE" /etc/systemd/system/$SERVICE

# Reload systemd and restart service
systemctl daemon-reload
systemctl enable --now $SERVICE
systemctl restart $SERVICE

echo "Install complete. Service status:"
systemctl status $SERVICE --no-pager
