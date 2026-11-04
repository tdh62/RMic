package ui

import (
	"errors"
	"reflect"
	"testing"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
)

func TestToolWindowsKeepDraftsAndReturnFocus(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("tools")
	defer w.Close()
	w.Resize(fyne.NewSize(720, 480))
	saved := launcher.Default()
	saved.Output = "saved output"
	v := buildView(w, nil, t.TempDir(), saved, "")
	selectExact(v.form.output, "audio draft")
	v.openPreferences()
	v.form.https.SetText("9443")
	w.Canvas().Focus(v.form.https)
	v.openDrivers()
	v.openPreferences()
	if w.Canvas().Focused() != v.form.https || v.form.https.Text != "9443" {
		t.Fatal("return from drivers lost focus or network draft")
	}
	v.tool.popup.Tapped(&fyne.PointEvent{Position: fyne.NewPos(1, 1)})
	if v.tool == nil {
		t.Fatal("outside click dismissed a tool")
	}
	nested := dialog.NewInformation("nested", "keep parent", w)
	nested.Show()
	v.handleToolEscape()
	if v.tool == nil {
		t.Fatal("Escape closed the parent behind another dialog")
	}
	nested.Hide()
	v.handleToolEscape()
	if v.tool != nil || v.form.https.Text != "9443" || v.saved.HTTPSPort != saved.HTTPSPort {
		t.Fatal("Escape consumed the network draft")
	}
	v.openPreferences()
	test.Tap(v.tool.cancel)
	if v.form.https.Text != "8443" || v.form.output.Selected != "audio draft" {
		t.Fatal("cancel crossed configuration scopes")
	}
}

func TestSignalControlsStayCompactAndMetricsExpire(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	SetTheme(a)
	w := a.NewWindow("signals")
	defer w.Close()
	saved := launcher.Default()
	saved.Mode = "duplex"
	saved.Output = "mic"
	saved.Capture = "capture"
	v := buildView(w, nil, t.TempDir(), saved, "")
	s := core.Snapshot{Running: true, Settings: saved, Status: transport.Status{State: "streaming", MetricsAgeMS: 100, Playback: transport.PlaybackMetrics{RTTMS: 8}, Receive: transport.Direction{Device: "mic", State: "streaming", Gain: 1}, Send: transport.Direction{Device: "capture", State: "streaming", Gain: 1}}}
	for _, width := range []float32{720, 820, 1100} {
		w.Resize(fyne.NewSize(width, 540))
		v.refresh(s)
		for _, c := range []*directionControl{v.receiveControl, v.sendControl} {
			if c.gain.Size().Width < 100 || c.gain.Size().Width > 120 {
				t.Fatalf("gain stretched to %v", c.gain.Size())
			}
			meter := a.Driver().AbsolutePositionForObject(c.meter)
			gain := a.Driver().AbsolutePositionForObject(c.gain)
			if delta := (meter.Y + c.meter.Size().Height/2) - (gain.Y + c.gain.Size().Height/2); delta < -1 || delta > 1 {
				t.Fatalf("meter/slider centers differ: %v", delta)
			}
			if c.latency.Text != "往返 8 ms" {
				t.Fatal("fresh RTT missing")
			}
		}
	}
	v.form.mode.SetSelected(modeLabel("playback"))
	v.refresh(s)
	if !v.receiveControl.card.Visible() || v.receiveControl.status.Text != "待应用" {
		t.Fatal("unapplied mode change hid the active microphone")
	}
	s.Status.MetricsAgeMS = 6000
	v.refresh(s)
	if v.sendControl.latency.Text != "往返 —" {
		t.Fatal("stale RTT was shown as current")
	}
	s.Status.State = "waiting"
	v.refresh(s)
	if v.receiveControl.signal.Visible() {
		t.Fatal("inactive signal controls reserve space")
	}
}

type queueBackend struct {
	Backend
	calls                  []string
	stopError, driverError error
	infos                  []virtual.DriverInfo
}

func (b *queueBackend) Stop() error { b.calls = append(b.calls, "stop"); return b.stopError }
func (b *queueBackend) Driver(action string) (string, error) {
	b.calls = append(b.calls, action)
	return "installer exited", b.driverError
}
func (b *queueBackend) InspectDrivers() []virtual.DriverInfo {
	b.calls = append(b.calls, "inspect")
	return b.infos
}
func TestDriverQueueStopsBeforeMutationAndInspectsActualResult(t *testing.T) {
	providers := []virtual.DriverInfo{{Name: "Steam Streaming", Prefix: "steam-"}, {Name: "CABLE-A", Prefix: "cable-a-"}}
	for _, scenario := range []struct {
		name                   string
		stopError, driverError error
		calls                  []string
	}{
		{"success", nil, nil, []string{"stop", "steam-install", "cable-a-install", "inspect"}},
		{"cannot stop", errors.New("stop failed"), nil, []string{"stop"}},
		{"UAC cancelled", nil, errors.New("cancelled"), []string{"stop", "steam-install", "inspect"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			backend := &queueBackend{stopError: scenario.stopError, driverError: scenario.driverError, infos: []virtual.DriverInfo{{Name: "Steam Streaming", Complete: false}}}
			result, err := executeDriverQueue(backend, "install", providers, func(string, int) {})
			if !reflect.DeepEqual(backend.calls, scenario.calls) {
				t.Fatalf("operations: %v", backend.calls)
			}
			if !errors.Is(err, scenario.stopError) || !errors.Is(result.failed, scenario.driverError) {
				t.Fatalf("errors: %v / %v", err, result.failed)
			}
			if len(result.infos) > 0 && result.infos[0].Complete {
				t.Fatal("installer exit fabricated an installed device")
			}
		})
	}
}

func TestSetupRequiresExplicitAvailableEndpoints(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("first use")
	defer w.Close()
	w.Resize(fyne.NewSize(720, 480))
	v := buildView(w, nil, t.TempDir(), launcher.Default(), "")
	v.openSetup()
	if !v.setupNext.Disabled() {
		t.Fatal("undetected drivers enable next")
	}
	v.acceptDriverInfo([]virtual.DriverInfo{{Name: v.driverItems[0].info.Name, Installed: true, Complete: true}})
	if v.setupNext.Disabled() {
		t.Fatal("existing driver cannot skip installation")
	}
	v.deviceList = core.Devices{Outputs: []string{"RMic_Virtual_Output"}, Captures: []string{"RMic_Computer_Capture"}}
	v.form.output.SetOptions(v.deviceList.Outputs)
	v.form.capture.SetOptions(v.deviceList.Captures)
	v.setupStep = 1
	v.renderSetup()
	if v.form.output.Selected != "" || !v.setupNext.Disabled() {
		t.Fatal("guide silently chose an endpoint")
	}
	v.setupOutput.SetSelected("RMic_Virtual_Output")
	if v.form.output.Selected != "RMic_Virtual_Output" || v.setupNext.Disabled() {
		t.Fatal("explicit route did not enable startup")
	}
	v.deviceList.Outputs = nil
	v.updateSetupActions()
	if !v.setupNext.Disabled() || v.form.output.Selected != "RMic_Virtual_Output" {
		t.Fatal("lost endpoint fell back or stayed startable")
	}
}

func TestSuggestedWindowsRoutesNeverInventOrChooseAmbiguousEndpoints(t *testing.T) {
	devices := core.Devices{Windows: true, Outputs: []string{"Speakers (Steam Streaming Microphone)"}, Captures: []string{"loopback:Speakers (Steam Streaming Speakers)"}}
	output, capture := suggestedRoutes(devices)
	if output != devices.Outputs[0] || capture != devices.Captures[0] {
		t.Fatalf("independent route not found: %q / %q", output, capture)
	}
	devices.Outputs = append(devices.Outputs, "Second (Steam Streaming Microphone)")
	if output, _ := suggestedRoutes(devices); output != "" {
		t.Fatal("ambiguous microphones were guessed")
	}
	devices.Outputs = []string{"Speakers (Realtek)"}
	devices.Captures = nil
	if output, capture := suggestedRoutes(devices); output != "" || capture != "" {
		t.Fatal("a physical default or missing name was invented")
	}
}
