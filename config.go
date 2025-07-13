package main

import (
	"log"
	"time"

	"github.com/devilmonastery/configloader"
)

const (
	defaultPollInterval = 100 * time.Millisecond
	minPollInterval     = 10 * time.Millisecond
	maxPollInterval     = 5000 * time.Millisecond

	defaultRainbowCycleTime = 4 * time.Second
	minRainbowCycleTime     = 1 * time.Second
	maxRainbowCycleTime     = 10 * time.Second
)

type Config struct {
	PollInterval      time.Duration `yaml:"poll_interval"`
	RainbowCycleTime  time.Duration `yaml:"rainbow_cycle_time"`
	EnableRainbow     *bool         `yaml:"enable_rainbow"`
	RainbowBrightness *byte         `yaml:"rainbow_brightness"`
}

func NewConfigLoader(path string) (*configloader.ConfigLoader[Config], error) {
	ret, err := configloader.NewConfigLoader[Config](path)
	if err != nil {
		return nil, err
	}

	// Register a callback to validate and set defaults
	ret.RegisterCallback(func(conf Config) (Config, error) {
		log.Printf("Loaded config: %+v", conf)

		if conf.PollInterval <= 0 {
			conf.PollInterval = defaultPollInterval
			log.Printf("Warning: PollInterval unset, using %s", conf.PollInterval)
		}
		if conf.PollInterval < minPollInterval {
			log.Printf("Warning: PollInterval %s too low, using %s", conf.PollInterval, minPollInterval)
			conf.PollInterval = minPollInterval
		}
		if conf.PollInterval > maxPollInterval {
			log.Printf("Warning: PollInterval %s too high, using %s", conf.PollInterval, maxPollInterval)
			conf.PollInterval = maxPollInterval
		}

		if conf.RainbowCycleTime <= 0 {
			conf.RainbowCycleTime = defaultRainbowCycleTime
			log.Printf("Warning: RainbowCycleTime unset, using %s", conf.RainbowCycleTime)
		}
		if conf.RainbowCycleTime < minRainbowCycleTime {
			log.Printf("Warning: RainbowCycleTime %s too low, using %s", conf.RainbowCycleTime, minRainbowCycleTime)
			conf.RainbowCycleTime = minRainbowCycleTime
		}
		if conf.RainbowCycleTime > maxRainbowCycleTime {
			log.Printf("Warning: RainbowCycleTime %s too high, using %s", conf.RainbowCycleTime, maxRainbowCycleTime)
			conf.RainbowCycleTime = maxRainbowCycleTime
		}

		if conf.EnableRainbow == nil {
			log.Printf("Warning: enable_rainbow unset, defaulting to true")
			v := true
			conf.EnableRainbow = &v
		}

		if conf.RainbowBrightness == nil {
			log.Printf("Warning: rainbow_brightness unset, defaulting to 48")
			v := byte(48)
			conf.RainbowBrightness = &v
		}

		return conf, nil
	})

	ret.Start()
	return ret, nil
}
