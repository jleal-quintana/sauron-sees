//go:build !windows

package tray

import "context"

type Options struct {
	Tooltip         string
	OnCaptureNow    func()
	OnCloseDay      func()
	OnWeeklySummary func()
	OnPause         func()
	OnResume        func()
	OnOpenDaily     func()
	OnOpenWeekly    func()
	OnOpenTemp      func()
	OnDoctor        func()
	OnExit          func()
}

func Start(ctx context.Context, options Options) error {
	return nil
}

func Supported() error {
	return nil
}
