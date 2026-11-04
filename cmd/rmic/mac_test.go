package main

import (
	"reflect"
	"testing"
)

func TestMacStartupOpensConfiguration(t *testing.T) {
	if got := startupArgs("darwin", nil); !reflect.DeepEqual(got, []string{"--setup"}) {
		t.Fatalf("mac startup: %v", got)
	}
	args := []string{"--headless", "--mode", "mic"}
	if got := startupArgs("darwin", args); !reflect.DeepEqual(got, args) {
		t.Fatalf("explicit args changed: %v", got)
	}
}
