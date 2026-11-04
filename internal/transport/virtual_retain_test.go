package transport

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/virtual"
)

func TestNativeRetainRunningTemporaryDevices(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("requires Linux PulseAudio")
	}
	dir := t.TempDir()
	temporary, err := virtual.CreateFor(true, true)
	if err != nil {
		t.Fatal(err)
	}
	manager := virtual.NewManager(dir, true, true, temporary)
	defer manager.Close()
	defer virtual.Manage("uninstall", dir, true, true)
	before, err := virtual.Manage("status", dir, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Run("install"); err != nil {
		t.Fatal(err)
	}
	after, err := manager.Run("status")
	if err != nil {
		t.Fatal(err)
	}
	// Keeping exactly the same module/endpoint IDs verifies that retaining
	// active routes does not destroy or replace the running audio endpoints.
	for _, line := range strings.Split(before, "\n") {
		if strings.Contains(line, "rmic_") && !strings.Contains(after, line) {
			t.Fatalf("retention replaced an active endpoint: %s", line)
		}
	}
	if !strings.Contains(after, "4/4") {
		t.Fatal(after)
	}
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	installed, err := virtual.Installed(dir)
	if err != nil || installed == nil {
		t.Fatalf("retained routes removed on shutdown: %v", err)
	}
	if _, err = manager.Run("install"); err == nil {
		t.Fatal("closed manager allowed a late command")
	}
	// A new TUI instance can uninstall the retained installation.
	next := virtual.NewManager(dir, true, true, nil)
	defer next.Close()
	if _, err = next.Run("uninstall"); err != nil {
		t.Fatal(err)
	}
	if installed, err = virtual.Installed(dir); err != nil || installed != nil {
		t.Fatalf("TUI uninstall left installation behind: %v", err)
	}
}
