//go:build !windows

package tui

// Terminal protocols do not provide a portable way to control the host IME.
type terminalInputLanguage struct{}

func newInputLanguage() inputLanguage { return terminalInputLanguage{} }

func (terminalInputLanguage) update(bool) {}

func (terminalInputLanguage) restore() {}
