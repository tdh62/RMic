package ui

import (
	"slices"
	"strconv"
	"strings"

	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func editChord(value *widget.Entry, window fyne.Window) {
	modifiers := []string{"ControlLeft", "ControlRight", "ShiftLeft", "ShiftRight", "AltLeft", "AltRight", "MetaLeft", "MetaRight"}
	labels := []string{"Ctrl 左", "Ctrl 右", "Shift 左", "Shift 右", "Alt 左", "Alt 右", "Win 左", "Win 右"}
	keys := []string{"Space", "Enter", "Tab", "Escape", "Backspace", "Delete", "Insert", "Home", "End", "PageUp", "PageDown", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Minus", "Equal", "BracketLeft", "BracketRight", "Backslash", "Semicolon", "Quote", "Backquote", "Comma", "Period", "Slash"}
	for letter := 'A'; letter <= 'Z'; letter++ {
		keys = append(keys, "Key"+string(letter))
	}
	for i := 0; i <= 9; i++ {
		keys = append(keys, "Digit"+strconv.Itoa(i))
	}
	for i := 1; i <= 12; i++ {
		keys = append(keys, "F"+strconv.Itoa(i))
	}
	draft := widget.NewEntry()
	draft.SetText(value.Text)
	primary := widget.NewSelect(keys, nil)
	primary.PlaceHolder = "主键（可只用修饰键）"
	checks := make([]*widget.Check, len(modifiers))
	grid := container.NewGridWithColumns(4)
	message := wrap("")
	message.Importance = widget.DangerImportance
	var d dialog.Dialog
	apply := widget.NewButton("确定", func() { value.SetText(draft.Text); d.Hide() })
	apply.Importance = widget.HighImportance
	syncing := false
	var mainKeys []string
	write := func() {
		if syncing {
			return
		}
		var parts []string
		for i, check := range checks {
			if check.Checked {
				parts = append(parts, modifiers[i])
			}
		}
		parts = append(parts, mainKeys...)
		draft.SetText(strings.Join(parts, "+"))
	}
	for i, label := range labels {
		checks[i] = widget.NewCheck(label, func(bool) { write() })
		grid.Add(checks[i])
	}
	primary.OnChanged = func(key string) {
		if !syncing {
			mainKeys = nil
			if key != "" {
				mainKeys = []string{key}
			}
			write()
		}
	}
	update := func() {
		syncing = true
		defer func() { syncing = false }()
		parts := strings.Split(draft.Text, "+")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		mainKeys = nil
		for i, key := range modifiers {
			checks[i].SetChecked(slices.Contains(parts, key))
		}
		for _, key := range parts {
			if !slices.Contains(modifiers, key) && key != "" {
				mainKeys = append(mainKeys, key)
			}
		}
		primary.ClearSelected()
		if len(mainKeys) == 1 {
			primary.SetSelected(mainKeys[0])
		}
		_, err := (shortcuts.Button{Name: "组合键", Mode: "tap", Action: draft.Text}).Steps()
		apply.Enable()
		message.Hide()
		if err != nil {
			apply.Disable()
			message.SetText(err.Error())
			message.Show()
		}
	}
	draft.OnChanged = func(string) { update() }
	update()
	cancel := widget.NewButton("取消", func() { d.Hide() })
	content := container.NewVBox(grid, field("主键", bounded(9999, primary)), field("表达式", bounded(9999, draft)), message, actionRow(layout.NewSpacer(), cancel, apply))
	d = dialog.NewCustomWithoutButtons("组合键", content, window)
	d.Resize(fyne.NewSize(min(540, window.Canvas().Size().Width-32), 260))
	d.Show()
	window.Canvas().Focus(draft)
}
