package main

import (
	"strings"
	"testing"
)

func TestInvalidDefaultModeFailsBeforeAudioStartup(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		message string
	}{
		{"unavailable direction", []string{"--mode", "mic", "--default-mode", "playback"}, "--default-mode"},
		{"invalid mode", []string{"--mode", "duplex", "--default-mode", "invalid"}, "--default-mode"},
		{"ultra microphone", []string{"--mode", "mic", "--quality", "ultra"}, "ultra"},
		{"ultra duplex session", []string{"--mode", "duplex", "--default-mode", "duplex", "--quality", "ultra"}, "ultra"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--headless", "--device", "missing-device", "--config-dir", t.TempDir()}, test.args...)
			err := runArgs(args, false)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.message) {
				t.Fatalf("invalid default reached device startup: %v", err)
			}
		})
	}
}
