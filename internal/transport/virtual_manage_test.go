package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/virtual"
)

func TestNativeRetainedVirtualDeviceLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("set RMIC_NATIVE_TEST=1 with a Linux audio session")
	}
	first, second := t.TempDir(), t.TempDir()
	manage := func(dir, action string) string {
		t.Helper()
		value, err := virtual.Manage(action, dir, true, true)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	defer virtual.Manage("uninstall", first, true, true)
	defer virtual.Manage("uninstall", second, true, true)
	manage(first, "install")
	if !strings.Contains(manage(first, "status"), "4/4") {
		t.Fatal("installed routes incomplete")
	}
	device, err := virtual.Installed(first)
	if err != nil || device == nil {
		t.Fatalf("cannot reuse installation: %v", err)
	}
	if err = device.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manage(first, "status"), "4/4") {
		t.Fatal("normal application close removed retained devices")
	}
	manage(first, "install")
	manage(second, "uninstall")
	if _, err = virtual.CreateFor(true, true); err == nil {
		t.Fatal("temporary setup took over retained devices")
	}
	statePath := filepath.Join(first, "virtual-devices.json")
	old, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	manage(first, "uninstall")
	if device, err := virtual.Installed(first); err != nil || device != nil {
		t.Fatalf("uninstall did not clear record: %v", err)
	}
	manage(second, "install")
	current, err := os.ReadFile(filepath.Join(second, "virtual-devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Model PulseAudio reusing module IDs after a restart: an old ownership
	// record must not remove a newer installation with the same device names.
	var stale, fresh struct {
		Owner                            string
		Modules                          []struct{ ID, Name, Argument string }
		Output, Input, Playback, Capture string
	}
	if err = json.Unmarshal(old, &stale); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(current, &fresh); err != nil {
		t.Fatal(err)
	}
	if stale.Owner == fresh.Owner {
		t.Fatal("installation owner was reused")
	}
	for i := range stale.Modules {
		stale.Modules[i].ID = fresh.Modules[i].ID
	}
	encoded, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(statePath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = virtual.Installed(first); err == nil {
		t.Fatal("stale ownership record accepted")
	}
	manage(first, "uninstall")
	if !strings.Contains(manage(second, "status"), "4/4") {
		t.Fatal("stale uninstall removed another installation")
	}
}
