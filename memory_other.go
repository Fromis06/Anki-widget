//go:build !windows

package main

import "runtime/debug"

func trimIdleProcessMemory() {
	debug.FreeOSMemory()
}

