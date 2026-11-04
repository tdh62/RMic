package tui

import (
	"testing"

	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

type recordedInputLanguage struct{ states []bool }

func (i *recordedInputLanguage) update(text bool) { i.states = append(i.states, text) }

func (*recordedInputLanguage) restore() {}

func TestInputLanguageFollowsEditing(t *testing.T) {
	s := NewSetup(SetupOptions{Settings: launcher.Settings{HTTPSPort: 8443}})
	s.busy, s.focus = false, 5
	input := &recordedInputLanguage{}
	m := &inputModel{Model: s, input: input}
	m.Update(inputLanguageTick{})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	s.shortcutEditor = &shortcutEditor{focus: 1}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中文名称")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.shortcutEditor.draft.Buttons[0].Name != "中文名称" {
		t.Fatal("text editing must preserve Unicode input")
	}
	want := []bool{false, true, false, true, true, false}
	if len(input.states) != len(want) {
		t.Fatalf("states = %v", input.states)
	}
	for n, state := range want {
		if input.states[n] != state {
			t.Fatalf("states = %v, want %v", input.states, want)
		}
	}
	if m.Model != s {
		t.Fatal("wrapper must retain the original setup result")
	}
}

func TestInputLanguageCaptureAndOverlays(t *testing.T) {
	s := &Setup{shortcutEditor: &shortcutEditor{editing: true}}
	if !textInputActive(s) {
		t.Fatal("editor should allow text input")
	}
	s.shortcutEditor.capture = &shortcutCapture{}
	if textInputActive(s) {
		t.Fatal("key capture must use English input")
	}
	s.shortcutEditor = nil
	s.editing = true
	s.driver.showVirtual = true
	if textInputActive(s) || textInputActive(model{}) {
		t.Fatal("driver and runtime screens must use English input")
	}
}
