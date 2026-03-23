//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	smCMonitors          = 80
	desktopSwitchDesktop = 0x0100
	udName               = 2
	processQueryLimited  = 0x1000
	maxPath              = 260
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procOpenInputDesktop       = user32.NewProc("OpenInputDesktop")
	procCloseDesktop           = user32.NewProc("CloseDesktop")
	procGetUserObjectInfoW     = user32.NewProc("GetUserObjectInformationW")
	procQueryFullProcessImage  = kernel32.NewProc("QueryFullProcessImageNameW")
)

type RealHost struct{}

func NewRealHost() Host {
	return RealHost{}
}

func (RealHost) CaptureCompositeJPEG(dest string, maxDimension int, quality int) error {
	if err := ensureDir(dest); err != nil {
		return err
	}
	script := `
& {
  param([string]$OutPath, [int]$Quality, [int]$MaxDimension)
  Add-Type -AssemblyName System.Windows.Forms
  Add-Type -AssemblyName System.Drawing
  $bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen
  $capture = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
  $graphics = [System.Drawing.Graphics]::FromImage($capture)
  $graphics.CopyFromScreen($bounds.Left, $bounds.Top, 0, 0, $capture.Size)

  $scale = 1.0
  if ($capture.Width -gt $capture.Height) {
    if ($capture.Width -gt $MaxDimension) { $scale = $MaxDimension / [double]$capture.Width }
  } else {
    if ($capture.Height -gt $MaxDimension) { $scale = $MaxDimension / [double]$capture.Height }
  }

  $targetWidth = [Math]::Max([int]($capture.Width * $scale), 1)
  $targetHeight = [Math]::Max([int]($capture.Height * $scale), 1)
  $resized = New-Object System.Drawing.Bitmap $targetWidth, $targetHeight
  $resizedGraphics = [System.Drawing.Graphics]::FromImage($resized)
  $resizedGraphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $resizedGraphics.DrawImage($capture, 0, 0, $targetWidth, $targetHeight)

  $codec = [System.Drawing.Imaging.ImageCodecInfo]::GetImageEncoders() | Where-Object { $_.MimeType -eq 'image/jpeg' }
  $params = New-Object System.Drawing.Imaging.EncoderParameters 1
  $params.Param[0] = New-Object System.Drawing.Imaging.EncoderParameter([System.Drawing.Imaging.Encoder]::Quality, [long]$Quality)
  $resized.Save($OutPath, $codec, $params)

  $resizedGraphics.Dispose()
  $graphics.Dispose()
  $resized.Dispose()
  $capture.Dispose()
}`
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
		dest,
		fmt.Sprintf("%d", quality),
		fmt.Sprintf("%d", maxDimension),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell capture failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (RealHost) DesktopMetadata() (DesktopMetadata, error) {
	locked, err := sessionLocked()
	if err != nil {
		return DesktopMetadata{}, err
	}
	if locked {
		return DesktopMetadata{
			MonitorCount:      monitorCount(),
			ActiveWindowTitle: "",
			ActiveProcess:     "",
			SessionLocked:     true,
		}, nil
	}
	title, pid, err := foregroundWindow()
	if err != nil {
		return DesktopMetadata{}, err
	}
	process := processName(pid)
	monitors := monitorCount()
	return DesktopMetadata{
		MonitorCount:      monitors,
		ActiveWindowTitle: title,
		ActiveProcess:     process,
		SessionLocked:     locked,
	}, nil
}

func ensureDir(dest string) error {
	return os.MkdirAll(filepath.Dir(dest), 0o755)
}

func monitorCount() int {
	value, _, _ := procGetSystemMetrics.Call(smCMonitors)
	if value == 0 {
		return 1
	}
	return int(value)
}

func foregroundWindow() (string, uint32, error) {
	hwnd, _, err := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", 0, err
	}
	var buffer [512]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return syscall.UTF16ToString(buffer[:int(n)]), pid, nil
}

func processName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	handle, err := syscall.OpenProcess(processQueryLimited, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(handle)

	buf := make([]uint16, maxPath)
	size := uint32(len(buf))
	r1, _, _ := procQueryFullProcessImage.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

func sessionLocked() (bool, error) {
	desktop, _, err := procOpenInputDesktop.Call(0, 0, desktopSwitchDesktop)
	if desktop == 0 {
		return true, err
	}
	defer procCloseDesktop.Call(desktop)

	nameBuf := make([]uint16, 256)
	var needed uint32
	r1, _, callErr := procGetUserObjectInfoW.Call(
		desktop,
		udName,
		uintptr(unsafe.Pointer(&nameBuf[0])),
		uintptr(len(nameBuf)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if r1 == 0 {
		return false, callErr
	}
	name := syscall.UTF16ToString(nameBuf)
	return !strings.EqualFold(name, "Default"), nil
}
