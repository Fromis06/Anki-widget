//go:build !windows

package app

type audioPlayer struct{}

func (p *audioPlayer) Play(_ string, _ []byte, _ string) {}
func (p *audioPlayer) Stop()                             {}

