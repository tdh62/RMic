package shortcuts

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type recordingBackend struct {
	mu     sync.Mutex
	events []string
	times  []time.Time
	fail   string
}

func (b *recordingBackend) Key(key string, down bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	event := "up:" + key
	if down {
		event = "down:" + key
	}
	b.events = append(b.events, event)
	b.times = append(b.times, time.Now())
	if b.fail == event {
		b.fail = ""
		return errors.New("injection failed")
	}
	return nil
}
func (b *recordingBackend) Text(r rune) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, "text:"+string(r))
	return nil
}
func (b *recordingBackend) snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.events...)
}
func hold(action string) Button { return Button{Name: "保持", Mode: "hold", Action: action} }
func TestSharedModifierAndReverseRelease(t *testing.T) {
	backend := &recordingBackend{}
	e := NewEngine(backend)
	for id, button := range []Button{hold("AltLeft+ShiftLeft"), hold("ShiftLeft+F8")} {
		if err := e.Begin(string(rune('a'+id)), button); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.End("a"); err != nil {
		t.Fatal(err)
	}
	pressed, _ := e.State()
	if !reflect.DeepEqual(pressed, []string{"ShiftLeft", "F8"}) {
		t.Fatal(pressed)
	}
	if err := e.End("b"); err != nil {
		t.Fatal(err)
	}
	want := []string{"down:AltLeft", "down:ShiftLeft", "down:F8", "up:AltLeft", "up:F8", "up:ShiftLeft"}
	if !reflect.DeepEqual(backend.snapshot(), want) {
		t.Fatal(backend.snapshot())
	}
}
func TestFailureReleasesPartiallyPressedChord(t *testing.T) {
	backend := &recordingBackend{fail: "down:ShiftLeft"}
	e := NewEngine(backend)
	if e.Begin("a", hold("AltLeft+ShiftLeft")) == nil {
		t.Fatal("expected failure")
	}
	pressed, busy := e.State()
	if len(pressed) > 0 || busy {
		t.Fatal(pressed, busy)
	}
	want := []string{"down:AltLeft", "down:ShiftLeft", "up:ShiftLeft", "up:AltLeft"}
	if !reflect.DeepEqual(backend.snapshot(), want) {
		t.Fatal(backend.snapshot())
	}
}
func TestCancelSequenceDoesNotTypeOrReleaseNewHold(t *testing.T) {
	backend := &recordingBackend{}
	e := NewEngine(backend)
	done, err := e.Run(Button{Name: "序列", Mode: "tap", Action: "wait:1000\ntext:不应输入"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Run(Button{Name: "重复", Mode: "tap", Action: "Enter"}); err == nil {
		t.Fatal("queued a second run")
	}
	if e.Begin("blocked", hold("F8")) == nil {
		t.Fatal("hold overlapped a sequence")
	}
	if err = e.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if err = e.Begin("new", hold("AltLeft")); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt wait")
	}
	pressed, _ := e.State()
	if !reflect.DeepEqual(pressed, []string{"AltLeft"}) {
		t.Fatal(pressed)
	}
	if !reflect.DeepEqual(backend.snapshot(), []string{"down:AltLeft"}) {
		t.Fatal(backend.snapshot())
	}
	_ = e.ReleaseAll()
}
func TestSequenceTypesTextAndChordsInOrder(t *testing.T) {
	backend := &recordingBackend{}
	e := NewEngine(backend)
	done, err := e.Run(Button{Name: "回复", Mode: "tap", Action: "text:你好🙂\nEnter"})
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	want := []string{"text:你", "text:好", "text:🙂", "down:Enter", "up:Enter"}
	if !reflect.DeepEqual(backend.snapshot(), want) {
		t.Fatal(backend.snapshot())
	}
}
func TestConfigurationValidation(t *testing.T) {
	for _, b := range []Button{
		hold("AltLeft+AltLeft"), hold("LeftAlt"), hold("text:hello"), hold("AltLeft\nShiftLeft"),
		{Name: "等待", Mode: "tap", Action: "wait:5000"},
		{Name: "太长", Mode: "tap", Action: "text:" + string(make([]byte, 501))},
	} {
		if _, err := b.Steps(); err == nil {
			t.Fatalf("accepted invalid button: %+v", b)
		}
	}
	if err := (Config{Enabled: true}).Validate(); err == nil {
		t.Fatal("enabled empty configuration")
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatal(err)
	}
}
