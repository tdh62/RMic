package ui

import (
	"sync/atomic"
	"testing"
	"time"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

type pollingBackend struct {
	Backend
	reads atomic.Int32
	state atomic.Pointer[core.Snapshot]
}

func (b *pollingBackend) Snapshot() core.Snapshot {
	b.reads.Add(1)
	if s := b.state.Load(); s != nil {
		return *s
	}
	return core.Snapshot{}
}

func TestPollBoundsQueueAndStopsWhileUIBlocked(t *testing.T) {
	backend := &pollingBackend{}
	v := &View{backend: backend, done: make(chan struct{})}
	ticks := make(chan time.Time)
	queued := make(chan func(), 100)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		v.pollUpdates(ticks, func(f func()) { queued <- f })
	}()
	t.Cleanup(func() {
		close(v.done)
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Error("polling did not stop with a blocked UI")
		}
	})
	tick := func() {
		t.Helper()
		select {
		case ticks <- time.Now():
		case <-time.After(time.Second):
			t.Fatal("polling blocked behind UI work")
		}
	}
	tick()
	var refresh func()
	select {
	case refresh = <-queued:
	case <-time.After(time.Second):
		t.Fatal("initial refresh not dispatched")
	}
	for range 100 {
		tick()
	}
	if n := backend.reads.Load(); n != 1 || len(queued) != 0 {
		t.Fatalf("blocked UI accumulated snapshots: reads=%d queued=%d", n, len(queued))
	}
	// A command invalidates the pending snapshot; completing it must still
	// release the queue slot so polling can recover.
	v.commandGeneration.Add(1)
	refresh()
	deadline := time.After(time.Second)
	for {
		select {
		case <-queued:
			return // Leave this callback blocked while cleanup stops polling.
		case ticks <- time.Now():
		case <-deadline:
			t.Fatal("polling did not resume after UI recovered")
		}
	}
}

func TestResizeStormPreservesDraftsAndInteraction(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("resize")
	defer w.Close()
	saved := launcher.Default()
	saved.Mode, saved.Output, saved.Capture = "duplex", "mic", "capture"
	v := buildView(w, nil, t.TempDir(), saved, "")
	v.openPreferences()
	v.form.https.SetText("9443")
	w.Canvas().Focus(v.form.https)
	s := core.Snapshot{Running: true, Settings: saved, Status: transport.Status{
		State:   "streaming",
		Receive: transport.Direction{Device: "mic", State: "streaming", Gain: 1},
		Send:    transport.Direction{Device: "capture", State: "streaming", Gain: 1},
	}}
	sizes := []fyne.Size{fyne.NewSize(720, 480), fyne.NewSize(1920, 1080), fyne.NewSize(800, 600), fyne.NewSize(1280, 720)}
	for i := range 100 {
		w.Resize(sizes[i%len(sizes)])
		v.refresh(s)
	}
	if v.form.https.Text != "9443" || w.Canvas().Focused() != v.form.https {
		t.Fatal("resize/refresh lost draft or focus")
	}
	v.handleToolEscape()
	if v.tool != nil || v.stop.Disabled() || v.disconnect.Disabled() {
		t.Fatal("controls did not remain interactive after resizing")
	}
}

func TestConstrainedLayoutsNeverPassNegativeSizes(t *testing.T) {
	for _, layout := range []fyne.Layout{formWidth{100}, fixedColumn{100}} {
		object := canvas.NewRectangle(nil)
		object.SetMinSize(fyne.NewSize(20, 20))
		for _, size := range []fyne.Size{fyne.NewSize(-20, -10), {}, fyne.NewSize(1, 1), fyne.NewSize(800, 600)} {
			layout.Layout([]fyne.CanvasObject{object}, size)
			if object.Size().Width < 0 || object.Size().Height < 0 || object.Position().Y < 0 {
				t.Fatalf("%T produced invalid geometry: %v at %v", layout, object.Size(), object.Position())
			}
		}
	}
}

func TestForegroundRefreshAfterUIStall(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "no queued refresh"
		if blocked {
			name = "refresh queued before lock"
		}
		t.Run(name, func(t *testing.T) {
			a := test.NewApp()
			defer a.Quit()
			w := a.NewWindow("resume")
			defer w.Close()
			backend := &pollingBackend{}
			saved := launcher.Default()
			saved.Output = "mic"
			v := buildView(w, backend, t.TempDir(), saved, "")
			v.openPreferences()
			v.form.https.SetText("9443")
			w.Canvas().Focus(v.form.https)
			tool := v.tool
			ticks := make(chan time.Time)
			queued := make(chan func(), 10)
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				v.pollUpdates(ticks, func(f func()) { queued <- f })
			}()
			defer func() {
				close(v.done)
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Error("polling failed to stop")
				}
			}()
			next := func() func() {
				t.Helper()
				select {
				case f := <-queued:
					return f
				case <-time.After(time.Second):
					t.Fatal("foreground refresh waited for a periodic tick")
					return nil
				}
			}
			var stale func()
			if blocked {
				backend.state.Store(&core.Snapshot{Error: "stale before lock"})
				select {
				case ticks <- time.Now():
				case <-time.After(time.Second):
					t.Fatal("polling did not start")
				}
				stale = next()
				// Simulate an hour of ticks while the event loop cannot run UI
				// callbacks. No sleep or real lock screen is needed.
				for range 7200 {
					select {
					case ticks <- time.Now():
					case <-time.After(time.Second):
						t.Fatal("polling blocked while screen was locked")
					}
				}
				if backend.reads.Load() != 1 || len(queued) != 0 {
					t.Fatal("lock duration caused refresh accumulation")
				}
			}
			backend.state.Store(&core.Snapshot{Running: true, Settings: saved, Status: transport.Status{State: "waiting"}})
			v.Resume()
			if stale != nil {
				stale()
				if v.snapshot.Error != "" || v.notice.Text != "" {
					t.Fatal("applied the snapshot captured before locking")
				}
			}
			next()()
			if !v.snapshot.Running || v.snapshot.Status.State != "waiting" {
				t.Fatal("foreground did not fetch current backend state")
			}
			if v.tool != tool || v.form.https.Text != "9443" || w.Canvas().Focused() != v.form.https {
				t.Fatal("foreground reset dialog, draft or focus")
			}
			// Regaining focus must not clear an in-flight operation or let a
			// polling result replace its progress/status.
			v.busy = true
			backend.state.Store(&core.Snapshot{Error: "must not replace command"})
			v.Resume()
			next()()
			if !v.busy || v.snapshot.Error != "" {
				t.Fatal("foreground interfered with an in-flight operation")
			}
			v.busy = false
			v.Resume()
			late := next()
			v.closing = true
			generation := v.commandGeneration.Load()
			v.Resume()
			late()
			if v.commandGeneration.Load() != generation || v.snapshot.Error != "" {
				t.Fatal("foreground or late callback changed a closing view")
			}
		})
	}
}

type repaintCanvas struct {
	fyne.Canvas
	refreshed []fyne.CanvasObject
}

func (c *repaintCanvas) Refresh(object fyne.CanvasObject) {
	c.refreshed = append(c.refreshed, object)
	c.Canvas.Refresh(object)
}

type repaintWindow struct {
	fyne.Window
	canvas fyne.Canvas
}

func (w repaintWindow) Canvas() fyne.Canvas { return w.canvas }

func TestForegroundRepaintsUnchangedContentAndDialogs(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("repaint")
	defer w.Close()
	v := buildView(w, nil, t.TempDir(), launcher.Default(), "")
	v.openPreferences()
	canvas := &repaintCanvas{Canvas: w.Canvas()}
	v.window = repaintWindow{Window: w, canvas: canvas}
	v.Resume()
	want := append([]fyne.CanvasObject{w.Content()}, w.Canvas().Overlays().List()...)
	if len(canvas.refreshed) != len(want) {
		t.Fatalf("repainted %d objects, want %d", len(canvas.refreshed), len(want))
	}
	for i, object := range want {
		if canvas.refreshed[i] != object {
			t.Fatal("foreground did not repaint existing content and overlays")
		}
	}
	// Focus notifications are coalesced even before the polling loop runs.
	for range 100 {
		v.Resume()
	}
	if len(v.refreshRequested) != 1 {
		t.Fatal("foreground notifications accumulated refresh requests")
	}
}
