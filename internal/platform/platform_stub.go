//go:build !windows

package platform

import "errors"

type RealHost struct{}

func NewRealHost() Host {
	return RealHost{}
}

func (RealHost) CaptureCompositeJPEG(dest string, maxDimension int, quality int) error {
	return errors.New("screen capture is only supported on Windows in this build")
}

func (RealHost) DesktopMetadata() (DesktopMetadata, error) {
	return DesktopMetadata{
		MonitorCount:      0,
		ActiveWindowTitle: "unsupported",
		ActiveProcess:     "unsupported",
		SessionLocked:     false,
	}, nil
}
