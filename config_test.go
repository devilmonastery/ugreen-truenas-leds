package main

import (
	"log"
	"os"
	"testing"
	"time"
)

func TestConfigLoader(t *testing.T) {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	// Write a temporary config file
	f, err := os.CreateTemp("", "testconfig-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("device: /dev/i2c-2\npoll_interval: 123ms\n")
	f.Close()

	loader, err := NewConfigLoader(f.Name())
	if err != nil {
		t.Fatalf("failed to create config loader: %v", err)
	}
	err = loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	cfg := loader.Config()
	if cfg.PollInterval != 123*time.Millisecond {
		t.Errorf("expected PollIntervalMs=123ms, got %v", cfg.PollInterval)
	}
	if cfg.Device != "/dev/i2c-2" {
		t.Errorf("expected Device=/dev/i2c-2, got %q", cfg.Device)
	}
}

func TestEmptyConfig(t *testing.T) {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	loader, err := NewConfigLoader("missing_config.yaml")
	if err != nil {
		t.Fatalf("failed to create config loader: %v", err)
	}

	cfg := loader.Config()
	if cfg == nil {
		t.Fatal("expected non-nil config after loading empty config")
	}

	if cfg.PollInterval != defaultPollInterval {
		t.Errorf("expected PollIntervalMs=100ms, got %v", cfg.PollInterval)
	}
	if cfg.Device != "" {
		t.Errorf("expected Device to be empty for auto-detection, got %q", cfg.Device)
	}
}
