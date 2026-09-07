//go:build windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openMigrationLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		_ = f.Close()
		return nil, ErrMigrationBusy
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func releaseMigrationLock(f *os.File) {
	if f == nil {
		return
	}
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
	_ = f.Close()
}
