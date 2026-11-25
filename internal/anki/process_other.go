//go:build !windows

package anki

func defaultIsAnkiRunning() bool {
	return false
}
