//go:build windows

package shortcuts

import (
	"testing"
	"unsafe"
)

func TestWindowsInputLayout(t *testing.T) {
	var input keyboardInput
	wantSize, wantOffset := uintptr(28), uintptr(4)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantSize, wantOffset = 40, 8
	}
	if unsafe.Sizeof(input) != wantSize || unsafe.Offsetof(input.VK) != wantOffset {
		t.Fatalf("INPUT ABI mismatch: size=%d VK offset=%d", unsafe.Sizeof(input), unsafe.Offsetof(input.VK))
	}
}
