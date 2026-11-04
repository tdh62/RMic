package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Keep controls at a readable width, while still shrinking with the window.
type formWidth struct{ width float32 }

func (l formWidth) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, objects[0].MinSize().Height)
}
func (l formWidth) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := max(0, min(size.Width, l.width))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(width, max(0, size.Height)))
}
func bounded(width float32, content fyne.CanvasObject) *fyne.Container {
	return container.New(formWidth{width}, content)
}

type fixedColumn struct{ width float32 }

func (l fixedColumn) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.width, objects[0].MinSize().Height)
}
func (l fixedColumn) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	height := max(0, min(size.Height, objects[0].MinSize().Height))
	objects[0].Resize(fyne.NewSize(max(0, min(l.width, size.Width)), height))
	objects[0].Move(fyne.NewPos(0, max(0, (size.Height-height)/2)))
}

// Short fields reserve only their useful width, even inside a horizontal row.
func fixedWidth(width float32, content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(fixedColumn{width}, content)
}
func field(label string, content fyne.CanvasObject) *fyne.Container {
	return container.NewBorder(nil, nil, fixedWidth(72, widget.NewLabel(label)), nil, content)
}
func actionRow(objects ...fyne.CanvasObject) *fyne.Container {
	return container.NewHBox(objects...)
}

func showWhen(object fyne.CanvasObject, visible bool) {
	if visible {
		if !object.Visible() {
			object.Show()
		}
	} else if object.Visible() {
		object.Hide()
	}
}

func enableWhen(object fyne.Disableable, enabled bool) {
	if enabled {
		if object.Disabled() {
			object.Enable()
		}
	} else if !object.Disabled() {
		object.Disable()
	}
}

// Fyne's text setters refresh even when the text is unchanged.
func setLabelText(label *widget.Label, text string) {
	if label.Text != text {
		label.SetText(text)
	}
}

func setButtonText(button *widget.Button, text string) {
	if button.Text != text {
		button.SetText(text)
	}
}
