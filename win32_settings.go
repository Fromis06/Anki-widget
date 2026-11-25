//go:build windows

package main

import (
	"fmt"
	"strings"

	"anki-passive-widget/internal/config"
)

func (w *WidgetWindow) renderSettings(c *pixelCanvas, memDC uintptr, cr rect, headerH int32, pal ThemePalette, headerTexts []textCmd) {
	if cr.Height()-w.dp(46)-headerH <= 0 {
		return
	}
	w.renderSettingsLayered(c, memDC, cr, headerH, pal, headerTexts)
}

func (w *WidgetWindow) renderSettingsLayered(c *pixelCanvas, memDC uintptr, cr rect, headerH int32, pal ThemePalette, headerTexts []textCmd) {
	cw, ch := cr.Width(), cr.Height()
	footerH := w.dp(46)
	bodyTop := headerH
	bodyBot := ch - footerH
	bodyH := bodyBot - bodyTop

	// Reset canvas under bodyTop..bodyBot (keep header clean) and recompute hits after header
	var keptHits []hitRegion
	for _, h := range w.hits {
		if strings.HasPrefix(h.id, "header:") {
			keptHits = append(keptHits, h)
		}
	}
	w.hits = keptHits

	texts := append([]textCmd(nil), headerTexts...)
	c.setClipY(bodyTop, bodyBot)
	c.fillRect(rect{Left: 0, Top: bodyTop, Right: cw, Bottom: bodyBot}, pal.CanvasBg)

	pad := w.dp(10)
	cardW := cw - pad*2 - w.dp(4)
	inPad := w.dp(12)
	y := bodyTop + w.dp(4) - w.settingsScrollY
	startY := y

	// --- Group 1: General ---
	g1H := w.dp(146)
	g1Rc := rect{Left: pad, Top: y, Right: pad + cardW, Bottom: y + g1H}
	c.drawRoundedBox(g1Rc, float64(w.dp(10)), pal.Surface, pal.Border, true)

	g1Y := y + w.dp(10)
	modeLabel := "Local SQLite"
	dotCol := pal.StatusOffline
	if w.sourceMode == "ankiconnect" {
		modeLabel = "AnkiConnect"
		dotCol = pal.StatusOnline
	}
	texts = append(texts,
		textCmd{font: w.fontTiny, txt: "Source", rc: rect{Left: pad + inPad, Top: g1Y, Right: pad + w.dp(100), Bottom: g1Y + w.dp(18)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtVCenter | dtSingleLine},
		textCmd{font: w.fontTiny, txt: modeLabel, rc: rect{Left: pad + w.dp(110), Top: g1Y, Right: pad + cardW - inPad, Bottom: g1Y + w.dp(18)}, color: pal.Ink.win32(), flags: dtRight | dtVCenter | dtSingleLine},
	)
	mw, _ := measureTextSize(memDC, w.fontTiny, modeLabel)
	c.fillCircle(float64(pad+cardW-inPad-mw-w.dp(8)), float64(g1Y)+float64(w.dp(9)), float64(w.dp(3)), dotCol)
	g1Y += w.dp(24)
	c.hline(pad+inPad, pad+cardW-inPad, g1Y, pal.Divider)
	g1Y += w.dp(8)

	// Theme 3-way segmented pill
	texts = append(texts, textCmd{font: w.fontTiny, txt: "Theme", rc: rect{Left: pad + inPad, Top: g1Y, Right: pad + w.dp(70), Bottom: g1Y + w.dp(24)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtVCenter | dtSingleLine})
	segW := cardW - inPad*2 - w.dp(56)
	segRc := rect{Left: pad + cardW - inPad - segW, Top: g1Y, Right: pad + cardW - inPad, Bottom: g1Y + w.dp(24)}
	c.drawRoundedBox(segRc, float64(w.dp(7)), pal.SurfaceSubtle, pal.Border, true)
	themeOpts := []struct {
		key, label, icon string
	}{
		{"system", "System", "monitor"},
		{"light", "Light", "sun"},
		{"dark", "Dark", "moon"},
	}
	segItemW := segW / 3
	for i, opt := range themeOpts {
		key := opt.key
		optRc := rect{Left: segRc.Left + int32(i)*segItemW + w.dp(2), Top: segRc.Top + w.dp(2), Right: segRc.Left + int32(i+1)*segItemW - w.dp(2), Bottom: segRc.Bottom - w.dp(2)}
		active := w.draft.Theme == key || (w.draft.Theme == "" && key == "system")
		fg := pal.TextSecondary
		if active {
			c.drawRoundedBox(optRc, float64(w.dp(5)), pal.PrimaryBg, pal.PrimaryBg, false)
			fg = pal.PrimaryFg
		}
		iSz := w.dp(11)
		lblW, _ := measureTextSize(memDC, w.fontTiny, opt.label)
		tot := iSz + w.dp(3) + lblW
		ix := optRc.Left + (optRc.Width()-tot)/2
		c.drawLucideIcon(opt.icon, ix, optRc.Top+(optRc.Height()-iSz)/2, iSz, fg)
		texts = append(texts, textCmd{
			font:  w.fontTiny,
			txt:   opt.label,
			rc:    rect{Left: ix + iSz + w.dp(3), Top: optRc.Top, Right: optRc.Right, Bottom: optRc.Bottom},
			color: fg.win32(),
			flags: dtLeft | dtVCenter | dtSingleLine,
		})
		if optRc.Bottom > bodyTop && optRc.Top < bodyBot {
			w.hits = append(w.hits, hitRegion{
				id: "sett:theme:" + key, rc: clampRect(optRc, bodyTop, bodyBot), cursor: hitCursorHand,
				action: func() {
					w.draft.Theme = key
					w.applyWindowFrameTheme()
					w.redraw()
				},
			})
		}
	}
	g1Y += w.dp(32)
	c.hline(pad+inPad, pad+cardW-inPad, g1Y, pal.Divider)
	g1Y += w.dp(8)

	// Profile
	texts = append(texts, textCmd{font: w.fontTiny, txt: "Profile", rc: rect{Left: pad + inPad, Top: g1Y, Right: pad + w.dp(74), Bottom: g1Y + w.dp(24)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtVCenter | dtSingleLine})
	profName := w.draft.SelectedProfile
	if profName == "" {
		profName = "Default"
	}
	profRc := rect{Left: pad + w.dp(84), Top: g1Y, Right: pad + cardW - inPad, Bottom: g1Y + w.dp(24)}
	pFill := pal.SurfaceSubtle
	if w.hoverID == "sett:profile" && len(w.profiles) > 1 {
		pFill = pal.SurfaceHover
	}
	c.drawRoundedBox(profRc, float64(w.dp(6)), pFill, pal.Border, true)
	rPad := w.dp(8)
	if len(w.profiles) > 1 {
		rPad = w.dp(24)
		c.drawLucideIcon("arrow-left-right", profRc.Right-w.dp(18), profRc.Top+(profRc.Height()-w.dp(12))/2, w.dp(12), pal.TextSecondary)
	}
	texts = append(texts, textCmd{font: w.fontTiny, txt: profName, rc: rect{Left: profRc.Left + w.dp(8), Top: profRc.Top, Right: profRc.Right - rPad, Bottom: profRc.Bottom}, color: pal.Ink.win32(), flags: dtLeft | dtVCenter | dtSingleLine | dtEndEllipsis})
	if profRc.Bottom > bodyTop && profRc.Top < bodyBot {
		w.hits = append(w.hits, hitRegion{
			id: "sett:profile", rc: clampRect(profRc, bodyTop, bodyBot), cursor: hitCursorHand,
			action: func() {
				if len(w.profiles) <= 1 {
					return
				}
				idx := 0
				for i, p := range w.profiles {
					if p.Name == w.draft.SelectedProfile {
						idx = (i + 1) % len(w.profiles)
						break
					}
				}
				w.draft.SelectedProfile = w.profiles[idx].Name
				w.redraw()
			},
		})
	}
	g1Y += w.dp(32)

	// Deck
	texts = append(texts, textCmd{font: w.fontTiny, txt: "Deck", rc: rect{Left: pad + inPad, Top: g1Y, Right: pad + w.dp(74), Bottom: g1Y + w.dp(24)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtVCenter | dtSingleLine})
	refSz := w.dp(24)
	refRc := rect{Left: pad + cardW - inPad - refSz, Top: g1Y, Right: pad + cardW - inPad, Bottom: g1Y + refSz}
	refBg := pal.SurfaceSubtle
	if w.hoverID == "sett:refresh" {
		refBg = pal.SurfaceHover
	}
	c.drawRoundedBox(refRc, float64(w.dp(6)), refBg, pal.Border, true)
	c.drawLucideIcon("refresh-cw", refRc.Left+(refRc.Width()-w.dp(12))/2, refRc.Top+(refRc.Height()-w.dp(12))/2, w.dp(12), pal.TextSecondary)
	if refRc.Bottom > bodyTop && refRc.Top < bodyBot {
		w.hits = append(w.hits, hitRegion{id: "sett:refresh", rc: clampRect(refRc, bodyTop, bodyBot), cursor: hitCursorHand, action: func() { w.triggerRefreshDecks() }})
	}
	deckLabel := "All Decks"
	if w.draft.SelectedDeck != "" {
		deckLabel = strings.ReplaceAll(w.draft.SelectedDeck, "::", " › ")
	}
	deckBtnRc := rect{Left: pad + w.dp(84), Top: g1Y, Right: refRc.Left - w.dp(6), Bottom: g1Y + w.dp(24)}
	dFill := pal.SurfaceSubtle
	if w.hoverID == "sett:deckBtn" {
		dFill = pal.SurfaceHover
	}
	c.drawRoundedBox(deckBtnRc, float64(w.dp(6)), dFill, pal.Border, true)
	c.drawLucideIcon("chevron-down", deckBtnRc.Right-w.dp(18), deckBtnRc.Top+(deckBtnRc.Height()-w.dp(12))/2, w.dp(12), pal.TextSecondary)
	texts = append(texts, textCmd{font: w.fontTiny, txt: deckLabel, rc: rect{Left: deckBtnRc.Left + w.dp(8), Top: deckBtnRc.Top, Right: deckBtnRc.Right - w.dp(20), Bottom: deckBtnRc.Bottom}, color: pal.Ink.win32(), flags: dtLeft | dtVCenter | dtSingleLine | dtEndEllipsis})
	if deckBtnRc.Bottom > bodyTop && deckBtnRc.Top < bodyBot {
		w.hits = append(w.hits, hitRegion{
			id: "sett:deckBtn", rc: clampRect(deckBtnRc, bodyTop, bodyBot), cursor: hitCursorHand,
			action: func() {
				w.deckDropdownOpen = !w.deckDropdownOpen
				w.deckSearch = ""
				w.deckScrollY = 0
				if w.deckDropdownOpen {
					w.focus = focusDeckSearch
				} else {
					w.focus = focusNone
				}
				w.redraw()
			},
		})
	}
	y += g1H + w.dp(8)

	// --- Group 2: Display Fields (compute height before drawing rounded card background!) ---
	fieldLeft := pad + inPad
	fieldRight := pad + cardW - inPad
	g2HeaderH := w.dp(34)
	var frontRects, backRects []rect
	var frontEndY, backEndY int32
	if w.loadingFields || w.fieldsInfo == nil || len(w.fieldsInfo.Fields) == 0 {
		frontEndY = g2HeaderH + w.dp(26)
	} else {
		frontRects, frontEndY = w.measureFieldChipRects(memDC, w.fieldsInfo.Fields, fieldLeft, fieldRight, g2HeaderH+w.dp(20))
		backRects, backEndY = w.measureFieldChipRects(memDC, w.fieldsInfo.Fields, fieldLeft, fieldRight, frontEndY+w.dp(26))
	}
	g2H := frontEndY + w.dp(8)
	if !w.loadingFields && w.fieldsInfo != nil && len(w.fieldsInfo.Fields) > 0 {
		g2H = backEndY + w.dp(10)
	}

	g2Top := y
	c.drawRoundedBox(rect{Left: pad, Top: g2Top, Right: pad + cardW, Bottom: g2Top + g2H}, float64(w.dp(10)), pal.Surface, pal.Border, true)

	texts = append(texts, textCmd{
		font:  w.fontSmall,
		txt:   "Display Fields",
		rc:    rect{Left: fieldLeft, Top: g2Top + w.dp(10), Right: fieldLeft + w.dp(130), Bottom: g2Top + w.dp(28)},
		color: pal.Ink.win32(),
		flags: dtLeft | dtVCenter | dtSingleLine,
	})
	if w.fieldsInfo != nil && len(w.fieldsInfo.Fields) > 0 {
		rstLabel := "Use Template"
		rw, _ := measureTextSize(memDC, w.fontTiny, rstLabel)
		rstW := rw + w.dp(24)
		rstRc := rect{Left: fieldRight - rstW, Top: g2Top + w.dp(9), Right: fieldRight, Bottom: g2Top + w.dp(29)}
		rBg := pal.SurfaceSubtle
		if w.hoverID == "sett:resetFields" {
			rBg = pal.SurfaceHover
		}
		c.drawRoundedBox(rstRc, float64(w.dp(6)), rBg, pal.Border, true)
		c.drawLucideIcon("rotate-ccw", rstRc.Left+w.dp(6), rstRc.Top+(rstRc.Height()-w.dp(11))/2, w.dp(11), pal.TextSecondary)
		texts = append(texts, textCmd{
			font:  w.fontTiny,
			txt:   rstLabel,
			rc:    rect{Left: rstRc.Left + w.dp(20), Top: rstRc.Top, Right: rstRc.Right - w.dp(4), Bottom: rstRc.Bottom},
			color: pal.TextSecondary.win32(),
			flags: dtLeft | dtVCenter | dtSingleLine,
		})
		if rstRc.Bottom > bodyTop && rstRc.Top < bodyBot {
			w.hits = append(w.hits, hitRegion{
				id: "sett:resetFields", rc: clampRect(rstRc, bodyTop, bodyBot), cursor: hitCursorHand,
				action: func() {
					if w.fieldsInfo == nil {
						return
					}
					if w.draft.DeckFields == nil {
						w.draft.DeckFields = make(map[string]config.DeckFieldSelection)
					}
					w.draft.DeckFields[w.draft.SelectedDeck] = config.DeckFieldSelection{
						FrontFields: append([]string(nil), w.fieldsInfo.DefaultFront...),
						BackFields:  append([]string(nil), w.fieldsInfo.DefaultBack...),
					}
					w.redraw()
				},
			})
		}
	}

	if w.loadingFields {
		texts = append(texts, textCmd{font: w.fontTiny, txt: "Loading fields...", rc: rect{Left: fieldLeft, Top: g2Top + g2HeaderH, Right: fieldRight, Bottom: g2Top + g2HeaderH + w.dp(20)}, color: pal.TextMuted.win32(), flags: dtLeft | dtSingleLine})
	} else if w.fieldsInfo == nil || len(w.fieldsInfo.Fields) == 0 {
		texts = append(texts, textCmd{font: w.fontTiny, txt: "No fields found for this deck.", rc: rect{Left: fieldLeft, Top: g2Top + g2HeaderH, Right: fieldRight, Bottom: g2Top + g2HeaderH + w.dp(20)}, color: pal.TextMuted.win32(), flags: dtLeft | dtSingleLine})
	} else {
		sel := w.activeFieldSel()
		texts = append(texts, textCmd{font: w.fontTiny, txt: "Front", rc: rect{Left: fieldLeft, Top: g2Top + g2HeaderH, Right: fieldRight, Bottom: g2Top + g2HeaderH + w.dp(16)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtSingleLine})
		w.paintFieldChipBatch(c, pal, w.fieldsInfo.Fields, sel.FrontFields, "front", g2Top, frontRects, bodyTop, bodyBot, &texts)

		texts = append(texts, textCmd{font: w.fontTiny, txt: "Back", rc: rect{Left: fieldLeft, Top: g2Top + frontEndY + w.dp(6), Right: fieldRight, Bottom: g2Top + frontEndY + w.dp(22)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtSingleLine})
		w.paintFieldChipBatch(c, pal, w.fieldsInfo.Fields, sel.BackFields, "back", g2Top, backRects, bodyTop, bodyBot, &texts)
	}
	y += g2H + w.dp(8)

	// --- Group 3: Preferences (Interval, Toggles, AnkiConnect URL) ---
	g3H := w.dp(188)
	g3Top := y
	c.drawRoundedBox(rect{Left: pad, Top: g3Top, Right: pad + cardW, Bottom: g3Top + g3H}, float64(w.dp(10)), pal.Surface, pal.Border, true)

	g3Y := g3Top + w.dp(10)
	texts = append(texts,
		textCmd{font: w.fontTiny, txt: "Interval", rc: rect{Left: fieldLeft, Top: g3Y, Right: fieldRight - w.dp(60), Bottom: g3Y + w.dp(18)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtVCenter | dtSingleLine},
		textCmd{font: w.fontTiny, txt: fmt.Sprintf("%d min", w.draft.IntervalMinutes), rc: rect{Left: fieldRight - w.dp(60), Top: g3Y, Right: fieldRight, Bottom: g3Y + w.dp(18)}, color: pal.Ink.win32(), flags: dtRight | dtVCenter | dtSingleLine},
	)
	g3Y += w.dp(22)

	minusRc := rect{Left: fieldLeft, Top: g3Y, Right: fieldLeft + w.dp(22), Bottom: g3Y + w.dp(22)}
	plusRc := rect{Left: fieldRight - w.dp(22), Top: g3Y, Right: fieldRight, Bottom: g3Y + w.dp(22)}
	mBg := pal.SurfaceSubtle
	if w.hoverID == "sett:intMinus" {
		mBg = pal.SurfaceHover
	}
	pBg := pal.SurfaceSubtle
	if w.hoverID == "sett:intPlus" {
		pBg = pal.SurfaceHover
	}
	w.drawBtn(c, minusRc, "sett:intMinus", mBg, pal.Border, true, "minus", pal.Ink, "", pal.Ink, &texts, func() {
		if w.draft.IntervalMinutes > 1 {
			w.draft.IntervalMinutes--
			w.redraw()
		}
	})
	w.drawBtn(c, plusRc, "sett:intPlus", pBg, pal.Border, true, "plus", pal.Ink, "", pal.Ink, &texts, func() {
		if w.draft.IntervalMinutes < 60 {
			w.draft.IntervalMinutes++
			w.redraw()
		}
	})

	trackX := minusRc.Right + w.dp(10)
	trackW := plusRc.Left - w.dp(10) - trackX
	trackH := w.dp(4)
	trackY := g3Y + (w.dp(22)-trackH)/2
	c.drawRoundedBox(rect{Left: trackX, Top: trackY, Right: trackX + trackW, Bottom: trackY + trackH}, float64(trackH)/2.0, pal.ProgressTrack, pal.ProgressTrack, false)
	fillPct := ((w.draft.IntervalMinutes - 1) * 100) / 59
	knobX := trackX + (trackW*int32(fillPct))/100
	if knobX > trackX+2 {
		c.drawRoundedBox(rect{Left: trackX, Top: trackY, Right: knobX, Bottom: trackY + trackH}, float64(trackH)/2.0, pal.Ink, pal.Ink, false)
	}
	c.fillCircle(float64(knobX), float64(trackY+trackH/2), float64(w.dp(6)), pal.Ink)
	c.fillCircle(float64(knobX), float64(trackY+trackH/2), float64(w.dp(3)), pal.InkInverse)
	g3Y += w.dp(30)

	c.hline(fieldLeft, fieldRight, g3Y, pal.Divider)
	g3Y += w.dp(8)

	g3Y = w.drawSwitchRow(c, pal, fieldLeft, fieldRight, g3Y, bodyTop, bodyBot, "sett:autoAudio", "Auto-play Audio", w.draft.AutoPlayAudio, &texts, func() {
		w.draft.AutoPlayAudio = !w.draft.AutoPlayAudio
		w.redraw()
	})
	g3Y += w.dp(2)
	g3Y = w.drawSwitchRow(c, pal, fieldLeft, fieldRight, g3Y, bodyTop, bodyBot, "sett:onlyDue", "Due Decks Only", w.draft.OnlyDueDecks, &texts, func() {
		w.draft.OnlyDueDecks = !w.draft.OnlyDueDecks
		w.redraw()
	})
	g3Y += w.dp(4)
	c.hline(fieldLeft, fieldRight, g3Y, pal.Divider)
	g3Y += w.dp(8)

	// AnkiConnect URL
	texts = append(texts, textCmd{font: w.fontTiny, txt: "AnkiConnect URL", rc: rect{Left: fieldLeft, Top: g3Y, Right: fieldRight, Bottom: g3Y + w.dp(15)}, color: pal.TextSecondary.win32(), flags: dtLeft | dtSingleLine})
	g3Y += w.dp(17)
	epRc := rect{Left: fieldLeft, Top: g3Y, Right: fieldRight, Bottom: g3Y + w.dp(24)}
	epBdr := pal.Border
	epTxt := w.draft.AnkiEndpoint
	if w.focus == focusEndpoint {
		epBdr = pal.Ink
		epTxt += "|"
	}
	c.drawRoundedBox(epRc, float64(w.dp(6)), pal.SurfaceSubtle, epBdr, true)
	texts = append(texts, textCmd{font: w.fontTiny, txt: epTxt, rc: rect{Left: epRc.Left + w.dp(8), Top: epRc.Top, Right: epRc.Right - w.dp(8), Bottom: epRc.Bottom}, color: pal.Ink.win32(), flags: dtLeft | dtVCenter | dtSingleLine | dtEndEllipsis})
	if epRc.Bottom > bodyTop && epRc.Top < bodyBot {
		w.hits = append(w.hits, hitRegion{
			id: "sett:endpoint", rc: clampRect(epRc, bodyTop, bodyBot), cursor: hitCursorIBeam,
			action: func() {
				w.focus = focusEndpoint
				w.deckDropdownOpen = false
				w.redraw()
			},
		})
	}
	y += g3H + w.dp(8)

	totalH := y - startY + w.dp(8)
	maxScr := totalH - bodyH
	if maxScr < 0 {
		maxScr = 0
	}
	w.settingsMaxScr = maxScr
	if w.settingsScrollY > maxScr {
		w.settingsScrollY = maxScr
	}

	// Scrollbar thumb inside right margin
	if maxScr > 0 && totalH > 0 {
		sbW := w.dp(3)
		sbX := cw - sbW - w.dp(3)
		thumbH := (bodyH * bodyH) / totalH
		if thumbH < w.dp(20) {
			thumbH = w.dp(20)
		}
		thumbY := bodyTop + ((bodyH - thumbH) * w.settingsScrollY / maxScr)
		c.drawRoundedBox(rect{Left: sbX, Top: thumbY, Right: sbX + sbW, Bottom: thumbY + thumbH}, float64(sbW)/2.0, pal.BorderStrong, pal.BorderStrong, false)
	}

	// Sticky Footer Bar painted AFTER scrollable body with clip reset
	c.resetClipY()
	c.fillRect(rect{Left: 0, Top: bodyBot, Right: cw, Bottom: ch}, pal.CanvasBg)
	c.hline(0, cw, bodyBot, pal.Divider)

	var footerTexts []textCmd
	if w.saveMsg != "" {
		c.drawLucideIcon("check", w.dp(12), bodyBot+(footerH-w.dp(13))/2, w.dp(13), pal.SuccessFg)
		footerTexts = append(footerTexts, textCmd{
			font:  w.fontTiny,
			txt:   w.saveMsg,
			rc:    rect{Left: w.dp(28), Top: bodyBot, Right: cw - w.dp(166), Bottom: ch},
			color: pal.SuccessFg.win32(),
			flags: dtLeft | dtVCenter | dtSingleLine,
		})
	}

	clsBg := pal.SecondaryBg
	if w.hoverID == "sett:close" {
		clsBg = pal.SecondaryHover
	}
	savBg := pal.PrimaryBg
	if w.hoverID == "sett:save" {
		savBg = pal.PrimaryHover
	}
	w.drawBtn(c, rect{Left: cw - w.dp(154), Top: bodyBot + w.dp(8), Right: cw - w.dp(92), Bottom: ch - w.dp(8)}, "sett:close", clsBg, pal.Border, true, "", pal.SecondaryFg, "Close", pal.SecondaryFg, &footerTexts, func() {
		w.closeSettingsToReview()
	})
	w.drawBtn(c, rect{Left: cw - w.dp(84), Top: bodyBot + w.dp(8), Right: cw - w.dp(10), Bottom: ch - w.dp(8)}, "sett:save", savBg, savBg, false, "", pal.PrimaryFg, "Save", pal.PrimaryFg, &footerTexts, func() {
		w.doSaveSettings()
	})

	// Flush Layer 1 DIB canvas & draw clipped Layer 1 text
	c.flushToHDC(memDC, 0, 0, 0, 0, cw, ch)

	savedDC, _, _ := procSaveDC.Call(memDC)
	_, _, _ = procIntersectClipRect.Call(memDC, uintptr(0), uintptr(bodyTop), uintptr(cw), uintptr(bodyBot))
	for _, t := range texts {
		if t.rc.Bottom > headerH {
			drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
		}
	}
	_, _, _ = procRestoreDC.Call(memDC, savedDC)

	// Draw header & footer unclipped texts cleanly above body clip
	for _, t := range headerTexts {
		drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
	}
	for _, t := range footerTexts {
		drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
	}

	// Optional Layer 2: Hierarchical Searchable Deck Dropdown Overlay
	if w.deckDropdownOpen {
		w.renderDeckDropdown(memDC, cr, bodyTop, bodyBot, pal)
	}
}

func (w *WidgetWindow) measureFieldChipRects(hdc uintptr, fields []string, left, right, startY int32) ([]rect, int32) {
	x := left
	y := startY
	chipH := w.dp(22)
	gapX := w.dp(5)
	gapY := w.dp(5)
	maxW := right - left
	out := make([]rect, 0, len(fields))
	for _, f := range fields {
		tw, _ := measureTextSize(hdc, w.fontTiny, f)
		cw := tw + w.dp(16)
		if cw > maxW {
			cw = maxW
		}
		if x > left && x+cw > right {
			x = left
			y += chipH + gapY
		}
		out = append(out, rect{Left: x, Top: y, Right: x + cw, Bottom: y + chipH})
		x += cw + gapX
	}
	return out, y + chipH
}

func (w *WidgetWindow) paintFieldChipBatch(c *pixelCanvas, pal ThemePalette, fields, selected []string, side string, offsetY int32, relRects []rect, clipTop, clipBot int32, texts *[]textCmd) {
	selMap := make(map[string]bool, len(selected))
	for _, s := range selected {
		selMap[s] = true
	}
	for i, f := range fields {
		if i >= len(relRects) {
			break
		}
		fieldName := f
		rc := rect{Left: relRects[i].Left, Top: offsetY + relRects[i].Top, Right: relRects[i].Right, Bottom: offsetY + relRects[i].Bottom}
		active := selMap[fieldName]
		bg := pal.SurfaceSubtle
		bdr := pal.Border
		fg := pal.TextSecondary
		if active {
			if side == "front" {
				bg = pal.ChipFrontBg
				bdr = pal.ChipFrontBdr
				fg = pal.ChipFrontFg
			} else {
				bg = pal.ChipBackBg
				bdr = pal.ChipBackBdr
				fg = pal.ChipBackFg
			}
		} else if w.hoverID == fmt.Sprintf("chip:%s:%s", side, fieldName) {
			bg = pal.SurfaceHover
		}
		c.drawRoundedBox(rc, float64(w.dp(6)), bg, bdr, true)
		*texts = append(*texts, textCmd{
			font:  w.fontTiny,
			txt:   fieldName,
			rc:    rc,
			color: fg.win32(),
			flags: dtCenter | dtVCenter | dtSingleLine | dtEndEllipsis,
		})
		if rc.Bottom > clipTop && rc.Top < clipBot {
			w.hits = append(w.hits, hitRegion{
				id:     fmt.Sprintf("chip:%s:%s", side, fieldName),
				rc:     clampRect(rc, clipTop, clipBot),
				cursor: hitCursorHand,
				action: func() { w.toggleFieldSelection(side, fieldName) },
			})
		}
	}
}

func (w *WidgetWindow) activeFieldSel() config.DeckFieldSelection {
	sel := config.DeckFieldSelection{}
	if w.draft.DeckFields != nil {
		if existing, ok := w.draft.DeckFields[w.draft.SelectedDeck]; ok {
			sel = existing
		}
	}
	if len(sel.FrontFields) == 0 && len(sel.BackFields) == 0 && w.fieldsInfo != nil {
		sel.FrontFields = w.fieldsInfo.DefaultFront
		sel.BackFields = w.fieldsInfo.DefaultBack
	}
	return sel
}

func (w *WidgetWindow) toggleFieldSelection(side, fieldName string) {
	if w.draft.DeckFields == nil {
		w.draft.DeckFields = make(map[string]config.DeckFieldSelection)
	}
	dk := w.draft.SelectedDeck
	cur := w.activeFieldSel()
	toggle := func(list []string) []string {
		var out []string
		found := false
		for _, item := range list {
			if item == fieldName {
				found = true
				continue
			}
			out = append(out, item)
		}
		if !found {
			out = append(out, fieldName)
		}
		return out
	}
	next := config.DeckFieldSelection{
		FrontFields: append([]string(nil), cur.FrontFields...),
		BackFields:  append([]string(nil), cur.BackFields...),
	}
	if side == "front" {
		next.FrontFields = toggle(next.FrontFields)
	} else {
		next.BackFields = toggle(next.BackFields)
	}
	w.draft.DeckFields[dk] = next
	w.redraw()
}

func (w *WidgetWindow) drawSwitchRow(c *pixelCanvas, pal ThemePalette, left, right, y, clipTop, clipBot int32, id, label string, checked bool, texts *[]textCmd, action func()) int32 {
	h := w.dp(26)
	rc := rect{Left: left, Top: y, Right: right, Bottom: y + h}
	swW := w.dp(34)
	swH := w.dp(18)
	swRc := rect{Left: right - swW, Top: y + (h-swH)/2, Right: right, Bottom: y + (h+swH)/2}
	c.drawToggleSwitch(swRc, checked, pal)
	*texts = append(*texts, textCmd{
		font:  w.fontTiny,
		txt:   label,
		rc:    rect{Left: left, Top: y, Right: swRc.Left - w.dp(8), Bottom: y + h},
		color: pal.Ink.win32(),
		flags: dtLeft | dtVCenter | dtSingleLine,
	})
	if rc.Bottom > clipTop && rc.Top < clipBot {
		w.hits = append(w.hits, hitRegion{id: id, rc: clampRect(rc, clipTop, clipBot), cursor: hitCursorHand, action: action})
	}
	return y + h
}

func (w *WidgetWindow) renderDeckDropdown(memDC uintptr, cr rect, bodyTop, bodyBot int32, pal ThemePalette) {
	pad := w.dp(14)
	dropRc := rect{Left: pad, Top: bodyTop + w.dp(8), Right: cr.Width() - pad, Bottom: bodyBot - w.dp(8)}
	dw, dh := dropRc.Width(), dropRc.Height()
	if dw <= 0 || dh <= 0 {
		return
	}

	w.hits = append(w.hits, hitRegion{id: "drop:shield", rc: dropRc, cursor: hitCursorArrow, action: func() {}})

	ov := newPixelCanvas(int(dw), int(dh))
	ov.clear(pal.CanvasBg)
	ov.drawRoundedBox(rect{Left: 0, Top: 0, Right: dw, Bottom: dh}, float64(w.dp(10)), pal.Surface, pal.BorderStrong, true)

	// Coordinates relative to dropRc origin
	searchH := w.dp(26)
	relSearch := rect{Left: w.dp(8), Top: w.dp(8), Right: dw - w.dp(38), Bottom: w.dp(8) + searchH}
	ov.drawRoundedBox(relSearch, float64(w.dp(6)), pal.SurfaceSubtle, pal.BorderStrong, true)
	ov.drawLucideIcon("search", relSearch.Left+w.dp(7), relSearch.Top+(relSearch.Height()-w.dp(12))/2, w.dp(12), pal.TextMuted)

	relCls := rect{Left: dw - w.dp(34), Top: w.dp(8), Right: dw - w.dp(8), Bottom: w.dp(8) + searchH}
	clsBg := pal.SurfaceSubtle
	if w.hoverID == "drop:close" {
		clsBg = pal.SurfaceHover
	}
	ov.drawRoundedBox(relCls, float64(w.dp(6)), clsBg, pal.Border, true)
	ov.drawLucideIcon("x", relCls.Left+(relCls.Width()-w.dp(12))/2, relCls.Top+(relCls.Height()-w.dp(12))/2, w.dp(12), pal.TextSecondary)

	absSearch := rect{Left: dropRc.Left + relSearch.Left, Top: dropRc.Top + relSearch.Top, Right: dropRc.Left + relSearch.Right, Bottom: dropRc.Top + relSearch.Bottom}
	absCls := rect{Left: dropRc.Left + relCls.Left, Top: dropRc.Top + relCls.Top, Right: dropRc.Left + relCls.Right, Bottom: dropRc.Top + relCls.Bottom}
	w.hits = append(w.hits,
		hitRegion{id: "drop:search", rc: absSearch, cursor: hitCursorIBeam, action: func() { w.focus = focusDeckSearch; w.redraw() }},
		hitRegion{id: "drop:close", rc: absCls, cursor: hitCursorHand, action: func() { w.deckDropdownOpen = false; w.focus = focusNone; w.redraw() }},
	)

	listTopRel := relSearch.Bottom + w.dp(6)
	listBotRel := dh - w.dp(6)
	listH := listBotRel - listTopRel
	if listH <= 0 {
		ov.flushToHDC(memDC, dropRc.Left, dropRc.Top, 0, 0, dw, dh)
		return
	}

	opts := FilterDeckOptions(w.decks, w.deckSearch)
	rowH := w.dp(24)
	totalItems := len(opts) + 1
	totalH := int32(totalItems) * rowH
	maxScr := totalH - listH
	if maxScr < 0 {
		maxScr = 0
	}
	w.deckMaxScroll = maxScr
	if w.deckScrollY > maxScr {
		w.deckScrollY = maxScr
	}

	var listTexts []textCmd
	paintRow := func(idx int, fullName, label string, depth int) {
		ry := listTopRel + int32(idx)*rowH - w.deckScrollY
		if ry+rowH <= listTopRel || ry >= listBotRel {
			return
		}
		relRow := rect{Left: w.dp(6), Top: ry, Right: dw - w.dp(6), Bottom: ry + rowH}
		selected := w.draft.SelectedDeck == fullName
		txtColor := pal.Ink
		if selected {
			ov.drawRoundedBox(relRow, float64(w.dp(5)), pal.RowSelectedBg, pal.RowSelectedBg, false)
			txtColor = pal.RowSelectedFg
		} else if w.hoverID == "drop:item:"+fullName {
			ov.drawRoundedBox(relRow, float64(w.dp(5)), pal.SurfaceSubtle, pal.SurfaceSubtle, false)
		}
		absRow := rect{Left: dropRc.Left + relRow.Left, Top: dropRc.Top + relRow.Top, Right: dropRc.Left + relRow.Right, Bottom: dropRc.Top + relRow.Bottom}
		indent := w.dp(8) + int32(depth)*w.dp(12)
		if absRow.Top >= dropRc.Top+listTopRel && absRow.Bottom <= dropRc.Top+listBotRel {
			listTexts = append(listTexts, textCmd{
				font:  w.fontTiny,
				txt:   label,
				rc:    rect{Left: absRow.Left + indent, Top: absRow.Top, Right: absRow.Right - w.dp(8), Bottom: absRow.Bottom},
				color: txtColor.win32(),
				flags: dtLeft | dtVCenter | dtSingleLine | dtEndEllipsis,
			})
		}
		chosen := fullName
		w.hits = append(w.hits, hitRegion{
			id:     "drop:item:" + chosen,
			rc:     clampRect(absRow, dropRc.Top+listTopRel, dropRc.Top+listBotRel),
			cursor: hitCursorHand,
			action: func() {
				w.draft.SelectedDeck = chosen
				w.deckDropdownOpen = false
				w.focus = focusNone
				w.triggerLoadDeckFields(chosen)
				w.redraw()
			},
		})
	}

	paintRow(0, "", "All Decks", 0)
	for i, opt := range opts {
		paintRow(i+1, opt.FullName, opt.Label, opt.Depth)
	}

	ov.flushToHDC(memDC, dropRc.Left, dropRc.Top, 0, 0, dw, dh)

	sTxt := w.deckSearch
	sClr := pal.Ink
	if sTxt == "" {
		sTxt = "Search decks..."
		sClr = pal.TextMuted
	} else {
		sTxt += "|"
	}
	drawTextBox(memDC, w.fontTiny, sTxt, rect{Left: absSearch.Left + w.dp(24), Top: absSearch.Top, Right: absSearch.Right - w.dp(6), Bottom: absSearch.Bottom}, sClr.win32(), dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis)

	for _, t := range listTexts {
		drawTextBox(memDC, t.font, t.txt, t.rc, t.color, t.flags)
	}
}

func clampRect(rc rect, minY, maxY int32) rect {
	out := rc
	if out.Top < minY {
		out.Top = minY
	}
	if out.Bottom > maxY {
		out.Bottom = maxY
	}
	return out
}
