package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDirectory keeps the entire portable identity beside the executable.
// Explicit paths never silently fall back to a different authorization store.
func ConfigDirectory(explicit string, portable bool) (string, string, error) {
	if explicit != "" {
		dir, err := filepath.Abs(explicit)
		if err != nil {
			return "", "", err
		}
		return dir, "", writableDirectory(dir)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	fallback := filepath.Join(root, "rmic")
	if portable {
		executable, err := os.Executable()
		if err != nil {
			return "", "", err
		}
		dir := filepath.Join(filepath.Dir(executable), "rmic-data")
		if err = writableDirectory(dir); err == nil {
			return dir, "", nil
		}
		if fallbackErr := writableDirectory(fallback); fallbackErr != nil {
			return "", "", fmt.Errorf("程序目录不可写（%v），用户配置目录也不可写：%w", err, fallbackErr)
		}
		return fallback, "程序目录不可写，已使用用户配置目录：" + fallback, nil
	}
	return fallback, "", writableDirectory(fallback)
}
func DefaultPortable() bool { return runtime.GOOS == "windows" }
func writableDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".rmic-write-*")
	if err != nil {
		return err
	}
	name := file.Name()
	err = file.Close()
	_ = os.Remove(name)
	return err
}
