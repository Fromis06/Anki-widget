//go:build windows

package main

import (
	"log"
	"runtime/debug"
	"syscall"

	"anki-passive-widget/internal/app"
	"anki-passive-widget/internal/ui"
	"golang.org/x/sys/windows"
)

func main() {
	debug.SetGCPercent(25)
	debug.SetMemoryLimit(8 << 20)

	mutexName, _ := syscall.UTF16PtrFromString("Local\\AnkiPassiveWidgetMutex")
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if handle != 0 {
		defer windows.CloseHandle(handle)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		ui.ActivateExistingInstance()
		return
	}

	win := ui.NewWidgetWindow()
	ctrl := app.NewController(win)
	win.SetController(ctrl)
	defer ctrl.Shutdown()

	if err := win.CreateAndRun(); err != nil {
		log.Fatalf("win32 widget error: %v", err)
	}
}
