package ui

import (
	"testing"

	"anki-passive-widget/internal/config"
)

func TestAllEmbeddedLucideIconsParseAndRasterize(t *testing.T) {
	required := []string{
		"volume-2", "settings", "x", "pause", "clock", "eye",
		"rotate-ccw", "refresh-cw", "chevron-down", "chevron-right", "search", "check",
		"circle-check", "info", "sun", "moon", "monitor",
		"minimize-2", "minus", "plus", "arrow-left-right",
	}
	for _, name := range required {
		mask := getLucideIconMask(name, 16)
		if mask == nil || len(mask.alpha) != 16*16 {
			t.Fatalf("failed to rasterize embedded Lucide icon %q", name)
		}
		nonZero := false
		for _, a := range mask.alpha {
			if a > 0 {
				nonZero = true
				break
			}
		}
		if !nonZero {
			t.Fatalf("Lucide icon %q produced empty mask", name)
		}
	}
}

func TestThemeConfigAndPaletteResolution(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Theme != "system" {
		t.Fatalf("expected default theme 'system', got %q", cfg.Theme)
	}
	cfg.Theme = "DARK"
	cfg.Normalize()
	if cfg.Theme != "dark" {
		t.Fatalf("expected normalized 'dark', got %q", cfg.Theme)
	}
	cfg.Theme = "invalid"
	cfg.Normalize()
	if cfg.Theme != "system" {
		t.Fatalf("expected invalid theme to normalize to 'system', got %q", cfg.Theme)
	}

	lp := resolvePalette("light")
	dp := resolvePalette("dark")
	if lp.IsDark || !dp.IsDark {
		t.Fatalf("unexpected palette IsDark states: light=%v dark=%v", lp.IsDark, dp.IsDark)
	}
}

func TestStudyModeConfigNormalization(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.StudyMode != config.StudyModeBoth {
		t.Fatalf("expected default StudyMode %q, got %q", config.StudyModeBoth, cfg.StudyMode)
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"due", config.StudyModeDue},
		{"  NEW  ", config.StudyModeNew},
		{"Both", config.StudyModeBoth},
		{"", config.StudyModeBoth},
		{"unknown", config.StudyModeBoth},
	} {
		cfg.StudyMode = tc.in
		cfg.Normalize()
		if cfg.StudyMode != tc.want {
			t.Fatalf("Normalize(%q) = %q, want %q", tc.in, cfg.StudyMode, tc.want)
		}
	}
}

func TestIntervalMinutesFromX(t *testing.T) {
	const left, width int32 = 50, 200
	if got := intervalMinutesFromX(left-20, left, width); got != config.MinIntervalMinutes {
		t.Fatalf("left clamp = %d, want %d", got, config.MinIntervalMinutes)
	}
	if got := intervalMinutesFromX(left, left, width); got != config.MinIntervalMinutes {
		t.Fatalf("start = %d, want %d", got, config.MinIntervalMinutes)
	}
	if got := intervalMinutesFromX(left+width, left, width); got != config.MaxIntervalMinutes {
		t.Fatalf("end = %d, want %d", got, config.MaxIntervalMinutes)
	}
	if got := intervalMinutesFromX(left+width+100, left, width); got != config.MaxIntervalMinutes {
		t.Fatalf("right clamp = %d, want %d", got, config.MaxIntervalMinutes)
	}
	mid := intervalMinutesFromX(left+width/2, left, width)
	if mid < 30 || mid > 31 {
		t.Fatalf("midpoint = %d, want 30 or 31", mid)
	}
	if got := intervalMinutesFromX(100, left, 0); got != config.DefaultIntervalMinutes {
		t.Fatalf("zero width fallback = %d, want %d", got, config.DefaultIntervalMinutes)
	}
}

func TestSegmentedProgressBarAndCanvasReuse(t *testing.T) {
	pal := resolvePalette("light")
	c1 := ensurePixelCanvas(nil, 100, 20)
	basePtr := &c1.pixels[0]
	c2 := ensurePixelCanvas(c1, 80, 16)
	if &c2.pixels[0] != basePtr {
		t.Fatalf("expected ensurePixelCanvas to reuse underlying backing slice without reallocating")
	}

	c2.clear(pal.CanvasBg)
	barRc := rect{Left: 4, Top: 5, Right: 64, Bottom: 11}
	c2.drawDualProgressBar(
		barRc, 0.5,
		pal.ProgressDueTrack, pal.ProgressDueFill, 0.5,
		pal.ProgressNewTrack, pal.ProgressNewFill, 0.5,
		pal.CanvasBg,
	)

	// Left filled region should match ProgressDueFill; right filled region should match ProgressNewFill.
	sample := func(x, y int) rgba {
		idx := (y*c2.w + x) * 4
		return rgba{r: c2.pixels[idx+2], g: c2.pixels[idx+1], b: c2.pixels[idx+0], a: 255}
	}
	duePx := sample(10, 8)
	if duePx != pal.ProgressDueFill {
		t.Fatalf("expected left filled pixel %+v, got %+v", pal.ProgressDueFill, duePx)
	}
	newPx := sample(40, 8)
	if newPx != pal.ProgressNewFill {
		t.Fatalf("expected right filled pixel %+v, got %+v", pal.ProgressNewFill, newPx)
	}
}
