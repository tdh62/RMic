package launcher

import "testing"

func TestDirectoryLockExcludesOtherFrontend(t *testing.T) {
	dir := t.TempDir()
	unlock, err := LockDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if release, err := LockDirectory(dir); err == nil {
		release()
		t.Fatal("second frontend acquired same directory")
	}
	other, err := LockDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other()
	unlock()
	unlock()
	reopened, err := LockDirectory(dir)
	if err != nil {
		t.Fatal("lock was not released:", err)
	}
	reopened()
}
