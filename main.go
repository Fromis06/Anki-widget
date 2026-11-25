//go:build windows

package main

import (
	"log"
	"runtime/debug"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const widgetWindowClass = "AnkiPassiveWidgetWindow"

func main() {
	debug.SetGCPercent(25)
	debug.SetMemoryLimit(8 << 20)

	mutexName, _ := syscall.UTF16PtrFromString("Local\\AnkiPassiveWidgetMutex")
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if handle != 0 {
		defer windows.CloseHandle(handle)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		className, _ := syscall.UTF16PtrFromString(widgetWindowClass)
		existing, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
		if existing != 0 {
			msgName, _ := syscall.UTF16PtrFromString("AnkiPassiveWidgetShowNow")
			regMsg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(msgName)))
			if regMsg != 0 {
				_, _, _ = procPostMessageW.Call(existing, regMsg, 0, 0)
			}
		}
		return
	}

	win := NewWidgetWindow()
	ctrl := NewController(win)
	win.SetController(ctrl)
	defer ctrl.Shutdown()

	if err := win.CreateAndRun(); err != nil {
		log.Fatalf("win32 widget error: %v", err)
	}
}
