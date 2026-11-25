//go:build windows

package main

import (
	"math"
	"unsafe"
)

// pixelCanvas renders smooth SDF anti-aliased UI shapes and embedded Lucide icons
// into a pure-Go 32-bit BGRA buffer before blitting cleanly to the Win32 memory DC.
type pixelCanvas struct {
	w, h    int
	clipY0  int
	clipY1  int
	pixels  []byte
}

func newPixelCanvas(w, h int) *pixelCanvas {
	return &pixelCanvas{
		w:      w,
		h:      h,
		clipY0: 0,
		clipY1: h,
		pixels: make([]byte, w*h*4),
	}
}

func (c *pixelCanvas) setClipY(y0, y1 int32) {
	c.clipY0 = maxInt(0, int(y0))
	c.clipY1 = minInt(c.h, int(y1))
}

func (c *pixelCanvas) resetClipY() {
	c.clipY0 = 0
	c.clipY1 = c.h
}

func (c *pixelCanvas) clear(col rgba) {
	p := c.pixels
	for i := 0; i < len(p); i += 4 {
		p[i+0] = col.b
		p[i+1] = col.g
		p[i+2] = col.r
		p[i+3] = 255
	}
}

func (c *pixelCanvas) blendPixel(x, y int, col rgba, cov float64) {
	if x < 0 || y < c.clipY0 || x >= c.w || y >= c.clipY1 || cov <= 0 {
		return
	}
	if cov > 1 {
		cov = 1
	}
	alpha := cov * (float64(col.a) / 255.0)
	inv := 1.0 - alpha
	idx := (y*c.w + x) * 4
	p := c.pixels
	p[idx+0] = uint8(float64(col.b)*alpha + float64(p[idx+0])*inv + 0.5)
	p[idx+1] = uint8(float64(col.g)*alpha + float64(p[idx+1])*inv + 0.5)
	p[idx+2] = uint8(float64(col.r)*alpha + float64(p[idx+2])*inv + 0.5)
	p[idx+3] = 255
}

func (c *pixelCanvas) fillRect(rc rect, col rgba) {
	x0 := int(rc.Left)
	y0 := int(rc.Top)
	x1 := int(rc.Right)
	y1 := int(rc.Bottom)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < c.clipY0 {
		y0 = c.clipY0
	}
	if x1 > c.w {
		x1 = c.w
	}
	if y1 > c.clipY1 {
		y1 = c.clipY1
	}
	for y := y0; y < y1; y++ {
		base := y * c.w * 4
		for x := x0; x < x1; x++ {
			idx := base + x*4
			c.pixels[idx+0] = col.b
			c.pixels[idx+1] = col.g
			c.pixels[idx+2] = col.r
			c.pixels[idx+3] = 255
		}
	}
}

func (c *pixelCanvas) hline(x1, x2, y int32, col rgba) {
	if int(y) < c.clipY0 || int(y) >= c.clipY1 {
		return
	}
	if x1 < 0 {
		x1 = 0
	}
	if int(x2) > c.w {
		x2 = int32(c.w)
	}
	base := int(y) * c.w * 4
	for x := int(x1); x < int(x2); x++ {
		idx := base + x*4
		c.pixels[idx+0] = col.b
		c.pixels[idx+1] = col.g
		c.pixels[idx+2] = col.r
		c.pixels[idx+3] = 255
	}
}

func roundedBoxSDF(px, py, cx, cy, halfW, halfH, r float64) float64 {
	qx := math.Abs(px-cx) - (halfW - r)
	qy := math.Abs(py-cy) - (halfH - r)
	ox := math.Max(qx, 0)
	oy := math.Max(qy, 0)
	return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - r
}

func (c *pixelCanvas) drawRoundedBox(rc rect, radius float64, fill rgba, border rgba, drawBorder bool) {
	rw := float64(rc.Width())
	rh := float64(rc.Height())
	if rw <= 0 || rh <= 0 {
		return
	}
	r := math.Min(radius, math.Min(rw/2, rh/2))
	cx := float64(rc.Left) + rw/2.0
	cy := float64(rc.Top) + rh/2.0
	hw := rw / 2.0
	hh := rh / 2.0

	x0 := maxInt(0, int(math.Floor(float64(rc.Left)))-1)
	y0 := maxInt(c.clipY0, int(math.Floor(float64(rc.Top)))-1)
	x1 := minInt(c.w, int(math.Ceil(float64(rc.Right)))+1)
	y1 := minInt(c.clipY1, int(math.Ceil(float64(rc.Bottom)))+1)

	// Fast interior span fill away from rounded edges/border
	ir := int(math.Ceil(r)) + 2
	fx0 := maxInt(x0, int(rc.Left)+ir)
	fx1 := minInt(x1, int(rc.Right)-ir)
	fy0 := maxInt(y0, int(rc.Top)+2)
	fy1 := minInt(y1, int(rc.Bottom)-2)

	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		if y >= fy0 && y < fy1 && fx0 < fx1 {
			for x := x0; x < fx0; x++ {
				c.drawRoundedBoxPixel(x, y, float64(x)+0.5, py, cx, cy, hw, hh, r, fill, border, drawBorder)
			}
			base := y * c.w * 4
			for x := fx0; x < fx1; x++ {
				idx := base + x*4
				c.pixels[idx+0] = fill.b
				c.pixels[idx+1] = fill.g
				c.pixels[idx+2] = fill.r
				c.pixels[idx+3] = 255
			}
			for x := fx1; x < x1; x++ {
				c.drawRoundedBoxPixel(x, y, float64(x)+0.5, py, cx, cy, hw, hh, r, fill, border, drawBorder)
			}
		} else {
			for x := x0; x < x1; x++ {
				c.drawRoundedBoxPixel(x, y, float64(x)+0.5, py, cx, cy, hw, hh, r, fill, border, drawBorder)
			}
		}
	}
}

func (c *pixelCanvas) drawRoundedBoxPixel(x, y int, px, py, cx, cy, hw, hh, r float64, fill, border rgba, drawBorder bool) {
	d := roundedBoxSDF(px, py, cx, cy, hw, hh, r)
	if d > 0.5 {
		return
	}
	outerCov := clamp01(0.5 - d)
	if !drawBorder {
		c.blendPixel(x, y, fill, outerCov)
		return
	}
	// Border occupies ring [-1.0, 0.0], fill occupies inside [-inf, -1.0]
	innerCov := clamp01(-0.5 - d)
	if innerCov >= 1.0 {
		c.blendPixel(x, y, fill, 1.0)
	} else if innerCov <= 0.0 {
		c.blendPixel(x, y, border, outerCov)
	} else {
		// Smooth transition between fill and border
		c.blendPixel(x, y, border, outerCov)
		c.blendPixel(x, y, fill, innerCov)
	}
}

func (c *pixelCanvas) fillCircle(cx, cy, r float64, col rgba) {
	x0 := maxInt(0, int(math.Floor(cx-r-1)))
	y0 := maxInt(c.clipY0, int(math.Floor(cy-r-1)))
	x1 := minInt(c.w, int(math.Ceil(cx+r+1)))
	y1 := minInt(c.clipY1, int(math.Ceil(cy+r+1)))
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			px := float64(x) + 0.5
			d := math.Hypot(px-cx, py-cy) - r
			if d < 0.5 {
				c.blendPixel(x, y, col, clamp01(0.5-d))
			}
		}
	}
}

func (c *pixelCanvas) drawToggleSwitch(rc rect, on bool, p ThemePalette) {
	trackCol := p.ToggleOff
	if on {
		trackCol = p.ToggleOn
	}
	h := float64(rc.Height())
	c.drawRoundedBox(rc, h/2.0, trackCol, trackCol, false)
	pad := math.Max(2.0, h*0.14)
	kr := (h - pad*2) / 2.0
	kx := float64(rc.Left) + pad + kr
	if on {
		kx = float64(rc.Right) - pad - kr
	}
	ky := float64(rc.Top) + h/2.0
	c.fillCircle(kx, ky, kr, p.ToggleKnob)
}

func (c *pixelCanvas) drawLucideIcon(name string, x, y, size int32, tint rgba) {
	mask := getLucideIconMask(name, int(size))
	if mask == nil {
		return
	}
	s := mask.size
	for iy := 0; iy < s; iy++ {
		dy := int(y) + iy
		if dy < c.clipY0 || dy >= c.clipY1 {
			continue
		}
		rowOff := iy * s
		for ix := 0; ix < s; ix++ {
			a := mask.alpha[rowOff+ix]
			if a == 0 {
				continue
			}
			dx := int(x) + ix
			if dx < 0 || dx >= c.w {
				continue
			}
			c.blendPixel(dx, dy, tint, float64(a)/255.0)
		}
	}
}

func (c *pixelCanvas) flushToHDC(hdc uintptr, dstX, dstY, srcX, srcY, w, h int32) {
	if w <= 0 || h <= 0 || len(c.pixels) == 0 {
		return
	}
	var bmi bitmapInfoHeader
	bmi.biSize = uint32(unsafe.Sizeof(bmi))
	bmi.biWidth = int32(c.w)
	bmi.biHeight = -int32(c.h)
	bmi.biPlanes = 1
	bmi.biBitCount = 32
	bmi.biCompression = biRGB

	_, _, _ = procStretchDIBits.Call(
		hdc,
		uintptr(dstX), uintptr(dstY), uintptr(w), uintptr(h),
		uintptr(srcX), uintptr(srcY), uintptr(w), uintptr(h),
		uintptr(unsafe.Pointer(&c.pixels[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		srcCopy,
	)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
