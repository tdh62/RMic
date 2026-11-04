package shortcuts

import (
	"reflect"
	"testing"
)

func TestSideSpecificInputEvents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vk    uint16
		scan  uint16
		flags uint32
	}{
		{"ShiftLeft", 0xa0, 0x2a, 0}, {"ShiftRight", 0xa1, 0x36, 0},
		{"AltLeft", 0xa4, 0x38, 0}, {"AltRight", 0xa5, 0x38, 1},
		{"ControlLeft", 0xa2, 0x1d, 0}, {"ControlRight", 0xa3, 0x1d, 1},
		{"MetaLeft", 0x5b, 0x5b, 1}, {"MetaRight", 0x5c, 0x5c, 1},
		{"ArrowLeft", 0, 0x4b, 9}, {"KeyA", 0, 0x1e, 8},
	} {
		for _, down := range []bool{true, false} {
			got, err := keyEvent(tc.name, down)
			flags := tc.flags
			if !down {
				flags |= 2
			}
			if err != nil || got.vk != tc.vk || got.scan != tc.scan || got.flags != flags {
				t.Fatalf("%s down=%v: %+v %v", tc.name, down, got, err)
			}
		}
		backend := &recordingBackend{}
		e := NewEngine(backend)
		if err := e.Begin("test", hold(tc.name)); err != nil {
			t.Fatal(err)
		}
		if err := e.End("test"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(backend.snapshot(), []string{"down:" + tc.name, "up:" + tc.name}) {
			t.Fatal(backend.snapshot())
		}
	}
	for _, action := range []string{"Shift+Left", "Alt+Left", "Shift+Right", "Alt+Right"} {
		if _, err := hold(action).Steps(); err == nil {
			t.Fatalf("ambiguous action accepted: %s", action)
		}
	}
}

func TestCaptureKeysCoverSupportedKeysOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range captureKeys() {
		if _, ok := keyCodes[name]; !ok || seen[name] {
			t.Fatal(name)
		}
		seen[name] = true
	}
	if len(seen) != len(keyCodes) {
		t.Fatal("missing recording keys")
	}
}
