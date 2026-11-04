package tui

import (
	"fmt"
	"strconv"
	"strings"

	"cnb.cool/tdh6/RMic/internal/transport"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) gainKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.editGain = false
	case "enter":
		value, err := strconv.Atoi(m.gainText)
		if err != nil || value < 0 || value > 300 {
			m.gainError = "音量须为 0–300 的整数"
			return m, nil
		}
		if m.selectedSend {
			m.control.SetCaptureGain(float64(value) / 100)
		} else {
			m.control.SetGain(float64(value) / 100)
		}
		m.editGain = false
		m.gainError = ""
	case "backspace", "ctrl+h":
		if len(m.gainText) > 0 {
			m.gainText = m.gainText[:len(m.gainText)-1]
		}
	case "ctrl+u":
		m.gainText = ""
	default:
		for _, r := range key.Runes {
			if r >= '0' && r <= '9' && len(m.gainText) < 3 {
				m.gainText += string(r)
			}
		}
	}
	return m, nil
}

func (m model) workspaceRows(status transport.Status, labels map[string]string) []string {
	quality := status.Quality.Label
	if status.Transport == "ws-pcm" {
		quality = fmt.Sprintf("PCM · 48 kHz · %d 声道", status.CaptureChannels)
		if status.PCMCompression == "deflate" {
			quality += " · 无损压缩"
		}
	}
	rows := []string{accent.Render(" RMic ") + "  " + labels[status.State] + " · " + quality}
	if m.notice != "" {
		rows = append(rows, m.notice)
	}
	if m.pairing != nil {
		if count := len(m.pairing.Requests()); count > 0 {
			rows = append(rows, clickable("key:p", fmt.Sprintf("%d 个配对申请 · p 核对", count)))
		}
	}
	for _, item := range []struct {
		title, target string
		direction     transport.Direction
		selected      bool
	}{
		{"手机 → 电脑", "receive", status.Receive, !m.selectedSend}, {"电脑 → 手机", "send", status.Send, m.selectedSend},
	} {
		d := item.direction
		if d.Device == "" {
			continue
		}
		marker := "  "
		if item.selected {
			marker = "> "
		}
		state := labels[d.State]
		if d.Muted {
			state = "已静音"
		}
		if d.Error != "" {
			state = "设备故障"
		}
		rows = append(rows, clickable(item.target, marker+item.title+"  "+state), "  设备："+fitText(d.Device, max(1, m.width-10)))
		filled := max(0, min(12, int(d.Audio.Level*36)))
		rows = append(rows, fmt.Sprintf("  电平 %s%s  音量 %.0f%%", accent.Render(strings.Repeat("█", filled)), dim.Render(strings.Repeat("░", 12-filled)), d.Gain*100))
		if d.Error != "" {
			rows = append(rows, "  "+d.Error)
		}
		if d.DeviceState == "released" || d.DeviceState == "idle" {
			rows = append(rows, "  "+d.DeviceStateLabel())
		}
	}
	if status.Error != "" {
		rows = append(rows, status.Error)
	}
	latency := "网络往返 —"
	if status.State != "reconnecting" && status.MetricsAgeMS >= 0 && status.MetricsAgeMS < 3000 && status.Playback.RTTMS > 0 {
		latency = fmt.Sprintf("网络往返 %.0f ms", status.Playback.RTTMS)
	}
	rows = append(rows, latency)
	if status.Shortcuts != nil {
		text := "快捷控制已开启"
		if status.Shortcuts.Busy {
			text += " · 执行中"
		}
		if len(status.Shortcuts.Pressed) > 0 {
			text += " · 保持 " + strings.Join(status.Shortcuts.Pressed, "+")
		}
		rows = append(rows, text+" · "+clickable("key:R", "释放 R"))
		if status.Shortcuts.Error != "" {
			rows = append(rows, status.Shortcuts.Error)
		}
	}
	if status.Receive.Device != "" {
		rows = append(rows, "应用麦克风："+fitText(m.input, max(1, m.width-16)))
	}
	rows = append(rows, fitText(m.urls[m.urlIndex], max(1, m.width-2)))
	if m.editGain {
		rows = append(rows, "音量百分比：["+m.gainText+"_]  Enter 确定 · Esc 取消", m.gainError)
	}
	return rows
}
