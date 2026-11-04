//go:build windows

package shortcuts

import "golang.org/x/sys/windows"

var (
	getAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")
	mapVirtualKey    = windows.NewLazySystemDLL("user32.dll").NewProc("MapVirtualKeyW")
)

// PressedKeys is used only while the local shortcut recorder is open. Query
// side-specific virtual keys; generic SHIFT/CONTROL/MENU merge both sides.
func PressedKeys() []string {
	var keys []string
	for _, name := range captureKeys() {
		code := keyCodes[name]
		scan := uintptr(code.scan)
		if code.extended {
			scan |= 0xe000
		}
		vk, _, _ := mapVirtualKey.Call(scan, 3) // MAPVK_VSC_TO_VK_EX
		if vk == 0 {
			continue
		}
		state, _, _ := getAsyncKeyState.Call(vk)
		if state&0x8000 != 0 {
			keys = append(keys, name)
		}
	}
	return keys
}
