package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A quiet surface groups controls without a heading, shadow or extra title gap.
type controlPanel struct {
	widget.BaseWidget
	content *fyne.Container
}

func panel(objects ...fyne.CanvasObject) *controlPanel {
	p := &controlPanel{content: container.NewVBox(objects...)}
	p.ExtendBaseWidget(p)
	return p
}
func (p *controlPanel) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(theme.InputBackgroundColor())
	background.CornerRadius = 6
	background.StrokeWidth = 1
	background.StrokeColor = theme.SeparatorColor()
	return &panelRenderer{panel: p, background: background}
}

type panelRenderer struct {
	panel      *controlPanel
	background *canvas.Rectangle
}

func (r *panelRenderer) MinSize() fyne.Size {
	return r.panel.content.MinSize().Add(fyne.NewSize(16, 16))
}
func (r *panelRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.panel.content.Move(fyne.NewPos(8, 8))
	r.panel.content.Resize(fyne.NewSize(max(0, size.Width-16), max(0, size.Height-16)))
}
func (r *panelRenderer) Refresh() {
	r.background.FillColor = theme.InputBackgroundColor()
	r.background.StrokeColor = theme.SeparatorColor()
	r.background.Refresh()
	r.panel.content.Refresh()
	r.Layout(r.panel.Size())
}
func (r *panelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.panel.content}
}
func (r *panelRenderer) Destroy() {}
