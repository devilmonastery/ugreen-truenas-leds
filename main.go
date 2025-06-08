package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
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

// DiskInfo describes a disk
type DiskInfo struct {
	Name   string
	HCTL   string
	Serial string
	Path   string // by-path link name, for sorting
	PCIBus string // e.g. 0000:59:00.0
	Port   int    // e.g. 1 for -ata-1
}

type DiskActivity struct {
	Reads    uint64
	Writes   uint64
	Activity uint64 // Reads + Writes
}

var (
	statusMu      sync.Mutex
	lastLedStatus = make(map[int]LedStatus)
)

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

// Wrapper to avoid redundant writes: only call modifyLedWithRetry if value changes
type ledState struct {
	color      [3]byte
	brightness byte
	mode       byte    // 0=off, 1=on, 2=blink, 3=breath
	params     [4]byte // for blink/breath params
}

var lastLedStates = make(map[int]ledState)

func updateLedStatus(fd, id int) {
	status, err := readLedStatus(fd, id)
	if err == nil {
		statusMu.Lock()
		lastLedStatus[id] = status
		statusMu.Unlock()
	}
}

func setLedColor(fd, id int, r, g, b byte) error {
	state := lastLedStates[id]
	if state.color == [3]byte{r, g, b} {
		return nil
	}
	err := modifyLedWithRetry(fd, id, 0x02, []byte{r, g, b}, nil)
	if err == nil {
		state.color = [3]byte{r, g, b}
		lastLedStates[id] = state
		updateLedStatus(fd, id)
	}
	return err
}

func setLedBrightness(fd, id int, brightness byte) error {
	state := lastLedStates[id]
	if state.brightness == brightness {
		return nil
	}
	err := modifyLedWithRetry(fd, id, 0x01, []byte{brightness}, nil)
	if err == nil {
		state.brightness = brightness
		lastLedStates[id] = state
		updateLedStatus(fd, id)
	}
	return err
}

func setLedMode(fd, id int, mode byte, params []byte) error {
	state := lastLedStates[id]
	// Only skip redundant writes for off/on, or for blink/breath if params match
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
		err = modifyLedWithRetry(fd, id, 0x03, []byte{0}, nil)
	case 1: // on
		err = modifyLedWithRetry(fd, id, 0x03, []byte{1}, nil)
	case 2: // blink
		err = modifyLedWithRetry(fd, id, 0x04, params, nil)
	case 3: // breath
		err = modifyLedWithRetry(fd, id, 0x05, params, nil)
	}
	if err == nil {
		state.mode = mode
		if params != nil && (mode == 2 || mode == 3) && len(params) == 4 {
			state.params = [4]byte{params[0], params[1], params[2], params[3]}
		} else {
			state.params = [4]byte{}
		}
		lastLedStates[id] = state
		updateLedStatus(fd, id)
	}
	return err
}

// discoverDisks returns disks sorted by HCTL, like lsblk -S -x hctl -o name,hctl,serial
func discoverDisks() ([]DiskInfo, error) {
	var disks []DiskInfo

	serials, err := getBlockDevicesSerials()
	if err != nil {
		log.Printf("Error getting block device serials: %v", err)
	}

	// Map device name -> HCTL
	hctlMap := make(map[string]string)

	scsiDiskDir := "/sys/class/scsi_disk/"
	entries, err := os.ReadDir(scsiDiskDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		hctl := entry.Name()
		devicePath := filepath.Join(scsiDiskDir, hctl, "device")

		blockLinks, err := os.ReadDir(filepath.Join(devicePath, "block"))
		if err != nil || len(blockLinks) == 0 {
			continue
		}
		name := blockLinks[0].Name()
		hctlMap[name] = hctl
	}

	byPathDir := "/dev/disk/by-path"
	byPathEntries, err := os.ReadDir(byPathDir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)

	for _, entry := range byPathEntries {
		name := entry.Name()
		fullPath := filepath.Join(byPathDir, name)

		resolved, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			continue
		}

		dev := filepath.Base(resolved)

		if !strings.HasPrefix(dev, "sd") || len(dev) != 3 {
			continue
		}

		if seen[dev] {
			continue
		}
		seen[dev] = true

		// Expect name like pci-0000:59:00.0-ata-1
		bus, port, err := parsePCIAta(name)
		if err != nil {
			log.Printf("Skipping entry %q: %v", name, err)
			continue
		}

		disks = append(disks, DiskInfo{
			Name:   dev,
			HCTL:   hctlMap[dev],
			Serial: serials[dev],
			Path:   name,
			PCIBus: bus,
			Port:   port,
		})
	}

	// Sort: first by PCI bus, then by ATA port
	sort.Slice(disks, func(i, j int) bool {
		if disks[i].PCIBus != disks[j].PCIBus {
			return disks[i].PCIBus > disks[j].PCIBus
		}
		return disks[i].Port < disks[j].Port
	})

	return disks, nil
}

// parsePCIAta parses a by-path name like "pci-0000:59:00.0-ata-1" and extracts bus address and port number
func parsePCIAta(name string) (string, int, error) {
	parts := strings.Split(name, "-")
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("invalid format")
	}

	var bus string
	var port int
	for i := 0; i < len(parts); i++ {
		if parts[i] == "pci" && i+1 < len(parts) {
			bus = parts[i+1]
		}
		if parts[i] == "ata" && i+1 < len(parts) {
			p, err := strconv.Atoi(parts[i+1])
			if err != nil {
				return "", 0, fmt.Errorf("invalid ata port")
			}
			port = p
		}
	}
	if bus == "" || port == 0 {
		return "", 0, fmt.Errorf("missing pci bus or ata port")
	}
	return bus, port, nil
}

// read disk serials from /run/udev by mapping major:minor to serial
func getBlockDevicesSerials() (map[string]string, error) {
	serials := make(map[string]string)

	blockDir := "/sys/block"
	udevDir := "/run/udev/data"

	entries, err := os.ReadDir(blockDir)
	if err != nil {
		return serials, err
	}

	for _, entry := range entries {
		dev := entry.Name()
		devPath := filepath.Join(blockDir, dev, "dev")
		devNumBytes, err := os.ReadFile(devPath)
		if err != nil {
			continue
		}
		devNum := strings.TrimSpace(string(devNumBytes)) // e.g. "8:0"
		udevFile := filepath.Join(udevDir, "b"+devNum)
		data, err := os.ReadFile(udevFile)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "E:ID_SERIAL_SHORT=") {
				serial := strings.TrimPrefix(line, "E:ID_SERIAL_SHORT=")
				serials[dev] = serial
				break
			}
		}
	}

	return serials, nil
}

func getDiskActivity(devices []string) (map[string]DiskActivity, error) {
	stats := make(map[string]DiskActivity)
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return stats, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		for _, dev := range devices {
			if name == dev {
				reads, _ := strconv.ParseUint(fields[5], 10, 64)  // sectors read
				writes, _ := strconv.ParseUint(fields[9], 10, 64) // sectors written
				stats[dev] = DiskActivity{
					Reads:    reads,
					Writes:   writes,
					Activity: reads + writes,
				}
			}
		}
	}
	return stats, nil
}

func colorForActivity(reads, writes uint64) (r, g, b byte) {
	if reads == 0 && writes == 0 {
		return 0, 0, 0 // off
	}
	total := reads + writes
	if total == 0 {
		return 0, 0, 0
	}
	// Blend: blue for reads, red for writes
	// Ratio: reads/(reads+writes) for blue, writes/(reads+writes) for red
	blue := float64(reads) / float64(total)
	red := float64(writes) / float64(total)
	return byte(red * 255), 0, byte(blue * 255)
}

func brightnessForActivity(activity uint64, maxActivity uint64) byte {
	if maxActivity == 0 {
		return 32 // minimum visible
	}
	// Scale: min 32, max 255
	val := 32 + int(float64(activity)/float64(maxActivity)*223)
	if val > 255 {
		val = 255
	}
	return byte(val)
}

func colorForNetActivity(rx, tx uint64) (r, g, b byte) {
	if rx == 0 && tx == 0 {
		return 0, 0, 0 // off
	}
	total := rx + tx
	if total == 0 {
		return 0, 0, 0
	}
	// Blend: blue for RX, red for TX
	blue := float64(rx) / float64(total)
	red := float64(tx) / float64(total)
	return byte(red * 255), 0, byte(blue * 255)
}

func brightnessForNetActivity(activity, maxActivity uint64) byte {
	if maxActivity == 0 {
		return 32 // minimum visible
	}
	val := 32 + int(float64(activity)/float64(maxActivity)*223)
	if val > 255 {
		val = 255
	}
	return byte(val)
}

// Call this in main for activity monitoring mode
func monitorDiskActivityAndSetLeds(fd int, disks []DiskInfo) {
	// Get initial stats
	devices := []string{}
	for _, disk := range disks {
		devices = append(devices, disk.Name)
	}

	prevStats, _ := getDiskActivity(devices)

	// Find max activity for scaling
	maxActivity := uint64(0)
	maxLanActivity := uint64(0)

	pollMs := 50

	for {
		time.Sleep(time.Duration(pollMs) * time.Millisecond)

		// get Disk activity
		currStats, _ := getDiskActivity(devices)

		deltas := make(map[string]DiskActivity)
		for dev, curr := range currStats {
			prev := prevStats[dev]
			reads := curr.Reads - prev.Reads
			writes := curr.Writes - prev.Writes
			activity := reads + writes
			if activity > maxActivity {
				maxActivity = activity
			}
			deltas[dev] = DiskActivity{Reads: reads, Writes: writes, Activity: activity}
		}

		// Set disk LEDs
		for i, disk := range disks {
			dev := disk.Name
			delta := deltas[dev]
			// fmt.Printf("Disk %s: Reads=%d, Writes=%d, Activity=%d\n", dev, delta.Reads, delta.Writes, delta.Activity)
			r, g, b := colorForActivity(delta.Reads, delta.Writes)
			brightness := brightnessForActivity(delta.Activity, maxActivity)
			if r == 0 && g == 0 && b == 0 {
				// Off
				setLedMode(fd, i+2, 0, nil)
			} else {
				setLedColor(fd, i+2, r, g, b)
				setLedBrightness(fd, i+2, brightness)
				setLedMode(fd, i+2, 1, nil) // on
			}
		}
		prevStats = currStats

		// Get network activity and set lan LED
		rxTotal, txTotal, err := getNetworkActivityAll()
		if err != nil {
			log.Printf("Error reading network activity: %v", err)
			continue
		}
		total := rxTotal + txTotal
		if total > maxLanActivity {
			maxLanActivity = total
		}
		r, g, b := colorForNetActivity(rxTotal, txTotal)
		brightness := brightnessForNetActivity(total, maxLanActivity)
		lanLedID := 1 // "lan" is index 1 in ledNames

		if r == 0 && g == 0 && b == 0 {
			// Off
			setLedMode(fd, lanLedID, 0, nil)
		} else {
			setLedColor(fd, lanLedID, r, g, b)
			setLedBrightness(fd, lanLedID, brightness)
			// Blink: on blinkMs, off blinkMs
			onMs := 100
			offMs := 100
			high := onMs + offMs
			params := []byte{
				byte(high >> 8), byte(high),
				byte(onMs >> 8), byte(onMs),
			}
			setLedMode(fd, lanLedID, 2, params)
		}
	}
}

func getNetworkActivityAll() (rxTotal, txTotal uint64, err error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") {
			continue
		}
		iface := strings.SplitN(line, ":", 2)[0]
		if iface == "lo" || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "docker") {
			continue // skip loopback and common virtuals
		}
		fields := strings.Fields(line[strings.Index(line, ":")+1:])
		if len(fields) < 9 {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		rxTotal += rxBytes
		txTotal += txBytes
	}
	return rxTotal, txTotal, nil
}

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root to access I2C devices.")
	}

	disks, err := discoverDisks()
	if err != nil {
		fmt.Println("Error discovering disks:", err)
		return
	}

	fmt.Printf("Discovered %d Disks:\n", len(disks))
	for i, disk := range disks {
		fmt.Printf("Disk%d: %s (HCTL: %s, Serial: %s)\n", i+1, disk.Name, disk.HCTL, disk.Serial)
	}

	fd, err := syscall.Open("/dev/i2c-0", syscall.O_RDWR, 0600)
	if err != nil {
		log.Fatalf("Failed to open /dev/i2c-0: %v", err)
	}
	defer syscall.Close(fd)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(I2C_SLAVE), uintptr(UGREEN_LED_I2C_ADDR)); errno != 0 {
		log.Fatalf("Failed to set I2C_SLAVE: %v", errno)
	}

	monitorDiskActivityAndSetLeds(fd, disks)
}
