package main

import (
	"os"
	"testing"
	"time"
)

func TestConfigLoader(t *testing.T) {
	// Write a temporary config file
	f, err := os.CreateTemp("", "testconfig-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString("poll_interval: 123\n")
	f.Close()

	loader, err := NewConfigLoader(f.Name())
	if err != nil {
		t.Fatalf("failed to create config loader: %v", err)
	}
	err = loader.Load(f.Name())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	cfg := loader.Config()
	if cfg.PollIntervalMs != 123*time.Millisecond {
		t.Errorf("expected PollIntervalMs=123ms, got %v", cfg.PollIntervalMs)
	}
}
