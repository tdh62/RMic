//go:build !windows

package shortcuts

import "fmt"

type nativeBackend struct{}

func Supported() bool    { return false }
func NewNative() Backend { return nativeBackend{} }
func (nativeBackend) Key(string, bool) error {
	return fmt.Errorf("快捷按钮目前仅支持 Windows 电脑")
}
func (nativeBackend) Text(rune) error {
	return fmt.Errorf("快捷按钮目前仅支持 Windows 电脑")
}
