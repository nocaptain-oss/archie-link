//go:build windows

package lock

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

var procLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func Acquire(path string) (*Handle, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	err = lockFileEx(file, &overlapped)
	if err != nil {
		_ = file.Close()
		return nil, ErrLocked
	}
	return &Handle{file: file}, nil
}

func lockFileEx(file *os.File, overlapped *syscall.Overlapped) error {
	result, _, callErr := procLockFileEx.Call(
		file.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(overlapped)),
	)
	if result != 0 {
		return nil
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return syscall.EINVAL
}
