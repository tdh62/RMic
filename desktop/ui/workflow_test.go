package ui

import (
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"cnb.cool/tdh6/RMic/internal/transport"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestSettingsScopesDoNotConsumeOtherDrafts(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("RMic")
	defer w.Close()
	saved := launcher.Default()
	saved.Output = "saved output"
	v := buildView(w, nil, t.TempDir(), saved, "")
	v.form.https.SetText("invalid network draft")
	v.form.shortcuts = func() (shortcuts.Config, error) { return shortcuts.Config{}, errors.New("invalid shortcut draft") }
	selectExact(v.form.output, "new output")
	audio, err := v.form.scopedValue(saved, "audio")
	if err != nil || audio.Output != "new output" || audio.HTTPSPort != saved.HTTPSPort {
		t.Fatalf("audio scope consumed other drafts: %+v %v", audio, err)
	}
	v.form.idle.SetText("bad audio draft")
	v.form.https.SetText("9443")
	network, err := v.form.scopedValue(saved, "network")
	if err != nil || network.Output != saved.Output || network.HTTPSPort != 9443 {
		t.Fatalf("network scope consumed audio draft: %+v %v", network, err)
	}
	v.form.shortcuts = func() (shortcuts.Config, error) { return saved.Shortcuts, nil }
	shortcut, err := v.form.scopedValue(saved, "shortcuts")
	if err != nil || shortcut.Output != saved.Output || shortcut.HTTPSPort != saved.HTTPSPort {
		t.Fatalf("shortcut scope consumed other drafts: %+v %v", shortcut, err)
	}
}

type previewTheme struct{ fyne.Theme }

func (t previewTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Theme.Color(name, theme.VariantLight)
}

func shortcutsButtonForPreview() shortcuts.Button {
	return shortcuts.Button{Name: "复制并输入", Mode: "tap", Action: "CtrlLeft+C\nwait:100\ntext:收到"}
}
func TestHomeWorkflowAndVisualStates(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	SetTheme(a)
	w := a.NewWindow("RMic")
	defer w.Close()
	w.Resize(fyne.NewSize(720, 480))
	saved := launcher.Default()
	v := buildView(w, nil, t.TempDir(), saved, "")
	test.Tap(v.start)
	if v.tool == nil || v.tool.name != "首次使用" {
		t.Fatal("first use did not open device preparation")
	}
	v.closeTool()
	capture := func(name string, sizes ...fyne.Size) {
		t.Helper()
		w.Resize(fyne.NewSize(1001, 641))
		size := fyne.NewSize(720, 480)
		if len(sizes) > 0 {
			size = sizes[0]
		}
		w.Resize(size)
		var checkBounds func(fyne.CanvasObject)
		checkBounds = func(o fyne.CanvasObject) {
			if !o.Visible() {
				return
			}
			if o.Size().Width < 0 || o.Size().Height < 0 {
				t.Fatalf("%s negative object %T size %v", name, o, o.Size())
			}
			if c, ok := o.(*fyne.Container); ok {
				for _, child := range c.Objects {
					checkBounds(child)
				}
			}
			if widget, ok := o.(fyne.Widget); ok {
				for _, child := range test.WidgetRenderer(widget).Objects() {
					checkBounds(child)
				}
			}
		}
		checkBounds(w.Content())
		dir := os.Getenv("RMIC_GUI_SCREENSHOTS")
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	capture("welcome")
	saved.Output = "RMic Microphone"
	v.saved = saved
	v.form.loadAudio(saved)
	v.scopeBars["audio"].baseline = v.scopeDraft("audio")
	s := core.Snapshot{Running: true, Settings: saved, URLs: []string{"https://192.168.1.10:8443"}, Status: transport.Status{State: "waiting"}}
	v.refresh(s)
	if v.start.Visible() || !v.stop.Visible() || !v.disconnect.Disabled() {
		t.Fatal("waiting actions incorrect")
	}
	s.Status.Receive = transport.Direction{Device: "Configured output", Gain: 1, DeviceState: "released"}
	s.Status.Send = transport.Direction{Device: "Configured capture", Gain: 1, DeviceState: "released"}
	v.refresh(s)
	if !v.receiveControl.card.Visible() || !v.receiveControl.gain.Disabled() {
		t.Fatal("waiting screen must retain route summary with disabled controls")
	}
	capture("connect")
	s.Status.Send = transport.Direction{}
	s.Status.State = "streaming"
	s.Status.Receive = transport.Direction{Device: "RMic Microphone", State: "streaming", DeviceState: "open", Gain: 1}
	v.refresh(s)
	if !v.receiveControl.card.Visible() || v.receiveControl.gain.Disabled() || v.disconnect.Disabled() {
		t.Fatal("streaming controls unavailable")
	}
	if v.sendControl.card.Visible() {
		t.Fatal("inactive direction shown")
	}
	capture("streaming")
	saved.Mode = "duplex"
	saved.Capture = "A very long explicit computer capture endpoint name"
	v.saved = saved
	v.form.loadAudio(saved)
	v.scopeBars["audio"].baseline = v.scopeDraft("audio")
	s.Settings = saved
	s.Status.Send = transport.Direction{Device: "A very long explicit computer capture endpoint name", State: "streaming", DeviceState: "open", Gain: 1}
	v.refresh(s)
	for i := 0; i < 30; i++ {
		s.Devices = append(s.Devices, pairing.Device{ID: fmt.Sprint(i), Name: fmt.Sprintf("Phone %d", i), Remember: true})
	}
	s.Requests = []pairing.Request{{ID: "pending", Name: "new phone", Code: "123456", Expires: time.Now().Add(time.Minute)}}
	v.refresh(s)
	capture("duplex")
	for _, control := range []*directionControl{v.receiveControl, v.sendControl} {
		for _, object := range []fyne.CanvasObject{control.card, control.gain, control.mute} {
			position := a.Driver().AbsolutePositionForObject(object)
			if position.Y+object.Size().Height > 480 || position.X+object.Size().Width > 720 {
				t.Fatalf("home control outside compact window: %T pos=%v size=%v", object, position, object.Size())
			}
		}
	}
	v.openShortcut(0)
	shortcuts := v.saved.Shortcuts
	shortcuts.Enabled = true
	shortcuts.Buttons[0] = shortcutsButtonForPreview()
	v.form.resetShortcuts(shortcuts)
	capture("shortcuts")
	v.closeTool()
	v.busy = true
	v.updateActions()
	if !v.receiveControl.gain.Disabled() || !v.disconnect.Disabled() {
		t.Fatal("controls active during lifecycle change")
	}
	v.busy = false
	v.updateActions()
	v.form.loadAudio(v.saved)
	capture("audio")
	v.openAudioOptions()
	capture("audio-options")
	v.closeTool()
	v.openPreferences()
	capture("settings")
	v.openDrivers()
	capture("drivers")
	v.closeTool()
	v.openSetup()
	capture("first-use")
	v.setupStep = 1
	v.renderSetup()
	capture("first-use-devices")
	v.closeTool()
	for _, size := range []fyne.Size{fyne.NewSize(820, 540), fyne.NewSize(1100, 720)} {
		capture(fmt.Sprintf("workspace-%dx%d", int(size.Width), int(size.Height)), size)
		v.openPreferences()
		capture(fmt.Sprintf("settings-%dx%d", int(size.Width), int(size.Height)), size)
		v.closeTool()
	}
	a.Settings().SetTheme(previewTheme{a.Settings().Theme()})
	capture("light-workspace")
	v.openPreferences()
	capture("light-settings")
	v.closeTool()
}
func TestDeviceSelectionOpensMenuAndPreservesExplicitIdentity(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("selection")
	defer w.Close()
	s := launcher.Default()
	s.Output = "saved unavailable device"
	v := buildView(w, nil, t.TempDir(), s, "")
	v.form.output.SetOptions([]string{"available device"})
	if v.form.output.Selected != s.Output {
		t.Fatal("refresh replaced missing selection")
	}
	test.Tap(v.form.output)
	if len(w.Canvas().Overlays().List()) == 0 {
		t.Fatal("device click did not open selection menu")
	}
	if v.form.output.Selected != s.Output {
		t.Fatal("opening menu changed selection")
	}
	v.form.output.SetSelected("available device")
	selected, err := v.form.scopedValue(s, "audio")
	if err != nil || selected.Output != "available device" {
		t.Fatalf("device selection not saved: %+v %v", selected, err)
	}
	selectExact(v.form.network, "")
	network, err := v.form.scopedValue(s, "network")
	if err != nil || network.Network != "" {
		t.Fatalf("automatic network selection lost: %+v %v", network, err)
	}
}

func TestStructuredShortcutRoundTrip(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	script := widget.NewMultiLineEntry()
	script.SetText("CtrlLeft+C\ntext:你好\nwait:100")
	editor := shortcutSteps(script, a.NewWindow("steps")).(*fyne.Container)
	rows := editor.Objects[0].(*container.Scroll).Content.(*fyne.Container)
	if len(rows.Objects) != 3 {
		t.Fatal("steps not parsed")
	}
	// Border's center is the editable value; locate by type rather than ordering.
	var value *widget.Entry
	row := rows.Objects[1].(*fyne.Container)
	for _, o := range row.Objects {
		if entry, ok := o.(*widget.Entry); ok {
			value = entry
		}
	}
	if value == nil {
		t.Fatal("text step missing")
	}
	value.SetText("收到")
	if script.Text != "CtrlLeft+C\ntext:收到\nwait:100" {
		t.Fatalf("structured edit damaged script: %q", script.Text)
	}
	script.SetText("AltLeft+ShiftLeft")
	if len(rows.Objects) != 1 {
		t.Fatal("advanced edit did not update steps")
	}
	// Ensure the full editor can be laid out without the advanced text editor open.
	w := a.NewWindow("steps")
	defer w.Close()
	w.SetContent(container.NewPadded(editor))
	w.Resize(fyne.NewSize(800, 500))
}
