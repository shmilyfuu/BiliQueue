//go:build windows

package nativeui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	nativeClassName = "BiliQueueNativeUI"

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	wsPopup            = 0x80000000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsTabStop          = 0x00010000
	wsOverlappedWindow = 0x00CF0000
	wsThickFrame       = 0x00040000
	wsMaximizeBox      = 0x00010000
	wsFixedWindow      = wsOverlappedWindow &^ (wsThickFrame | wsMaximizeBox)
	esAutoHScroll      = 0x0080

	wsExToolWindow    = 0x00000080
	wsExNoActivate    = 0x08000000
	wsExDlgModalFrame = 0x00000001
	wsExTransparent   = 0x00000020
	wsExAppWindow     = 0x00040000

	swHide    = 0
	swShow    = 5
	swRestore = 9

	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmClose          = 0x0010
	wmPaint          = 0x000F
	wmEraseBkgnd     = 0x0014
	wmSetCursor      = 0x0020
	wmGetMinMaxInfo  = 0x0024
	wmSetIcon        = 0x0080
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmKeyDown        = 0x0100
	wmCtlColorEdit   = 0x0133
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmMouseWheel     = 0x020A
	wmCaptureChanged = 0x0215
	wmMouseLeave     = 0x02A3
	wmNCHitTest      = 0x0084
	wmDPIChanged     = 0x02E0
	wmAppRequest     = 0x8001

	vkEscape = 0x1B
	vkReturn = 0x0D

	htClient  = 1
	htCaption = 2

	idcArrow       = 32512
	idiApplication = 32512

	imageIcon      = 1
	lrLoadFromFile = 0x0010
	iconSmall      = 0
	iconBig        = 1

	dwmUseImmersiveDarkMode   = 20
	dwmWindowCornerPreference = 33
	dwmBorderColor            = 34
	dwmCaptionColor           = 35
	dwmTextColor              = 36
	dwmSystemBackdropType     = 38
	dwmCornerRound            = 2
	dwmBackdropNone           = 1
	dwmBackdropMica           = 2
	dwmBackdropAcrylic        = 3
	dwmBackdropMicaAlt        = 4
	dwmColorNone              = 0xFFFFFFFE

	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	hwndTopmost    = ^uintptr(0)
	hwndNotTopmost = ^uintptr(1)

	gwlpUserData = ^uintptr(20)
	gwlpWndProc  = ^uintptr(3)
)

var (
	nativeUser32   = syscall.NewLazyDLL("user32.dll")
	nativeKernel32 = syscall.NewLazyDLL("kernel32.dll")
	nativeGDI32    = syscall.NewLazyDLL("gdi32.dll")
	nativeDWMAPI   = syscall.NewLazyDLL("dwmapi.dll")

	procRegisterClassExW      = nativeUser32.NewProc("RegisterClassExW")
	procCreateWindowExW       = nativeUser32.NewProc("CreateWindowExW")
	procDefWindowProcW        = nativeUser32.NewProc("DefWindowProcW")
	procDestroyWindow         = nativeUser32.NewProc("DestroyWindow")
	procShowWindow            = nativeUser32.NewProc("ShowWindow")
	procUpdateWindow          = nativeUser32.NewProc("UpdateWindow")
	procGetMessageW           = nativeUser32.NewProc("GetMessageW")
	procPeekMessageW          = nativeUser32.NewProc("PeekMessageW")
	procTranslateMessage      = nativeUser32.NewProc("TranslateMessage")
	procDispatchMessageW      = nativeUser32.NewProc("DispatchMessageW")
	procPostThreadMessageW    = nativeUser32.NewProc("PostThreadMessageW")
	procBeginPaint            = nativeUser32.NewProc("BeginPaint")
	procEndPaint              = nativeUser32.NewProc("EndPaint")
	procGetClientRect         = nativeUser32.NewProc("GetClientRect")
	procGetWindowRect         = nativeUser32.NewProc("GetWindowRect")
	procAdjustWindowRectEx    = nativeUser32.NewProc("AdjustWindowRectEx")
	procInvalidateRect        = nativeUser32.NewProc("InvalidateRect")
	procLoadCursorW           = nativeUser32.NewProc("LoadCursorW")
	procLoadIconW             = nativeUser32.NewProc("LoadIconW")
	procLoadImageW            = nativeUser32.NewProc("LoadImageW")
	procSetCursor             = nativeUser32.NewProc("SetCursor")
	procSetForegroundWindow   = nativeUser32.NewProc("SetForegroundWindow")
	procSetWindowPos          = nativeUser32.NewProc("SetWindowPos")
	procSetFocus              = nativeUser32.NewProc("SetFocus")
	procSetCapture            = nativeUser32.NewProc("SetCapture")
	procReleaseCapture        = nativeUser32.NewProc("ReleaseCapture")
	procTrackMouseEvent       = nativeUser32.NewProc("TrackMouseEvent")
	procGetWindowTextLengthW  = nativeUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW        = nativeUser32.NewProc("GetWindowTextW")
	procSetWindowTextW        = nativeUser32.NewProc("SetWindowTextW")
	procSendMessageW          = nativeUser32.NewProc("SendMessageW")
	procSetTimer              = nativeUser32.NewProc("SetTimer")
	procKillTimer             = nativeUser32.NewProc("KillTimer")
	procSetWindowLongPtrW     = nativeUser32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW       = nativeUser32.NewProc("CallWindowProcW")
	procScreenToClient        = nativeUser32.NewProc("ScreenToClient")
	procGetDPIForWindow       = nativeUser32.NewProc("GetDpiForWindow")
	procGetSystemMetrics      = nativeUser32.NewProc("GetSystemMetrics")
	procMonitorFromPoint      = nativeUser32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW       = nativeUser32.NewProc("GetMonitorInfoW")
	procSetProcessDPIContext  = nativeUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetModuleHandleW      = nativeKernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID    = nativeKernel32.NewProc("GetCurrentThreadId")
	procCopyMemory            = nativeKernel32.NewProc("RtlMoveMemory")
	procCreateFontW           = nativeGDI32.NewProc("CreateFontW")
	procDeleteObject          = nativeGDI32.NewProc("DeleteObject")
	procSetTextColor          = nativeGDI32.NewProc("SetTextColor")
	procSetBkMode             = nativeGDI32.NewProc("SetBkMode")
	procGetStockObject        = nativeGDI32.NewProc("GetStockObject")
	procDwmSetWindowAttribute = nativeDWMAPI.NewProc("DwmSetWindowAttribute")
	procDwmExtendFrame        = nativeDWMAPI.NewProc("DwmExtendFrameIntoClientArea")

	globalHostOnce  sync.Once
	globalHost      *Host
	globalHostErr   error
	windowCallback  = syscall.NewCallback(nativeWindowProc)
	windowRegistry  sync.Map
	windowIconBig   uintptr
	windowIconSmall uintptr
)

type windowHandler interface {
	proc(message uint32, wParam, lParam uintptr) uintptr
}

type nativePoint struct{ X, Y int32 }
type nativeRect struct{ Left, Top, Right, Bottom int32 }
type nativeMessage struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       nativePoint
	LPrivate uint32
}
type nativePaintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     nativeRect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
type nativeWndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type nativeMargins struct {
	Left, Right, Top, Bottom int32
}
type nativeMonitorInfo struct {
	CbSize  uint32
	Monitor nativeRect
	Work    nativeRect
	Flags   uint32
}
type nativeTrackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type uiRequest struct {
	run func()
}

type Host struct {
	design   Design
	requests chan uiRequest
	ready    chan struct{}
	threadID uint32
	startErr error
	closed   atomic.Bool
	mini     *miniWindow
	dialogMu sync.Mutex
	renderer *rendererResources
}

func DefaultHost() (*Host, error) {
	globalHostOnce.Do(func() {
		design, err := LoadDesign()
		if err != nil {
			globalHostErr = err
			return
		}
		host := &Host{
			design:   design,
			requests: make(chan uiRequest, 64),
			ready:    make(chan struct{}),
		}
		globalHost = host
		go host.run()
		<-host.ready
		globalHostErr = host.startErr
	})
	return globalHost, globalHostErr
}

func Preload() error {
	_, err := DefaultHost()
	return err
}

func (h *Host) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// PER_MONITOR_AWARE_V2. A failure simply means the process selected its
	// awareness earlier; window-specific DPI handling still remains active.
	procSetProcessDPIContext.Call(^uintptr(3))
	threadID, _, _ := procGetCurrentThreadID.Call()
	h.threadID = uint32(threadID)
	h.startErr = registerNativeClass()
	if h.startErr == nil {
		h.renderer, h.startErr = newRendererResources()
	}
	// Force creation of this thread's message queue before callers can post a
	// request immediately after ready is closed.
	var queued nativeMessage
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&queued)), 0, 0, 0, 0)
	close(h.ready)
	if h.startErr != nil {
		return
	}

	var message nativeMessage
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		if message.HWnd == 0 && message.Message == wmAppRequest {
			h.drainRequests()
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
	h.closed.Store(true)
	h.renderer.release()
}

func registerNativeClass() error {
	className, _ := syscall.UTF16PtrFromString(nativeClassName)
	instance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	windowIconBig, windowIconSmall = loadNativeWindowIcons()
	wc := nativeWndClassEx{
		CbSize:        uint32(unsafe.Sizeof(nativeWndClassEx{})),
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   windowCallback,
		HInstance:     instance,
		HIcon:         windowIconBig,
		HCursor:       cursor,
		LpszClassName: className,
		HIconSm:       windowIconSmall,
	}
	result, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if result == 0 && callErr != syscall.Errno(1410) {
		return fmt.Errorf("register native UI class: %w", callErr)
	}
	return nil
}

func loadNativeWindowIcons() (uintptr, uintptr) {
	instance, _, _ := procGetModuleHandleW.Call(0)
	big, _, _ := procLoadImageW.Call(instance, 1, imageIcon, 32, 32, 0)
	small, _, _ := procLoadImageW.Call(instance, 1, imageIcon, 16, 16, 0)
	if big != 0 {
		if small == 0 {
			small = big
		}
		return big, small
	}

	var paths []string
	if executable, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), "assets", "biliqueue.ico"))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(workingDirectory, "assets", "biliqueue.ico"))
	}

	seen := make(map[string]bool)
	for _, path := range paths {
		clean := filepath.Clean(path)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		if _, err := os.Stat(clean); err != nil {
			continue
		}
		pathPtr, _ := syscall.UTF16PtrFromString(clean)
		big, _, _ = procLoadImageW.Call(
			0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 32, 32, lrLoadFromFile,
		)
		small, _, _ = procLoadImageW.Call(
			0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 16, 16, lrLoadFromFile,
		)
		if big != 0 {
			if small == 0 {
				small = big
			}
			return big, small
		}
	}

	fallback, _, _ := procLoadIconW.Call(0, idiApplication)
	return fallback, fallback
}

func applyNativeWindowIcons(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if windowIconBig != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, windowIconBig)
	}
	if windowIconSmall != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, windowIconSmall)
	}
}

func (h *Host) dispatch(run func()) error {
	if h == nil || h.closed.Load() || h.threadID == 0 {
		return ErrUnavailable
	}
	select {
	case h.requests <- uiRequest{run: run}:
	default:
		return fmt.Errorf("native UI request queue is full")
	}
	result, _, callErr := procPostThreadMessageW.Call(uintptr(h.threadID), wmAppRequest, 0, 0)
	if result == 0 {
		return fmt.Errorf("wake native UI thread: %w", callErr)
	}
	return nil
}

func (h *Host) drainRequests() {
	for {
		select {
		case request := <-h.requests:
			if request.run != nil {
				request.run()
			}
		default:
			return
		}
	}
}

func (h *Host) CloseDialogs() {
	if h == nil {
		return
	}
	_ = h.dispatch(func() {
		windowRegistry.Range(func(_, value any) bool {
			if dialog, ok := value.(*window); ok && dialog.hwnd != 0 {
				if dialog.progress {
					dialog.hide()
				} else {
					dialog.resolve(false)
				}
			}
			return true
		})
	})
}

func nativeWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if value, ok := windowRegistry.Load(hwnd); ok {
		return value.(windowHandler).proc(message, wParam, lParam)
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func scaleForDPI(value int, dpi uint32) int {
	if dpi == 0 {
		dpi = 96
	}
	return (value*int(dpi) + 48) / 96
}

func unscaleForDPI(value int, dpi uint32) int {
	if dpi == 0 {
		dpi = 96
	}
	return (value*96 + int(dpi)/2) / int(dpi)
}

func windowDPI(hwnd uintptr) uint32 {
	dpi, _, _ := procGetDPIForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return uint32(dpi)
}

func workAreaForPoint(x, y int) nativeRect {
	packed := uintptr(uint64(uint32(int32(x))) | uint64(uint32(int32(y)))<<32)
	monitor, _, _ := procMonitorFromPoint.Call(packed, 2)
	if monitor != 0 {
		info := nativeMonitorInfo{CbSize: uint32(unsafe.Sizeof(nativeMonitorInfo{}))}
		if result, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); result != 0 {
			return info.Work
		}
	}
	width, _, _ := procGetSystemMetrics.Call(0)
	height, _, _ := procGetSystemMetrics.Call(1)
	return nativeRect{Right: int32(width), Bottom: int32(height)}
}

func trackMouseLeave(hwnd uintptr) {
	event := nativeTrackMouseEvent{
		CbSize:    uint32(unsafe.Sizeof(nativeTrackMouseEvent{})),
		DwFlags:   0x00000002,
		HwndTrack: hwnd,
	}
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&event)))
}

func colorRef(value string) uint32 {
	color := parseColorF(value, 1)
	return uint32(color.R*255) | uint32(color.G*255)<<8 | uint32(color.B*255)<<16
}

func createUIFont(family string, logicalSize int, dpi uint32) uintptr {
	if family == "" {
		family = "Segoe UI"
	}
	face, _ := syscall.UTF16PtrFromString(family)
	height := maxInt(10, scaleForDPI(logicalSize, dpi))
	font, _, _ := procCreateFontW.Call(
		uintptr(uint32(int32(-height))),
		0, 0, 0, 400,
		0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(face)),
	)
	return font
}

func editOverlayRect(rect Rect, leftInset, rightInset, fontSize int) Rect {
	height := minInt(maxInt(1, rect.H-2), maxInt(18, fontSize+8))
	return Rect{
		X: rect.X + leftInset,
		Y: rect.Y + (rect.H-height)/2,
		W: maxInt(1, rect.W-leftInset-rightInset),
		H: height,
	}
}

func applyDWM(hwnd uintptr, material WindowMaterial, borderless bool) bool {
	dark := int32(1)
	procDwmSetWindowAttribute.Call(hwnd, dwmUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	corner := int32(dwmCornerRound)
	procDwmSetWindowAttribute.Call(hwnd, dwmWindowCornerPreference, uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))

	backdrop := int32(dwmBackdropMica)
	switch material.Backdrop {
	case "none":
		backdrop = dwmBackdropNone
	case "acrylic", "desktopAcrylic", "desktop-acrylic":
		backdrop = dwmBackdropAcrylic
	case "micaAlt", "mica-alt", "tabbed":
		backdrop = dwmBackdropMicaAlt
	}

	text := colorRef(material.TitleBarTextColor)
	caption := colorRef(material.TitleBarColor)
	border := colorRef(material.TitleBarBorderColor)
	if material.TitleBarFollowBackground && backdrop != dwmBackdropNone {
		caption = dwmColorNone
	}
	if borderless {
		caption = dwmColorNone
		border = dwmColorNone
	}
	procDwmSetWindowAttribute.Call(hwnd, dwmTextColor, uintptr(unsafe.Pointer(&text)), unsafe.Sizeof(text))
	procDwmSetWindowAttribute.Call(hwnd, dwmCaptionColor, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	procDwmSetWindowAttribute.Call(hwnd, dwmBorderColor, uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))

	result, _, _ := procDwmSetWindowAttribute.Call(hwnd, dwmSystemBackdropType, uintptr(unsafe.Pointer(&backdrop)), unsafe.Sizeof(backdrop))
	if backdrop == dwmBackdropNone {
		// Keep the non-client title bar under DWM control, but do not extend
		// glass into the Direct2D client area. The caller paints the client
		// with the same opaque color as the caption.
		margins := nativeMargins{}
		procDwmExtendFrame.Call(hwnd, uintptr(unsafe.Pointer(&margins)))
		return false
	}
	margins := nativeMargins{-1, -1, -1, -1}
	extended, _, _ := procDwmExtendFrame.Call(hwnd, uintptr(unsafe.Pointer(&margins)))
	active := !hresultFailed(result) && !hresultFailed(extended)
	if !active {
		if material.TitleBarFollowBackground && !borderless {
			fallbackCaption := colorRef(material.TitleBarColor)
			fallbackBorder := colorRef(material.TitleBarBorderColor)
			procDwmSetWindowAttribute.Call(hwnd, dwmCaptionColor, uintptr(unsafe.Pointer(&fallbackCaption)), unsafe.Sizeof(fallbackCaption))
			procDwmSetWindowAttribute.Call(hwnd, dwmBorderColor, uintptr(unsafe.Pointer(&fallbackBorder)), unsafe.Sizeof(fallbackBorder))
		}
		log.Printf("native UI DWM backdrop unavailable; using Direct2D fallback (set=0x%08X extend=0x%08X)", uint32(result), uint32(extended))
	}
	return active
}
