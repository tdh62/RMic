package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type Backend interface {
	Snapshot() core.Snapshot
	Start(launcher.Settings) error
	Save(launcher.Settings) error
	Apply(launcher.Settings) error
	Stop() error
	SetLevels(bool, float64, bool) error
	Disconnect() error
	Decide(string, bool, bool) error
	Revoke(string) error
	Driver(string) (string, error)
	InspectDrivers() []virtual.DriverInfo
	StopShortcuts() error
	Close() error
}

type View struct {
	window                               fyne.Window
	backend                              Backend
	dir                                  string
	saved                                launcher.Settings
	snapshot                             core.Snapshot
	busy, closing                        bool
	done                                 chan struct{}
	refreshRequested                     chan struct{}
	state                                *widget.Label
	homeError                            *messageLabel
	notice                               *messageLabel
	requests, devices                    *fyne.Container
	pairSignature                        string
	commandGeneration                    atomic.Uint64
	form                                 *settingsForm
	start, stop, qr                      *widget.Button
	discover                             func() (core.Devices, error)
	tray                                 trayProcess
	trayReady                            bool
	trayGeneration                       uint64
	trayTimer                            *time.Timer
	trayCheck                            *widget.Check
	trayHide                             *widget.Button
	startTray                            func(func(string)) (trayProcess, error)
	tool                                 *toolWindow
	preferences, advanced                fyne.CanvasObject
	driverContent                        fyne.CanvasObject
	toolFocus                            map[string]fyne.Focusable
	driverScanning                       bool
	driverActive, driverOperation        string
	workspace                            fyne.CanvasObject
	shortcut                             *shortcutEditor
	driverItems                          []*driverItem
	driverRows                           *fyne.Container
	driverCheck, driverInstall           *widget.Button
	driverProgress                       *widget.Label
	driverChecked                        bool
	deviceList                           core.Devices
	firstUse, setupOffered               bool
	setupStep                            int
	setupBody, setupFooter               *fyne.Container
	setupNext                            *widget.Button
	setupRoutes                          *fyne.Container
	setupMode, setupOutput, setupCapture *widget.Select
	setupSync                            bool
	setupWarning                         *widget.Label
	setupReceiveRow, setupSendRow        *fyne.Container
	setupSuggest                         *widget.Button
	deviceAction, sessionAction          *widget.Button
	connection, homeRequests             *fyne.Container
	connectionSignature                  string
	disconnect                           *widget.Button
	receiveControl, sendControl          *directionControl
	pairAction                           *widget.Button
	scopeBars                            map[string]*scopeBar
	pairDialog                           dialog.Dialog
	pairRequestID                        string
	pairApprove                          []*widget.Button
}

func New(window fyne.Window, backend Backend, dir string, settings launcher.Settings, notice string) *View {
	v := buildView(window, backend, dir, settings, notice)
	v.restoreWindowSize()
	go v.poll()
	v.refreshDevices()
	if enabled, err := loadTraySettings(dir); err != nil {
		v.notice.SetText("无法读取 GUI 设置：" + err.Error())
	} else if enabled {
		v.setTray(true)
	}
	return v
}

func buildView(window fyne.Window, backend Backend, dir string, settings launcher.Settings, notice string) *View {
	v := &View{window: window, backend: backend, dir: dir, saved: settings, done: make(chan struct{}), refreshRequested: make(chan struct{}, 1), discover: core.Discover}
	v.notice = &messageLabel{Label: wrap("")}
	v.notice.Wrapping = fyne.TextWrapOff
	v.notice.Truncation = fyne.TextTruncateEllipsis
	v.notice.details = widget.NewButton("详情", func() { dialog.ShowInformation("运行提示", v.notice.Text, window) })
	v.notice.SetText(notice)
	v.state = wrap("服务未启动")
	v.state.Wrapping = fyne.TextWrapOff
	v.requests, v.devices = container.NewVBox(), container.NewVBox()
	v.form = v.settingsPage(settings)
	v.firstUse = !v.configured()
	v.initShortcutEditor()
	v.preferences = v.preferencesPage()
	v.workspace = v.audioPage()
	v.watchDrafts()
	v.notice.area = container.NewBorder(nil, nil, nil, v.notice.details, v.notice)
	v.notice.SetText(notice)
	window.SetContent(container.NewBorder(nil, container.NewVBox(v.homeRequests, v.notice.area), nil, nil, v.workspace))
	window.SetCloseIntercept(func() {
		if v.trayReady {
			v.hideToTray()
			return
		}
		v.requestExit()
	})
	v.refresh(core.Snapshot{})
	return v
}

type messageLabel struct {
	*widget.Label
	details *widget.Button
	area    *fyne.Container
}

func (l *messageLabel) SetText(text string) {
	setLabelText(l.Label, text)
	visible := text != ""
	if l.area != nil {
		showWhen(l.area, visible)
	}
	if l.details != nil {
		showWhen(l.details, visible)
	}
	showWhen(l, visible)
}

func wrap(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// Keep user supplied names from forcing a wider window. Full names remain in
// detail dialogs and editable fields; retain the suffix to distinguish devices.
func compactName(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	tail := limit / 3
	return string(runes[:limit-tail-1]) + "…" + string(runes[len(runes)-tail:])
}

// All UI access, including command capture, happens on Fyne's event goroutine.
// Backend calls run outside it; only one UI command can be in flight.
func (v *View) perform(message string, work func() error, success func()) {
	if v.busy || v.closing {
		return
	}
	v.busy = true
	v.commandGeneration.Add(1)
	v.notice.SetText(message)
	v.updateActions()
	go func() {
		err := work()
		snapshot := v.backend.Snapshot()
		fyne.Do(func() {
			if v.closing {
				return
			}
			v.busy = false
			if err != nil {
				v.notice.SetText("操作失败：" + err.Error())
			} else {
				v.notice.SetText("")
				if success != nil {
					success()
				}
			}
			v.refresh(snapshot)
		})
	}()
}

func (v *View) poll() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	v.pollUpdates(ticker.C, fyne.Do)
}

func (v *View) pollUpdates(ticks <-chan time.Time, dispatch func(func())) {
	// Keep at most one refresh queued while resize/layout or screen locking
	// occupies the UI. A foreground request is coalesced until it completes.
	applied := make(chan struct{}, 1)
	pending, refreshAgain := false, false
	queue := func() {
		select {
		case <-v.done:
			return
		default:
		}
		generation := v.commandGeneration.Load()
		s := v.backend.Snapshot()
		pending = true
		dispatch(func() {
			// Buffered so shutdown never waits for the UI acknowledgement.
			defer func() { applied <- struct{}{} }()
			if !v.closing && !v.busy && generation == v.commandGeneration.Load() {
				v.refresh(s)
			}
		})
	}
	for {
		select {
		case <-v.done:
			return
		case <-applied:
			pending = false
			if refreshAgain {
				refreshAgain = false
				queue()
			}
		case <-v.refreshRequested:
			if pending {
				refreshAgain = true
			} else {
				queue()
			}
		case <-ticks:
			if !pending {
				queue()
			}
		}
	}
}

func (v *View) refresh(s core.Snapshot) {
	v.snapshot = s
	if s.Error != "" {
		v.notice.SetText(s.Error)
	}
	v.refreshPairing(s)
	v.updateActions()
}

func (v *View) updateActions() {
	v.updateHome()
	v.updateScopeBars()
	v.updateToolActions()
	v.updateDriverActions()
	v.updateSetupActions()
	if v.start == nil {
		return
	}
	enabled := !v.busy && !v.closing
	enableWhen(v.start, enabled && !v.snapshot.Running)
	enableWhen(v.stop, enabled && v.snapshot.Running)
	enableWhen(v.qr, enabled && v.snapshot.Running)
}

func (v *View) requestExit() {
	if v.closing {
		return
	}
	if v.busy {
		dialog.ShowInformation("操作进行中", "请等待当前操作结束后退出。驱动安装器可能正在等待 UAC 或用户操作。", v.window)
		return
	}
	dialog.ShowConfirm("退出 RMic GUI", "将停止传音并释放本次运行资源。是否退出？", func(ok bool) {
		if !ok || v.busy || v.closing {
			return
		}
		v.rememberWindowSize()
		v.closing = true
		v.stopTray()
		close(v.done)
		v.notice.SetText("正在停止服务并释放资源…")
		v.updateActions()
		go func() {
			err := v.backend.Close()
			fyne.Do(func() {
				finish := func() { v.window.SetCloseIntercept(nil); v.window.Close() }
				if err != nil {
					d := dialog.NewInformation("清理资源失败", err.Error(), v.window)
					d.SetOnClosed(finish)
					d.Show()
				} else {
					finish()
				}
			})
		}()
	}, v.window)
}

type desktopTheme struct {
	font    fyne.Resource
	variant string
}

func (t desktopTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.variant == "浅色" {
		v = theme.VariantLight
	}
	if t.variant == "深色" {
		v = theme.VariantDark
	}
	if n == theme.ColorNamePrimary {
		return color.NRGBA{R: 44, G: 112, B: 210, A: 255}
	}
	if v == theme.VariantLight {
		switch n {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 244, G: 246, B: 250, A: 255}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 32, G: 43, B: 61, A: 255}
		case theme.ColorNameInputBackground:
			return color.White
		}
	}
	return theme.DefaultTheme().Color(n, v)
}
func (t desktopTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (t desktopTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameHeadingText:
		return 18
	case theme.SizeNameSubHeadingText:
		return 14
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameInputRadius:
		return 6
	}
	return theme.DefaultTheme().Size(n)
}
func (t desktopTheme) Font(s fyne.TextStyle) fyne.Resource {
	if t.font != nil {
		return t.font
	}
	return theme.DefaultTheme().Font(s)
}

// Use an installed CJK font, never copy proprietary system fonts into artifacts.
func SetTheme(a fyne.App) {
	a.Settings().SetTheme(desktopTheme{font: findThemeFont()})
}
func findThemeFont() fyne.Resource {
	paths := []string{os.Getenv("FYNE_FONT"), filepath.Join(os.Getenv("WINDIR"), "Fonts", "msyh.ttc"), "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc", "/System/Library/Fonts/PingFang.ttc"}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if data, err := os.ReadFile(path); err == nil {
			data, err = standaloneFont(data)
			if err != nil {
				continue
			}
			return fyne.NewStaticResource(filepath.Base(path), data)
		}
	}
	return nil
}
