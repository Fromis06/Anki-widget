//go:build windows

package ui

import (
	"golang.org/x/sys/windows/registry"
)

type rgba struct {
	r, g, b, a uint8
}

func cRGB(r, g, b uint8) rgba {
	return rgba{r: r, g: g, b: b, a: 255}
}

func (c rgba) win32() uint32 {
	return uint32(c.r) | (uint32(c.g) << 8) | (uint32(c.b) << 16)
}

type ThemePalette struct {
	IsDark bool

	CanvasBg      rgba
	Surface       rgba
	SurfaceSubtle rgba
	SurfaceHover  rgba
	Border        rgba
	BorderStrong  rgba
	Divider       rgba

	Ink           rgba
	InkInverse    rgba
	TextSecondary rgba
	TextMuted     rgba
	RubyText      rgba

	PrimaryBg    rgba
	PrimaryHover rgba
	PrimaryFg    rgba

	SecondaryBg    rgba
	SecondaryHover rgba
	SecondaryFg    rgba

	ProgressTrack rgba
	ProgressFill  rgba
	AutoHideFill  rgba
	AutoHidePause rgba

	ChipFrontBg  rgba
	ChipFrontBdr rgba
	ChipFrontFg  rgba
	ChipBackBg   rgba
	ChipBackBdr  rgba
	ChipBackFg   rgba

	ToggleOn   rgba
	ToggleOff  rgba
	ToggleKnob rgba

	StatusOnline  rgba
	StatusOffline rgba

	AgainDot rgba
	AgainFg  rgba
	AgainHov rgba
	HardDot  rgba
	HardFg   rgba
	HardHov  rgba
	GoodDot  rgba
	GoodFg   rgba
	GoodHov  rgba
	EasyDot  rgba
	EasyFg   rgba
	EasyHov  rgba

	RowSelectedBg rgba
	RowSelectedFg rgba
	OverlayDimBg  rgba
	DangerFg      rgba
	SuccessFg     rgba
}

var lightPalette = ThemePalette{
	IsDark: false,

	// Clean bento light inspired by Swiss/Fluent card UI (no AI neon glow)
	CanvasBg:      cRGB(241, 243, 246),
	Surface:       cRGB(255, 255, 255),
	SurfaceSubtle: cRGB(243, 244, 247),
	SurfaceHover:  cRGB(233, 236, 240),
	Border:        cRGB(224, 227, 232),
	BorderStrong:  cRGB(203, 208, 216),
	Divider:       cRGB(235, 237, 240),

	Ink:           cRGB(24, 24, 27),
	InkInverse:    cRGB(255, 255, 255),
	TextSecondary: cRGB(82, 82, 91),
	TextMuted:     cRGB(140, 145, 155),
	RubyText:      cRGB(37, 99, 235),

	PrimaryBg:    cRGB(24, 24, 27),
	PrimaryHover: cRGB(39, 39, 42),
	PrimaryFg:    cRGB(255, 255, 255),

	SecondaryBg:    cRGB(255, 255, 255),
	SecondaryHover: cRGB(244, 244, 245),
	SecondaryFg:    cRGB(39, 39, 42),

	ProgressTrack: cRGB(229, 231, 235),
	ProgressFill:  cRGB(16, 185, 129),
	AutoHideFill:  cRGB(24, 24, 27),
	AutoHidePause: cRGB(217, 119, 6),

	ChipFrontBg:  cRGB(239, 246, 255),
	ChipFrontBdr: cRGB(191, 219, 254),
	ChipFrontFg:  cRGB(29, 78, 216),
	ChipBackBg:   cRGB(236, 253, 245),
	ChipBackBdr:  cRGB(167, 243, 208),
	ChipBackFg:   cRGB(4, 120, 87),

	ToggleOn:   cRGB(24, 24, 27),
	ToggleOff:  cRGB(209, 213, 219),
	ToggleKnob: cRGB(255, 255, 255),

	StatusOnline:  cRGB(16, 185, 129),
	StatusOffline: cRGB(107, 114, 128),

	AgainDot: cRGB(239, 68, 68),
	AgainFg:  cRGB(185, 28, 28),
	AgainHov: cRGB(254, 242, 242),
	HardDot:  cRGB(245, 158, 11),
	HardFg:   cRGB(180, 83, 9),
	HardHov:  cRGB(255, 251, 235),
	GoodDot:  cRGB(16, 185, 129),
	GoodFg:   cRGB(4, 120, 87),
	GoodHov:  cRGB(236, 253, 245),
	EasyDot:  cRGB(59, 130, 246),
	EasyFg:   cRGB(29, 78, 216),
	EasyHov:  cRGB(239, 246, 255),

	RowSelectedBg: cRGB(24, 24, 27),
	RowSelectedFg: cRGB(255, 255, 255),
	OverlayDimBg:  cRGB(244, 245, 247),
	DangerFg:      cRGB(220, 38, 38),
	SuccessFg:     cRGB(5, 150, 105),
}

var darkPalette = ThemePalette{
	IsDark: true,

	// Matte neutral charcoal bento (zero neon cyberpunk blues)
	CanvasBg:      cRGB(20, 20, 23),
	Surface:       cRGB(29, 29, 33),
	SurfaceSubtle: cRGB(39, 39, 44),
	SurfaceHover:  cRGB(48, 48, 54),
	Border:        cRGB(46, 46, 53),
	BorderStrong:  cRGB(63, 63, 72),
	Divider:       cRGB(38, 38, 44),

	Ink:           cRGB(244, 244, 245),
	InkInverse:    cRGB(24, 24, 27),
	TextSecondary: cRGB(161, 161, 170),
	TextMuted:     cRGB(113, 113, 122),
	RubyText:      cRGB(147, 197, 253),

	PrimaryBg:    cRGB(244, 244, 245),
	PrimaryHover: cRGB(255, 255, 255),
	PrimaryFg:    cRGB(24, 24, 27),

	SecondaryBg:    cRGB(29, 29, 33),
	SecondaryHover: cRGB(39, 39, 44),
	SecondaryFg:    cRGB(228, 228, 231),

	ProgressTrack: cRGB(39, 39, 44),
	ProgressFill:  cRGB(52, 211, 153),
	AutoHideFill:  cRGB(228, 228, 231),
	AutoHidePause: cRGB(251, 191, 36),

	ChipFrontBg:  cRGB(30, 41, 59),
	ChipFrontBdr: cRGB(59, 130, 246),
	ChipFrontFg:  cRGB(147, 197, 253),
	ChipBackBg:   cRGB(6, 78, 59),
	ChipBackBdr:  cRGB(16, 185, 129),
	ChipBackFg:   cRGB(110, 231, 183),

	ToggleOn:   cRGB(244, 244, 245),
	ToggleOff:  cRGB(63, 63, 70),
	ToggleKnob: cRGB(24, 24, 27),

	StatusOnline:  cRGB(52, 211, 153),
	StatusOffline: cRGB(161, 161, 170),

	AgainDot: cRGB(248, 113, 113),
	AgainFg:  cRGB(252, 165, 165),
	AgainHov: cRGB(54, 26, 30),
	HardDot:  cRGB(251, 191, 36),
	HardFg:   cRGB(253, 230, 138),
	HardHov:  cRGB(51, 38, 18),
	GoodDot:  cRGB(52, 211, 153),
	GoodFg:   cRGB(167, 243, 208),
	GoodHov:  cRGB(16, 51, 39),
	EasyDot:  cRGB(96, 165, 250),
	EasyFg:   cRGB(191, 219, 254),
	EasyHov:  cRGB(23, 38, 64),

	RowSelectedBg: cRGB(244, 244, 245),
	RowSelectedFg: cRGB(24, 24, 27),
	OverlayDimBg:  cRGB(18, 18, 21),
	DangerFg:      cRGB(248, 113, 113),
	SuccessFg:     cRGB(52, 211, 153),
}

func isWindowsLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()
	val, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return val != 0
}

func resolvePalette(mode string) ThemePalette {
	switch mode {
	case "light":
		return lightPalette
	case "dark":
		return darkPalette
	default:
		if isWindowsLightTheme() {
			return lightPalette
		}
		return darkPalette
	}
}
