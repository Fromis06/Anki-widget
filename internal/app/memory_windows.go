//go:build windows

package app

import (
	"runtime/debug"

	"golang.org/x/sys/windows"
)

func trimIdleProcessMemory() {
	debug.FreeOSMemory()
	// Native Win32 app has zero child WebView2 processes; only trim current process working set.
	_ = windows.SetProcessWorkingSetSizeEx(windows.CurrentProcess(), ^uintptr(0), ^uintptr(0), 0)
}

