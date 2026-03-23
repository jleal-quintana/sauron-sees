package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrUnavailable = errors.New("file lock unavailable")

type Guard struct {
	path string
	file *os.File
}

func Lock(path string) (*Guard, error) {
	return acquire(path, false)
}

func TryLock(path string) (*Guard, error) {
	return acquire(path, true)
}

func acquire(path string, nonBlocking bool) (*Guard, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(file, nonBlocking); err != nil {
		file.Close()
		return nil, err
	}
	return &Guard{path: path, file: file}, nil
}

func (g *Guard) File() *os.File {
	return g.file
}

func (g *Guard) Close() error {
	if g == nil || g.file == nil {
		return nil
	}
	if err := unlockFile(g.file); err != nil {
		_ = g.file.Close()
		return err
	}
	return g.file.Close()
}
