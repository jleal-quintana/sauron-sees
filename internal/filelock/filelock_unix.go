//go:build !windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockFile(file *os.File, nonBlocking bool) error {
	flags := syscall.LOCK_EX
	if nonBlocking {
		flags |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(file.Fd()), flags); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrUnavailable
		}
		return fmt.Errorf("lock file: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("unlock file: %w", err)
	}
	return nil
}
