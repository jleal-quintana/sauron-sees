//go:build windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockFile(file *os.File, nonBlocking bool) error {
	var overlapped syscall.Overlapped
	flags := uint32(lockfileExclusiveLock)
	if nonBlocking {
		flags |= lockfileFailImmediately
	}
	r1, _, err := procLockFileEx.Call(file.Fd(), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if r1 == 0 {
		if errors.Is(err, syscall.Errno(33)) {
			return ErrUnavailable
		}
		return fmt.Errorf("lock file: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if r1 == 0 {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}
