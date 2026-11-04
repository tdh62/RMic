package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Run manages the terminal input language for the lifetime of a TUI session.
func Run(m tea.Model, options ...tea.ProgramOption) (tea.Model, error) {
	input := newInputLanguage()
	defer input.restore()
	wrapped := &inputModel{Model: m, input: input}
	result, err := tea.NewProgram(wrapped, options...).Run()
	if result == wrapped {
		result = wrapped.Model
	}
	return result, err
}

type inputLanguage interface {
	update(text bool)
	restore()
}

type inputModel struct {
	tea.Model
	input inputLanguage
}

type inputLanguageTick struct{}

func inputLanguageTimer() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return inputLanguageTick{} })
}

func (m *inputModel) Init() tea.Cmd {
	m.input.update(textInputActive(m.Model))
	return tea.Batch(m.Model.Init(), inputLanguageTimer())
}

func (m *inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(inputLanguageTick); ok {
		m.input.update(textInputActive(m.Model))
		return m, inputLanguageTimer()
	}
	next, cmd := m.Model.Update(msg)
	m.Model = next
	m.input.update(textInputActive(next))
	return m, cmd
}

func textInputActive(m tea.Model) bool {
	s, ok := m.(*Setup)
	if !ok || s.busy || s.showHelp || s.driver.showVirtual || s.picker != nil {
		return false
	}
	if e := s.shortcutEditor; e != nil {
		if e.steps != nil {
			return e.steps.editing
		}
		return e.editing && e.capture == nil
	}
	return s.editing
}
