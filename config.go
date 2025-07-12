package main

import (
	"time"

	"github.com/devilmonastery/configloader"
)

type Config struct {
	PollIntervalMs time.Duration `yaml:"poll_interval" default:"50"`
}

func NewConfigLoader(path string) (*configloader.ConfigLoader[Config], error) {
	return configloader.NewConfigLoader[Config](path)
}
