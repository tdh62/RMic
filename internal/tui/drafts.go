package tui

import (
	"cnb.cool/tdh6/RMic/internal/launcher"
	tea "github.com/charmbracelet/bubbletea"
)

// MergeScope keeps independent, possibly invalid, drafts out of other saves.
func MergeScope(base, draft launcher.Settings, scope string) launcher.Settings {
	switch scope {
	case "audio":
		base.Mode = draft.Mode
		base.DefaultMode = draft.DefaultMode
		base.Output = draft.Output
		base.Capture = draft.Capture
		base.Quality = draft.Quality
		base.CaptureChannels = draft.CaptureChannels
		base.AudioIdleSeconds = draft.AudioIdleSeconds
	case "network":
		base.Network = draft.Network
		base.HTTPSPort = draft.HTTPSPort
		base.UDPPort = draft.UDPPort
	case "shortcuts":
		base.Shortcuts = draft.Shortcuts
	}
	return base
}
func (m *Setup) currentScope() string {
	if m.shortcutEditor != nil {
		return "shortcuts"
	}
	if m.focus >= 4 && m.focus <= 6 {
		return "network"
	}
	if m.scope != "" && m.focus >= 9 {
		return m.scope
	}
	return "audio"
}
func (m *Setup) scopedSettings() launcher.Settings {
	return MergeScope(m.saved, m.Settings, m.currentScope())
}
func (m *Setup) resetScope() {
	m.Settings = MergeScope(m.Settings, m.saved, m.currentScope())
	m.notice = "已还原当前分组"
}
func (m *Setup) saveScope(start bool) tea.Cmd {
	scope := m.currentScope()
	settings := m.scopedSettings()
	if start && !m.options.Running {
		settings = m.Settings
		scope = "all"
	}
	return func() tea.Msg {
		var err error
		if start && m.options.Running {
			if m.options.ApplyScope != nil {
				err = m.options.ApplyScope(settings, scope)
			} else {
				err = m.options.Apply(settings)
			}
		} else {
			err = m.options.Save(settings)
		}
		return setupSaved{err: err, start: start, settings: settings, scope: scope}
	}
}

func (m *Setup) runningImpact() string {
	current := m.options.Settings
	if m.options.RunningSettings != nil {
		current = *m.options.RunningSettings
	}
	return current.Impact(MergeScope(current, m.scopedSettings(), m.currentScope()))
}
