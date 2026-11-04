package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
)

// Window geometry is independent of service settings and the tray preference.
func (v *View) restoreWindowSize() {
	data, err := os.ReadFile(filepath.Join(v.dir, "gui-window.json"))
	if os.IsNotExist(err) {
		return
	}
	var size fyne.Size
	if err == nil {
		err = json.Unmarshal(data, &size)
	}
	if err != nil {
		v.notice.SetText("无法读取窗口大小：" + err.Error())
		return
	}
	if size.Width >= 720 && size.Height >= 480 && size.Width <= 3840 && size.Height <= 2160 {
		v.window.Resize(size)
	}
}
func (v *View) saveWindowSize() error {
	size := v.window.Canvas().Size()
	if size.Width < 720 || size.Height < 480 {
		return nil
	}
	data, err := json.Marshal(size)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(v.dir, ".window-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(v.dir, "gui-window.json"))
}

func (v *View) rememberWindowSize() {
	if err := v.saveWindowSize(); err != nil {
		v.notice.SetText(fmt.Sprintf("无法保存窗口大小：%v", err))
	}
}

// Resume runs on Fyne's event goroutine when the window regains foreground.
// Foreground is a focus notification, not a platform screen-unlock guarantee.
func (v *View) Resume() {
	if v.closing {
		return
	}
	// Discard a snapshot queued before locking or switching away. Fetch the
	// replacement on the polling goroutine, never block the UI on the backend.
	v.commandGeneration.Add(1)
	select {
	case v.refreshRequested <- struct{}{}:
	default:
	}
	// Repaint even when all values are unchanged, including any open dialogs.
	canvas := v.window.Canvas()
	canvas.Refresh(v.window.Content())
	for _, overlay := range canvas.Overlays().List() {
		canvas.Refresh(overlay)
	}
}
