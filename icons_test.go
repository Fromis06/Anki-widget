package main

import (
	"testing"

	"anki-passive-widget/internal/config"
)

func TestAllEmbeddedLucideIconsParseAndRasterize(t *testing.T) {
	required := []string{
		"volume-2", "settings", "x", "pause", "clock", "eye",
		"rotate-ccw", "refresh-cw", "chevron-down", "search", "check",
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
