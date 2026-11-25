//go:build windows

package main

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unsafe"

	"anki-passive-widget/internal/anki"
	"anki-passive-widget/internal/config"
)

const (
	autoHideSeconds   = 20
	timerAutoHideID   = 1
	timerTransientMsg = 2

	wmMsgCardLoaded   = wmApp + 1
	wmMsgCardError    = wmApp + 2
	wmMsgOpenSettings = wmApp + 3
	wmMsgPauseChanged = wmApp + 4
	wmMsgWidgetHidden = wmApp + 5
	wmMsgQuit         = wmApp + 6
	wmMsgDecksLoaded  = wmApp + 7
	wmMsgFieldsLoaded = wmApp + 8
	wmMsgImageReady   = wmApp + 9
	wmMsgSaveDone     = wmApp + 10
	wmMsgAnswerDone   = wmApp + 11
)

type activeTab int

const (
	tabReview activeTab = iota
	tabSettings
)

type textFocus int

const (
	focusNone textFocus = iota
	focusDeckSearch
	focusEndpoint
)

const (
	hitCursorArrow = 0
	hitCursorHand  = 1
	hitCursorIBeam = 2
)

type hitRegion struct {
	id     string
	rc     rect
	cursor int
	action func()
}

type loadedImageResult struct {
	cardID int64
	src    string
	dib    *DecodedDIB
}

type WidgetWindow struct {
	hwnd             uintptr
	hInstance        uintptr
	wmShowSecondInst uint32
	tray             nativeTray
	ctrl             *Controller

	dpi int32

	// Cached GDI fonts recreated on DPI change and deleted on destroy.
	fontTiny     uintptr
	fontSmall    uintptr
	fontBody     uintptr
	fontBodyBold uintptr
	fontFront    uintptr
	fontRuby     uintptr
	fontTitle    uintptr

	// UI state accessed strictly on the GUI thread.
	visible      bool
	tab          activeTab
	card         *anki.ParsedCard
	frontNodes   []CardNode
	backNodes    []CardNode
	progress     DailyProgress
	errorMsg     string
	saveMsg      string
	paused       bool
	isRevealed   bool
	isHovered    bool
	isSubmitting bool
	secondsLeft  int
	hoverID      string
	hits         []hitRegion
	headerH      int32

	cardScrollY      int32
	cardMaxScroll    int32
	settingsScrollY  int32
	settingsMaxScr   int32
	deckScrollY      int32
	deckMaxScroll    int32
	deckDropdownOpen bool
	deckSearch       string
	focus            textFocus

	zoomedSrc    string
	imageCache   map[string]*DecodedDIB
	imageLoading map[string]bool

	// Settings & Theme state
	cfg            config.Config
	draft          config.Config
	profiles       []config.ProfileInfo
	decks          []string
	sourceMode     string
	fieldsInfo     *anki.DeckFieldsInfo
	loadingFields  bool
	fieldsLoadedDk string

	// Thread-safe inbox for background goroutine -> GUI thread updates.
	inboxMu      sync.Mutex
	inCard       *anki.ParsedCard
	inProgress   DailyProgress
	inCardErr    string
	inPaused     bool
	inDecks      []string
	inProfiles   []config.ProfileInfo
	inSourceMode string
	inFieldsDeck string
	inFieldsInfo *anki.DeckFieldsInfo
	inFieldsErr  bool
	inImages     []loadedImageResult
	inSaveErr    string
	inSaveClose  bool
	inAnswerErr  string
}

var globalWin *WidgetWindow

func NewWidgetWindow() *WidgetWindow {
	return &WidgetWindow{
		dpi:          96,
		secondsLeft:  autoHideSeconds,
		imageCache:   make(map[string]*DecodedDIB),
		imageLoading: make(map[string]bool),
		sourceMode:   "sqlite",
	}
}

func (w *WidgetWindow) SetController(c *Controller) {
	w.ctrl = c
	w.cfg = c.GetConfig()
	w.draft = c.GetConfig()
}

func (w *WidgetWindow) activeThemeMode() string {
	if w.tab == tabSettings {
		return w.draft.Theme
	}
	return w.cfg.Theme
}

func (w *WidgetWindow) palette() ThemePalette {
	return resolvePalette(w.activeThemeMode())
}

func (w *WidgetWindow) applyWindowFrameTheme() {
	if w.hwnd == 0 {
		return
	}
	pal := w.palette()
	cornerPref := uint32(dwmwcpRound)
	_, _, _ = procDwmSetWindowAttribute.Call(w.hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&cornerPref)), 4)

	var darkFlag uint32
	if pal.IsDark {
		darkFlag = 1
	}
	_, _, _ = procDwmSetWindowAttribute.Call(w.hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&darkFlag)), 4)

	borderClr := pal.BorderStrong.win32()
	_, _, _ = procDwmSetWindowAttribute.Call(w.hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&borderClr)), 4)
}

func (w *WidgetWindow) toggleQuickTheme() {
	pal := w.palette()
	nextTheme := "dark"
	if pal.IsDark {
		nextTheme = "light"
	}
	w.cfg.Theme = nextTheme
	w.draft.Theme = nextTheme
	w.applyWindowFrameTheme()
	w.redraw()
	if w.ctrl != nil {
		cfgCopy := w.cfg
		go func() {
			_ = w.ctrl.SaveConfig(cfgCopy)
		}()
	}
}

func (w *WidgetWindow) PostCardLoaded(card *anki.ParsedCard, progress DailyProgress) {
	w.inboxMu.Lock()
	w.inCard = card
	w.inProgress = progress
	w.inboxMu.Unlock()
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgCardLoaded, 0, 0)
	}
}

func (w *WidgetWindow) PostCardError(errMsg string) {
	w.inboxMu.Lock()
	w.inCardErr = errMsg
	w.inboxMu.Unlock()
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgCardError, 0, 0)
	}
}

func (w *WidgetWindow) PostOpenSettings() {
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgOpenSettings, 0, 0)
	}
}

func (w *WidgetWindow) PostPauseChanged(paused bool) {
	w.inboxMu.Lock()
	w.inPaused = paused
	w.inboxMu.Unlock()
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgPauseChanged, 0, 0)
	}
}

func (w *WidgetWindow) PostWidgetHidden() {
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgWidgetHidden, 0, 0)
	}
}

func (w *WidgetWindow) PostQuit() {
	if w.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgQuit, 0, 0)
	}
}

func (w *WidgetWindow) CreateAndRun() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	_, _, _ = procSetProcessDpiAwarenessContext.Call(^uintptr(3))

	hInst, _, _ := procGetModuleHandleW.Call(0)
	w.hInstance = hInst

	msgName, _ := syscall.UTF16PtrFromString("AnkiPassiveWidgetShowNow")
	regMsg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(msgName)))
	w.wmShowSecondInst = uint32(regMsg)

	className, _ := syscall.UTF16PtrFromString(widgetWindowClass)
	title, _ := syscall.UTF16PtrFromString("Anki Passive Widget")
	arrowCursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	globalWin = w
	wndProcCB := syscall.NewCallback(winProcTrampoline)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         0,
		lpfnWndProc:   wndProcCB,
		hInstance:     hInst,
		hCursor:       arrowCursor,
		hbrBackground: 0,
		lpszClassName: className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return err
	}

	hwnd, _, err := procCreateWindowExW.Call(
		wsExTopmost|wsExToolWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsClipChildren,
		0, 0, FrontWidth, FrontHeight,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return err
	}
	w.hwnd = hwnd

	if dpiRet, _, _ := procGetDpiForWindow.Call(hwnd); dpiRet > 0 {
		w.dpi = int32(dpiRet)
	}
	w.rebuildFonts()
	w.applyWindowFrameTheme()
	w.positionBottomRight(FrontWidth, FrontHeight)
	w.tray.init(hwnd, hInst)

	if w.ctrl != nil {
		w.ctrl.Start()
	}

	var msg winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	w.freeFonts()
	return nil
}

func (w *WidgetWindow) dp(px int32) int32 {
	if w.dpi <= 0 {
		return px
	}
	return (px*w.dpi + 48) / 96
}

func (w *WidgetWindow) rebuildFonts() {
	w.freeFonts()
	w.fontTiny = createUIFont(w.dp(12), fwNormal, "Segoe UI")
	w.fontSmall = createUIFont(w.dp(13), fwSemiBold, "Segoe UI")
	w.fontBody = createUIFont(w.dp(15), fwNormal, "Segoe UI")
	w.fontBodyBold = createUIFont(w.dp(15), fwSemiBold, "Segoe UI")
	w.fontFront = createUIFont(w.dp(24), fwSemiBold, "Segoe UI")
	w.fontRuby = createUIFont(w.dp(12), fwMedium, "Segoe UI")
	w.fontTitle = createUIFont(w.dp(16), fwBold, "Segoe UI")
}

func (w *WidgetWindow) freeFonts() {
	for _, fp := range []*uintptr{
		&w.fontTiny, &w.fontSmall, &w.fontBody, &w.fontBodyBold,
		&w.fontFront, &w.fontRuby, &w.fontTitle,
	} {
		if *fp != 0 {
			_, _, _ = procDeleteObject.Call(*fp)
			*fp = 0
		}
	}
}

func (w *WidgetWindow) positionBottomRight(logicalW, logicalH int32) {
	if w.hwnd == 0 {
		return
	}
	pxW := w.dp(logicalW)
	pxH := w.dp(logicalH)
	work := getMonitorWorkArea(w.hwnd)
	marginX := w.dp(16)
	marginY := w.dp(12)
	if work.Width() > 0 && pxW > work.Width()-marginX*2 {
		pxW = work.Width() - marginX*2
	}
	if work.Height() > 0 && pxH > work.Height()-marginY*2 {
		pxH = work.Height() - marginY*2
	}
	x := work.Right - pxW - marginX
	y := work.Bottom - pxH - marginY
	if x < work.Left {
		x = work.Left
	}
	if y < work.Top {
		y = work.Top
	}
	_, _, _ = procSetWindowPos.Call(w.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(pxW), uintptr(pxH), swpNoActivate)
}

func (w *WidgetWindow) expandForImage() {
	if w.hwnd == 0 {
		return
	}
	work := getMonitorWorkArea(w.hwnd)
	targetW := work.Width() / 2
	targetH := work.Height() / 2
	minW := w.dp(520)
	minH := w.dp(380)
	if targetW < minW {
		targetW = minW
	}
	if targetH < minH {
		targetH = minH
	}
	marginX := w.dp(16)
	marginY := w.dp(12)
	if targetW > work.Width()-marginX*2 {
		targetW = work.Width() - marginX*2
	}
	if targetH > work.Height()-marginY*2 {
		targetH = work.Height() - marginY*2
	}
	x := work.Right - targetW - marginX
	y := work.Bottom - targetH - marginY
	_, _, _ = procSetWindowPos.Call(w.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(targetW), uintptr(targetH), 0)
}

func (w *WidgetWindow) showAndPosition(logicalW, logicalH int32, activate bool) {
	w.positionBottomRight(logicalW, logicalH)
	w.visible = true
	if activate {
		_, _, _ = procShowWindow.Call(w.hwnd, swShow)
		_, _, _ = procSetForegroundWindow.Call(w.hwnd)
	} else {
		_, _, _ = procShowWindow.Call(w.hwnd, swShowNA)
	}
	w.redraw()
}

func (w *WidgetWindow) hideWindowUI() {
	w.visible = false
	w.zoomedSrc = ""
	w.deckDropdownOpen = false
	w.focus = focusNone
	_, _, _ = procKillTimer.Call(w.hwnd, timerAutoHideID)
	_, _, _ = procShowWindow.Call(w.hwnd, swHide)
	w.imageCache = make(map[string]*DecodedDIB)
	w.imageLoading = make(map[string]bool)
}

func (w *WidgetWindow) redraw() {
	if w.hwnd != 0 {
		_, _, _ = procInvalidateRect.Call(w.hwnd, 0, 0)
	}
}

func (w *WidgetWindow) syncAutoHideTimer() {
	if w.hwnd == 0 {
		return
	}
	shouldTick := w.visible && w.tab == tabReview && !w.isRevealed && !w.isHovered &&
		!w.isSubmitting && w.zoomedSrc == "" && w.card != nil && w.card.TotalRemaining > 0 && w.errorMsg == ""
	if shouldTick {
		_, _, _ = procSetTimer.Call(w.hwnd, timerAutoHideID, 1000, 0)
	} else {
		_, _, _ = procKillTimer.Call(w.hwnd, timerAutoHideID)
	}
}

func (w *WidgetWindow) scheduleImagesForCard(card *anki.ParsedCard) {
	if card == nil || w.ctrl == nil {
		return
	}
	cardID := card.CardID
	for _, n := range append(append([]CardNode(nil), w.frontNodes...), w.backNodes...) {
		if n.Kind != NodeImage || n.Src == "" {
			continue
		}
		if _, ok := w.imageCache[n.Src]; ok || w.imageLoading[n.Src] {
			continue
		}
		w.imageLoading[n.Src] = true
		src := n.Src
		go func() {
			dib, err := w.ctrl.LoadImageDIB(src)
			if err != nil || dib == nil {
				return
			}
			w.inboxMu.Lock()
			w.inImages = append(w.inImages, loadedImageResult{cardID: cardID, src: src, dib: dib})
			w.inboxMu.Unlock()
			if w.hwnd != 0 {
				_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgImageReady, 0, 0)
			}
		}()
	}
}

func (w *WidgetWindow) triggerRefreshDecks() {
	if w.ctrl == nil {
		return
	}
	go func() {
		decks, _ := w.ctrl.GetDecks()
		profiles, _ := w.ctrl.GetProfiles()
		mode := w.ctrl.GetDataSourceMode()
		w.inboxMu.Lock()
		w.inDecks = decks
		w.inProfiles = profiles
		w.inSourceMode = mode
		w.inboxMu.Unlock()
		if w.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgDecksLoaded, 0, 0)
		}
	}()
}

func (w *WidgetWindow) triggerLoadDeckFields(deckKey string) {
	if w.ctrl == nil {
		return
	}
	w.loadingFields = true
	w.fieldsLoadedDk = deckKey
	w.redraw()
	go func() {
		info, err := w.ctrl.GetDeckFields(deckKey)
		w.inboxMu.Lock()
		w.inFieldsDeck = deckKey
		w.inFieldsInfo = info
		w.inFieldsErr = err != nil
		w.inboxMu.Unlock()
		if w.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgFieldsLoaded, 0, 0)
		}
	}()
}

func (w *WidgetWindow) doReveal() {
	if w.card == nil || w.card.CardID <= 0 || w.isRevealed {
		return
	}
	w.isRevealed = true
	w.syncAutoHideTimer()
	w.showAndPosition(BackWidth, BackHeight, true)
	if w.cfg.AutoPlayAudio && len(w.card.BackAudio) > 0 {
		front0 := ""
		if len(w.card.FrontAudio) > 0 {
			front0 = w.card.FrontAudio[0]
		}
		if w.card.BackAudio[0] != "" && w.card.BackAudio[0] != front0 {
			w.ctrl.PlayAudioFile(w.card.BackAudio[0])
		}
	}
}

func (w *WidgetWindow) doAnswer(ease int) {
	if w.card == nil || w.card.CardID <= 0 || w.isSubmitting || w.ctrl == nil {
		return
	}
	w.isSubmitting = true
	w.errorMsg = ""
	cardID := w.card.CardID
	w.syncAutoHideTimer()
	w.redraw()
	go func() {
		err := w.ctrl.SubmitAnswer(cardID, ease)
		w.inboxMu.Lock()
		if err != nil {
			w.inAnswerErr = err.Error()
		} else {
			w.inAnswerErr = ""
		}
		w.inboxMu.Unlock()
		if w.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgAnswerDone, 0, 0)
		}
	}()
}

func (w *WidgetWindow) doSnooze() {
	w.isRevealed = false
	w.zoomedSrc = ""
	if w.ctrl != nil {
		w.ctrl.Snooze(5)
	} else {
		w.hideWindowUI()
	}
}

func (w *WidgetWindow) doReplayAudio() {
	if w.card == nil || w.ctrl == nil {
		return
	}
	var file string
	if w.isRevealed && len(w.card.BackAudio) > 0 {
		file = w.card.BackAudio[0]
	}
	if file == "" && len(w.card.FrontAudio) > 0 {
		file = w.card.FrontAudio[0]
	}
	if file != "" {
		w.ctrl.PlayAudioFile(file)
	}
}

func (w *WidgetWindow) openSettingsTab() {
	w.tab = tabSettings
	w.zoomedSrc = ""
	w.deckDropdownOpen = false
	w.focus = focusNone
	w.settingsScrollY = 0
	if w.ctrl != nil {
		w.cfg = w.ctrl.GetConfig()
		w.draft = w.ctrl.GetConfig()
	}
	w.saveMsg = ""
	w.applyWindowFrameTheme()
	w.showAndPosition(SettingsWidth, SettingsHeight, true)
	w.syncAutoHideTimer()
	w.triggerRefreshDecks()
	w.triggerLoadDeckFields(w.draft.SelectedDeck)
}

func (w *WidgetWindow) closeSettingsToReview() {
	w.tab = tabReview
	w.deckDropdownOpen = false
	w.focus = focusNone
	w.applyWindowFrameTheme()
	if w.isRevealed {
		w.showAndPosition(BackWidth, BackHeight, true)
	} else {
		w.showAndPosition(FrontWidth, FrontHeight, true)
	}
	w.syncAutoHideTimer()
}

func (w *WidgetWindow) doSaveSettings() {
	if w.ctrl == nil {
		return
	}
	prev := w.cfg
	next := w.draft
	if next.DeckFields == nil {
		next.DeckFields = make(map[string]config.DeckFieldSelection)
	}
	deckChanged := next.SelectedDeck != prev.SelectedDeck || next.SelectedProfile != prev.SelectedProfile
	pSel := prev.DeckFields[next.SelectedDeck]
	nSel := next.DeckFields[next.SelectedDeck]
	fieldsChanged := strings.Join(pSel.FrontFields, "|") != strings.Join(nSel.FrontFields, "|") ||
		strings.Join(pSel.BackFields, "|") != strings.Join(nSel.BackFields, "|")

	go func() {
		err := w.ctrl.SaveConfig(next)
		w.inboxMu.Lock()
		if err != nil {
			w.inSaveErr = err.Error()
			w.inSaveClose = false
		} else {
			w.inSaveErr = ""
			w.inSaveClose = deckChanged || fieldsChanged
		}
		w.inboxMu.Unlock()
		if w.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(w.hwnd, wmMsgSaveDone, 0, 0)
		}
	}()
}

func winProcTrampoline(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if globalWin == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}
	return globalWin.wndProc(hwnd, msg, wParam, lParam)
}

func (w *WidgetWindow) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if w.wmShowSecondInst != 0 && msg == w.wmShowSecondInst {
		if w.ctrl != nil {
			go func() { _, _ = w.ctrl.ShowCardNow() }()
		}
		return 0
	}
	if w.tray.wmTaskbarCreated != 0 && msg == w.tray.wmTaskbarCreated {
		w.tray.addOrUpdate(false, w.paused)
		return 0
	}

	switch msg {
	case wmTrayCallback:
		switch uint32(lParam) {
		case wmLButtonUp:
			if w.ctrl != nil {
				go func() { _, _ = w.ctrl.ShowCardNow() }()
			}
		case wmRButtonUp:
			w.tray.showPopupMenu(
				w.paused,
				func() {
					if w.ctrl != nil {
						go func() { _, _ = w.ctrl.ShowCardNow() }()
					}
				},
				func() {
					if w.ctrl != nil {
						w.ctrl.TogglePause()
					}
				},
				func() {
					w.openSettingsTab()
				},
				func() {
					if w.ctrl != nil {
						w.ctrl.Shutdown()
					}
					w.PostQuit()
				},
			)
		}
		return 0

	case wmSettingChange:
		if w.activeThemeMode() == "system" {
			w.applyWindowFrameTheme()
			if w.visible {
				w.redraw()
			}
		}
		return 0

	case wmEraseBkgnd:
		return 1

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		w.renderToHDC(hwnd, hdc)
		_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0

	case wmDpiChanged:
		newDPI := int32(wParam & 0xFFFF)
		if newDPI > 0 && newDPI != w.dpi {
			w.dpi = newDPI
			w.rebuildFonts()
			if w.visible {
				if w.tab == tabSettings {
					w.positionBottomRight(SettingsWidth, SettingsHeight)
				} else if w.zoomedSrc != "" {
					w.expandForImage()
				} else if w.isRevealed {
					w.positionBottomRight(BackWidth, BackHeight)
				} else {
					w.positionBottomRight(FrontWidth, FrontHeight)
				}
			}
			w.redraw()
		}
		return 0

	case wmNcHitTest:
		return htClient

	case wmSetCursor:
		if (lParam & 0xFFFF) == htClient {
			for _, h := range w.hits {
				if h.id == w.hoverID {
					curID := idcArrow
					if h.cursor == hitCursorHand {
						curID = idcHand
					} else if h.cursor == hitCursorIBeam {
						curID = idcIBeam
					}
					cHandle, _, _ := procLoadCursorW.Call(0, uintptr(curID))
					if cHandle != 0 {
						_, _, _ = procSetCursor.Call(cHandle)
						return 1
					}
				}
			}
		}

	case wmMouseMove:
		if !w.isHovered {
			w.isHovered = true
			tme := trackMouseEvent{
				cbSize:    uint32(unsafe.Sizeof(trackMouseEvent{})),
				dwFlags:   tmeLeave,
				hwndTrack: hwnd,
			}
			_, _, _ = procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			w.syncAutoHideTimer()
			w.redraw()
		}
		x := signedLoWord(lParam)
		y := signedHiWord(lParam)
		newHover := ""
		for i := len(w.hits) - 1; i >= 0; i-- {
			if w.hits[i].rc.Contains(x, y) {
				newHover = w.hits[i].id
				break
			}
		}
		if newHover != w.hoverID {
			w.hoverID = newHover
			w.redraw()
		}
		return 0

	case wmMouseLeave:
		w.isHovered = false
		w.hoverID = ""
		w.syncAutoHideTimer()
		w.redraw()
		return 0

	case wmLButtonDown:
		x := signedLoWord(lParam)
		y := signedHiWord(lParam)
		for i := len(w.hits) - 1; i >= 0; i-- {
			if w.hits[i].rc.Contains(x, y) {
				w.hits[i].action()
				return 0
			}
		}
		if w.focus != focusNone {
			w.focus = focusNone
			w.redraw()
		}
		if y < w.headerH && w.zoomedSrc == "" {
			_, _, _ = procReleaseCapture.Call()
			_, _, _ = procSendMessageW.Call(hwnd, wmNcLButtonDn, htCaption, 0)
		}
		return 0

	case wmMouseWheel:
		delta := wheelDelta(wParam)
		step := w.dp(32)
		if delta < 0 {
			step = -step
		}
		if w.tab == tabSettings {
			if w.deckDropdownOpen {
				w.deckScrollY -= step
				if w.deckScrollY < 0 {
					w.deckScrollY = 0
				}
				if w.deckScrollY > w.deckMaxScroll {
					w.deckScrollY = w.deckMaxScroll
				}
			} else {
				w.settingsScrollY -= step
				if w.settingsScrollY < 0 {
					w.settingsScrollY = 0
				}
				if w.settingsScrollY > w.settingsMaxScr {
					w.settingsScrollY = w.settingsMaxScr
				}
			}
			w.redraw()
		} else if w.tab == tabReview && w.zoomedSrc == "" {
			w.cardScrollY -= step
			if w.cardScrollY < 0 {
				w.cardScrollY = 0
			}
			if w.cardScrollY > w.cardMaxScroll {
				w.cardScrollY = w.cardMaxScroll
			}
			w.redraw()
		}
		return 0

	case wmTimer:
		if wParam == timerAutoHideID {
			if w.visible && w.tab == tabReview && !w.isRevealed && !w.isHovered && !w.isSubmitting && w.zoomedSrc == "" {
				if w.secondsLeft <= 1 {
					w.secondsLeft = 0
					if w.ctrl != nil {
						w.ctrl.HideWidget()
					}
				} else {
					w.secondsLeft--
					w.redraw()
				}
			}
		} else if wParam == timerTransientMsg {
			_, _, _ = procKillTimer.Call(hwnd, timerTransientMsg)
			w.saveMsg = ""
			w.redraw()
		}
		return 0

	case wmKeyDown:
		key := uint32(wParam)
		if key == vkEscape {
			if w.zoomedSrc != "" {
				w.zoomedSrc = ""
				if w.isRevealed {
					w.showAndPosition(BackWidth, BackHeight, true)
				} else {
					w.showAndPosition(FrontWidth, FrontHeight, true)
				}
				w.syncAutoHideTimer()
				return 0
			}
			if w.deckDropdownOpen {
				w.deckDropdownOpen = false
				w.focus = focusNone
				w.redraw()
				return 0
			}
			if w.tab == tabSettings {
				w.closeSettingsToReview()
				return 0
			}
			w.doSnooze()
			return 0
		}

		if w.focus != focusNone {
			if isCtrlDown() && (key == 'V' || key == 'v') {
				clip := strings.TrimSpace(readClipboardText())
				if clip != "" {
					if w.focus == focusDeckSearch {
						w.deckSearch += clip
						w.deckScrollY = 0
					} else if w.focus == focusEndpoint {
						w.draft.AnkiEndpoint += clip
					}
					w.redraw()
				}
				return 0
			}
			if key == vkReturn && w.focus == focusEndpoint {
				w.focus = focusNone
				w.redraw()
				return 0
			}
			return 0
		}

		if w.tab == tabReview && w.zoomedSrc == "" {
			if (key == vkSpace || key == vkReturn) && !w.isRevealed && w.card != nil && w.card.CardID > 0 {
				w.doReveal()
				return 0
			}
			if w.isRevealed && key >= '1' && key <= '4' {
				w.doAnswer(int(key - '0'))
				return 0
			}
			if key == 'R' || key == 'r' {
				w.doReplayAudio()
				return 0
			}
		}
		return 0

	case wmChar:
		if w.focus == focusNone {
			return 0
		}
		ch := rune(wParam)
		if ch == '\b' || wParam == vkBack {
			if w.focus == focusDeckSearch && len(w.deckSearch) > 0 {
				rs := []rune(w.deckSearch)
				w.deckSearch = string(rs[:len(rs)-1])
				w.deckScrollY = 0
				w.redraw()
			} else if w.focus == focusEndpoint && len(w.draft.AnkiEndpoint) > 0 {
				rs := []rune(w.draft.AnkiEndpoint)
				w.draft.AnkiEndpoint = string(rs[:len(rs)-1])
				w.redraw()
			}
			return 0
		}
		if unicode.IsPrint(ch) {
			if w.focus == focusDeckSearch {
				w.deckSearch += string(ch)
				w.deckScrollY = 0
				w.redraw()
			} else if w.focus == focusEndpoint {
				w.draft.AnkiEndpoint += string(ch)
				w.redraw()
			}
		}
		return 0

	case wmMsgCardLoaded:
		w.inboxMu.Lock()
		card := w.inCard
		prog := w.inProgress
		w.inboxMu.Unlock()
		if card != nil {
			w.card = card
			w.progress = prog
			w.frontNodes = ParseCardHTML(card.FrontHTML)
			w.backNodes = ParseCardHTML(card.BackHTML)
			w.errorMsg = ""
			w.isRevealed = false
			w.isSubmitting = false
			w.zoomedSrc = ""
			w.cardScrollY = 0
			w.tab = tabReview
			w.secondsLeft = autoHideSeconds
			w.imageCache = make(map[string]*DecodedDIB)
			w.imageLoading = make(map[string]bool)
			w.cfg = w.ctrl.GetConfig()
			w.sourceMode = card.SourceMode
			w.applyWindowFrameTheme()
			w.showAndPosition(FrontWidth, FrontHeight, false)
			w.syncAutoHideTimer()
			w.scheduleImagesForCard(card)
			if w.cfg.AutoPlayAudio && len(card.FrontAudio) > 0 {
				w.ctrl.PlayAudioFile(card.FrontAudio[0])
			}
		}
		return 0

	case wmMsgCardError:
		w.inboxMu.Lock()
		errMsg := w.inCardErr
		w.inboxMu.Unlock()
		w.errorMsg = errMsg
		w.tab = tabReview
		w.isRevealed = false
		w.isSubmitting = false
		w.zoomedSrc = ""
		w.cardScrollY = 0
		w.secondsLeft = autoHideSeconds
		w.applyWindowFrameTheme()
		w.showAndPosition(FrontWidth, FrontHeight, false)
		w.syncAutoHideTimer()
		return 0

	case wmMsgOpenSettings:
		w.openSettingsTab()
		return 0

	case wmMsgPauseChanged:
		w.inboxMu.Lock()
		w.paused = w.inPaused
		w.inboxMu.Unlock()
		w.tray.setPaused(w.paused)
		w.redraw()
		return 0

	case wmMsgWidgetHidden:
		w.hideWindowUI()
		return 0

	case wmMsgDecksLoaded:
		w.inboxMu.Lock()
		w.decks = w.inDecks
		w.profiles = w.inProfiles
		if w.inSourceMode != "" {
			w.sourceMode = w.inSourceMode
		}
		w.inboxMu.Unlock()
		w.redraw()
		return 0

	case wmMsgFieldsLoaded:
		w.inboxMu.Lock()
		dk := w.inFieldsDeck
		info := w.inFieldsInfo
		hasErr := w.inFieldsErr
		w.inboxMu.Unlock()
		if dk == w.draft.SelectedDeck {
			w.loadingFields = false
			if hasErr || info == nil {
				w.fieldsInfo = nil
			} else {
				w.fieldsInfo = info
				if w.draft.DeckFields == nil {
					w.draft.DeckFields = make(map[string]config.DeckFieldSelection)
				}
				existing := w.draft.DeckFields[dk]
				if len(existing.FrontFields) == 0 && len(existing.BackFields) == 0 {
					w.draft.DeckFields[dk] = config.DeckFieldSelection{
						FrontFields: append([]string(nil), info.DefaultFront...),
						BackFields:  append([]string(nil), info.DefaultBack...),
					}
				}
			}
			w.redraw()
		}
		return 0

	case wmMsgImageReady:
		w.inboxMu.Lock()
		imgs := w.inImages
		w.inImages = nil
		w.inboxMu.Unlock()
		updated := false
		for _, im := range imgs {
			if w.card != nil && w.card.CardID == im.cardID && im.dib != nil {
				w.imageCache[im.src] = im.dib
				updated = true
			}
		}
		if updated {
			w.redraw()
		}
		return 0

	case wmMsgSaveDone:
		w.inboxMu.Lock()
		saveErr := w.inSaveErr
		reloadCard := w.inSaveClose
		w.inboxMu.Unlock()
		if saveErr != "" {
			w.errorMsg = saveErr
			w.closeSettingsToReview()
			return 0
		}
		w.cfg = w.draft
		w.saveMsg = "Saved"
		w.redraw()
		_, _, _ = procSetTimer.Call(hwnd, timerTransientMsg, 900, 0)
		w.closeSettingsToReview()
		if reloadCard && w.ctrl != nil {
			go func() { _, _ = w.ctrl.ShowCardNow() }()
		}
		return 0

	case wmMsgAnswerDone:
		w.inboxMu.Lock()
		ansErr := w.inAnswerErr
		w.inboxMu.Unlock()
		w.isSubmitting = false
		if ansErr != "" {
			w.errorMsg = ansErr
			w.isRevealed = false
			w.redraw()
		}
		return 0

	case wmMsgQuit:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0

	case wmClose:
		w.doSnooze()
		return 0

	case wmDestroy:
		w.tray.destroy()
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}
