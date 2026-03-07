//go:build !windows

package startup

import "errors"

func Install(taskName string, executable string, configPath string) error {
	return errors.New("startup task installation is only supported on Windows")
}

func Uninstall(taskName string) error {
	return errors.New("startup task removal is only supported on Windows")
}
