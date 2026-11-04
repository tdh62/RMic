package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/pairing"
	"cnb.cool/tdh6/RMic/internal/virtual"
)

func TestControllerLifetimeAndLocalApproval(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if release, err := launcher.LockDirectory(dir); err == nil {
		release()
		t.Fatal("TUI could acquire GUI's directory")
	}
	ticket, err := c.pairing.Create("phone", "127.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Decide(ticket.ID, true, true); err != nil {
		t.Fatal(err)
	}
	result, err := c.pairing.Poll(ticket.ID, ticket.Secret)
	if err != nil || result.Remember || !c.pairing.Valid(result.Token) {
		t.Fatalf("temporary approval: %+v %v", result, err)
	}
	if err = c.Decide("stale-request", true, false); err != pairing.ErrUnknown {
		t.Fatal("stale request accepted:", err)
	}
	if err = c.Stop(); err != nil {
		t.Fatal(err)
	}
	if !c.pairing.Valid(result.Token) {
		t.Fatal("stopping service discarded process-local authorization")
	}
	if err = c.Revoke(result.DeviceID); err != nil {
		t.Fatal(err)
	}
	if c.pairing.Valid(result.Token) {
		t.Fatal("revoked token still valid")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if err = c.Save(launcher.Default()); err == nil {
		t.Fatal("late save accepted after close")
	}
	if _, err = c.Driver("install"); err == nil {
		t.Fatal("late driver installation accepted")
	}
	if release, err := launcher.LockDirectory(dir); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
}

func freePort(t *testing.T, udp bool) int {
	t.Helper()
	if udp {
		l, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestNativeControllerApplyAndRollback(t *testing.T) {
	if os.Getenv("RMIC_NATIVE_TEST") != "1" {
		t.Skip("requires isolated PulseAudio session")
	}
	devices, err := virtual.CreateFor(true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := launcher.Default()
	s.Mode, s.Output, s.Capture = audio.ModeDuplex, devices.Output, devices.Capture
	s.Network, s.HTTPSPort, s.UDPPort = "127.0.0.1", freePort(t, false), freePort(t, true)
	if err = c.Start(s); err != nil {
		t.Fatal(err)
	}
	if !c.Snapshot().Running {
		t.Fatal("not running")
	}
	if _, err := c.Driver("uninstall"); err == nil {
		t.Fatal("allowed uninstall during active service")
	}
	if err := c.SetLevels(false, .7, true); err != nil {
		t.Fatal(err)
	}
	engine := c.run.engine
	h := httptest.NewServer(c.run.server.Handler())
	defer h.Close()
	req, _ := http.NewRequest("POST", h.URL+"/api/pcm", strings.NewReader(`{"mode":"playback"}`))
	req.Header.Set("Authorization", "Bearer "+c.run.server.Token())
	resp, err := h.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PCM reserve: %d %+v", resp.StatusCode, session)
	}
	revision := c.Snapshot().Status.ConfigRevision
	next := s
	next.DefaultMode = audio.ModePlayback
	next.Quality = "music"
	if err = c.Apply(next); err != nil {
		t.Fatal(err)
	}
	if c.run.engine != engine || c.Snapshot().Status.ConfigRevision != revision || c.Snapshot().Status.Quality.ID != "balanced" {
		t.Fatal("defaults interrupted current session")
	}
	s = next
	next.Output = "missing-exact-device"
	if c.Apply(next) == nil || c.run.engine != engine {
		t.Fatal("missing device changed current engine")
	}
	// Force atomic save to fail after preparing a valid replacement.
	if err = os.Remove(filepath.Join(dir, "desktop.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "desktop.json"), 0700); err != nil {
		t.Fatal(err)
	}
	next = s
	next.Mode = audio.ModePlayback
	if c.Apply(next) == nil || c.run.engine != engine {
		t.Fatal("save failure replaced current engine")
	}
	if err = os.Remove(filepath.Join(dir, "desktop.json")); err != nil {
		t.Fatal(err)
	}
	if err = launcher.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	blocked, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	next = s
	next.HTTPSPort = blocked.Addr().(*net.TCPAddr).Port
	if err = c.Apply(next); err == nil || !strings.Contains(err.Error(), "已恢复") {
		t.Fatalf("expected rollback: %v", err)
	}
	state := c.Snapshot()
	if !state.Running || state.Settings != s || state.Status.Gain != .7 || !state.Status.Muted {
		t.Fatalf("rollback lost state: %+v", state)
	}
	if saved, err := launcher.Load(dir); err != nil || saved != s {
		t.Fatalf("disk changed: %v", err)
	}
	next = s
	next.Mode = audio.ModePlayback
	if err = c.Apply(next); err != nil {
		t.Fatal(err)
	}
	if c.run.engine == engine || c.Snapshot().Settings.Mode != audio.ModePlayback {
		t.Fatal("valid replacement did not apply")
	}
	if err = c.Stop(); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Running {
		t.Fatal("stop retained service")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", s.HTTPSPort))
	if err != nil {
		t.Fatal("listener leaked:", err)
	}
	listener.Close()
}
