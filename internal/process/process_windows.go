//go:build windows

package process

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
)

func IsRunning(pid int) bool {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	r1, _, _ := procGetExitCodeProcess.Call(uintptr(handle), uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 {
		return false
	}
	return exitCode == stillActive
}

func SignalStop(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("send os.Interrupt: %w", err)
	}
	return nil
}
