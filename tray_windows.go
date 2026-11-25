//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	wmTrayCallback = wmApp + 50

	cmdTrayShow     = 1001
	cmdTrayPause    = 1002
	cmdTraySettings = 1003
	cmdTrayQuit     = 1004

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800

	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmReturnCmd   = 0x0100

	wmRButtonUp    = 0x0205
	idiApplication = 32512
)

var (
	shell32DLL          = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon = shell32DLL.NewProc("Shell_NotifyIconW")
	procExtractIconW    = shell32DLL.NewProc("ExtractIconW")

	procLoadIconW       = user32DLL.NewProc("LoadIconW")
	procDestroyIcon     = user32DLL.NewProc("DestroyIcon")
	procCreatePopupMenu = user32DLL.NewProc("CreatePopupMenu")
	procDestroyMenu     = user32DLL.NewProc("DestroyMenu")
	procAppendMenuW     = user32DLL.NewProc("AppendMenuW")
	procTrackPopupMenu  = user32DLL.NewProc("TrackPopupMenu")
	procGetCursorPos    = user32DLL.NewProc("GetCursorPos")
)

type trayGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type trayNotifyIconData struct {
	Size                       uint32
	Wnd                        uintptr
	ID, Flags, CallbackMessage uint32
	Icon                       uintptr
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   trayGUID
	BalloonIcon                uintptr
}

type nativeTray struct {
	hwnd             uintptr
	hIcon            uintptr
	ownIcon          bool
	added            bool
	wmTaskbarCreated uint32
}

func (t *nativeTray) init(hwnd, hInst uintptr) {
	t.hwnd = hwnd

	taskbarStr, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	reg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarStr)))
	t.wmTaskbarCreated = uint32(reg)

	if exePath, err := os.Executable(); err == nil {
		if exePtr, err2 := syscall.UTF16PtrFromString(exePath); err2 == nil {
			ic, _, _ := procExtractIconW.Call(hInst, uintptr(unsafe.Pointer(exePtr)), 0)
			if ic > 1 {
				t.hIcon = ic
				t.ownIcon = true
			}
		}
	}
	if t.hIcon == 0 {
		ic, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
		t.hIcon = ic
	}

	t.addOrUpdate(false, false)
}

func (t *nativeTray) addOrUpdate(modify bool, paused bool) {
	if t.hwnd == 0 {
		return
	}
	var nid trayNotifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = t.hwnd
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmTrayCallback
	nid.Icon = t.hIcon

	tip := "Anki Passive Widget"
	if paused {
		tip = "Anki Passive Widget (Paused)"
	}
	if u16, err := syscall.UTF16FromString(tip); err == nil {
		copy(nid.Tip[:], u16)
	}

	cmd := uintptr(nimAdd)
	if modify {
		cmd = uintptr(nimModify)
	}
	res, _, _ := procShellNotifyIcon.Call(cmd, uintptr(unsafe.Pointer(&nid)))
	if !modify && res != 0 {
		t.added = true
	}
}

func (t *nativeTray) setPaused(paused bool) {
	if t.added {
		t.addOrUpdate(true, paused)
	}
}

func (t *nativeTray) showPopupMenu(paused bool, onShow, onTogglePause, onSettings, onQuit func()) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer func() { _, _, _ = procDestroyMenu.Call(menu) }()

	appendItem := func(flags uintptr, id uintptr, label string) {
		var ptr uintptr
		if label != "" {
			u16, _ := syscall.UTF16PtrFromString(label)
			ptr = uintptr(unsafe.Pointer(u16))
		}
		_, _, _ = procAppendMenuW.Call(menu, flags, id, ptr)
	}

	pauseLabel := "Pause Timer"
	if paused {
		pauseLabel = "Resume Timer"
	}

	appendItem(mfString, cmdTrayShow, "Show Card")
	appendItem(mfString, cmdTrayPause, pauseLabel)
	appendItem(mfString, cmdTraySettings, "Settings")
	appendItem(mfSeparator, 0, "")
	appendItem(mfString, cmdTrayQuit, "Quit")

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	chosen, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightAlign|tpmBottomAlign|tpmReturnCmd,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		t.hwnd,
		0,
	)
	_, _, _ = procPostMessageW.Call(t.hwnd, 0, 0, 0)

	switch chosen {
	case cmdTrayShow:
		if onShow != nil {
			onShow()
		}
	case cmdTrayPause:
		if onTogglePause != nil {
			onTogglePause()
		}
	case cmdTraySettings:
		if onSettings != nil {
			onSettings()
		}
	case cmdTrayQuit:
		if onQuit != nil {
			onQuit()
		}
	}
}

func (t *nativeTray) destroy() {
	if t.added && t.hwnd != 0 {
		var nid trayNotifyIconData
		nid.Size = uint32(unsafe.Sizeof(nid))
		nid.Wnd = t.hwnd
		nid.ID = 1
		_, _, _ = procShellNotifyIcon.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(&nid)))
		t.added = false
	}
	if t.ownIcon && t.hIcon != 0 {
		_, _, _ = procDestroyIcon.Call(t.hIcon)
		t.hIcon = 0
		t.ownIcon = false
	}
}
