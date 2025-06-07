package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	UGREEN_LED_I2C_ADDR      = 0x3a
	NUM_LEDS                 = 10
	I2C_SLAVE                = 0x0703
	I2C_SMBUS                = 0x0720
	I2C_SMBUS_READ           = 1
	I2C_SMBUS_I2C_BLOCK_DATA = 8
	maxRetry                 = 5
	usleepModification       = 500 * time.Microsecond
	usleepModificationRetry  = 3 * time.Millisecond
	usleepQueryResult        = 2 * time.Millisecond
)

var ledNames = []string{
	"power", "lan", "disk1", "disk2", "disk3", "disk4", "disk5", "disk6",
}

type i2cSmbusData struct {
	block [34]byte
}

type i2cSmbusIoctlData struct {
	readWrite byte
	command   byte
	size      uint32
	data      uintptr
}

type LedStatus struct {
	Available  bool
	OpMode     string
	Brightness uint8
	ColorR     uint8
	ColorG     uint8
	ColorB     uint8
	TOn        uint16
	TOff       uint16
}

func verifyChecksum(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	sum := 0
	for i := 0; i < len(data)-2; i++ {
		sum += int(data[i])
	}
	want := int(binary.BigEndian.Uint16(data[len(data)-2:]))
	return sum != 0 && sum == want
}

func parseLedStatus(data []byte) LedStatus {
	status := LedStatus{}
	if len(data) != 11 || !verifyChecksum(data) {
		return status
	}
	opModes := []string{"off", "on", "blink", "breath"}
	opModeIdx := int(data[0])
	if opModeIdx < len(opModes) {
		status.OpMode = opModes[opModeIdx]
	} else {
		status.OpMode = "unknown"
	}
	status.Brightness = data[1]
	status.ColorR = data[2]
	status.ColorG = data[3]
	status.ColorB = data[4]
	tHigh := binary.BigEndian.Uint16(data[5:7])
	tLow := binary.BigEndian.Uint16(data[7:9])
	status.TOn = tLow
	status.TOff = tHigh - tLow
	status.Available = true
	return status
}

func readLedStatus(fd int, ledID int) (LedStatus, error) {
	cmd := 0x81 + byte(ledID)
	var smbusData i2cSmbusData
	ioctlData := i2cSmbusIoctlData{
		readWrite: I2C_SMBUS_READ,
		command:   cmd,
		size:      I2C_SMBUS_I2C_BLOCK_DATA,
		data:      uintptr(unsafe.Pointer(&smbusData)),
	}
	// Set block length to 11
	smbusData.block[0] = 11
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(I2C_SMBUS),
		uintptr(unsafe.Pointer(&ioctlData)),
	)
	if errno != 0 {
		return LedStatus{}, fmt.Errorf("ioctl error: %v", errno)
	}
	// Data is in smbusData.block[1:12]
	return parseLedStatus(smbusData.block[1:12]), nil
}

func writeLedCommand(fd int, ledID int, command byte, params []byte) error {
	data := []byte{
		0x00,                   // placeholder for LED ID
		0xa0,                   // fixed
		0x01,                   // fixed
		0x00,                   // fixed
		0x00,                   // fixed
		command,                // command
		0x00, 0x00, 0x00, 0x00, // up to 4 params
	}
	copy(data[6:], params)

	// Compute checksum
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	data = append(data, byte(sum>>8), byte(sum&0xff))

	// Now set LED ID in data[0] (after checksum is appended)
	data[0] = byte(ledID)

	// Prepare SMBus block write
	var smbusData i2cSmbusData
	smbusData.block[0] = byte(len(data))
	copy(smbusData.block[1:], data)
	ioctlData := i2cSmbusIoctlData{
		readWrite: 0, // write
		command:   byte(ledID),
		size:      I2C_SMBUS_I2C_BLOCK_DATA,
		data:      uintptr(unsafe.Pointer(&smbusData)),
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(I2C_SMBUS),
		uintptr(unsafe.Pointer(&ioctlData)),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl error: %v", errno)
	}
	return nil
}

func ledNameToID(name string) (int, error) {
	for i, n := range ledNames {
		if n == name {
			return i, nil
		}
	}
	return -1, fmt.Errorf("unknown LED name: %s", name)
}

func usage() {
	fmt.Println("Usage: truenas-leds <led> [commands]")
	fmt.Println("LED names: power netdev disk1 disk2 ... disk8")
	fmt.Println("Commands:")
	fmt.Println("  -on | -off")
	fmt.Println("  -blink <on_ms> <off_ms>")
	fmt.Println("  -breath <on_ms> <off_ms>")
	fmt.Println("  -color <r> <g> <b>")
	fmt.Println("  -brightness <val>")
	fmt.Println("  -status")
	os.Exit(1)
}

func confirmStatus(fd int, id int, wantOn *bool) bool {
	for retry := 0; retry < maxRetry; retry++ {
		time.Sleep(usleepQueryResult)
		status, err := readLedStatus(fd, id)
		if err == nil && status.Available {
			if wantOn == nil {
				return true // for color/brightness, just check available
			}
			if (*wantOn && status.OpMode == "on") || (!*wantOn && status.OpMode == "off") {
				return true
			}
		}
		time.Sleep(usleepModificationRetry)
	}
	return false
}

func modifyLedWithRetry(fd int, id int, command byte, params []byte, wantOn *bool) error {
	var lastErr error
	for retry := 0; retry < maxRetry; retry++ {
		if retry == 0 {
			time.Sleep(usleepModification)
		} else {
			time.Sleep(usleepModificationRetry)
		}
		lastErr = writeLedCommand(fd, id, command, params)
		if lastErr == nil && confirmStatus(fd, id, wantOn) {
			time.Sleep(2 * time.Millisecond) // Give device time before next modification
			return nil
		}
	}
	return fmt.Errorf("failed to set %s after %d retries: %v", ledNames[id], maxRetry, lastErr)
}

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root to access I2C devices.")
	}

	fd, err := syscall.Open("/dev/i2c-0", syscall.O_RDWR, 0600)
	if err != nil {
		log.Fatalf("Failed to open /dev/i2c-0: %v", err)
	}
	defer syscall.Close(fd)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(I2C_SLAVE), uintptr(UGREEN_LED_I2C_ADDR)); errno != 0 {
		log.Fatalf("Failed to set I2C_SLAVE: %v", errno)
	}

	var availableLEDs []int
	for i, _ := range ledNames {
		status, err := readLedStatus(fd, i)
		if err == nil && status.Available {
			availableLEDs = append(availableLEDs, i)
		}
	}

	if len(os.Args) == 1 {
		// No args: show all statuses
		for i, name := range ledNames {
			time.Sleep(8 * time.Millisecond)
			status, err := readLedStatus(fd, i)
			if err != nil || !status.Available {
				fmt.Printf("%s: unavailable or non-existent\n", name)
				continue
			}
			fmt.Printf("%s: status = %s, brightness = %d, color = RGB(%d, %d, %d)",
				name, status.OpMode, status.Brightness, status.ColorR, status.ColorG, status.ColorB)
			if status.OpMode == "blink" {
				fmt.Printf(", blink_on = %d ms, blink_off = %d ms", status.TOn, status.TOff)
			}
			fmt.Println()
		}
		return
	}

	// Parse LED names
	args := os.Args[1:]
	ledIDs := []int{}
	for len(args) > 0 && args[0][0] != '-' {
		id, err := ledNameToID(args[0])
		if err != nil {
			usage()
		}
		ledIDs = append(ledIDs, id)
		args = args[1:]
	}
	if len(ledIDs) == 0 {
		usage()
	}

	// Parse and sequence commands
	type ledOp struct {
		command byte
		params  []byte
		wantOn  *bool // nil for color/brightness, pointer for on/off
	}

	var ops []ledOp
	for len(args) > 0 {
		cmd := args[0]
		args = args[1:]
		switch cmd {
		case "-on", "-off":
			val := byte(0)
			wantOn := false
			if cmd == "-on" {
				val = 1
				wantOn = true
			}
			ops = append(ops, ledOp{command: 0x03, params: []byte{val}, wantOn: &wantOn})
		case "-blink", "-breath":
			if len(args) < 2 {
				usage()
			}
			tOn := uint16(atoi(args[0]))
			tOff := uint16(atoi(args[1]))
			args = args[2:]
			tHigh := tOn + tOff
			params := []byte{byte(tHigh >> 8), byte(tHigh), byte(tOn >> 8), byte(tOn)}
			command := byte(0x04)
			if cmd == "-breath" {
				command = 0x05
			}
			ops = append(ops, ledOp{command: command, params: params, wantOn: nil})
		case "-color":
			if len(args) < 3 {
				usage()
			}
			r := byte(atoi(args[0]))
			g := byte(atoi(args[1]))
			b := byte(atoi(args[2]))
			args = args[3:]
			ops = append(ops, ledOp{command: 0x02, params: []byte{r, g, b}, wantOn: nil})
		case "-brightness":
			if len(args) < 1 {
				usage()
			}
			val := byte(atoi(args[0]))
			args = args[1:]
			ops = append(ops, ledOp{command: 0x01, params: []byte{val}, wantOn: nil})
		case "-status":
			for _, id := range ledIDs {
				status, err := readLedStatus(fd, id)
				if err != nil || !status.Available {
					fmt.Printf("%s: unavailable or non-existent\n", ledNames[id])
					continue
				}
				fmt.Printf("%s: status = %s, brightness = %d, color = RGB(%d, %d, %d)",
					ledNames[id], status.OpMode, status.Brightness, status.ColorR, status.ColorG, status.ColorB)
				if status.OpMode == "blink" {
					fmt.Printf(", blink_on = %d ms, blink_off = %d ms", status.TOn, status.TOff)
				}
				fmt.Println()
			}
		default:
			usage()
		}
	}

	// Sequence all modifications for each LED
	for _, id := range ledIDs {
		for _, op := range ops {
			if err := modifyLedWithRetry(fd, id, op.command, op.params, op.wantOn); err != nil {
				fmt.Printf("Failed to set %s: %v\n", ledNames[id], err)
			}
		}
	}
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		fmt.Printf("Invalid number: %s\n", s)
		os.Exit(1)
	}
	return v
}
