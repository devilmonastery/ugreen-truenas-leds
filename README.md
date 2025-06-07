# ugreen-truenas-leds

This is a quick and dirty program to poll for disk and network activity on
a UGREEN DXP6800 Pro running TrueNAS SCALE, and update the front panel
LEDs accordingly.

It has no settings (yet), everything is hardcoded.

These are the LED colors:

- Red: disk write or lan transmit.
- Blue: disk read or lan receive.

The LEDs will appear purple if a mixture of activity is happening.

Brightness: scaled to the delta of activity over the last polling period.

