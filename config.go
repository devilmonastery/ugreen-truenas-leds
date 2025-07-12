package main

import (
	"log"
	"time"

	"github.com/devilmonastery/configloader"
)

type Config struct {
	PollInterval time.Duration `yaml:"poll_interval" default:"100"`
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
		return conf, nil
	})

	return ret, nil
}
