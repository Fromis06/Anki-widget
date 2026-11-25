//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	winmmDLL           = syscall.NewLazyDLL("winmm.dll")
	procMCISendStringW = winmmDLL.NewProc("mciSendStringW")
)

type audioPlayer struct {
	mu       sync.Mutex
	tempPath string
}

func mciSend(cmd string) error {
	p, err := syscall.UTF16PtrFromString(cmd)
	if err != nil {
		return err
	}
	r, _, _ := procMCISendStringW.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0)
	if r != 0 {
		return fmt.Errorf("mci error %d", r)
	}
	return nil
}

func (p *audioPlayer) Play(diskPath string, data []byte, filename string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopLocked()

	target := diskPath
	if target == "" && len(data) > 0 {
		ext := filepath.Ext(filename)
		if ext == "" {
			ext = ".mp3"
		}
		f, err := os.CreateTemp("", "anki-lite-*"+ext)
		if err != nil {
			return
		}
		_, _ = f.Write(data)
		_ = f.Close()
		p.tempPath = f.Name()
		target = f.Name()
	}
	if strings.TrimSpace(target) == "" {
		return
	}

	safePath := strings.ReplaceAll(target, `"`, ``)
	if err := mciSend(fmt.Sprintf(`open "%s" alias ankiw`, safePath)); err != nil {
		return
	}
	_ = mciSend(`play ankiw`)
}

func (p *audioPlayer) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *audioPlayer) stopLocked() {
	_ = mciSend(`stop ankiw`)
	_ = mciSend(`close ankiw`)
	if p.tempPath != "" {
		_ = os.Remove(p.tempPath)
		p.tempPath = ""
	}
}

