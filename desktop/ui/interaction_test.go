package ui

import (
	"testing"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestCompactAudioFitsAndShortFieldsStayInline(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	SetTheme(a)
	w := a.NewWindow("compact")
	defer w.Close()
	settings := launcher.Default()
	settings.Mode = "duplex"
	settings.Output = "RMic Microphone"
	settings.Capture = "RMic Monitor"
	v := buildView(w, nil, t.TempDir(), settings, "")
	w.Resize(fyne.NewSize(720, 480))
	for _, o := range []fyne.CanvasObject{v.form.output, v.form.capture, v.form.quality} {
		p := a.Driver().AbsolutePositionForObject(o)
		if p.Y+o.Size().Height > 480 || p.X+o.Size().Width > 720 {
			t.Fatalf("audio field outside compact viewport: %T %v %v", o, p, o.Size())
		}
	}
	if v.form.quality.Size().Width > 240 {
		t.Fatal("quality consumes a full row")
	}
	v.openAudioOptions()
	if v.form.channels.Size().Width > 80 {
		t.Fatal("channel count consumes a full row")
	}
	channelPos := a.Driver().AbsolutePositionForObject(v.form.channels)
	idlePos := a.Driver().AbsolutePositionForObject(v.form.idle)
	if channelPos.Y+v.form.channels.Size().Height/2 != idlePos.Y+v.form.idle.Size().Height/2 {
		t.Fatal("short audio parameters are not inline")
	}
	v.closeTool()
	v.openPreferences()
	var rowY float32
	for i, o := range []fyne.CanvasObject{v.form.network, v.form.https, v.form.udp} {
		p := a.Driver().AbsolutePositionForObject(o)
		if i == 0 {
			rowY = p.Y + o.Size().Height/2
		}
		if p.Y+o.Size().Height/2 != rowY || p.X+o.Size().Width > 720 {
			t.Fatal("network fields did not fit on one compact row")
		}
	}
	if v.notice.area.Visible() {
		t.Fatal("empty notification reserves space")
	}
	v.notice.SetText("failure")
	if !v.notice.area.Visible() {
		t.Fatal("failure is hidden")
	}
	v.notice.SetText("")
	if v.notice.area.Visible() {
		t.Fatal("cleared notification reserves space")
	}
}
func TestDraftFeedbackSurvivesNavigationAndRefresh(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("drafts")
	defer w.Close()
	settings := launcher.Default()
	settings.Output = "configured"
	v := buildView(w, nil, t.TempDir(), settings, "")
	audio, network := v.scopeBars["audio"], v.scopeBars["network"]
	if audio.content.Visible() || network.content.Visible() {
		t.Fatal("unchanged settings reserve an action row")
	}
	if !audio.apply.Disabled() {
		t.Fatal("unchanged configuration can be submitted")
	}
	v.form.https.SetText("9443")
	v.form.idle.SetText("0")
	v.showTool("设备与授权", "", v.pairingPage(), nil)
	v.refresh(core.Snapshot{Settings: settings})
	if !audio.content.Visible() || !network.content.Visible() {
		t.Fatal("edited settings did not expose actions")
	}
	if audio.apply.Disabled() || network.apply.Disabled() {
		t.Fatal("draft state lost on navigation or refresh")
	}
	v.submitScope("audio")
	if audio.errorText == "" {
		t.Fatal("invalid field did not produce page feedback")
	}
	test.Tap(audio.reset)
	if !audio.apply.Disabled() || network.apply.Disabled() || v.form.https.Text != "9443" {
		t.Fatal("reset crossed configuration scope")
	}
	if audio.content.Visible() || !network.content.Visible() {
		t.Fatal("reset did not collapse only its own actions")
	}
}

func TestWindowGeometryDoesNotChangeTrayPreference(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("geometry")
	defer w.Close()
	dir := t.TempDir()
	if err := saveTraySettings(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := saveAppearance(dir, "深色"); err != nil {
		t.Fatal(err)
	}
	v := buildView(w, nil, dir, launcher.Default(), "")
	w.Resize(fyne.NewSize(900, 600))
	if err := v.saveWindowSize(); err != nil {
		t.Fatal(err)
	}
	w.Resize(fyne.NewSize(720, 480))
	v.restoreWindowSize()
	if w.Canvas().Size().Width != 900 {
		t.Fatal("window size not restored")
	}
	if appearance, err := loadAppearance(dir); err != nil || appearance != "深色" {
		t.Fatal("window geometry damaged appearance settings")
	}
	enabled, err := loadTraySettings(dir)
	if err != nil || !enabled {
		t.Fatal("window geometry damaged tray preference")
	}
}
