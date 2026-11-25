//go:build windows

package anki

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultIsAnkiRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return false
	}
	for {
		exeName := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(exeName, "anki.exe") {
			return true
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return false
}
