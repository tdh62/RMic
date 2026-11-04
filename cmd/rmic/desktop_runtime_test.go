package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
)

func TestLiveAudioApplicationAndRollback(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("set RMIC_NATIVE_TEST=1 with PulseAudio for live configuration")
	}
	devices, err := virtual.CreateFor(true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	current := launcher.Default()
	current.Mode, current.Output, current.Capture = audio.ModeDuplex, devices.Output, devices.Capture
	engine, err := prepareAudio(current)
	if err != nil {
		t.Fatal(err)
	}
	live := &liveAudio{engine: engine, mode: current.Mode}
	defer live.Close()
	server, err := transport.NewServer(engine, engine.Name(), 0, transport.WithInput(engine, engine.CaptureName()))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	h := httptest.NewServer(server.Handler())
	defer h.Close()
	request, _ := http.NewRequest("POST", h.URL+"/api/pcm", strings.NewReader(`{"mode":"playback"}`))
	request.Header.Set("Authorization", "Bearer "+server.Token())
	created, err := h.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()
	if created.StatusCode != 200 {
		t.Fatal("could not create active PCM session")
	}
	dir := t.TempDir()
	if err := launcher.Save(dir, current); err != nil {
		t.Fatal(err)
	}
	revision := server.Status().ConfigRevision
	next := current
	next.DefaultMode = audio.ModePlayback
	next.Quality = "music"
	if err := live.apply(server, dir, current, next); err != nil {
		t.Fatal(err)
	}
	if live.engine != engine || server.Status().ConfigRevision != revision || server.Status().Quality.ID != "balanced" || server.Status().Mode != audio.ModePlayback {
		t.Fatal("defaults replaced engine or session")
	}
	current = next
	next = current
	next.Output = "missing exact device"
	if live.apply(server, dir, current, next) == nil {
		t.Fatal("missing device accepted")
	}
	saved, err := launcher.Load(dir)
	if err != nil || saved != current || live.engine != engine {
		t.Fatal("failed preparation changed running or saved state")
	}

	next = current
	next.Mode = audio.ModePlayback
	// A write failure after device preparation also preserves the old engine.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if live.apply(server, blocked, current, next) == nil || live.engine != engine {
		t.Fatal("save failure replaced engine")
	}
	engine.SetGain(.7)
	engine.ToggleCaptureMute()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); live.watchRoutes(ctx) }()
	defer func() { cancel(); <-done }()
	if err := live.apply(server, dir, current, next); err != nil {
		t.Fatal(err)
	}
	if live.engine == engine || server.Status().ConfigRevision != revision+1 || !live.engine.CaptureMuted() || live.engine.Gain() != .7 {
		t.Fatal("replacement lost state")
	}
	response, err := func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", h.URL+"/api/status", nil)
		req.Header.Set("Authorization", "Bearer "+server.Token())
		return h.Client().Do(req)
	}()
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status transport.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || len(status.Modes) != 1 || status.Modes[0] != audio.ModePlayback {
		t.Fatal("HTTP listener did not survive reconfiguration")
	}
	stored := next
	stored.HTTPSPort = 9443
	previousStored := next
	if err := live.applyStored(server, dir, next, next, stored, previousStored); err != nil {
		t.Fatal(err)
	}
	persisted, err := launcher.Load(dir)
	if err != nil || persisted.HTTPSPort != 9443 || server.Status().ConfigRevision != status.ConfigRevision {
		t.Fatal("saved network was lost or applied while updating audio")
	}
}
