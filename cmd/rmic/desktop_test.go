package main

import (
	"reflect"
	"testing"
)

func TestWindowsStartupPreservesCommandLine(t *testing.T) {
	if got := startupArgs("windows", nil); !reflect.DeepEqual(got, []string{"--setup"}) {
		t.Fatal("double click must open setup before audio startup")
	}
	for _, args := range [][]string{{"--version"}, {"--headless", "--mode", "mic", "--device", "CABLE Input"}, {"virtual", "install"}, {"--setup", "--config-dir", "custom"}} {
		if got := startupArgs("windows", args); !reflect.DeepEqual(got, args) {
			t.Fatalf("explicit invocation changed: %q", got)
		}
	}
	if got := startupArgs("linux", nil); len(got) != 0 {
		t.Fatal("Linux default startup changed")
	}
}
