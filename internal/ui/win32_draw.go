//go:build windows

package ui

import (
	"fmt"
	"strings"
	"unsafe"

	"anki-passive-widget/internal/app"
)

type lineAtom struct {
	node    app.CardNode
	text    string
	ruby    string
	width   int32
	baseW   int32
	rubyW   int32
	height  int32
	baseH   int32
	rubyH   int32
	isImage bool
}

type layoutLine struct {
	atoms  []lineAtom
	width  int32
	height int32
}

type textCmd struct {
	font  uintptr
	txt   string
	rc    rect
	color uint32
	flags uint32
}

type dibImgCmd struct {
	rc  rect
	dib *app.DecodedDIB
}

func (w *WidgetWindow) renderToHDC(hwnd, hdc uintptr) {
	var cr rect
	_, _, _ = procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	cw, ch := cr.Width(), cr.Height()
	if cw <= 0 || ch <= 0 {
		return
	}

	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return
	}
	defer func() { _, _, _ = procDeleteDC.Call(memDC) }()

	memBmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(cw), uintptr(ch))
	if memBmp == 0 {
		return
	}
	defer func() { _, _, _ = procDeleteObject.Call(memBmp) }()

	oldBmp, _, _ := procSelectObject.Call(memDC, memBmp)
	defer func() { _, _, _ = procSelectObject.Call(memDC, oldBmp) }()

	pal := w.palette()
	canvas := newPixelCanvas(int(cw), int(ch))
	canvas.clear(pal.CanvasBg)

	w.hits = w.hits[:0]

	if w.zoomedSrc != "" && w.tab == tabReview {
		w.renderZoomOverlay(canvas, memDC, cr, pal)
		_, _, _ = procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), memDC, 0, 0, srcCopy)
		return
	}

	var texts []textCmd
	var imgs []dibImgCmd

	// Header Bar (clean, airy, blends naturally with canvas)
	headerH := w.dp(36)
	w.headerH = headerH
	iconSz := w.dp(14)
	btnSz := w.dp(24)
	btnY := (headerH - btnSz) / 2
	rightX := cw - w.dp(10)

	// Close / Snooze button (Lucide x)
	closeRc := rect{Left: rightX - btnSz, Top: btnY, Right: rightX, Bottom: btnY + btnSz}
	w.drawLucideHeaderBtn(canvas, closeRc, "header:close", "x", iconSz, pal, pal.TextSecondary, func() {
		w.doSnooze()
	})
	rightX -= btnSz + w.dp(4)

	// Settings button (Lucide settings)
	settRc := rect{Left: rightX - btnSz, Top: btnY, Right: rightX, Bottom: btnY + btnSz}
	w.drawLucideToggleHeaderBtn(canvas, settRc, "header:settings", "settings", iconSz, pal, w.tab == tabSettings, func() {
		if w.tab == tabSettings {
			w.closeSettingsToReview()
		} else {
			w.openSettingsTab()
		}
	})
	rightX -= btnSz + w.dp(4)

	// Quick Light/Dark Toggle button (Lucide sun / moon)
	themeIcon := "moon"
	if pal.IsDark {
		themeIcon = "sun"
	}
	themeRc := rect{Left: rightX - btnSz, Top: btnY, Right: rightX, Bottom: btnY + btnSz}
	w.drawLucideHeaderBtn(canvas, themeRc, "header:theme", themeIcon, iconSz, pal, pal.TextSecondary, func() {
		w.toggleQuickTheme()
	})
	rightX -= btnSz + w.dp(4)

	// Replay audio button (Lucide volume-2)
	hasAudio := w.card != nil && (len(w.card.FrontAudio) > 0 || len(w.card.BackAudio) > 0)
	if w.tab == tabReview && hasAudio {
		audRc := rect{Left: rightX - btnSz, Top: btnY, Right: rightX, Bottom: btnY + btnSz}
		w.drawLucideHeaderBtn(canvas, audRc, "header:audio", "volume-2", iconSz, pal, pal.Ink, func() {
			w.doReplayAudio()
		})
		rightX -= btnSz + w.dp(4)
	}

	// Pause badge
	if w.paused {
		canvas.drawLucideIcon("pause", rightX-iconSz-w.dp(2), (headerH-iconSz)/2, iconSz, pal.AutoHidePause)
		rightX -= iconSz + w.dp(6)
	}

	// Deck pill / Settings title on the left
	titleTxt := "Anki"
	if w.tab == tabSettings {
		titleTxt = "Settings"
	} else if w.card != nil && w.card.DeckName != "" {
		titleTxt = shortDeckLabel(w.card.DeckName)
	} else if w.cfg.SelectedDeck != "" {
		titleTxt = shortDeckLabel(w.cfg.SelectedDeck)
	}

	if w.tab == tabSettings {
		texts = append(texts, textCmd{
			font:  w.fontBodyBold,
			txt:   titleTxt,
			rc:    rect{Left: w.dp(12), Top: 0, Right: rightX - w.dp(8), Bottom: headerH},
			color: pal.Ink.win32(),
			flags: dtLeft | dtVCenter | dtSingleLine,
		})
	} else {
		pillPadX := w.dp(8)
		tw, _ := measureTextSize(memDC, w.fontTiny, titleTxt)
		maxPillW := rightX - w.dp(14)
		showProg := w.card != nil && w.card.CardID > 0 && rightX > w.dp(210)
		if showProg {
			maxPillW = rightX - w.dp(106)
		}
		pillW := tw + pillPadX*2
		if pillW > maxPillW {
			pillW = maxPillW
		}
		if pillW > w.dp(36) {
			pillRc := rect{Left: w.dp(10), Top: w.dp(7), Right: w.dp(10) + pillW, Bottom: headerH - w.dp(7)}
			canvas.drawRoundedBox(pillRc, float64(pillRc.Height())/2.0, pal.Surface, pal.Border, true)
			texts = append(texts, textCmd{
				font:  w.fontTiny,
				txt:   titleTxt,
				rc:    rect{Left: pillRc.Left + pillPadX, Top: pillRc.Top, Right: pillRc.Right - pillPadX, Bottom: pillRc.Bottom},
				color: pal.TextSecondary.win32(),
				flags: dtLeft | dtVCenter | dtSingleLine | dtEndEllipsis,
			})

			if showProg {
				barX := pillRc.Right + w.dp(8)
				barW := w.dp(36)
				if barX+barW+w.dp(46) <= rightX {
					barH := w.dp(4)
					barY := (headerH - barH) / 2
					barRc := rect{Left: barX, Top: barY, Right: barX + barW, Bottom: barY + barH}
					canvas.drawRoundedBox(barRc, float64(barH)/2.0, pal.ProgressTrack, pal.ProgressTrack, false)
					rem := w.card.TotalRemaining
					initTot := w.progress.InitialTotal
					if rem > initTot {
						initTot = rem
					}
					if initTot > 0 && initTot > rem {
						fillW := (barW * int32(initTot-rem)) / int32(initTot)
						if fillW > 0 {
							if fillW < barH {
								fillW = barH
							}
							canvas.drawRoundedBox(rect{Left: barX, Top: barY, Right: barX + fillW, Bottom: barY + barH}, float64(barH)/2.0, pal.ProgressFill, pal.ProgressFill, false)
						}
					}
					cntTxt := fmt.Sprintf("%d new", w.card.RemainingNew)
					if w.card.RemainingDue > 0 {
						cntTxt = fmt.Sprintf("%d due", w.card.RemainingDue)
					}
					texts = append(texts, textCmd{
						font:  w.fontTiny,
						txt:   cntTxt,
						rc:    rect{Left: barX + barW + w.dp(6), Top: 0, Right: rightX - w.dp(4), Bottom: headerH},
						color: pal.TextMuted.win32(),
						flags: dtLeft | dtVCenter | dtSingleLine,
					})
				}
			}
		}
	}

	if w.tab == tabSettings {
		w.renderSettings(canvas, memDC, cr, headerH, pal, texts)
		_, _, _ = procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), memDC, 0, 0, srcCopy)
		return
	}

	// Review Mode
	w.renderReview(canvas, memDC, cr, headerH, pal, &texts, &imgs)

	// Blit vector canvas, draw crisp images & ClearType text cleanly on top
	canvas.flushToHDC(memDC, 0, 0, 0, 0, cw, ch)
	for _, im := range imgs {
		drawDIBScaled(memDC, im.rc, im.dib)
	}
	// Re-apply card viewport clip only to scrolled card texts if needed — handled cleanly inside texts loop via SaveDC
	for _, t := range texts {
		drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
	}

	_, _, _ = procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), memDC, 0, 0, srcCopy)
}

func shortDeckLabel(full string) string {
	parts := strings.Split(full, "::")
	if len(parts) <= 1 {
		return full
	}
	return parts[len(parts)-1]
}

func (w *WidgetWindow) drawLucideHeaderBtn(c *pixelCanvas, rc rect, id, icon string, iconSz int32, pal ThemePalette, tint rgba, action func()) {
	bg := pal.CanvasBg
	bdr := pal.CanvasBg
	hasBdr := false
	if w.hoverID == id {
		bg = pal.Surface
		bdr = pal.Border
		hasBdr = true
		tint = pal.Ink
	}
	if hasBdr {
		c.drawRoundedBox(rc, float64(w.dp(6)), bg, bdr, true)
	}
	ix := rc.Left + (rc.Width()-iconSz)/2
	iy := rc.Top + (rc.Height()-iconSz)/2
	c.drawLucideIcon(icon, ix, iy, iconSz, tint)
	w.hits = append(w.hits, hitRegion{id: id, rc: rc, cursor: hitCursorHand, action: action})
}

func (w *WidgetWindow) drawLucideToggleHeaderBtn(c *pixelCanvas, rc rect, id, icon string, iconSz int32, pal ThemePalette, active bool, action func()) {
	bg := pal.CanvasBg
	bdr := pal.Border
	tint := pal.TextSecondary
	if active {
		bg = pal.Ink
		bdr = pal.Ink
		tint = pal.InkInverse
		c.drawRoundedBox(rc, float64(w.dp(6)), bg, bdr, false)
	} else if w.hoverID == id {
		bg = pal.Surface
		tint = pal.Ink
		c.drawRoundedBox(rc, float64(w.dp(6)), bg, bdr, true)
	}
	ix := rc.Left + (rc.Width()-iconSz)/2
	iy := rc.Top + (rc.Height()-iconSz)/2
	c.drawLucideIcon(icon, ix, iy, iconSz, tint)
	w.hits = append(w.hits, hitRegion{id: id, rc: rc, cursor: hitCursorHand, action: action})
}

func (w *WidgetWindow) drawBtn(c *pixelCanvas, rc rect, id string, bg, bdr rgba, hasBdr bool, icon string, iconTint rgba, label string, fg rgba, texts *[]textCmd, action func()) {
	c.drawRoundedBox(rc, float64(w.dp(8)), bg, bdr, hasBdr)
	if icon != "" {
		iSz := w.dp(13)
		gap := w.dp(5)
		// Measure label if needed via memDC? Approximating or placing icon left-aligned inside centered span is crisp; let's center text with small icon offset.
		ix := rc.Left + w.dp(10)
		iy := rc.Top + (rc.Height()-iSz)/2
		if label == "" {
			ix = rc.Left + (rc.Width()-iSz)/2
		}
		c.drawLucideIcon(icon, ix, iy, iSz, iconTint)
		if label != "" {
			*texts = append(*texts, textCmd{
				font:  w.fontTiny,
				txt:   label,
				rc:    rect{Left: ix + iSz + gap, Top: rc.Top, Right: rc.Right - w.dp(6), Bottom: rc.Bottom},
				color: fg.win32(),
				flags: dtLeft | dtVCenter | dtSingleLine,
			})
		}
	} else if label != "" {
		*texts = append(*texts, textCmd{
			font:  w.fontTiny,
			txt:   label,
			rc:    rc,
			color: fg.win32(),
			flags: dtCenter | dtVCenter | dtSingleLine,
		})
	}
	w.hits = append(w.hits, hitRegion{id: id, rc: rc, cursor: hitCursorHand, action: action})
}

func (w *WidgetWindow) renderReview(c *pixelCanvas, memDC uintptr, cr rect, headerH int32, pal ThemePalette, texts *[]textCmd, imgs *[]dibImgCmd) {
	cw, ch := cr.Width(), cr.Height()
	padX := w.dp(10)

	// Error State
	if w.errorMsg != "" {
		cardRc := rect{Left: padX, Top: headerH + w.dp(2), Right: cw - padX, Bottom: ch - w.dp(54)}
		c.drawRoundedBox(cardRc, float64(w.dp(10)), pal.Surface, pal.Border, true)
		c.drawLucideIcon("info", (cw-w.dp(22))/2, cardRc.Top+w.dp(18), w.dp(22), pal.DangerFg)
		*texts = append(*texts,
			textCmd{
				font:  w.fontBodyBold,
				txt:   "Unable to Load Card",
				rc:    rect{Left: cardRc.Left + w.dp(12), Top: cardRc.Top + w.dp(46), Right: cardRc.Right - w.dp(12), Bottom: cardRc.Top + w.dp(68)},
				color: pal.Ink.win32(),
				flags: dtCenter | dtSingleLine,
			},
			textCmd{
				font:  w.fontTiny,
				txt:   w.errorMsg,
				rc:    rect{Left: cardRc.Left + w.dp(14), Top: cardRc.Top + w.dp(70), Right: cardRc.Right - w.dp(14), Bottom: cardRc.Bottom - w.dp(10)},
				color: pal.TextMuted.win32(),
				flags: dtCenter | dtWordBreak,
			},
		)
		btnW := w.dp(92)
		btnH := w.dp(30)
		gap := w.dp(8)
		bx := (cw - (btnW*2 + gap)) / 2
		by := ch - w.dp(44)
		pBg := pal.PrimaryBg
		if w.hoverID == "err:retry" {
			pBg = pal.PrimaryHover
		}
		sBg := pal.SecondaryBg
		if w.hoverID == "err:hide" {
			sBg = pal.SecondaryHover
		}
		w.drawBtn(c, rect{Left: bx, Top: by, Right: bx + btnW, Bottom: by + btnH}, "err:retry", pBg, pBg, false, "rotate-ccw", pal.PrimaryFg, "Retry", pal.PrimaryFg, texts, func() {
			if w.ctrl != nil {
				go func() { _, _ = w.ctrl.ShowCardNow() }()
			}
		})
		w.drawBtn(c, rect{Left: bx + btnW + gap, Top: by, Right: bx + btnW*2 + gap, Bottom: by + btnH}, "err:hide", sBg, pal.Border, true, "", pal.SecondaryFg, "Hide", pal.SecondaryFg, texts, func() {
			w.doSnooze()
		})
		return
	}

	// Loading State
	if w.card == nil {
		cardRc := rect{Left: padX, Top: headerH + w.dp(2), Right: cw - padX, Bottom: ch - w.dp(12)}
		c.drawRoundedBox(cardRc, float64(w.dp(10)), pal.Surface, pal.Border, true)
		*texts = append(*texts, textCmd{
			font:  w.fontSmall,
			txt:   "Loading card...",
			rc:    cardRc,
			color: pal.TextMuted.win32(),
			flags: dtCenter | dtVCenter | dtSingleLine,
		})
		return
	}

	// All Caught Up State
	if w.card.TotalRemaining == 0 {
		cardRc := rect{Left: padX, Top: headerH + w.dp(2), Right: cw - padX, Bottom: ch - w.dp(54)}
		c.drawRoundedBox(cardRc, float64(w.dp(10)), pal.Surface, pal.Border, true)
		c.drawLucideIcon("circle-check", (cw-w.dp(26))/2, cardRc.Top+w.dp(22), w.dp(26), pal.SuccessFg)
		*texts = append(*texts,
			textCmd{
				font:  w.fontTitle,
				txt:   "All Caught Up",
				rc:    rect{Left: cardRc.Left + w.dp(12), Top: cardRc.Top + w.dp(54), Right: cardRc.Right - w.dp(12), Bottom: cardRc.Top + w.dp(78)},
				color: pal.Ink.win32(),
				flags: dtCenter | dtSingleLine,
			},
			textCmd{
				font:  w.fontTiny,
				txt:   "No due or new cards remaining.",
				rc:    rect{Left: cardRc.Left + w.dp(12), Top: cardRc.Top + w.dp(80), Right: cardRc.Right - w.dp(12), Bottom: cardRc.Bottom - w.dp(10)},
				color: pal.TextMuted.win32(),
				flags: dtCenter | dtSingleLine,
			},
		)
		btnW := w.dp(96)
		btnH := w.dp(30)
		sBg := pal.SecondaryBg
		if w.hoverID == "done:close" {
			sBg = pal.SecondaryHover
		}
		w.drawBtn(c, rect{Left: (cw - btnW) / 2, Top: ch - w.dp(44), Right: (cw + btnW) / 2, Bottom: ch - w.dp(44) + btnH}, "done:close", sBg, pal.Border, true, "", pal.SecondaryFg, "Close", pal.SecondaryFg, texts, func() {
			if w.ctrl != nil {
				w.ctrl.HideWidget()
			}
		})
		return
	}

	// Footer & Auto-hide Strip
	stripH := int32(0)
	if !w.isRevealed {
		stripH = w.dp(3)
	}
	footerH := w.dp(44)
	footerTop := ch - stripH - footerH

	btnTop := footerTop + w.dp(6)
	btnBot := footerTop + footerH - w.dp(6)
	if !w.isRevealed {
		snzW := w.dp(90)
		gap := w.dp(8)
		sBg := pal.SecondaryBg
		if w.hoverID == "rev:snooze" {
			sBg = pal.SecondaryHover
		}
		pBg := pal.PrimaryBg
		if w.hoverID == "rev:show" {
			pBg = pal.PrimaryHover
		}
		w.drawBtn(c, rect{Left: padX, Top: btnTop, Right: padX + snzW, Bottom: btnBot}, "rev:snooze", sBg, pal.Border, true, "clock", pal.TextSecondary, "Snooze", pal.SecondaryFg, texts, func() {
			w.doSnooze()
		})
		showRc := rect{Left: padX + snzW + gap, Top: btnTop, Right: cw - padX, Bottom: btnBot}
		c.drawRoundedBox(showRc, float64(w.dp(8)), pBg, pBg, false)
		iSz := w.dp(14)
		lblW, _ := measureTextSize(memDC, w.fontSmall, "Show Answer")
		totW := iSz + w.dp(6) + lblW
		sx := showRc.Left + (showRc.Width()-totW)/2
		c.drawLucideIcon("eye", sx, showRc.Top+(showRc.Height()-iSz)/2, iSz, pal.PrimaryFg)
		*texts = append(*texts, textCmd{
			font:  w.fontSmall,
			txt:   "Show Answer",
			rc:    rect{Left: sx + iSz + w.dp(6), Top: showRc.Top, Right: showRc.Right - w.dp(6), Bottom: showRc.Bottom},
			color: pal.PrimaryFg.win32(),
			flags: dtLeft | dtVCenter | dtSingleLine,
		})
		w.hits = append(w.hits, hitRegion{id: "rev:show", rc: showRc, cursor: hitCursorHand, action: func() { w.doReveal() }})
	} else {
		gap := w.dp(6)
		avail := cw - padX*2 - gap*3
		bw := avail / 4
		opts := []struct {
			ease int
			txt  string
			dot  rgba
			fg   rgba
			hov  rgba
		}{
			{1, "Again", pal.AgainDot, pal.AgainFg, pal.AgainHov},
			{2, "Hard", pal.HardDot, pal.HardFg, pal.HardHov},
			{3, "Good", pal.GoodDot, pal.GoodFg, pal.GoodHov},
			{4, "Easy", pal.EasyDot, pal.EasyFg, pal.EasyHov},
		}
		for i, o := range opts {
			ease := o.ease
			x := padX + int32(i)*(bw+gap)
			rc := rect{Left: x, Top: btnTop, Right: x + bw, Bottom: btnBot}
			id := fmt.Sprintf("ease:%d", ease)
			bg := pal.Surface
			bdr := pal.Border
			if w.hoverID == id {
				bg = o.hov
				bdr = pal.BorderStrong
			}
			c.drawRoundedBox(rc, float64(w.dp(8)), bg, bdr, true)
			dotR := float64(w.dp(3))
			dotX := float64(rc.Left + w.dp(12))
			dotY := float64(rc.Top) + float64(rc.Height())/2.0
			c.fillCircle(dotX, dotY, dotR, o.dot)
			*texts = append(*texts, textCmd{
				font:  w.fontSmall,
				txt:   o.txt,
				rc:    rect{Left: rc.Left + w.dp(18), Top: rc.Top, Right: rc.Right - w.dp(4), Bottom: rc.Bottom},
				color: o.fg.win32(),
				flags: dtCenter | dtVCenter | dtSingleLine,
			})
			w.hits = append(w.hits, hitRegion{id: id, rc: rc, cursor: hitCursorHand, action: func() { w.doAnswer(ease) }})
		}
	}

	if stripH > 0 {
		stripTop := ch - stripH
		c.fillRect(rect{Left: 0, Top: stripTop, Right: cw, Bottom: ch}, pal.ProgressTrack)
		pct := int32(w.secondsLeft*100) / autoHideSeconds
		barClr := pal.AutoHideFill
		if w.isHovered {
			barClr = pal.AutoHidePause
		}
		c.fillRect(rect{Left: 0, Top: stripTop, Right: (cw * pct) / 100, Bottom: ch}, barClr)
	}

	// Main Elevated White/Charcoal Card Container
	cardRc := rect{Left: padX, Top: headerH + w.dp(2), Right: cw - padX, Bottom: footerTop - w.dp(4)}
	c.drawRoundedBox(cardRc, float64(w.dp(10)), pal.Surface, pal.Border, true)

	bodyTop := cardRc.Top + w.dp(2)
	bodyBot := cardRc.Bottom - w.dp(2)
	bodyH := bodyBot - bodyTop
	if bodyH <= 0 {
		return
	}

	contentPadX := w.dp(14)
	contentPadY := w.dp(10)
	maxLineW := cardRc.Width() - contentPadX*2 - w.dp(6)

	frontLines := w.layoutNodes(memDC, w.frontNodes, maxLineW, w.fontFront, w.fontRuby)
	var backLines []layoutLine
	if w.isRevealed {
		backLines = w.layoutNodes(memDC, w.backNodes, maxLineW, w.fontBody, w.fontRuby)
	}

	totalH := contentPadY * 2
	for _, ln := range frontLines {
		totalH += ln.height
	}
	dividerGap := w.dp(14)
	if w.isRevealed {
		totalH += dividerGap
		for _, ln := range backLines {
			totalH += ln.height
		}
	}

	maxScr := totalH - bodyH
	if maxScr < 0 {
		maxScr = 0
	}
	w.cardMaxScroll = maxScr
	if w.cardScrollY > maxScr {
		w.cardScrollY = maxScr
	}
	if w.cardScrollY < 0 {
		w.cardScrollY = 0
	}

	startY := bodyTop + contentPadY - w.cardScrollY
	if maxScr == 0 {
		startY = bodyTop + (bodyH-totalH)/2 + contentPadY
	}

	curY := startY
	c.setClipY(bodyTop, bodyBot)
	curY = w.collectLinesCentered(c, frontLines, cardRc.Left+contentPadX, maxLineW, curY, bodyTop, bodyBot, pal.Ink, pal, true, texts, imgs)
	if w.isRevealed {
		divY := curY + dividerGap/2
		if divY >= bodyTop && divY <= bodyBot {
			c.hline(cardRc.Left+contentPadX, cardRc.Right-contentPadX, divY, pal.Divider)
		}
		curY += dividerGap
		_ = w.collectLinesCentered(c, backLines, cardRc.Left+contentPadX, maxLineW, curY, bodyTop, bodyBot, pal.TextSecondary, pal, false, texts, imgs)
	}
	c.resetClipY()

	// Clean thin scrollbar pill inside right edge of card
	if maxScr > 0 && totalH > 0 {
		sbW := w.dp(3)
		sbX := cardRc.Right - sbW - w.dp(4)
		thumbH := (bodyH * bodyH) / totalH
		if thumbH < w.dp(20) {
			thumbH = w.dp(20)
		}
		thumbY := bodyTop + ((bodyH - thumbH) * w.cardScrollY / maxScr)
		c.drawRoundedBox(rect{Left: sbX, Top: thumbY, Right: sbX + sbW, Bottom: thumbY + thumbH}, float64(sbW)/2.0, pal.BorderStrong, pal.BorderStrong, false)
	}
}

func (w *WidgetWindow) layoutNodes(hdc uintptr, nodes []app.CardNode, maxW int32, mainFont, rubyFont uintptr) []layoutLine {
	if len(nodes) == 0 {
		return nil
	}
	_, lineH := measureTextSize(hdc, mainFont, "Ag漢字")
	_, rtH := measureTextSize(hdc, rubyFont, "あ")
	if lineH <= 0 {
		lineH = w.dp(18)
	}
	if rtH <= 0 {
		rtH = w.dp(12)
	}

	var lines []layoutLine
	var cur layoutLine

	flushLine := func(forceEmpty bool) {
		if len(cur.atoms) == 0 {
			if forceEmpty {
				lines = append(lines, layoutLine{height: lineH / 2})
			}
			return
		}
		var maxH, sumW int32
		for _, a := range cur.atoms {
			sumW += a.width
			if a.height > maxH {
				maxH = a.height
			}
		}
		cur.width = sumW
		cur.height = maxH + w.dp(4)
		lines = append(lines, cur)
		cur = layoutLine{}
	}

	addAtom := func(a lineAtom) {
		if a.isImage {
			flushLine(false)
			lines = append(lines, layoutLine{atoms: []lineAtom{a}, width: a.width, height: a.height + w.dp(6)})
			return
		}
		if len(cur.atoms) > 0 && cur.width+a.width > maxW {
			flushLine(false)
		}
		cur.atoms = append(cur.atoms, a)
		cur.width += a.width
	}

	for _, n := range nodes {
		switch n.Kind {
		case app.NodeBreak:
			flushLine(true)
		case app.NodeImage:
			imgW, imgH := maxW, w.dp(90)
			if dib, ok := w.imageCache[n.Src]; ok && dib != nil && dib.Width > 0 && dib.Height > 0 {
				maxImgH := w.dp(110)
				imgW = int32(dib.Width)
				imgH = int32(dib.Height)
				if imgW > maxW {
					imgH = (imgH * maxW) / imgW
					imgW = maxW
				}
				if imgH > maxImgH {
					imgW = (imgW * maxImgH) / imgH
					imgH = maxImgH
				}
			}
			addAtom(lineAtom{node: n, width: imgW, height: imgH, isImage: true})
		case app.NodeRuby:
			bw, bh := measureTextSize(hdc, mainFont, n.Text)
			rw, _ := measureTextSize(hdc, rubyFont, n.Ruby)
			aw := bw
			if rw > aw {
				aw = rw
			}
			aw += w.dp(2)
			addAtom(lineAtom{
				node:   n,
				text:   n.Text,
				ruby:   n.Ruby,
				width:  aw,
				baseW:  bw,
				rubyW:  rw,
				height: bh + rtH,
				baseH:  bh,
				rubyH:  rtH,
			})
		case app.NodeText:
			runes := []rune(n.Text)
			i := 0
			for i < len(runes) {
				j := i + 1
				if runes[i] < 128 && runes[i] != ' ' {
					for j < len(runes) && runes[j] < 128 && runes[j] != ' ' {
						j++
					}
				}
				chunk := string(runes[i:j])
				cw, ch := measureTextSize(hdc, mainFont, chunk)
				if len(cur.atoms) == 0 && chunk == " " {
					i = j
					continue
				}
				if len(cur.atoms) > 0 && cur.width+cw > maxW {
					flushLine(false)
					if chunk == " " {
						i = j
						continue
					}
				}
				cur.atoms = append(cur.atoms, lineAtom{
					node:   n,
					text:   chunk,
					width:  cw,
					baseW:  cw,
					height: ch,
					baseH:  ch,
				})
				cur.width += cw
				i = j
			}
		}
	}
	flushLine(false)
	return lines
}

func (w *WidgetWindow) collectLinesCentered(
	c *pixelCanvas,
	lines []layoutLine,
	areaX, areaW, startY, clipTop, clipBot int32,
	fg rgba,
	pal ThemePalette,
	isFront bool,
	texts *[]textCmd,
	imgs *[]dibImgCmd,
) int32 {
	font := w.fontBody
	if isFront {
		font = w.fontFront
	}
	y := startY
	for _, ln := range lines {
		if y+ln.height > clipTop && y < clipBot {
			x := areaX + (areaW-ln.width)/2
			if x < areaX {
				x = areaX
			}
			for _, a := range ln.atoms {
				if a.isImage {
					imgRc := rect{Left: x, Top: y + w.dp(3), Right: x + a.width, Bottom: y + w.dp(3) + a.height}
					// Clamp only partially visible images inside card bounds
					if imgRc.Top >= clipTop && imgRc.Bottom <= clipBot {
						src := a.node.Src
						if dib, ok := w.imageCache[src]; ok && dib != nil {
							borderRc := rect{Left: imgRc.Left - 1, Top: imgRc.Top - 1, Right: imgRc.Right + 1, Bottom: imgRc.Bottom + 1}
							bdr := pal.Border
							if w.hoverID == "img:"+src {
								bdr = pal.Ink
							}
							c.drawRoundedBox(borderRc, float64(w.dp(6)), pal.Surface, bdr, true)
							*imgs = append(*imgs, dibImgCmd{rc: imgRc, dib: dib})
							w.hits = append(w.hits, hitRegion{
								id:     "img:" + src,
								rc:     imgRc,
								cursor: hitCursorHand,
								action: func() {
									w.zoomedSrc = src
									w.expandForImage()
									w.syncAutoHideTimer()
									w.redraw()
								},
							})
						} else {
							c.drawRoundedBox(imgRc, float64(w.dp(6)), pal.SurfaceSubtle, pal.Border, true)
							*texts = append(*texts, textCmd{
								font:  w.fontTiny,
								txt:   "Loading image...",
								rc:    imgRc,
								color: pal.TextMuted.win32(),
								flags: dtCenter | dtVCenter | dtSingleLine,
							})
						}
					}
					x += a.width
					continue
				}

				if a.ruby != "" {
					rtY := y + (ln.height - w.dp(4) - a.height)
					if rtY < y {
						rtY = y
					}
					baseY := rtY + a.rubyH
					if rtY >= clipTop && baseY+a.baseH <= clipBot {
						*texts = append(*texts,
							textCmd{
								font:  w.fontRuby,
								txt:   a.ruby,
								rc:    rect{Left: x, Top: rtY, Right: x + a.width, Bottom: rtY + a.rubyH},
								color: pal.RubyText.win32(),
								flags: dtCenter | dtSingleLine,
							},
							textCmd{
								font:  font,
								txt:   a.text,
								rc:    rect{Left: x, Top: baseY, Right: x + a.width, Bottom: baseY + a.baseH},
								color: fg.win32(),
								flags: dtCenter | dtSingleLine,
							},
						)
					}
				} else {
					baseY := y + (ln.height-w.dp(4)-a.baseH)/2
					if baseY >= clipTop && baseY+a.baseH <= clipBot {
						*texts = append(*texts, textCmd{
							font:  font,
							txt:   a.text,
							rc:    rect{Left: x, Top: baseY, Right: x + a.width + w.dp(2), Bottom: baseY + a.baseH},
							color: fg.win32(),
							flags: dtLeft | dtSingleLine,
						})
					}
				}
				x += a.width
			}
		}
		y += ln.height
	}
	return y
}

func (w *WidgetWindow) renderZoomOverlay(c *pixelCanvas, memDC uintptr, cr rect, pal ThemePalette) {
	w.hits = w.hits[:0]
	cw, ch := cr.Width(), cr.Height()
	c.clear(pal.CanvasBg)

	pad := w.dp(10)
	topH := w.dp(34)
	var texts []textCmd

	closeW := w.dp(74)
	closeRc := rect{Left: cw - pad - closeW, Top: w.dp(5), Right: cw - pad, Bottom: topH - w.dp(3)}
	bg := pal.Surface
	if w.hoverID == "zoom:close" {
		bg = pal.SurfaceHover
	}
	w.drawBtn(c, closeRc, "zoom:close", bg, pal.Border, true, "minimize-2", pal.Ink, "Close", pal.Ink, &texts, func() {
		w.zoomedSrc = ""
		if w.isRevealed {
			w.showAndPosition(app.BackWidth, app.BackHeight, true)
		} else {
			w.showAndPosition(app.FrontWidth, app.FrontHeight, true)
		}
		w.syncAutoHideTimer()
	})

	box := rect{Left: pad, Top: topH + w.dp(2), Right: cw - pad, Bottom: ch - pad}
	c.drawRoundedBox(box, float64(w.dp(10)), pal.Surface, pal.Border, true)

	c.flushToHDC(memDC, 0, 0, 0, 0, cw, ch)

	if dib, ok := w.imageCache[w.zoomedSrc]; ok && dib != nil && dib.Width > 0 && dib.Height > 0 {
		availW := box.Width() - w.dp(14)
		availH := box.Height() - w.dp(14)
		dw, dh := int32(dib.Width), int32(dib.Height)
		if dw > availW {
			dh = (dh * availW) / dw
			dw = availW
		}
		if dh > availH {
			dw = (dw * availH) / dh
			dh = availH
		}
		if dw < availW/2 && dh < availH/2 {
			scaleW := availW / dw
			scaleH := availH / dh
			scale := scaleW
			if scaleH < scale {
				scale = scaleH
			}
			if scale > 2 {
				scale = 2
			}
			if scale >= 2 {
				dw *= scale
				dh *= scale
			}
		}
		dst := rect{
			Left:   box.Left + (box.Width()-dw)/2,
			Top:    box.Top + (box.Height()-dh)/2,
			Right:  box.Left + (box.Width()+dw)/2,
			Bottom: box.Top + (box.Height()+dh)/2,
		}
		drawDIBScaled(memDC, dst, dib)
	}

	for _, t := range texts {
		drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
	}

	w.hits = append(w.hits, hitRegion{
		id:     "zoom:backdrop",
		rc:     box,
		cursor: hitCursorHand,
		action: func() {
			w.zoomedSrc = ""
			if w.isRevealed {
				w.showAndPosition(app.BackWidth, app.BackHeight, true)
			} else {
				w.showAndPosition(app.FrontWidth, app.FrontHeight, true)
			}
			w.syncAutoHideTimer()
		},
	})
}
