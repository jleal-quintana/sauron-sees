//go:build windows

package tray

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

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

const (
	wmApp           = 0x8000
	wmTrayIcon      = wmApp + 1
	wmCommand       = 0x0111
	wmDestroy       = 0x0002
	wmRButtonUp     = 0x0205
	wmLButtonUp     = 0x0202
	nimAdd          = 0x00000000
	nimDelete       = 0x00000002
	nifMessage      = 0x00000001
	nifIcon         = 0x00000002
	nifTip          = 0x00000004
	idCaptureNow    = 1001
	idCloseDay      = 1002
	idWeeklySummary = 1003
	idPause         = 1004
	idResume        = 1005
	idOpenDaily     = 1006
	idOpenWeekly    = 1007
	idOpenTemp      = 1008
	idDoctor        = 1009
	idExit          = 1010
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	shell32                 = windows.NewLazySystemDLL("shell32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
}

var currentOptions Options

func Supported() error {
	return nil
}

func Start(ctx context.Context, options Options) error {
	currentOptions = options
	go func() {
		<-ctx.Done()
		if trayWindow != 0 {
			procPostMessageW.Call(trayWindow, wmDestroy, 0, 0)
		}
	}()
	return runLoop(options)
}

var trayWindow uintptr

func runLoop(options Options) error {
	instance, _, err := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return fmt.Errorf("get module handle: %v", err)
	}
	className, _ := windows.UTF16PtrFromString("SauronSeesTrayWindow")
	icon, _, _ := procLoadIconW.Call(0, 32512)
	wndProc := syscall.NewCallback(windowProc)
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   wndProc,
		Instance:  windows.Handle(instance),
		Icon:      windows.Handle(icon),
		ClassName: className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("register tray class: %v", err)
	}
	title, _ := windows.UTF16PtrFromString("Sauron Sees")
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %v", err)
	}
	trayWindow = hwnd
	if err := addTrayIcon(hwnd, options.Tooltip); err != nil {
		return err
	}
	defer deleteTrayIcon(hwnd)

	var message msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func addTrayIcon(hwnd uintptr, tooltip string) error {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = windows.Handle(hwnd)
	data.UID = 1
	data.UFlags = nifMessage | nifIcon | nifTip
	data.UCallbackMessage = wmTrayIcon
	icon, _, _ := procLoadIconW.Call(0, 32512)
	data.HIcon = windows.Handle(icon)
	copy(data.SzTip[:], windows.StringToUTF16(tooltip))
	ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIconW add failed")
	}
	return nil
}

func deleteTrayIcon(hwnd uintptr) {
	var data notifyIconData
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = windows.Handle(hwnd)
	data.UID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
}

func windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		if uint32(lparam) == wmRButtonUp || uint32(lparam) == wmLButtonUp {
			showMenu(hwnd)
		}
		return 0
	case wmCommand:
		switch uint32(wparam) {
		case idCaptureNow:
			if currentOptions.OnCaptureNow != nil {
				go currentOptions.OnCaptureNow()
			}
		case idCloseDay:
			if currentOptions.OnCloseDay != nil {
				go currentOptions.OnCloseDay()
			}
		case idWeeklySummary:
			if currentOptions.OnWeeklySummary != nil {
				go currentOptions.OnWeeklySummary()
			}
		case idPause:
			if currentOptions.OnPause != nil {
				go currentOptions.OnPause()
			}
		case idResume:
			if currentOptions.OnResume != nil {
				go currentOptions.OnResume()
			}
		case idOpenDaily:
			if currentOptions.OnOpenDaily != nil {
				go currentOptions.OnOpenDaily()
			}
		case idOpenWeekly:
			if currentOptions.OnOpenWeekly != nil {
				go currentOptions.OnOpenWeekly()
			}
		case idOpenTemp:
			if currentOptions.OnOpenTemp != nil {
				go currentOptions.OnOpenTemp()
			}
		case idDoctor:
			if currentOptions.OnDoctor != nil {
				go currentOptions.OnDoctor()
			}
		case idExit:
			if currentOptions.OnExit != nil {
				go currentOptions.OnExit()
			}
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func showMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	appendMenu(menu, idCaptureNow, "Capture Now")
	appendMenu(menu, idCloseDay, "Close Day Now")
	appendMenu(menu, idWeeklySummary, "Generate Weekly Summary Now")
	appendMenu(menu, idPause, "Pause 1 Hour")
	appendMenu(menu, idResume, "Resume")
	appendMenu(menu, idOpenDaily, "Open Daily Folder")
	appendMenu(menu, idOpenWeekly, "Open Weekly Folder")
	appendMenu(menu, idOpenTemp, "Open Temp Folder")
	appendMenu(menu, idDoctor, "Run Doctor")
	appendMenu(menu, idExit, "Exit Agent")
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	procTrackPopupMenu.Call(menu, 0, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procDestroyMenu.Call(menu)
}

func appendMenu(menu uintptr, id uint32, label string) {
	text, _ := windows.UTF16PtrFromString(label)
	procAppendMenuW.Call(menu, 0, uintptr(id), uintptr(unsafe.Pointer(text)))
}
