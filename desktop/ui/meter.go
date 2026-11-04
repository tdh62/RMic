package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A level indicator needs no text or the height of a form control.
type levelMeter struct {
	widget.BaseWidget
	value float64
}

func newLevelMeter() *levelMeter { m := &levelMeter{}; m.ExtendBaseWidget(m); return m }
func (m *levelMeter) SetValue(value float64) {
	value = max(0, min(1, value))
	if m.value == value {
		return
	}
	m.value = value
	m.Refresh()
}
func (m *levelMeter) CreateRenderer() fyne.WidgetRenderer {
	r := &levelRenderer{meter: m, track: canvas.NewRectangle(theme.DisabledColor()), fill: canvas.NewRectangle(theme.PrimaryColor())}
	return r
}

type levelRenderer struct {
	meter       *levelMeter
	track, fill *canvas.Rectangle
}

func (r *levelRenderer) MinSize() fyne.Size { return fyne.NewSize(40, 4) }
func (r *levelRenderer) Layout(size fyne.Size) {
	r.track.Resize(size)
	r.fill.Resize(fyne.NewSize(size.Width*float32(r.meter.value), size.Height))
}
func (r *levelRenderer) Refresh() {
	r.track.FillColor = theme.DisabledColor()
	r.fill.FillColor = theme.PrimaryColor()
	r.Layout(r.meter.Size())
	r.track.Refresh()
	r.fill.Refresh()
}
func (r *levelRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.fill} }
func (r *levelRenderer) Destroy()                     {}
