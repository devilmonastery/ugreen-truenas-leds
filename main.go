package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/devilmonastery/configloader"
)

var (
	confFile = flag.String("config", "config.yaml", "path to the config file")
)

// ActivityMonitor encapsulates disk and network activity monitoring and LED control
type ActivityMonitor struct {
	disks          []DiskInfo
	leds           *UGreenLeds
	maxActivity    uint64
	maxLanActivity uint64
	configLoader   *configloader.ConfigLoader[Config]
}

func NewActivityMonitor(configPath string) (*ActivityMonitor, error) {

	configLoader, err := NewConfigLoader(configPath)
	if err != nil {
		log.Fatalf("error reading config at %q: %v", *confFile, err)
	}

	disks, err := discoverDisks()
	if err != nil {
		return nil, fmt.Errorf("error discovering disks: %v", err)
	}

	leds, err := NewUGreenLeds()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LEDs: %v", err)
	}

	return &ActivityMonitor{
		configLoader: configLoader,
		disks:        disks,
		leds:         leds,
	}, nil
}

func (am *ActivityMonitor) Close() {
	if am.leds != nil {
		am.leds.Close()
		am.leds = nil
	}
}

func (am *ActivityMonitor) brightnessForActivity(activity uint64, maxActivity uint64) byte {
	if maxActivity < activity {
		maxActivity = activity
	}

	if activity == 0 {
		return 0 // No activity, no brightness
	}

	val := 127 + int(float64(activity)/float64(maxActivity)*128)
	if val > 255 {
		val = 255
	}
	return byte(val)
}

func (am *ActivityMonitor) Monitor() {
	conf := am.configLoader.Config()
	subscriber := am.configLoader.Subscribe()

	ticker := time.NewTicker(conf.PollInterval * time.Millisecond)
	defer ticker.Stop()

	devices := []string{}
	for _, disk := range am.disks {
		devices = append(devices, disk.Name)
	}

	prevStats, _ := getDiskActivity(devices)
	lastRxTotal, lastTxTotal, err := am.getNetworkActivityAll()
	if err != nil {
		log.Printf("Error reading network activity: %v", err)
	}

	for {
		select {
		case newconf := <-subscriber:
			conf = &newconf
			log.Printf("new config, poll_interval=%v", conf.PollInterval*time.Millisecond)
			ticker.Reset(conf.PollInterval * time.Millisecond)
		case <-ticker.C:

			// Set Disk activity lights
			currStats, _ := getDiskActivity(devices)
			deltas := make(map[string]DiskActivity)
			for dev, curr := range currStats {
				prev := prevStats[dev]
				reads := curr.Reads - prev.Reads
				writes := curr.Writes - prev.Writes
				activity := reads + writes
				if activity > am.maxActivity {
					am.maxActivity = activity
				}
				deltas[dev] = DiskActivity{Reads: reads, Writes: writes, Activity: activity}
				//log.Printf("deltas for %s: activity:%d max:%d, bright:%d", dev, activity, am.maxActivity, am.brightnessForActivity(activity, am.maxActivity))
			}
			for i, disk := range am.disks {
				am.leds.SetLedColor(i+2, 255, 255, 255)
				dev := disk.Name
				delta := deltas[dev]
				brightness := am.brightnessForActivity(delta.Activity, am.maxActivity)
				if brightness == 0 {
					am.leds.SetLedMode(i+2, LedModeOff, nil)
				} else {
					// am.leds.SetLedColor(i+2, r, g, b)
					am.leds.SetLedBrightness(i+2, brightness)
					am.leds.SetLedMode(i+2, LedModeOn, nil)
				}
			}
			prevStats = currStats

			// Set Network activity lights
			rxTotal, txTotal, err := am.getNetworkActivityAll()
			if err != nil {
				log.Printf("Error reading network activity: %v", err)
				continue
			}
			rxDelta := rxTotal - lastRxTotal
			lastRxTotal = rxTotal
			txDelta := txTotal - lastTxTotal
			lastTxTotal = txTotal

			total := rxDelta + txDelta
			if total > am.maxLanActivity {
				am.maxLanActivity = total
			}

			brightness := am.brightnessForActivity(total, am.maxLanActivity)
			lanLedID := 1 // "lan" is index 1 in ledNames
			//log.Printf("deltas for net: activity:%d max:%d, bright:%d", total, am.maxLanActivity, brightness)

			if brightness == 0 {
				am.leds.SetLedMode(lanLedID, LedModeOff, nil)
			} else {
				// am.leds.SetLedColor(lanLedID, r, g, b)
				am.leds.SetLedColor(lanLedID, 255, 255, 255)
				am.leds.SetLedBrightness(lanLedID, brightness)
				// Blink: on blinkMs, off blinkMs
				onMs := 100
				offMs := 100
				high := onMs + offMs
				params := []byte{
					byte(high >> 8), byte(high),
					byte(onMs >> 8), byte(onMs),
				}
				am.leds.SetLedMode(lanLedID, LedModeBlink, params)
			}
		}
	}
}

func (am *ActivityMonitor) getNetworkActivityAll() (rxTotal, txTotal uint64, err error) {
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
	flag.Parse()
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	am, err := NewActivityMonitor(*confFile)
	if err != nil {
		log.Fatalf("Failed to create ActivityMonitor: %v", err)
	}
	fmt.Printf("Discovered %d Disks:\n", len(am.disks))
	for i, disk := range am.disks {
		fmt.Printf("Disk%d: %s (HCTL: %s, Serial: %s Path:%s)\n", i+1, disk.Name, disk.HCTL, disk.Serial, disk.Path)
	}
	log.Println("Starting activity monitoring...")
	am.Monitor()
}
