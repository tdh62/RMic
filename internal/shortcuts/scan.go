package shortcuts

import "fmt"

type scanEvent struct {
	vk    uint16
	scan  uint16
	flags uint32
}

// Windows exposes both generic modifier VKs and side-specific VKs. Use only
// side-specific values for modifiers so consumers of virtual-key events also
// receive the intended side. Keep the scan code for scan-code consumers.
var modifierVK = map[string]uint16{
	"ShiftLeft": 0xa0, "ShiftRight": 0xa1,
	"ControlLeft": 0xa2, "ControlRight": 0xa3,
	"AltLeft": 0xa4, "AltRight": 0xa5,
	"MetaLeft": 0x5b, "MetaRight": 0x5c,
}

// Ordinary keys use scan-code input; modifiers use explicit virtual-key input.
// Right Alt/Control need E0, right Shift does not.
func keyEvent(key string, down bool) (scanEvent, error) {
	code, ok := keyCodes[key]
	if !ok {
		return scanEvent{}, fmt.Errorf("未知按键：%s", key)
	}
	flags := uint32(0x0008) // KEYEVENTF_SCANCODE
	vk := modifierVK[key]
	if vk != 0 {
		// KEYEVENTF_SCANCODE would make Windows ignore wVk.
		flags = 0
	}
	if code.extended {
		flags |= 0x0001
	}
	if !down {
		flags |= 0x0002
	}
	return scanEvent{vk: vk, scan: code.scan, flags: flags}, nil
}
