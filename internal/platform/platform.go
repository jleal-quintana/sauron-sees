package platform

type DesktopMetadata struct {
	MonitorCount      int
	ActiveWindowTitle string
	ActiveProcess     string
	SessionLocked     bool
}

type Host interface {
	CaptureCompositeJPEG(dest string, maxDimension int, quality int) error
	DesktopMetadata() (DesktopMetadata, error)
}
