//go:build !windows

package main

type audioPlayer struct{}

func (p *audioPlayer) Play(_ string, _ []byte, _ string) {}
func (p *audioPlayer) Stop()                             {}

