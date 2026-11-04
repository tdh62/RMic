package shortcuts

import (
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestChordKeyInterval(t *testing.T) {
	for _, mode := range []string{"hold", "tap"} {
		for _, ms := range []int{0, 20} {
			t.Run(mode+"/"+(time.Duration(ms)*time.Millisecond).String(), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b := &recordingBackend{}
					e := NewEngine(b)
					if err := e.SetKeyInterval(ms); err != nil {
						t.Fatal(err)
					}
					button := Button{Name: "测试", Mode: mode, Action: "AltLeft+ShiftLeft"}
					if mode == "hold" {
						if err := e.Begin("a", button); err != nil {
							t.Fatal(err)
						}
						if err := e.End("a"); err != nil {
							t.Fatal(err)
						}
					} else {
						done, err := e.Run(button)
						if err != nil {
							t.Fatal(err)
						}
						if err := <-done; err != nil {
							t.Fatal(err)
						}
					}
					want := []string{"down:AltLeft", "down:ShiftLeft", "up:ShiftLeft", "up:AltLeft"}
					if !reflect.DeepEqual(b.snapshot(), want) {
						t.Fatal(b.snapshot())
					}
					b.mu.Lock()
					times := append([]time.Time(nil), b.times...)
					b.mu.Unlock()
					for _, gap := range []time.Duration{times[1].Sub(times[0]), times[3].Sub(times[2])} {
						if gap != time.Duration(ms)*time.Millisecond {
							t.Fatalf("interval %v, want %dms", gap, ms)
						}
					}
				})
			})
		}
	}
}

func TestDelayedSharedKeysAndFailureCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &recordingBackend{}
		e := NewEngine(b)
		if err := e.SetKeyInterval(20); err != nil {
			t.Fatal(err)
		}
		if err := e.Begin("a", hold("AltLeft+ShiftLeft")); err != nil {
			t.Fatal(err)
		}
		if err := e.Begin("b", hold("ShiftLeft+F8")); err != nil {
			t.Fatal(err)
		}
		if err := e.End("a"); err != nil {
			t.Fatal(err)
		}
		pressed, _ := e.State()
		if !reflect.DeepEqual(pressed, []string{"ShiftLeft", "F8"}) {
			t.Fatal(pressed)
		}
		start := time.Now()
		if err := e.ReleaseAll(); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != 0 {
			t.Fatal("emergency release waited between keys")
		}
		want := []string{"down:AltLeft", "down:ShiftLeft", "down:F8", "up:AltLeft", "up:F8", "up:ShiftLeft"}
		if !reflect.DeepEqual(b.snapshot(), want) {
			t.Fatal(b.snapshot())
		}
		b.fail = "down:ShiftLeft"
		if err := e.Begin("fail", hold("AltLeft+ShiftLeft")); err == nil {
			t.Fatal("expected failure")
		}
		pressed, busy := e.State()
		if len(pressed) != 0 || busy {
			t.Fatalf("keys retained after failure: %v busy=%v", pressed, busy)
		}
	})
}

func TestKeyIntervalValidation(t *testing.T) {
	for _, ms := range []int{-1, 101} {
		if err := (Config{KeyIntervalMS: ms}).Validate(); err == nil {
			t.Fatalf("accepted %d", ms)
		}
		if err := NewEngine(&recordingBackend{}).SetKeyInterval(ms); err == nil {
			t.Fatalf("engine accepted %d", ms)
		}
	}
}
