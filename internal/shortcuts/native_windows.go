//go:build windows

package shortcuts

import (
	"fmt"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// INPUT's union has pointer alignment and the size of MOUSEINPUT on both ABIs.
type keyboardInput struct {
	Type        uint32
	_           [unsafe.Sizeof(uintptr(0)) - 4]byte
	VK, Scan    uint16
	Flags, Time uint32
	ExtraInfo   uintptr
	_           [8]byte
}
type nativeBackend struct{}

var sendInput = windows.NewLazySystemDLL("user32.dll").NewProc("SendInput")

func Supported() bool    { return true }
func NewNative() Backend { return nativeBackend{} }
func send(events []keyboardInput) error {
	n, _, err := sendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if n != uintptr(len(events)) {
		return fmt.Errorf("Windows 未接受按键输入（%d/%d）：%v；请检查目标窗口权限", n, len(events), err)
	}
	return nil
}
func (nativeBackend) Key(key string, down bool) error {
	event, err := keyEvent(key, down)
	if err != nil {
		return err
	}
	return send([]keyboardInput{{Type: 1, VK: event.vk, Scan: event.scan, Flags: event.flags}})
}
func (nativeBackend) Text(r rune) error {
	for _, unit := range utf16.Encode([]rune{r}) {
		// Always attempt key-up, including after a partially accepted input batch.
		err := send([]keyboardInput{{Type: 1, Scan: unit, Flags: 0x0004}})
		releaseErr := send([]keyboardInput{{Type: 1, Scan: unit, Flags: 0x0004 | 0x0002}})
		if err != nil {
			return err
		}
		if releaseErr != nil {
			return releaseErr
		}
	}
	return nil
}
