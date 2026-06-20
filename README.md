# ugreen-truenas-leds

Polls disk and network activity on UGREEN NAS systems running Linux and
updates the front-panel LEDs.

The program can auto-detect the LED controller on `/dev/i2c-*`. If detection
chooses the wrong bus, set `device` in the config file or pass `--device`.

## Build

```bash
make build
```

The binary is written to `bin/truenas-leds`.

## Run

```bash
cp config.example.yaml config.yaml
./bin/truenas-leds --config=config.yaml
```

Useful commands:

```bash
./bin/truenas-leds --config=/etc/truenas-leds/config.yaml
./bin/truenas-leds --device=/dev/i2c-2
./bin/truenas-leds get 1
./bin/truenas-leds set 2 255 255 255 64
```

## Install

```bash
make build
sudo scripts/install.sh
```

The installer copies:

- `bin/truenas-leds` to `/usr/local/bin/truenas-leds`
- `config.example.yaml` to `/etc/truenas-leds/config.yaml`
- `packaging/systemd/truenas-leds.service` to `/etc/systemd/system/truenas-leds.service`

## Configure

The program uses a YAML configuration file (default: `config.yaml`) to control its behavior. Use `config.example.yaml` as a starting point:

```yaml
# I2C device path for LED control
# Optional. If omitted, the program scans /dev/i2c-* for the UGREEN LED controller.
# Verify manually with: i2cdetect -l
device: /dev/i2c-2

# How often to poll for disk and network activity
# Valid range: 10ms to 5000ms
# Default: 100ms
poll_interval: 100ms

# Rainbow cycle time for inactive disks (when enable_rainbow is true)
# How long it takes to complete one full rainbow color cycle
# Valid range: 1s to 10s
# Default: 3s
rainbow_cycle_time: 3s

# Enable rainbow color cycling for inactive disks
# When true: inactive disks show rainbow colors
# When false: inactive disks turn off
# Default: true
enable_rainbow: true

# Brightness level for rainbow colors (0-255)
# Only applies when enable_rainbow is true
# Default: 48
rainbow_brightness: 48
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `device` | string | auto-detect | I2C device path for communicating with LEDs |
| `poll_interval` | duration | `100ms` | Frequency of disk/network activity polling |
| `rainbow_cycle_time` | duration | `3s` | Time for one complete rainbow cycle |
| `enable_rainbow` | boolean | `true` | Show rainbow colors on inactive disks |
| `rainbow_brightness` | integer | `48` | Rainbow brightness, from `0` to `255` |

## Auto-Detection

When `device` is unset, startup scans `/dev/i2c-*` for the UGREEN LED controller
at I2C address `0x3a`. The logs include the discovered buses and the selected
device:

```text
Discovered 17 I2C devices: /dev/i2c-0, /dev/i2c-1, /dev/i2c-2, ...
Using auto-detected LED I2C device: /dev/i2c-2
Set device: /dev/i2c-2 in config.yaml to skip auto-detection if needed
```

To force a device, set it in the config:

```yaml
device: /dev/i2c-2
```

or pass it on the command line:

```bash
./bin/truenas-leds --device=/dev/i2c-2
```

## LED Behavior

- **Disk activity**: Active disk LEDs turn white. Brightness scales with total read/write activity during the polling interval.
- **Network activity**: The LAN LED turns white and blinks when receive/transmit traffic is detected.
- **Inactive LEDs**: Inactive disk and LAN LEDs show rainbow colors when `enable_rainbow: true`.
- **Off**: Inactive disk and LAN LEDs turn off when `enable_rainbow: false`.

**Brightness**: Automatically scaled against the highest disk or network activity observed since startup.

## Troubleshooting

Check which I2C buses exist:

```bash
i2cdetect -l
ls /dev/i2c-*
```

If no I2C devices are listed, check that the kernel has I2C support loaded. At
minimum, Linux needs the `i2c-dev` module to expose `/dev/i2c-*` device nodes:

```bash
lsmod | grep -E '(^i2c_dev|^i2c_i801|^i2c_designware)'
sudo modprobe i2c-dev
```

Depending on the hardware, the bus driver may also need to be loaded. Common
drivers include `i2c_i801` for Intel SMBus adapters and `i2c_designware_platform`
or `i2c_designware_pci` for DesignWare I2C controllers.

Probe a candidate bus for the LED controller address. The controller is expected
at `0x3a`:

```bash
sudo i2cdetect -y 2
```

Check service logs:

```bash
journalctl -u truenas-leds.service -b --no-pager
journalctl -u truenas-leds.service -f
```

Check service state:

```bash
systemctl status truenas-leds.service --no-pager
systemctl restart truenas-leds.service
```

Run manually with a known config and device:

```bash
sudo /usr/local/bin/truenas-leds -config /etc/truenas-leds/config.yaml --device=/dev/i2c-2
```

Common failure points:

- No `/dev/i2c-*` devices: load `i2c-dev` and the platform's I2C bus driver.
- Permission denied opening `/dev/i2c-*`: run as root or adjust device permissions.
- Auto-detection picks the wrong bus: set `device` in `/etc/truenas-leds/config.yaml`.
- No disk activity lights: confirm disks are visible under `/sys/class/scsi_disk` and `/dev/disk/by-path`.
