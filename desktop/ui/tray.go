package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cnb.cool/tdh6/RMic/desktop/internal/tray"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type trayProcess interface{ Stop() }
type traySettings struct {
	Enabled bool `json:"minimize_to_tray"`
}

func loadTraySettings(dir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "gui.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var settings traySettings
	err = json.Unmarshal(data, &settings)
	return settings.Enabled, err
}

func saveTraySettings(dir string, enabled bool) error {
	data, err := json.MarshalIndent(traySettings{enabled}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gui-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "gui.json"))
}

func (v *View) trayControls() fyne.CanvasObject {
	v.startTray = func(event func(string)) (trayProcess, error) { return tray.Start(event) }
	v.trayCheck = widget.NewCheck("最小化到系统托盘", func(enabled bool) { v.setTray(enabled) })
	v.trayHide = widget.NewButton("最小化到托盘", v.hideToTray)
	v.trayHide.Disable()
	return container.NewHBox(v.trayCheck, v.trayHide, widget.NewButton("退出", v.requestExit))
}

func (v *View) restoreWindow() {
	v.window.Show()
	v.window.RequestFocus()
}

func (v *View) hideToTray() {
	if v.trayReady && !v.closing {
		v.rememberWindowSize()
		v.window.Hide()
	}
}

func (v *View) setTrayChecked(enabled bool) {
	callback := v.trayCheck.OnChanged
	v.trayCheck.OnChanged = nil
	v.trayCheck.SetChecked(enabled)
	v.trayCheck.OnChanged = callback
}

func (v *View) setTray(enabled bool) {
	if v.closing {
		return
	}
	if !enabled {
		if err := saveTraySettings(v.dir, false); err != nil {
			v.notice.SetText("托盘已关闭，但无法保存设置：" + err.Error())
		}
		v.restoreWindow()
		v.stopTray()
		v.setTrayChecked(false)
		return
	}
	if v.tray != nil {
		return
	}
	v.trayGeneration++
	generation := v.trayGeneration
	v.trayCheck.Disable()
	v.trayHide.Disable()
	p, err := v.startTray(func(event string) {
		fyne.Do(func() { v.trayEvent(generation, event) })
	})
	if err != nil {
		v.trayCheck.Enable()
		v.setTrayChecked(false)
		v.notice.SetText("无法启动托盘：" + err.Error())
		return
	}
	v.tray = p
	v.trayTimer = time.AfterFunc(10*time.Second, func() {
		fyne.Do(func() { v.trayEvent(generation, "timeout") })
	})
}

func (v *View) trayEvent(generation uint64, event string) {
	if generation != v.trayGeneration || v.closing {
		return
	}
	switch event {
	case "ready":
		if v.trayTimer != nil {
			v.trayTimer.Stop()
		}
		if err := saveTraySettings(v.dir, true); err != nil {
			v.stopTray()
			v.setTrayChecked(false)
			v.notice.SetText("无法保存托盘设置：" + err.Error())
			return
		}
		v.trayReady = true
		v.setTrayChecked(true)
		v.trayCheck.Enable()
		v.trayHide.Enable()
	case "show":
		v.restoreWindow()
	case "disable":
		v.setTray(false)
	case "quit":
		v.restoreWindow()
		v.requestExit()
	case "stopped", "timeout":
		v.restoreWindow()
		v.stopTray()
		v.setTrayChecked(false)
		v.notice.SetText(fmt.Sprintf("托盘不可用（%s），窗口已恢复。请确认桌面支持系统托盘。", event))
	}
}

// Called on the GUI thread, including the shutdown path. Native cleanup is
// bounded to three seconds and must finish before another helper may start.
func (v *View) stopTray() {
	v.trayGeneration++
	v.trayReady = false
	if v.trayTimer != nil {
		v.trayTimer.Stop()
		v.trayTimer = nil
	}
	if v.tray != nil {
		v.tray.Stop()
		v.tray = nil
	}
	v.trayHide.Disable()
	v.trayCheck.Enable()
}

func (v *View) CloseTray() { v.stopTray() }
