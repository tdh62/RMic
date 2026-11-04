package ui

import (
	"strconv"

	"fyne.io/fyne/v2/widget"
)

type gainEntry struct {
	widget.Entry
	commit func()
}

func newGainEntry(commit func(float64)) *gainEntry {
	e := &gainEntry{}
	e.ExtendBaseWidget(e)
	e.Validator = integerRange("音量", 0, 300)
	e.commit = func() {
		if e.Disabled() || e.Validate() != nil {
			return
		}
		value, _ := strconv.Atoi(e.Text)
		commit(float64(value) / 100)
	}
	e.OnSubmitted = func(string) { e.commit() }
	e.SetText("100")
	return e
}
func (e *gainEntry) FocusLost() { e.Entry.FocusLost(); e.commit() }
