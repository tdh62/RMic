package launcher

import (
	"strings"
	"testing"
	"time"
)

func TestAudioIdlePreference(t *testing.T) {
	s := Default()
	if s.AudioIdleTimeout() != 30*time.Second {
		t.Fatal("legacy config lost 30-second default")
	}
	next := s
	next.AudioIdleSeconds = 30
	if s.AudioChanged(next) {
		t.Fatal("explicit default unnecessarily replaces audio")
	}
	next.AudioIdleSeconds = 7
	if !s.AudioChanged(next) {
		t.Fatal("new idle timeout not applied")
	}
	dir := t.TempDir()
	if err := Save(dir, next); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || loaded.AudioIdleTimeout() != 7*time.Second {
		t.Fatalf("idle setting lost: %+v %v", loaded, err)
	}
	if !strings.Contains(strings.Join(loaded.Args(dir), " "), "--audio-idle-timeout 7s") {
		t.Fatal("TUI startup lost idle timeout")
	}
	for _, invalid := range []int{-1, 3601} {
		next.AudioIdleSeconds = invalid
		if next.Validate() == nil {
			t.Fatalf("accepted idle duration %d", invalid)
		}
	}
}
