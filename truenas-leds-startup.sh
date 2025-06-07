#!/bin/bash
# Start truenas-leds as a background service

LED_BIN="/mnt/tank/bin/truenas-leds"
PIDFILE="/var/run/truenas-leds.pid"
LOGFILE="/var/log/truenas-leds.log"

if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
    echo "truenas-leds already running."
    exit 0
fi

nohup "$LED_BIN" >> "$LOGFILE" 2>&1 &
echo $! > "$PIDFILE"
echo "truenas-leds started."