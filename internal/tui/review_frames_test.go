package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnb.cool/tdh6/RMic/internal/audio"
	"cnb.cool/tdh6/RMic/internal/launcher"
	"cnb.cool/tdh6/RMic/internal/transport"
	"cnb.cool/tdh6/RMic/internal/virtual"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkspaceReviewFrames(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {60, 20}} {
		width, height := size[0], size[1]
		save := func(name, view string) {
			t.Helper()
			if lipgloss.Width(view) > width || strings.Count(view, "\n") >= height {
				t.Fatalf("%s exceeds %dx%d", name, width, height)
			}
			if dir := os.Getenv("RMIC_TUI_FRAMES"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%dx%d.txt", name, width, height)), []byte(ansi.Strip(view)+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
		m := model{mouse: &mouseFrame{}, width: width, height: height, urls: []string{"https://192.168.1.10:8443"}, input: "Steam Streaming Microphone"}
		status := transport.Status{State: "streaming", Quality: audio.Quality{Label: "均衡"}, MetricsAgeMS: 100, Playback: transport.PlaybackMetrics{RTTMS: 8}, Receive: transport.Direction{Device: "扬声器 (Steam Streaming Microphone)", State: "streaming", Gain: 1}, Send: transport.Direction{Device: "loopback:扬声器 (Steam Streaming Speakers)", State: "streaming", Gain: .8}}
		rows := m.workspaceRows(status, map[string]string{"streaming": "传音中"})
		footer := actionBar([]string{"二维码 x", "配对 p", "静音 m", "断开 D", "驱动 v", "配置 c", "退出 q"}, 0, width) + "\n↑↓ 方向 · +/- 音量 · g 数值 · ? 帮助"
		save("workspace", m.mouse.render(page(rows, footer, width, height, 0), "main"))
		manager := &inspectedManager{items: []virtual.DriverInfo{{Name: "Steam Streaming", Prefix: "steam-", Description: "独立麦克风与扬声器", Installed: true, Complete: true}, {Name: "VB-CABLE", Description: "基础虚拟通道"}, {Name: "CABLE-A", Prefix: "cable-a-"}, {Name: "CABLE-B", Prefix: "cable-b-"}}}
		drivers := NewVirtual(manager).(model)
		drivers.width = width
		drivers.height = height
		updated, _ := drivers.Update(drivers.Init()())
		save("drivers", updated.View())
		settings := launcher.Default()
		settings.Mode = audio.ModeDuplex
		settings.Output = status.Receive.Device
		settings.Capture = status.Send.Device
		setup := NewSetup(SetupOptions{Settings: settings})
		setup.busy = false
		setup.width = width
		setup.height = height
		setup.devices = SetupDevices{Outputs: []string{settings.Output}, Captures: []string{settings.Capture}}
		save("configuration", setup.View())
		setup.guide = true
		save("first-use", setup.View())
		setup.guide = false
		setup.Update(mouseKey("K"))
		save("shortcut-slots", setup.View())
		setup.Update(mouseKey("1"))
		setup.Update(mouseKey("f4"))
		setup.Update(mouseKey("a"))
		setup.Update(mouseKey("w"))
		save("shortcut-steps", setup.View())
	}
}
