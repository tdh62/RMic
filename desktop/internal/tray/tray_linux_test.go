//go:build linux

package tray

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type testWatcher struct{ registered chan string }

func (w testWatcher) RegisterStatusNotifierItem(sender dbus.Sender, path string) *dbus.Error {
	w.registered <- string(sender)
	return nil
}

func TestTrayHelperProcess(t *testing.T) {
	if os.Getenv("RMIC_TRAY_TEST_HELPER") != "1" {
		return
	}
	if err := Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Run with dbus-run-session and RMIC_TRAY_INTEGRATION=1. A private watcher
// observes real registration/removal without depending on a desktop panel.
func TestNativeTrayLifecycle(t *testing.T) {
	if os.Getenv("RMIC_TRAY_INTEGRATION") != "1" {
		t.Skip("requires a private D-Bus session")
	}
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	reply, err := bus.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("private watcher: %v %v", reply, err)
	}
	watcher := testWatcher{registered: make(chan string, 4)}
	if err := bus.Export(watcher, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTrayHelperProcess$")
		cmd.Env = append(os.Environ(), "RMIC_TRAY_TEST_HELPER=1")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		ready := make(chan struct{})
		go func() {
			scanner := bufio.NewScanner(output)
			for scanner.Scan() {
				if scanner.Text() == "ready" {
					close(ready)
					return
				}
			}
		}()
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			t.Fatal("helper did not become ready")
		}
		select {
		case <-watcher.registered:
		case <-time.After(5 * time.Second):
			t.Fatal("tray never registered")
		}
		// Closing the parent's pipe is also what happens when the GUI crashes.
		_ = input.Close()
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case err := <-exited:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("helper survived parent pipe closure")
		}
		var owned bool
		name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", cmd.Process.Pid)
		if err := bus.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned); err != nil {
			t.Fatal(err)
		}
		if owned {
			t.Fatal("tray remains registered after opt-out")
		}
	}
}
