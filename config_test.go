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
	f.WriteString("poll_interval: 123ms\n")
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
}
