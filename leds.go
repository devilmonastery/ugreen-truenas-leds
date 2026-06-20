package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	UGREEN_LED_I2C_ADDR      = 0x3a
	I2C_SLAVE                = 0x0703
	I2C_SMBUS                = 0x0720
	I2C_SMBUS_READ           = 1
	I2C_SMBUS_I2C_BLOCK_DATA = 8
	maxRetry                 = 5
	usleepModification       = 500 * time.Microsecond
	usleepModificationRetry  = 500 * time.Microsecond
	usleepQueryResult        = 500 * time.Microsecond
)

// Exported LED mode constants
const (
	LedModeOff    = 0
	LedModeOn     = 1
	LedModeBlink  = 2
	LedModeBreath = 3
)

var ledNames = []string{
	"power", "lan", "disk1", "disk2", "disk3", "disk4", "disk5", "disk6", "disk7", "disk8",
}

// GetMaxLedIndex returns the maximum valid LED index
func GetMaxLedIndex() int {
	return len(ledNames) - 1
}

// IsValidLedIndex checks if the given LED index is valid
func IsValidLedIndex(index int) bool {
	return index >= 0 && index < len(ledNames)
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

type UGreenLeds struct {
	fd            int
	lastLedStates map[int]ledState
	lastLedStatus map[int]LedStatus
	statusMu      sync.Mutex
}

// NewUGreenLeds initializes and returns a new UGreenLeds instance
func NewUGreenLeds(device string) (*UGreenLeds, error) {
	if device == "" {
		var err error
		device, err = detectUGreenLedDevice()
		if err != nil {
			return nil, err
		}
		log.Printf("Using auto-detected LED I2C device: %s", device)
		log.Printf("Set device: %s in config.yaml to skip auto-detection if needed", device)
	} else {
		log.Printf("Using configured LED I2C device: %s", device)
	}
	fd, err := syscall.Open(device, syscall.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C device %q: %w", device, err)
	}
	if err := ioctlSetSlave(fd, UGREEN_LED_I2C_ADDR); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to set I2C slave: %w", err)
	}
	return &UGreenLeds{
		fd:            fd,
		lastLedStates: make(map[int]ledState),
		lastLedStatus: make(map[int]LedStatus),
	}, nil
}

func detectUGreenLedDevice() (string, error) {
	paths, err := filepath.Glob("/dev/i2c-*")
	if err != nil {
		return "", fmt.Errorf("failed to list I2C devices: %w", err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no I2C devices found at /dev/i2c-*")
	}

	sort.Slice(paths, func(i, j int) bool {
		return i2cBusNumber(paths[i]) < i2cBusNumber(paths[j])
	})
	log.Printf("Discovered %d I2C devices: %s", len(paths), strings.Join(paths, ", "))

	for _, path := range paths {
		fd, err := syscall.Open(path, syscall.O_RDWR, 0600)
		if err != nil {
			continue
		}

		if err := ioctlSetSlave(fd, UGREEN_LED_I2C_ADDR); err == nil && probeLedController(fd) {
			syscall.Close(fd)
			return path, nil
		}
		syscall.Close(fd)
	}

	return "", fmt.Errorf("failed to auto-detect UGREEN LED controller at I2C address 0x%x across %d buses", UGREEN_LED_I2C_ADDR, len(paths))
}

func i2cBusNumber(path string) int {
	bus, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "i2c-"))
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return bus
}

func probeLedController(fd int) bool {
	for id := range ledNames {
		status, err := readLedStatus(fd, id)
		if err == nil && status.Available {
			return true
		}
	}
	return false
}

func (u *UGreenLeds) Close() {
	if u.fd > 0 {
		syscall.Close(u.fd)
		u.fd = 0
	}
}

func (u *UGreenLeds) SetLedColor(id int, r, g, b byte) error {
	if !IsValidLedIndex(id) {
		return fmt.Errorf("invalid LED index %d (valid range: 0-%d)", id, GetMaxLedIndex())
	}
	return u.setLedColor(id, r, g, b)
}

func (u *UGreenLeds) SetLedBrightness(id int, brightness byte) error {
	if !IsValidLedIndex(id) {
		return fmt.Errorf("invalid LED index %d (valid range: 0-%d)", id, GetMaxLedIndex())
	}
	return u.setLedBrightness(id, brightness)
}

func (u *UGreenLeds) SetLedMode(id int, mode byte, params []byte) error {
	if !IsValidLedIndex(id) {
		return fmt.Errorf("invalid LED index %d (valid range: 0-%d)", id, GetMaxLedIndex())
	}
	return u.setLedMode(id, mode, params)
}

// --- Internal methods ---
func (u *UGreenLeds) updateLedStatus(id int) {
	status, err := readLedStatus(u.fd, id)
	if err == nil {
		u.statusMu.Lock()
		u.lastLedStatus[id] = status
		u.statusMu.Unlock()
	}
}

func (u *UGreenLeds) setLedColor(id int, r, g, b byte) error {
	state := u.lastLedStates[id]
	if state.color == [3]byte{r, g, b} {
		return nil
	}
	err := modifyLedWithRetry(u.fd, id, 0x02, []byte{r, g, b}, nil)
	if err == nil {
		state.color = [3]byte{r, g, b}
		u.lastLedStates[id] = state
		u.updateLedStatus(id)
	}
	return err
}

func (u *UGreenLeds) setLedBrightness(id int, brightness byte) error {
	state := u.lastLedStates[id]
	if state.brightness == brightness {
		return nil
	}
	err := modifyLedWithRetry(u.fd, id, 0x01, []byte{brightness}, nil)
	if err == nil {
		state.brightness = brightness
		u.lastLedStates[id] = state
		u.updateLedStatus(id)
	}
	return err
}

func (u *UGreenLeds) setLedMode(id int, mode byte, params []byte) error {
	state := u.lastLedStates[id]
	if state.mode == mode {
		if mode == 0 || mode == 1 {
			return nil
		}
		if (mode == 2 || mode == 3) && params != nil && state.params == [4]byte{params[0], params[1], params[2], params[3]} {
			return nil
		}
	}
	var err error
	switch mode {
	case 0: // off
		err = modifyLedWithRetry(u.fd, id, 0x03, []byte{0}, nil)
	case 1: // on
		err = modifyLedWithRetry(u.fd, id, 0x03, []byte{1}, nil)
	case 2: // blink
		err = modifyLedWithRetry(u.fd, id, 0x04, params, nil)
	case 3: // breath
		err = modifyLedWithRetry(u.fd, id, 0x05, params, nil)
	}
	if err == nil {
		state.mode = mode
		if params != nil && (mode == 2 || mode == 3) && len(params) == 4 {
			state.params = [4]byte{params[0], params[1], params[2], params[3]}
		} else {
			state.params = [4]byte{}
		}
		u.lastLedStates[id] = state
		u.updateLedStatus(id)
	}
	return err
}

// --- Low-level I2C and LED access functions ---

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

func confirmStatus(fd int, id int, wantOn *bool) bool {
	for range maxRetry {
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
	// Validate LED index before attempting to modify
	if !IsValidLedIndex(id) {
		return fmt.Errorf("invalid LED index %d (valid range: 0-%d)", id, GetMaxLedIndex())
	}

	var lastErr error
	for retry := 0; retry < maxRetry; retry++ {
		lastErr = writeLedCommand(fd, id, command, params)
		if lastErr == nil && confirmStatus(fd, id, wantOn) {
			return nil
		}
		if retry == 0 {
			time.Sleep(usleepModification)
		} else {
			time.Sleep(usleepModificationRetry)
		}
	}
	return fmt.Errorf("failed to set %s after %d retries: %v", ledNames[id], maxRetry, lastErr)
}

type ledState struct {
	color      [3]byte
	brightness byte
	mode       byte    // 0=off, 1=on, 2=blink, 3=breath
	params     [4]byte // for blink/breath params
}

func ioctlSetSlave(fd int, addr int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(I2C_SLAVE), uintptr(addr))
	if errno != 0 {
		return errno
	}
	return nil
}
