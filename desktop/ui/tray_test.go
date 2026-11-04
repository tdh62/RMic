package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

type fakeTray struct{ stopped bool }

func (f *fakeTray) Stop() { f.stopped = true }

type trackedWindow struct {
	fyne.Window
	hidden bool
}

func (w *trackedWindow) Hide() { w.hidden = true; w.Window.Hide() }
func (w *trackedWindow) Show() { w.hidden = false; w.Window.Show() }

func TestTrayOptInDisableAndRecovery(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &trackedWindow{Window: a.NewWindow("tray")}
	defer w.Close()
	dir := t.TempDir()
	v := buildView(w, nil, dir, launcher.Default(), "")
	defer v.CloseTray()
	if v.tray != nil || v.trayCheck.Checked || !v.trayHide.Disabled() {
		t.Fatal("tray attached without opt-in")
	}
	if v.notice.Visible() {
		t.Fatal("empty notice consumes layout space")
	}
	var p *fakeTray
	starts := 0
	v.startTray = func(func(string)) (trayProcess, error) { starts++; p = &fakeTray{}; return p, nil }
	v.hideToTray()
	if w.hidden {
		t.Fatal("window hidden without a ready tray")
	}
	for i := 0; i < 2; i++ {
		v.setTray(true)
		generation := v.trayGeneration
		v.hideToTray()
		if w.hidden {
			t.Fatal("window hidden before tray ready")
		}
		v.trayEvent(generation, "ready")
		enabled, err := loadTraySettings(dir)
		if err != nil || !enabled {
			t.Fatalf("opt-in not persisted: %v", err)
		}
		v.hideToTray()
		if !w.hidden {
			t.Fatal("minimize did not hide window")
		}
		v.trayEvent(generation, "disable")
		if !p.stopped || w.hidden || v.tray != nil || v.trayCheck.Checked {
			t.Fatal("disable left tray attached or window hidden")
		}
		enabled, err = loadTraySettings(dir)
		if err != nil || enabled {
			t.Fatalf("opt-out not persisted: %v", err)
		}
		v.trayEvent(generation, "ready")
		if v.trayReady {
			t.Fatal("late ready revived a disabled tray")
		}
	}
	if starts != 2 {
		t.Fatal("tray cannot be re-enabled")
	}
	v.setTray(true)
	v.trayEvent(v.trayGeneration, "ready")
	v.hideToTray()
	v.trayEvent(v.trayGeneration, "stopped")
	if w.hidden || v.trayReady || !p.stopped {
		t.Fatal("tray crash stranded hidden window")
	}
	v.setTray(true)
	v.trayEvent(v.trayGeneration, "timeout")
	if w.hidden || v.trayReady || !p.stopped || v.trayCheck.Checked {
		t.Fatal("tray startup timeout left an inaccessible window or helper")
	}
}

func TestTrayErrorsKeepWindowAccessible(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &trackedWindow{Window: a.NewWindow("tray errors")}
	defer w.Close()
	dir := t.TempDir()
	v := buildView(w, nil, dir, launcher.Default(), "")
	defer v.CloseTray()
	v.startTray = func(func(string)) (trayProcess, error) { return nil, errors.New("launch failed") }
	v.setTray(true)
	if v.trayCheck.Checked || v.trayReady || !v.notice.Visible() {
		t.Fatal("launch failure not handled")
	}
	p := &fakeTray{}
	v.startTray = func(func(string)) (trayProcess, error) { return p, nil }
	v.setTray(true)
	v.trayEvent(v.trayGeneration, "ready")
	v.hideToTray()
	// A settings write failure must never prevent removal of a running tray.
	v.dir = filepath.Join(dir, "missing")
	v.setTray(false)
	if !p.stopped || w.hidden || v.tray != nil {
		t.Fatal("settings error prevented opt-out")
	}
	if err := os.WriteFile(filepath.Join(dir, "gui.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTraySettings(dir); err == nil {
		t.Fatal("corrupt preferences accepted")
	}
}
