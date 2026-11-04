package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LockDirectory prevents two frontends from changing the same configuration or
// authorization store. The OS releases the lock after a crash; keep the file.
func LockDirectory(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, ".instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("无法锁定配置目录 %s（可能已有 RMic 实例运行）：%w", dir, err)
	}
	var once sync.Once
	return func() { once.Do(func() { unlockFile(file); file.Close() }) }, nil
}
