//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32DLL   = syscall.NewLazyDLL("user32.dll")
	gdi32DLL    = syscall.NewLazyDLL("gdi32.dll")
	dwmapiDLL   = syscall.NewLazyDLL("dwmapi.dll")
	kernel32DLL = syscall.NewLazyDLL("kernel32.dll")

	procSetProcessDpiAwarenessContext = user32DLL.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow               = user32DLL.NewProc("GetDpiForWindow")
	procRegisterClassExW              = user32DLL.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32DLL.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32DLL.NewProc("DestroyWindow")
	procDefWindowProcW                = user32DLL.NewProc("DefWindowProcW")
	procShowWindow                    = user32DLL.NewProc("ShowWindow")
	procSetForegroundWindow           = user32DLL.NewProc("SetForegroundWindow")
	procSetWindowPos                  = user32DLL.NewProc("SetWindowPos")
	procGetClientRect                 = user32DLL.NewProc("GetClientRect")
	procInvalidateRect                = user32DLL.NewProc("InvalidateRect")
	procBeginPaint                    = user32DLL.NewProc("BeginPaint")
	procEndPaint                      = user32DLL.NewProc("EndPaint")
	procGetMessageW                   = user32DLL.NewProc("GetMessageW")
	procTranslateMessage              = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW              = user32DLL.NewProc("DispatchMessageW")
	procPostMessageW                  = user32DLL.NewProc("PostMessageW")
	procSendMessageW                  = user32DLL.NewProc("SendMessageW")
	procPostQuitMessage               = user32DLL.NewProc("PostQuitMessage")
	procRegisterWindowMessageW        = user32DLL.NewProc("RegisterWindowMessageW")
	procSetTimer                      = user32DLL.NewProc("SetTimer")
	procKillTimer                     = user32DLL.NewProc("KillTimer")
	procTrackMouseEvent               = user32DLL.NewProc("TrackMouseEvent")
	procReleaseCapture                = user32DLL.NewProc("ReleaseCapture")
	procMonitorFromWindow             = user32DLL.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32DLL.NewProc("GetMonitorInfoW")
	procLoadCursorW                   = user32DLL.NewProc("LoadCursorW")
	procSetCursor                     = user32DLL.NewProc("SetCursor")
	procFillRect                      = user32DLL.NewProc("FillRect")
	procFrameRect                     = user32DLL.NewProc("FrameRect")
	procDrawTextW                     = user32DLL.NewProc("DrawTextW")
	procOpenClipboard                 = user32DLL.NewProc("OpenClipboard")
	procGetClipboardData              = user32DLL.NewProc("GetClipboardData")
	procCloseClipboard                = user32DLL.NewProc("CloseClipboard")
	procGetKeyState                   = user32DLL.NewProc("GetKeyState")
	procFindWindowW                   = user32DLL.NewProc("FindWindowW")

	procCreateCompatibleDC     = gdi32DLL.NewProc("CreateCompatibleDC")
	procDeleteDC               = gdi32DLL.NewProc("DeleteDC")
	procCreateCompatibleBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32DLL.NewProc("SelectObject")
	procDeleteObject           = gdi32DLL.NewProc("DeleteObject")
	procBitBlt                 = gdi32DLL.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32DLL.NewProc("CreateSolidBrush")
	procCreatePen              = gdi32DLL.NewProc("CreatePen")
	procRoundRect              = gdi32DLL.NewProc("RoundRect")
	procMoveToEx               = gdi32DLL.NewProc("MoveToEx")
	procLineTo                 = gdi32DLL.NewProc("LineTo")
	procCreateFontW            = gdi32DLL.NewProc("CreateFontW")
	procSetTextColor           = gdi32DLL.NewProc("SetTextColor")
	procSetBkMode              = gdi32DLL.NewProc("SetBkMode")
	procGetTextExtentPoint32W  = gdi32DLL.NewProc("GetTextExtentPoint32W")
	procExtTextOutW            = gdi32DLL.NewProc("ExtTextOutW")
	procSaveDC                 = gdi32DLL.NewProc("SaveDC")
	procRestoreDC              = gdi32DLL.NewProc("RestoreDC")
	procIntersectClipRect      = gdi32DLL.NewProc("IntersectClipRect")
	procSetStretchBltMode      = gdi32DLL.NewProc("SetStretchBltMode")
	procSetBrushOrgEx          = gdi32DLL.NewProc("SetBrushOrgEx")
	procStretchDIBits          = gdi32DLL.NewProc("StretchDIBits")
	procGdiFlush               = gdi32DLL.NewProc("GdiFlush")

	procDwmSetWindowAttribute = dwmapiDLL.NewProc("DwmSetWindowAttribute")

	procGetModuleHandleW = kernel32DLL.NewProc("GetModuleHandleW")
	procGlobalLock       = kernel32DLL.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32DLL.NewProc("GlobalUnlock")
	procLstrlenW         = kernel32DLL.NewProc("lstrlenW")
	procRtlMoveMemory    = kernel32DLL.NewProc("RtlMoveMemory")
)

const (
	wsPopup        = 0x80000000
	wsClipChildren = 0x02000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080

	swHide   = 0
	swShow   = 5
	swShowNA = 8

	swpNoActivate = 0x0010
	swpNoZOrder   = 0x0004
	hwndTopmost   = ^uintptr(0) // -1

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmEraseBkgnd  = 0x0014
	wmSetCursor     = 0x0020
	wmSettingChange = 0x001A
	wmDpiChanged    = 0x02E0
	wmTimer       = 0x0113
	wmKeyDown     = 0x0100
	wmChar        = 0x0102
	wmNcHitTest   = 0x0084
	wmNcLButtonDn = 0x00A1
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmMouseWheel  = 0x020A
	wmMouseLeave  = 0x02A3
	wmApp         = 0x8000

	htClient  = 1
	htCaption = 2

	tmeLeave = 0x00000002

	monitorDefaultToNearest = 2

	transparentMode = 1
	srcCopy         = 0x00CC0020
	psSolid         = 0
	halftoneMode    = 4
	dibRGBColors    = 0
	biRGB           = 0

	dtLeft        = 0x00000000
	dtCenter      = 0x00000001
	dtRight       = 0x00000002
	dtVCenter     = 0x00000004
	dtWordBreak   = 0x00000010
	dtSingleLine  = 0x00000020
	dtEndEllipsis = 0x00008000
	dtNoPrefix    = 0x00000800

	idcArrow = 32512
	idcIBeam = 32513
	idcHand  = 32649

	vkBack   = 0x08
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkSpace  = 0x20
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28

	cfUnicodeText = 13

	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwaBorderColor            = 34
	dwmwcpRound                 = 2

	fwNormal   = 400
	fwMedium   = 500
	fwSemiBold = 600
	fwBold     = 700
)

type rect struct {
	Left, Top, Right, Bottom int32
}

func (r rect) Width() int32  { return r.Right - r.Left }
func (r rect) Height() int32 { return r.Bottom - r.Top }
func (r rect) Contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

type point struct {
	X, Y int32
}

type winSize struct {
	CX, CY int32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type trackMouseEvent struct {
	cbSize      uint32
	dwFlags     uint32
	hwndTrack   uintptr
	dwHoverTime uint32
}

type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

func rgb(r, g, b uint8) uint32 {
	return uint32(r) | (uint32(g) << 8) | (uint32(b) << 16)
}

func fillSolidRect(hdc uintptr, rc rect, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if brush == 0 {
		return
	}
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	_, _, _ = procDeleteObject.Call(brush)
}

func strokeRect(hdc uintptr, rc rect, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if brush == 0 {
		return
	}
	_, _, _ = procFrameRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	_, _, _ = procDeleteObject.Call(brush)
}

func drawRoundBox(hdc uintptr, rc rect, radius int32, fillColor, borderColor uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(fillColor))
	pen, _, _ := procCreatePen.Call(psSolid, 1, uintptr(borderColor))
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	_, _, _ = procRoundRect.Call(
		hdc,
		uintptr(rc.Left),
		uintptr(rc.Top),
		uintptr(rc.Right),
		uintptr(rc.Bottom),
		uintptr(radius),
		uintptr(radius),
	)
	_, _, _ = procSelectObject.Call(hdc, oldBrush)
	_, _, _ = procSelectObject.Call(hdc, oldPen)
	_, _, _ = procDeleteObject.Call(brush)
	_, _, _ = procDeleteObject.Call(pen)
}

func drawLine(hdc uintptr, x1, y1, x2, y2 int32, color uint32, width int32) {
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), uintptr(color))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	_, _, _ = procMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	_, _, _ = procLineTo.Call(hdc, uintptr(x2), uintptr(y2))
	_, _, _ = procSelectObject.Call(hdc, oldPen)
	_, _, _ = procDeleteObject.Call(pen)
}

func createUIFont(height int32, weight int32, face string) uintptr {
	facePtr, _ := syscall.UTF16PtrFromString(face)
	// CLEARTYPE_QUALITY = 5, DEFAULT_CHARSET = 1
	hFont, _, _ := procCreateFontW.Call(
		uintptr(uint32(-height)),
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(facePtr)),
	)
	return hFont
}

func measureTextSize(hdc, hFont uintptr, txt string) (int32, int32) {
	if txt == "" {
		return 0, 0
	}
	u16, err := syscall.UTF16FromString(txt)
	if err != nil || len(u16) <= 1 {
		return 0, 0
	}
	old, _, _ := procSelectObject.Call(hdc, hFont)
	var sz winSize
	_, _, _ = procGetTextExtentPoint32W.Call(hdc, uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)-1), uintptr(unsafe.Pointer(&sz)))
	_, _, _ = procSelectObject.Call(hdc, old)
	return sz.CX, sz.CY
}

func drawTextBox(hdc, hFont uintptr, txt string, rc rect, color uint32, flags uint32) {
	u16, err := syscall.UTF16FromString(txt)
	if err != nil || len(u16) <= 1 {
		return
	}
	oldFont, _, _ := procSelectObject.Call(hdc, hFont)
	_, _, _ = procSetBkMode.Call(hdc, transparentMode)
	_, _, _ = procSetTextColor.Call(hdc, uintptr(color))
	rCopy := rc
	_, _, _ = procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)-1), uintptr(unsafe.Pointer(&rCopy)), uintptr(flags|dtNoPrefix))
	_, _, _ = procSelectObject.Call(hdc, oldFont)
}

func drawDIBScaled(hdc uintptr, dst rect, dib *DecodedDIB) {
	if dib == nil || dib.Width <= 0 || dib.Height <= 0 || len(dib.Pixels) == 0 {
		return
	}
	var bmi bitmapInfoHeader
	bmi.biSize = uint32(unsafe.Sizeof(bmi))
	bmi.biWidth = int32(dib.Width)
	bmi.biHeight = -int32(dib.Height) // top-down
	bmi.biPlanes = 1
	bmi.biBitCount = 32
	bmi.biCompression = biRGB

	_, _, _ = procSetStretchBltMode.Call(hdc, halftoneMode)
	_, _, _ = procSetBrushOrgEx.Call(hdc, 0, 0, 0)
	_, _, _ = procStretchDIBits.Call(
		hdc,
		uintptr(dst.Left), uintptr(dst.Top), uintptr(dst.Width()), uintptr(dst.Height()),
		0, 0, uintptr(dib.Width), uintptr(dib.Height),
		uintptr(unsafe.Pointer(&dib.Pixels[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		srcCopy,
	)
}

func getMonitorWorkArea(hwnd uintptr) rect {
	hMon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if hMon != 0 {
		ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi)))
		if ok != 0 {
			return mi.rcWork
		}
	}
	return rect{Left: 0, Top: 0, Right: 1280, Bottom: 720}
}

func readClipboardText() string {
	ok, _, _ := procOpenClipboard.Call(0)
	if ok == 0 {
		return ""
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	hMem, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if hMem == 0 {
		return ""
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return ""
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(hMem) }()
	n, _, _ := procLstrlenW.Call(ptr)
	if n == 0 {
		return ""
	}
	if n > 1024 {
		n = 1024
	}
	units := make([]uint16, n)
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&units[0])), ptr, n*2)
	return syscall.UTF16ToString(units)
}

func isCtrlDown() bool {
	state, _, _ := procGetKeyState.Call(0x11)
	return (uint16(state) & 0x8000) != 0
}

func signedLoWord(l uintptr) int32 {
	return int32(int16(uint16(l & 0xFFFF)))
}

func signedHiWord(l uintptr) int32 {
	return int32(int16(uint16((l >> 16) & 0xFFFF)))
}

func wheelDelta(w uintptr) int32 {
	return int32(int16(uint16((w >> 16) & 0xFFFF)))
}

var _ = fmt.Sprintf

