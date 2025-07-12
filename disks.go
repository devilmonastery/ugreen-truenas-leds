package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
