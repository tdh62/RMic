package ui

import (
	"fmt"
	"slices"
	"strconv"

	core "cnb.cool/tdh6/RMic/internal/app"
	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/shortcuts"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type settingsForm struct {
	mode, defaultMode, quality, channels *widget.Select
	output, capture, network             *widget.Select
	https, udp, idle                     *widget.Entry
	warning                              *widget.Label
	shortcuts                            func() (shortcuts.Config, error)
	resetShortcuts                       func(shortcuts.Config)
}

var qualityNames = []string{"语音通话", "均衡", "音乐", "弱网优先", "超高音质"}
var qualityIDs = []string{"voice", "balanced", "music", "weak", "ultra"}

func qualityLabel(id string) string {
	for i, value := range qualityIDs {
		if id == value {
			return qualityNames[i]
		}
	}
	return id
}
func qualityID(label string) string {
	for i, value := range qualityNames {
		if label == value {
			return qualityIDs[i]
		}
	}
	return label
}

var modes = []string{"手机麦克风", "电脑声音", "双向传音"}

func modeLabel(m audio.Mode) string {
	switch m {
	case audio.ModePlayback:
		return modes[1]
	case audio.ModeDuplex:
		return modes[2]
	default:
		return modes[0]
	}
}
func modeValue(s string) audio.Mode {
	switch s {
	case "电脑声音":
		return audio.ModePlayback
	case "双向传音":
		return audio.ModeDuplex
	default:
		return audio.ModeMic
	}
}

func integerRange(name string, low, high int) fyne.StringValidator {
	return func(value string) error {
		n, err := strconv.Atoi(value)
		if err != nil || n < low || n > high {
			return fmt.Errorf("%s须为 %d–%d 的整数", name, low, high)
		}
		return nil
	}
}

func (v *View) settingsPage(s launcher.Settings) *settingsForm {
	f := &settingsForm{
		mode: widget.NewSelect(modes, nil), defaultMode: widget.NewSelect(modes, nil),
		quality: widget.NewSelect(qualityNames, nil), channels: widget.NewSelect([]string{"2", "6", "8"}, nil),
		output: widget.NewSelect(nil, nil), capture: widget.NewSelect(nil, nil), network: widget.NewSelect(nil, nil),
		idle: widget.NewEntry(), https: widget.NewEntry(), udp: widget.NewEntry(), warning: wrap(""),
	}
	f.output.PlaceHolder = "选择设备…"
	f.capture.PlaceHolder = "选择设备…"
	f.network.PlaceHolder = "自动选择"
	f.idle.Validator = integerRange("释放等待", 1, 3600)
	f.https.Validator = integerRange("HTTPS 端口", 1, 65535)
	f.udp.Validator = integerRange("音频端口", 1, 65535)
	f.load(s)
	f.updateMode()
	f.warning.Hide()
	f.mode.OnChanged = func(value string) { f.defaultMode.SetSelected(value); f.updateMode() }
	return f
}

func (f *settingsForm) load(s launcher.Settings) {
	f.mode.SetSelected(modeLabel(s.Mode))
	f.defaultMode.SetSelected(modeLabel(s.SessionMode()))
	f.quality.SetSelected(qualityLabel(s.Quality))
	f.channels.SetSelected(strconv.Itoa(s.Channels()))
	f.idle.SetText(strconv.Itoa(int(s.AudioIdleTimeout().Seconds())))
	selectExact(f.output, s.Output)
	selectExact(f.capture, s.Capture)
	selectExact(f.network, s.Network)
	f.https.SetText(strconv.Itoa(s.HTTPSPort))
	f.udp.SetText(strconv.Itoa(s.UDPPort))
	if f.resetShortcuts != nil {
		f.resetShortcuts(s.Shortcuts)
	}
}

// Each page merges only its own fields into the latest saved configuration.
func (v *View) submitScope(scope string) {
	v.submitScopeAfter(scope, nil)
}
func (v *View) submitScopeAfter(scope string, after func()) {
	if v.busy || v.closing {
		return
	}
	s, err := v.form.scopedValue(v.saved, scope)
	if err != nil {
		if bar := v.scopeBars[scope]; bar != nil {
			bar.errorText = err.Error()
			v.updateScopeBars()
		} else {
			dialog.ShowError(err, v.window)
		}
		return
	}
	draft := v.scopeDraft(scope)
	sourceTool := v.tool
	apply := func() {
		running := v.snapshot.Running
		startAfter := scope == "audio" && !running && (!v.configured() || sourceTool != nil && sourceTool.name == "首次使用")
		v.perform("正在应用设置…", func() error {
			if startAfter {
				return v.backend.Start(s)
			}
			if running {
				return v.backend.Apply(s)
			}
			return v.backend.Save(s)
		}, func() {
			v.saved = s
			if startAfter {
				v.firstUse = false
			}
			if after != nil && v.tool == sourceTool && v.scopeDraft(scope) == draft {
				after()
				if sourceTool != nil && sourceTool.name == "首次使用" && v.tool == nil {
					v.window.Canvas().Focus(v.form.mode)
				}
			}
			if bar := v.scopeBars[scope]; bar != nil {
				bar.baseline = draft
				bar.errorText = ""
			}
			v.updateScopeBars()
			v.updateHome()
		})
	}
	if v.snapshot.Running && (v.snapshot.Settings.AudioChanged(s) || v.snapshot.Settings.NetworkChanged(s)) {
		message := "这会重新配置音频，当前手机需要重新连接。"
		if v.snapshot.Settings.NetworkChanged(s) {
			message = "这会重启网络监听，手机可能需要重新扫码。"
		}
		dialog.ShowConfirm("应用设置并重新连接？", message, func(ok bool) {
			if ok {
				apply()
			}
		}, v.window)
	} else {
		apply()
	}
}

func (f *settingsForm) updateMode() {
	m := modeValue(f.mode.Selected)
	f.output.Disable()
	f.capture.Disable()
	f.channels.Disable()
	if m.Microphone() {
		f.output.Enable()
	}
	if m.Playback() {
		f.capture.Enable()
		f.channels.Enable()
	}
}

func (f *settingsForm) loadAudio(s launcher.Settings) {
	f.mode.SetSelected(modeLabel(s.Mode))
	f.defaultMode.SetSelected(modeLabel(s.SessionMode()))
	f.quality.SetSelected(qualityLabel(s.Quality))
	f.channels.SetSelected(strconv.Itoa(s.Channels()))
	f.idle.SetText(strconv.Itoa(int(s.AudioIdleTimeout().Seconds())))
	selectExact(f.output, s.Output)
	selectExact(f.capture, s.Capture)
}

func (f *settingsForm) scopedValue(base launcher.Settings, scope string) (launcher.Settings, error) {
	// Merge only fields belonging to this scope so network/shortcut drafts cannot block it.
	s := base
	var err error
	switch scope {
	case "audio":
		s.Mode, s.DefaultMode = modeValue(f.mode.Selected), modeValue(f.defaultMode.Selected)
		s.Output, s.Capture, s.Quality = f.output.Selected, f.capture.Selected, qualityID(f.quality.Selected)
		if s.AudioIdleSeconds, err = strconv.Atoi(f.idle.Text); err != nil || s.AudioIdleSeconds < 1 || s.AudioIdleSeconds > 3600 {
			return s, fmt.Errorf("释放时间必须为 1–3600 秒")
		}
		if s.CaptureChannels, err = strconv.Atoi(f.channels.Selected); err != nil {
			return s, fmt.Errorf("请选择采集声道")
		}
	case "network":
		s.Network = f.network.Selected
		if s.Network == "自动选择" {
			s.Network = ""
		}
		if s.HTTPSPort, err = strconv.Atoi(f.https.Text); err != nil {
			return s, fmt.Errorf("HTTPS 端口必须为整数")
		}
		if s.UDPPort, err = strconv.Atoi(f.udp.Text); err != nil {
			return s, fmt.Errorf("音频端口必须为整数")
		}
	case "shortcuts":
		if f.shortcuts != nil {
			s.Shortcuts, err = f.shortcuts()
			if err != nil {
				return s, err
			}
		}
	default:
		return s, fmt.Errorf("未知设置范围")
	}
	return s, s.Validate()
}

func selectExact(selectBox *widget.Select, value string) {
	if value != "" && !slices.Contains(selectBox.Options, value) {
		selectBox.SetOptions(append(selectBox.Options, value))
	}
	if value == "" && selectBox.PlaceHolder == "自动选择" {
		value = "自动选择"
		if !slices.Contains(selectBox.Options, value) {
			selectBox.SetOptions(append([]string{value}, selectBox.Options...))
		}
	}
	selectBox.SetSelected(value)
}

func (v *View) refreshDevices() {
	var d core.Devices
	v.perform("正在读取音频设备与网卡…", func() error { var err error; d, err = v.discover(); return err }, func() {
		v.deviceList = d
		v.form.output.SetOptions(d.Outputs)
		v.form.capture.SetOptions(d.Captures)
		v.form.network.SetOptions(append([]string{"自动选择"}, d.Networks...))
		warning := d.Warning
		if value := v.form.output.Selected; value != "" && !slices.Contains(d.Outputs, value) {
			warning += "\n已保存的麦克风送入设备不存在，请重新选择。"
		}
		if value := v.form.capture.Selected; value != "" && !slices.Contains(d.Captures, value) {
			warning += "\n已保存的电脑声音来源不存在，请重新选择。"
		}
		v.form.warning.SetText(warning)
		if warning == "" {
			v.form.warning.Hide()
		} else {
			v.form.warning.Show()
		}
		v.notice.SetText("")
		v.syncSetupFields()
		v.updateSetupActions()
		if v.firstUse && !v.setupOffered {
			v.openSetup()
		}
	})
}
