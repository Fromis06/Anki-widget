package main

import (
	"embed"
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed assets/icons/*.svg
var embeddedIconsFS embed.FS

type svgDoc struct {
	Paths   []svgPathEl   `xml:"path"`
	Circles []svgCircleEl `xml:"circle"`
	Rects   []svgRectEl   `xml:"rect"`
	Lines   []svgLineEl   `xml:"line"`
}

type svgPathEl struct {
	D string `xml:"d,attr"`
}

type svgCircleEl struct {
	Cx float64 `xml:"cx,attr"`
	Cy float64 `xml:"cy,attr"`
	R  float64 `xml:"r,attr"`
}

type svgRectEl struct {
	X      float64 `xml:"x,attr"`
	Y      float64 `xml:"y,attr"`
	Width  float64 `xml:"width,attr"`
	Height float64 `xml:"height,attr"`
	Rx     float64 `xml:"rx,attr"`
}

type svgLineEl struct {
	X1 float64 `xml:"x1,attr"`
	Y1 float64 `xml:"y1,attr"`
	X2 float64 `xml:"x2,attr"`
	Y2 float64 `xml:"y2,attr"`
}

type iconSeg struct {
	x1, y1, x2, y2 float64
}

type iconMask struct {
	size  int
	alpha []uint8
}

var (
	iconCacheMu sync.RWMutex
	iconSegs    = make(map[string][]iconSeg)
	iconMasks   = make(map[iconMaskKey]*iconMask)
)

type iconMaskKey struct {
	name string
	size int
}

func getLucideIconMask(name string, pxSize int) *iconMask {
	if pxSize <= 0 {
		return nil
	}
	key := iconMaskKey{name: name, size: pxSize}
	iconCacheMu.RLock()
	m, ok := iconMasks[key]
	iconCacheMu.RUnlock()
	if ok {
		return m
	}

	segs := loadIconSegments(name)
	if len(segs) == 0 {
		return nil
	}
	m = rasterizeLucideMask(segs, pxSize)
	iconCacheMu.Lock()
	iconMasks[key] = m
	iconCacheMu.Unlock()
	return m
}

func loadIconSegments(name string) []iconSeg {
	iconCacheMu.RLock()
	if segs, ok := iconSegs[name]; ok {
		iconCacheMu.RUnlock()
		return segs
	}
	iconCacheMu.RUnlock()

	raw, err := embeddedIconsFS.ReadFile("assets/icons/" + name + ".svg")
	if err != nil {
		return nil
	}
	var doc svgDoc
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	var segs []iconSeg
	for _, l := range doc.Lines {
		segs = append(segs, iconSeg{l.X1, l.Y1, l.X2, l.Y2})
	}
	for _, c := range doc.Circles {
		const steps = 28
		for i := 0; i < steps; i++ {
			a1 := (2 * math.Pi * float64(i)) / steps
			a2 := (2 * math.Pi * float64(i+1)) / steps
			segs = append(segs, iconSeg{
				c.Cx + c.R*math.Cos(a1), c.Cy + c.R*math.Sin(a1),
				c.Cx + c.R*math.Cos(a2), c.Cy + c.R*math.Sin(a2),
			})
		}
	}
	for _, rc := range doc.Rects {
		r := rc.Rx
		if r > rc.Width/2 {
			r = rc.Width / 2
		}
		if r > rc.Height/2 {
			r = rc.Height / 2
		}
		if r <= 0 {
			segs = append(segs,
				iconSeg{rc.X, rc.Y, rc.X + rc.Width, rc.Y},
				iconSeg{rc.X + rc.Width, rc.Y, rc.X + rc.Width, rc.Y + rc.Height},
				iconSeg{rc.X + rc.Width, rc.Y + rc.Height, rc.X, rc.Y + rc.Height},
				iconSeg{rc.X, rc.Y + rc.Height, rc.X, rc.Y},
			)
		} else {
			x0, y0, x1, y1 := rc.X, rc.Y, rc.X+rc.Width, rc.Y+rc.Height
			segs = append(segs,
				iconSeg{x0 + r, y0, x1 - r, y0},
				iconSeg{x1, y0 + r, x1, y1 - r},
				iconSeg{x1 - r, y1, x0 + r, y1},
				iconSeg{x0, y1 - r, x0, y0 + r},
			)
			appendArcQuarter(&segs, x1-r, y0+r, r, -math.Pi/2, 0)
			appendArcQuarter(&segs, x1-r, y1-r, r, 0, math.Pi/2)
			appendArcQuarter(&segs, x0+r, y1-r, r, math.Pi/2, math.Pi)
			appendArcQuarter(&segs, x0+r, y0+r, r, math.Pi, 3*math.Pi/2)
		}
	}
	for _, p := range doc.Paths {
		segs = append(segs, parseSVGPathSegments(p.D)...)
	}

	iconCacheMu.Lock()
	iconSegs[name] = segs
	iconCacheMu.Unlock()
	return segs
}

func appendArcQuarter(segs *[]iconSeg, cx, cy, r, a0, a1 float64) {
	const steps = 6
	for i := 0; i < steps; i++ {
		t0 := a0 + (a1-a0)*float64(i)/steps
		t1 := a0 + (a1-a0)*float64(i+1)/steps
		*segs = append(*segs, iconSeg{
			cx + r*math.Cos(t0), cy + r*math.Sin(t0),
			cx + r*math.Cos(t1), cy + r*math.Sin(t1),
		})
	}
}

// rasterizeLucideMask renders 24x24 Lucide stroke paths (stroke-width=2) with smooth SDF anti-aliasing.
func rasterizeLucideMask(segs []iconSeg, pxSize int) *iconMask {
	alpha := make([]uint8, pxSize*pxSize)
	scale := float64(pxSize) / 24.0
	halfStroke := 1.0 * scale // Lucide default stroke-width=2 -> radius 1.0 in 24x24 viewBox

	// 4x4 subpixel coverage sampling produces clean anti-aliased Lucide icons with round caps.
	const sub = 4
	const invSub2 = 1.0 / float64(sub*sub)
	for py := 0; py < pxSize; py++ {
		for px := 0; px < pxSize; px++ {
			covered := 0.0
			for sy := 0; sy < sub; sy++ {
				fy := float64(py) + (float64(sy)+0.5)/sub
				for sx := 0; sx < sub; sx++ {
					fx := float64(px) + (float64(sx)+0.5)/sub
					minDistSq := math.MaxFloat64
					for _, sg := range segs {
						d2 := distPointToSegmentSq(fx, fy, sg.x1*scale, sg.y1*scale, sg.x2*scale, sg.y2*scale)
						if d2 < minDistSq {
							minDistSq = d2
						}
					}
					if minDistSq <= halfStroke*halfStroke {
						covered += invSub2
					}
				}
			}
			if covered > 0 {
				v := int(math.Round(covered * 255))
				if v > 255 {
					v = 255
				}
				alpha[py*pxSize+px] = uint8(v)
			}
		}
	}
	return &iconMask{size: pxSize, alpha: alpha}
}

func distPointToSegmentSq(px, py, x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	l2 := dx*dx + dy*dy
	if l2 <= 1e-9 {
		ex, ey := px-x1, py-y1
		return ex*ex + ey*ey
	}
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	qx := x1 + t*dx
	qy := y1 + t*dy
	ex, ey := px-qx, py-qy
	return ex*ex + ey*ey
}

func parseSVGPathSegments(d string) []iconSeg {
	tokens := tokenizeSVGPath(d)
	var segs []iconSeg
	var cx, cy, sx, sy float64
	var lastCtrlX, lastCtrlY float64
	var lastCmd byte
	idx := 0

	nextNum := func() (float64, bool) {
		if idx >= len(tokens) {
			return 0, false
		}
		v, err := strconv.ParseFloat(tokens[idx], 64)
		if err != nil {
			return 0, false
		}
		idx++
		return v, true
	}

	nextFlag := func() (bool, bool) {
		if idx >= len(tokens) {
			return false, false
		}
		v, err := strconv.ParseFloat(tokens[idx], 64)
		if err != nil {
			return false, false
		}
		idx++
		return v != 0, true
	}

	isCmd := func(tok string) bool {
		if len(tok) != 1 {
			return false
		}
		r := rune(tok[0])
		return unicode.IsLetter(r)
	}

	var cmd byte
	for idx < len(tokens) {
		if isCmd(tokens[idx]) {
			cmd = tokens[idx][0]
			idx++
		} else if cmd == 0 {
			break
		}

		switch cmd {
		case 'M', 'm':
			x, ok1 := nextNum()
			y, ok2 := nextNum()
			if !ok1 || !ok2 {
				return segs
			}
			if cmd == 'm' {
				x += cx
				y += cy
				cmd = 'l'
			} else {
				cmd = 'L'
			}
			cx, cy = x, y
			sx, sy = x, y
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = 'M'
		case 'L', 'l':
			x, ok1 := nextNum()
			y, ok2 := nextNum()
			if !ok1 || !ok2 {
				return segs
			}
			if cmd == 'l' {
				x += cx
				y += cy
			}
			segs = append(segs, iconSeg{cx, cy, x, y})
			cx, cy = x, y
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = cmd
		case 'H', 'h':
			x, ok := nextNum()
			if !ok {
				return segs
			}
			if cmd == 'h' {
				x += cx
			}
			segs = append(segs, iconSeg{cx, cy, x, cy})
			cx = x
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = cmd
		case 'V', 'v':
			y, ok := nextNum()
			if !ok {
				return segs
			}
			if cmd == 'v' {
				y += cy
			}
			segs = append(segs, iconSeg{cx, cy, cx, y})
			cy = y
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = cmd
		case 'C', 'c':
			x1, ok1 := nextNum()
			y1, ok2 := nextNum()
			x2, ok3 := nextNum()
			y2, ok4 := nextNum()
			x, ok5 := nextNum()
			y, ok6 := nextNum()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
				return segs
			}
			if cmd == 'c' {
				x1 += cx
				y1 += cy
				x2 += cx
				y2 += cy
				x += cx
				y += cy
			}
			appendCubic(&segs, cx, cy, x1, y1, x2, y2, x, y)
			lastCtrlX, lastCtrlY = x2, y2
			cx, cy = x, y
			lastCmd = cmd
		case 'S', 's':
			x2, ok1 := nextNum()
			y2, ok2 := nextNum()
			x, ok3 := nextNum()
			y, ok4 := nextNum()
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return segs
			}
			x1, y1 := cx, cy
			if lastCmd == 'C' || lastCmd == 'c' || lastCmd == 'S' || lastCmd == 's' {
				x1 = 2*cx - lastCtrlX
				y1 = 2*cy - lastCtrlY
			}
			if cmd == 's' {
				x2 += cx
				y2 += cy
				x += cx
				y += cy
			}
			appendCubic(&segs, cx, cy, x1, y1, x2, y2, x, y)
			lastCtrlX, lastCtrlY = x2, y2
			cx, cy = x, y
			lastCmd = cmd
		case 'Q', 'q':
			x1, ok1 := nextNum()
			y1, ok2 := nextNum()
			x, ok3 := nextNum()
			y, ok4 := nextNum()
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return segs
			}
			if cmd == 'q' {
				x1 += cx
				y1 += cy
				x += cx
				y += cy
			}
			appendQuadratic(&segs, cx, cy, x1, y1, x, y)
			lastCtrlX, lastCtrlY = x1, y1
			cx, cy = x, y
			lastCmd = cmd
		case 'A', 'a':
			rx, ok1 := nextNum()
			ry, ok2 := nextNum()
			rot, ok3 := nextNum()
			largeArc, ok4 := nextFlag()
			sweep, ok5 := nextFlag()
			x, ok6 := nextNum()
			y, ok7 := nextNum()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
				return segs
			}
			if cmd == 'a' {
				x += cx
				y += cy
			}
			appendSVGArc(&segs, cx, cy, rx, ry, rot, largeArc, sweep, x, y)
			cx, cy = x, y
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = cmd
		case 'Z', 'z':
			if cx != sx || cy != sy {
				segs = append(segs, iconSeg{cx, cy, sx, sy})
			}
			cx, cy = sx, sy
			lastCtrlX, lastCtrlY = cx, cy
			lastCmd = cmd
		default:
			idx++
		}
	}
	return segs
}

func appendCubic(segs *[]iconSeg, x0, y0, x1, y1, x2, y2, x3, y3 float64) {
	const steps = 10
	px, py := x0, y0
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		u := 1 - t
		nx := u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
		ny := u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
		*segs = append(*segs, iconSeg{px, py, nx, ny})
		px, py = nx, ny
	}
}

func appendQuadratic(segs *[]iconSeg, x0, y0, x1, y1, x2, y2 float64) {
	const steps = 8
	px, py := x0, y0
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		u := 1 - t
		nx := u*u*x0 + 2*u*t*x1 + t*t*x2
		ny := u*u*y0 + 2*u*t*y1 + t*t*y2
		*segs = append(*segs, iconSeg{px, py, nx, ny})
		px, py = nx, ny
	}
}

func appendSVGArc(segs *[]iconSeg, x1, y1, rx, ry, angleDeg float64, largeArc, sweep bool, x2, y2 float64) {
	if rx == 0 || ry == 0 || (x1 == x2 && y1 == y2) {
		*segs = append(*segs, iconSeg{x1, y1, x2, y2})
		return
	}
	rx = math.Abs(rx)
	ry = math.Abs(ry)
	phi := angleDeg * math.Pi / 180.0
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)

	dx2 := (x1 - x2) / 2.0
	dy2 := (y1 - y2) / 2.0
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	lambda := (x1p*x1p)/(rx*rx) + (y1p*y1p)/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}

	num := math.Max(0, rx*rx*ry*ry-rx*rx*y1p*y1p-ry*ry*x1p*x1p)
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	coef := 0.0
	if den > 1e-12 {
		coef = math.Sqrt(num / den)
	}
	if largeArc == sweep {
		coef = -coef
	}
	cxp := coef * ((rx * y1p) / ry)
	cyp := coef * (-(ry * x1p) / rx)

	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2.0
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2.0

	vecAngle := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		lenU := math.Hypot(ux, uy)
		lenV := math.Hypot(vx, vy)
		if lenU == 0 || lenV == 0 {
			return 0
		}
		cosT := dot / (lenU * lenV)
		if cosT < -1 {
			cosT = -1
		} else if cosT > 1 {
			cosT = 1
		}
		ang := math.Acos(cosT)
		if ux*vy-uy*vx < 0 {
			ang = -ang
		}
		return ang
	}

	theta1 := vecAngle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := vecAngle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	steps := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 8)))
	if steps < 4 {
		steps = 4
	}
	px, py := x1, y1
	for i := 1; i <= steps; i++ {
		t := theta1 + dTheta*float64(i)/float64(steps)
		ex := rx * math.Cos(t)
		ey := ry * math.Sin(t)
		nx := cosPhi*ex - sinPhi*ey + cx
		ny := sinPhi*ex + cosPhi*ey + cy
		*segs = append(*segs, iconSeg{px, py, nx, ny})
		px, py = nx, ny
	}
}

func tokenizeSVGPath(d string) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for i := 0; i < len(d); i++ {
		ch := d[i]
		if ch == ' ' || ch == ',' || ch == '\t' || ch == '\n' || ch == '\r' {
			flush()
			continue
		}
		if unicode.IsLetter(rune(ch)) && ch != 'e' && ch != 'E' {
			flush()
			out = append(out, string(ch))
			continue
		}
		if ch == '-' {
			if buf.Len() > 0 {
				prev := buf.String()[buf.Len()-1]
				if prev != 'e' && prev != 'E' {
					flush()
				}
			}
			buf.WriteByte(ch)
			continue
		}
		if ch == '.' && strings.ContainsRune(buf.String(), '.') {
			flush()
		}
		buf.WriteByte(ch)
	}
	flush()
	return out
}
