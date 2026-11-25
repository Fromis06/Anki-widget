//go:build !windows

package app

import "runtime/debug"

func trimIdleProcessMemory() {
	debug.FreeOSMemory()
}

