package ui

import (
	"testing"
	"time"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSettingsDraftValidationAndCancel(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	defer w.Close()
	s := launcher.Default()
	s.Output = "saved-exact-device"
	v := buildView(w, nil, t.TempDir(), s, "")
	selectExact(v.form.output, "new-device")
	if v.saved.Output != "saved-exact-device" {
		t.Fatal("draft changed saved settings")
	}
	v.form.load(v.saved)
	if v.form.output.Selected != s.Output {
		t.Fatal("cancel did not restore saved device")
	}
	v.form.idle.SetText("0")
	if _, err := v.form.scopedValue(s, "audio"); err == nil {
		t.Fatal("invalid idle timeout accepted")
	}
	v.form.idle.SetText("12")
	if value, err := v.form.scopedValue(s, "audio"); err != nil || value.AudioIdleSeconds != 12 {
		t.Fatalf("idle timeout draft: %+v %v", value, err)
	}
	v.form.https.SetText("not-a-port")
	if _, err := v.form.scopedValue(s, "network"); err == nil {
		t.Fatal("invalid port accepted")
	}
	v.form.load(s)
	v.form.quality.SetSelected(qualityLabel("ultra"))
	if _, err := v.form.scopedValue(s, "audio"); err == nil {
		t.Fatal("ultra microphone mode accepted")
	}
	v.form.mode.SetSelected(modeLabel(audio.ModePlayback))
	v.form.defaultMode.SetSelected(modeLabel(audio.ModePlayback))
	v.form.channels.SetSelected("8")
	value, err := v.form.scopedValue(s, "audio")
	if err != nil || value.Channels() != 8 {
		t.Fatalf("playback settings: %+v %v", value, err)
	}
}

func TestPairRequestDialogKeepsIdentityAfterRefresh(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	defer w.Close()
	v := buildView(w, nil, t.TempDir(), launcher.Default(), "")
	old := pairing.Request{ID: "old", Name: "first", Code: "111111", Expires: time.Now().Add(-time.Second)}
	v.refresh(core.Snapshot{Requests: []pairing.Request{old}})
	button := v.requests.Objects[0].(*widget.Button)
	test.Tap(button)
	v.refresh(core.Snapshot{Requests: []pairing.Request{{ID: "new", Name: "second", Code: "222222", Expires: time.Now().Add(time.Minute)}}})
	// The old modal remains bound to its request. Its approval must reject the
	// expired request without calling the backend or approving the new item.
	overlays := w.Canvas().Overlays().List()
	if len(overlays) == 0 {
		t.Fatal("pairing dialog not shown")
	}
	var approve *widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "仅本次允许" {
			approve = b
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if b, ok := o.(*widget.PopUp); ok {
			walk(b.Content)
		}
	}
	for _, o := range overlays {
		walk(o)
	}
	if approve == nil {
		t.Fatal("approval button missing")
	}
	if !approve.Disabled() {
		t.Fatal("expired request approval remained enabled after refresh")
	}
}

func TestBusyStateAndOfflineQR(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	defer w.Close()
	v := buildView(w, nil, t.TempDir(), launcher.Default(), "")
	if v.start.Disabled() || !v.qr.Disabled() || !v.stop.Disabled() {
		t.Fatal("incorrect stopped actions")
	}
	v.showQR()
	if len(w.Canvas().Overlays().List()) != 0 {
		t.Fatal("offline QR displayed")
	}
	v.busy = true
	v.updateActions()
	if !v.start.Disabled() {
		t.Fatal("start enabled during command")
	}
	called := false
	v.perform("duplicate", func() error { called = true; return nil }, nil)
	if called {
		t.Fatal("duplicate command executed")
	}
}
