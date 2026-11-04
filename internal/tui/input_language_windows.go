//go:build windows

package tui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	inputUser32           = windows.NewLazySystemDLL("user32.dll")
	inputConsoleWindow    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	inputForegroundWindow = inputUser32.NewProc("GetForegroundWindow")
	inputWindowThread     = inputUser32.NewProc("GetWindowThreadProcessId")
	inputKeyboardLayout   = inputUser32.NewProc("GetKeyboardLayout")
	inputLoadLayout       = inputUser32.NewProc("LoadKeyboardLayoutW")
	inputSendMessage      = inputUser32.NewProc("SendMessageTimeoutW")
)

type windowsInputLanguage struct {
	window, original, english uintptr
}

func newInputLanguage() inputLanguage { return &windowsInputLanguage{} }

func (i *windowsInputLanguage) update(text bool) {
	if text {
		i.restore()
		return
	}
	window, _, _ := inputConsoleWindow.Call()
	foreground, _, _ := inputForegroundWindow.Call()
	// A pseudoconsole (Windows Terminal/SSH) does not own the visible window.
	// Never change an unrelated foreground application's input language.
	if window == 0 || window != foreground {
		return
	}
	thread, _, _ := inputWindowThread.Call(window, 0)
	if thread == 0 {
		return
	}
	layout, _, _ := inputKeyboardLayout.Call(thread)
	if layout == 0 {
		return
	}
	if i.english == 0 {
		name, _ := windows.UTF16PtrFromString("00000409")
		// Load US English without activating it on our own thread or notifying
		// the shell; only the console window receives the activation request.
		i.english, _, _ = inputLoadLayout.Call(uintptr(unsafe.Pointer(name)), 0x80)
	}
	if i.english == 0 || layout == i.english {
		return
	}
	if i.original == 0 {
		i.window, i.original = window, layout
	}
	setInputLayout(window, i.english)
}

func (i *windowsInputLanguage) restore() {
	if i.original != 0 {
		setInputLayout(i.window, i.original)
		i.window, i.original = 0, 0
	}
}

func setInputLayout(window, layout uintptr) {
	var result uintptr
	// WM_INPUTLANGCHANGEREQUEST, SMTO_ABORTIFHUNG. Bound the wait so a hung
	// console host cannot block navigation or shutdown indefinitely.
	inputSendMessage.Call(window, 0x0050, 0, layout, 0x0002, 50, uintptr(unsafe.Pointer(&result)))
}
