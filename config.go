package main

import (
	"log"
	"time"

	"github.com/devilmonastery/configloader"
)

type Config struct {
	PollInterval     time.Duration `yaml:"poll_interval" default:"100ms"`
	RainbowCycleTime time.Duration `yaml:"rainbow_cycle_time" default:"4s"`
}

func NewConfigLoader(path string) (*configloader.ConfigLoader[Config], error) {
	ret, err := configloader.NewConfigLoader[Config](path)
	if err != nil {
		return nil, err
	}

	ret.RegisterCallback(func(conf Config) (Config, error) {
		if conf.PollInterval < 10 {
			log.Printf("Warning: PollInterval %dms is too low, setting to minimum of 10ms", conf.PollInterval)
			conf.PollInterval = 10
		}
		if conf.PollInterval > 5000 {
			log.Printf("Warning: PollInterval > 5000ms (%d), did you mean that?", conf.PollInterval)
		}
		if conf.RainbowCycleTime <= 0 {
			log.Printf("Warning: RainbowCycleTime %s is too low, setting to minimum of 4s", conf.RainbowCycleTime)
			conf.RainbowCycleTime = 4 * time.Second
		}
		return conf, nil
	})

	return ret, nil
}
