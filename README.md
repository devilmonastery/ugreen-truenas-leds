# ugreen-truenas-leds

This is a quick and dirty program to poll for disk and network activity on
a UGREEN DXP6800 Pro and other models, and update the front panel
LEDs accordingly.

## Configuration

The program uses a YAML configuration file (default: `config.yaml`) to control its behavior. Here's a complete configuration example with all available options:

```yaml
# I2C device path for LED control
# Optional. If omitted, the program scans /dev/i2c-* for the UGREEN LED controller.
# Find devices with: i2cdetect -l
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

### Configuration Options Explained

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `device` | string | auto-detect | I2C device path for communicating with LEDs |
| `poll_interval` | duration | `100ms` | Frequency of disk/network activity polling |
| `rainbow_cycle_time` | duration | `3s` | Time for one complete rainbow cycle |
| `enable_rainbow` | boolean | `true` | Show rainbow colors on inactive disks |
| `rainbow_brightness` | integer | `48` | Brightness level for rainbow (0-255) |

### LED Behavior

- **Red LED**: Disk write activity or network transmit
- **Blue LED**: Disk read activity or network receive  
- **Purple LED**: Mixed read/write or transmit/receive activity
- **Rainbow Colors**: Inactive disks (when `enable_rainbow: true`)
- **Off**: Inactive disks (when `enable_rainbow: false`)

**Brightness**: Automatically scaled based on the intensity of disk/network activity.

## Building

```bash
go build -o truenas-leds .
```

## Running

```bash
./truenas-leds --config=config.yaml
```

## Finding your i2c device

The default i2c device may not work for you. Find one that does.

If auto-detection picks the wrong bus, set `device: /dev/i2c-2` in your config
or pass `--device=/dev/i2c-2`.

```bash
$ i2cdetect -l
```