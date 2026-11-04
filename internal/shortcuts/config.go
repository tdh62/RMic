// Package shortcuts implements computer-configured remote shortcut buttons.
package shortcuts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fixed arrays keep desktop settings value-copyable when editing a draft.
type Config struct {
	Enabled       bool      `json:"enabled"`
	KeyIntervalMS int       `json:"key_interval_ms"`
	Buttons       [3]Button `json:"buttons"`
}
type Button struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`   // tap or hold; an empty name disables this slot.
	Action string `json:"action"` // One chord, text:literal or wait:milliseconds per line.
}
type Step struct {
	Keys   []string
	Text   string
	WaitMS int
}

func (b Button) Steps() ([]Step, error) {
	if b.Name == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(b.Name) > 30 || strings.IndexFunc(b.Name, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("按钮名称最多 30 字符且不可包含控制符")
	}
	if b.Mode != "tap" && b.Mode != "hold" {
		return nil, fmt.Errorf("请选择点击执行或按住保持")
	}
	lines := strings.Split(b.Action, "\n")
	if len(lines) > 10 {
		return nil, fmt.Errorf("每个按钮最多 10 个步骤")
	}
	var steps []Step
	totalWait, textCount := 0, 0
	for _, line := range lines {
		var step Step
		switch {
		case strings.HasPrefix(line, "text:"):
			step.Text = strings.TrimPrefix(line, "text:")
			textCount += utf8.RuneCountInString(step.Text)
			if step.Text == "" || strings.IndexFunc(step.Text, unicode.IsControl) >= 0 {
				return nil, fmt.Errorf("文字不能为空或包含控制符")
			}
		case strings.HasPrefix(line, "wait:"):
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "wait:")))
			if err != nil || n < 1 || n > 1000 {
				return nil, fmt.Errorf("等待时间须为 1–1000 毫秒")
			}
			step.WaitMS = n
			totalWait += n
		default:
			if strings.Contains(line, "Shift+Left") || strings.Contains(line, "Alt+Left") || strings.Contains(line, "Shift+Right") || strings.Contains(line, "Alt+Right") {
				return nil, fmt.Errorf("左右修饰键请写 ShiftLeft / ShiftRight / AltLeft / AltRight；方向键请写 ArrowLeft / ArrowRight，+ 表示同时按下")
			}
			seen := map[string]bool{}
			for _, key := range strings.Split(strings.TrimSpace(line), "+") {
				key = strings.TrimSpace(key)
				if _, ok := keyCodes[key]; !ok || seen[key] {
					return nil, fmt.Errorf("未知或重复按键：%s", key)
				}
				seen[key] = true
				step.Keys = append(step.Keys, key)
			}
			if len(step.Keys) > 8 {
				return nil, fmt.Errorf("组合最多 8 个按键")
			}
		}
		steps = append(steps, step)
	}
	if textCount > 500 || totalWait > 4000 {
		return nil, fmt.Errorf("文字最多 500 字符，累计等待最多 4000 毫秒")
	}
	if b.Mode == "hold" && (len(steps) != 1 || len(steps[0].Keys) == 0) {
		return nil, fmt.Errorf("按住保持只能配置一组按键")
	}
	return steps, nil
}

const MaxKeyIntervalMS = 100

func validateKeyInterval(ms int) error {
	if ms < 0 || ms > MaxKeyIntervalMS {
		return fmt.Errorf("组合键间隔须为 0–%d 毫秒", MaxKeyIntervalMS)
	}
	return nil
}

func (c Config) Validate() error {
	if err := validateKeyInterval(c.KeyIntervalMS); err != nil {
		return err
	}
	count := 0
	for i, b := range c.Buttons {
		if b.Name == "" {
			continue
		}
		count++
		if _, err := b.Steps(); err != nil {
			return fmt.Errorf("快捷按钮 %d：%w", i+1, err)
		}
	}
	if c.Enabled && count == 0 {
		return fmt.Errorf("请至少配置一个快捷按钮")
	}
	return nil
}

type keyCode struct {
	scan     uint16
	extended bool
}

var keyCodes = map[string]keyCode{
	"Escape": {0x01, false}, "Backspace": {0x0e, false}, "Tab": {0x0f, false}, "Enter": {0x1c, false}, "Space": {0x39, false},
	"ControlLeft": {0x1d, false}, "ControlRight": {0x1d, true}, "ShiftLeft": {0x2a, false}, "ShiftRight": {0x36, false},
	"AltLeft": {0x38, false}, "AltRight": {0x38, true}, "MetaLeft": {0x5b, true}, "MetaRight": {0x5c, true},
	"ArrowUp": {0x48, true}, "ArrowDown": {0x50, true}, "ArrowLeft": {0x4b, true}, "ArrowRight": {0x4d, true},
	"Home": {0x47, true}, "End": {0x4f, true}, "PageUp": {0x49, true}, "PageDown": {0x51, true}, "Insert": {0x52, true}, "Delete": {0x53, true},
	"Minus": {0x0c, false}, "Equal": {0x0d, false}, "BracketLeft": {0x1a, false}, "BracketRight": {0x1b, false},
	"Backslash": {0x2b, false}, "Semicolon": {0x27, false}, "Quote": {0x28, false}, "Backquote": {0x29, false},
	"Comma": {0x33, false}, "Period": {0x34, false}, "Slash": {0x35, false},
}

func init() {
	for i, scan := range []uint16{0x1e, 0x30, 0x2e, 0x20, 0x12, 0x21, 0x22, 0x23, 0x17, 0x24, 0x25, 0x26, 0x32, 0x31, 0x18, 0x19, 0x10, 0x13, 0x1f, 0x14, 0x16, 0x2f, 0x11, 0x2d, 0x15, 0x2c} {
		keyCodes["Key"+string(rune('A'+i))] = keyCode{scan, false}
	}
	for i := 1; i <= 9; i++ {
		keyCodes["Digit"+strconv.Itoa(i)] = keyCode{uint16(i + 1), false}
	}
	keyCodes["Digit0"] = keyCode{0x0b, false}
	for i := 1; i <= 12; i++ {
		scan := uint16(0x3a + i)
		if i > 10 {
			scan = uint16(0x57 + i - 11)
		}
		keyCodes["F"+strconv.Itoa(i)] = keyCode{scan, false}
	}
}
