//go:build windows

package main

import (
	"net"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wsExDlgModalFrame = 0x00000001
	wsCaption         = 0x00C00000
	wsSysMenu         = 0x00080000
	wsVisible         = 0x10000000
	wsChild           = 0x40000000
	wsTabStop         = 0x00010000
	wsBorder          = 0x00800000
	esAutoHScroll     = 0x00000080
	bsDefPushButton   = 0x00000001
	ssIcon            = 0x00000003
	ssLeft            = 0x00000000
	ssNotify          = 0x00000100
	colorBtnFace      = 15
	idiInformation    = 32516
	idiQuestion       = 32514
	wmSetFont         = 0x0030
	stmSetIcon        = 0x0170
	defaultGuiFont    = 17
	swShow            = 5

	promptIDOK     = 1
	promptIDCancel = 2
)

var (
	promptUser32 = syscall.NewLazyDLL("user32.dll")
	promptKernel = syscall.NewLazyDLL("kernel32.dll")
	promptGDI32  = syscall.NewLazyDLL("gdi32.dll")

	procGetSystemMetrics      = promptUser32.NewProc("GetSystemMetrics")
	procGetWindowTextLengthW  = promptUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW        = promptUser32.NewProc("GetWindowTextW")
	procSetFocus              = promptUser32.NewProc("SetFocus")
	procShowWindow            = promptUser32.NewProc("ShowWindow")
	procUpdateWindow          = promptUser32.NewProc("UpdateWindow")
	procPromptGetModuleHandle = promptKernel.NewProc("GetModuleHandleW")
	procPromptSendMessageW    = promptUser32.NewProc("SendMessageW")
	procGetStockObject        = promptGDI32.NewProc("GetStockObject")

	promptMu     sync.Mutex
	activePrompt *promptDialog
)

type promptDialog struct {
	hwnd     uintptr
	hostEdit uintptr
	portEdit uintptr
	done     bool
	ok       bool
	value    string
	closed   chan struct{}
}

func promptListenAddress(title, message, defaultValue string) (string, bool) {
	promptMu.Lock()
	if activePrompt != nil && !activePrompt.done {
		if activePrompt.hwnd != 0 {
			procShowWindow.Call(activePrompt.hwnd, swShow)
			procSetForegroundWnd.Call(activePrompt.hwnd)
			if activePrompt.portEdit != 0 {
				procSetFocus.Call(activePrompt.portEdit)
			} else if activePrompt.hostEdit != 0 {
				procSetFocus.Call(activePrompt.hostEdit)
			}
		}
		promptMu.Unlock()
		return "", false
	}
	d := &promptDialog{closed: make(chan struct{})}
	activePrompt = d
	promptMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		promptMu.Lock()
		if activePrompt == d {
			activePrompt = nil
		}
		promptMu.Unlock()
	}()

	className, _ := syscall.UTF16PtrFromString("BiliQueuePortPromptWindow")
	windowName, _ := syscall.UTF16PtrFromString(title)
	hInstance, _, _ := procPromptGetModuleHandle.Call(0)

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(portPromptWndProc),
		hInstance:     hInstance,
		hbrBackground: uintptr(colorBtnFace + 1),
		lpszClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(1410) {
		return "", false
	}

	width := int32(520)
	height := int32(232)
	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	x := int32((int(screenW) - int(width)) / 2)
	y := int32((int(screenH) - int(height)) / 2)
	if x < 0 {
		x = 60
	}
	if y < 0 {
		y = 60
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExDlgModalFrame,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		wsCaption|wsSysMenu|wsVisible,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return "", false
	}
	d.hwnd = hwnd

	createPromptChildren(hwnd, hInstance, message, defaultValue)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWnd.Call(hwnd)
	if d.portEdit != 0 {
		procSetFocus.Call(d.portEdit)
	} else if d.hostEdit != 0 {
		procSetFocus.Call(d.hostEdit)
	}

	var m msg
	for {
		select {
		case <-d.closed:
			return d.value, d.ok
		default:
		}
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return "", false
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createPromptChildren(hwnd, hInstance uintptr, message, defaultValue string) {
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	editClass, _ := syscall.UTF16PtrFromString("EDIT")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	msg, _ := syscall.UTF16PtrFromString(message)
	defaultHost, defaultPort := splitListenAddress(defaultValue)
	defHost, _ := syscall.UTF16PtrFromString(defaultHost)
	defPort, _ := syscall.UTF16PtrFromString(defaultPort)
	hostLabel, _ := syscall.UTF16PtrFromString("地址")
	portLabel, _ := syscall.UTF16PtrFromString("端口")
	okText, _ := syscall.UTF16PtrFromString("确定")
	cancelText, _ := syscall.UTF16PtrFromString("取消")
	font, _, _ := procGetStockObject.Call(defaultGuiFont)

	iconCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), 0, wsChild|wsVisible|ssIcon,
		24, 24, 36, 36, hwnd, 0, hInstance, 0)
	if iconCtl != 0 {
		hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiInformation))
		procPromptSendMessageW.Call(iconCtl, stmSetIcon, hIcon, 0)
	}
	msgCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(msg)), wsChild|wsVisible|ssLeft|ssNotify,
		72, 24, 420, 38, hwnd, 0, hInstance, 0)

	hostLabelCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(hostLabel)), wsChild|wsVisible|ssLeft,
		72, 82, 44, 22, hwnd, 0, hInstance, 0)
	hostEdit, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(editClass)), uintptr(unsafe.Pointer(defHost)), wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll,
		124, 80, 260, 22, hwnd, 0, hInstance, 0)

	portLabelCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(portLabel)), wsChild|wsVisible|ssLeft,
		72, 119, 44, 22, hwnd, 0, hInstance, 0)
	portEdit, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(editClass)), uintptr(unsafe.Pointer(defPort)), wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll,
		124, 117, 260, 22, hwnd, 0, hInstance, 0)

	okBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(okText)), wsChild|wsVisible|wsTabStop|bsDefPushButton,
		310, 154, 84, 30, hwnd, promptIDOK, hInstance, 0)
	cancelBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(cancelText)), wsChild|wsVisible|wsTabStop,
		408, 154, 84, 30, hwnd, promptIDCancel, hInstance, 0)

	for _, ctl := range []uintptr{msgCtl, hostLabelCtl, hostEdit, portLabelCtl, portEdit, okBtn, cancelBtn} {
		if ctl != 0 && font != 0 {
			procPromptSendMessageW.Call(ctl, wmSetFont, font, 1)
		}
	}

	promptMu.Lock()
	if activePrompt != nil && activePrompt.hwnd == hwnd {
		activePrompt.hostEdit = hostEdit
		activePrompt.portEdit = portEdit
	}
	promptMu.Unlock()
}

func splitListenAddress(value string) (string, string) {
	value = normalizeListenAddress(value, defaultConfig().ListenAddress)
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "127.0.0.1", listenPort(value)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port
}

func portPromptWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		id := uint16(wParam & 0xffff)
		switch id {
		case promptIDOK:
			promptFinish(hwnd, true)
			return 0
		case promptIDCancel:
			promptFinish(hwnd, false)
			return 0
		}
	case wmClose:
		promptFinish(hwnd, false)
		return 0
	case wmDestroy:
		promptClose(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func promptFinish(hwnd uintptr, ok bool) {
	promptMu.Lock()
	d := activePrompt
	if d != nil && d.hwnd == hwnd {
		d.ok = ok
		if ok {
			host := strings.TrimSpace(readWindowText(d.hostEdit))
			port := strings.TrimSpace(readWindowText(d.portEdit))
			if host == "" {
				host = "127.0.0.1"
			}
			d.value = net.JoinHostPort(host, port)
		}
	}
	promptMu.Unlock()
	procDestroyWindow.Call(hwnd)
}

func promptClose(hwnd uintptr) {
	promptMu.Lock()
	d := activePrompt
	if d != nil && d.hwnd == hwnd && !d.done {
		d.done = true
		close(d.closed)
	}
	promptMu.Unlock()
}

func closeActivePrompt() {
	closeWebView2MissingDialog()

	promptMu.Lock()
	d := activePrompt
	if d != nil && d.hwnd != 0 && !d.done {
		procPostMessageW.Call(d.hwnd, wmClose, 0, 0)
	}
	promptMu.Unlock()

	confirmMu.Lock()
	if activeConfirm != nil && activeConfirm.hwnd != 0 && !activeConfirm.done {
		procPostMessageW.Call(activeConfirm.hwnd, wmClose, 0, 0)
	}
	confirmMu.Unlock()

	infoMu.Lock()
	if activeInfo != nil && activeInfo.hwnd != 0 && !activeInfo.done {
		procPostMessageW.Call(activeInfo.hwnd, wmClose, 0, 0)
	}
	infoMu.Unlock()
}

func readWindowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buf := make([]uint16, int(length)+2)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

type confirmDialog struct {
	hwnd   uintptr
	done   bool
	ok     bool
	closed chan struct{}
}

var (
	confirmMu     sync.Mutex
	activeConfirm *confirmDialog
)

func showStyledConfirmDialog(title, message string) bool {
	confirmMu.Lock()
	if activeConfirm != nil && !activeConfirm.done {
		if activeConfirm.hwnd != 0 {
			procShowWindow.Call(activeConfirm.hwnd, swShow)
			procSetForegroundWnd.Call(activeConfirm.hwnd)
		}
		confirmMu.Unlock()
		return false
	}
	d := &confirmDialog{closed: make(chan struct{})}
	activeConfirm = d
	confirmMu.Unlock()
	defer func() {
		confirmMu.Lock()
		if activeConfirm == d {
			activeConfirm = nil
		}
		confirmMu.Unlock()
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("BiliQueueConfirmDialogWindow")
	windowName, _ := syscall.UTF16PtrFromString(title)
	hInstance, _, _ := procPromptGetModuleHandle.Call(0)
	wc := wndClassEx{
		cbSize: uint32(unsafe.Sizeof(wndClassEx{})), lpfnWndProc: syscall.NewCallback(confirmDialogWndProc),
		hInstance: hInstance, hbrBackground: uintptr(colorBtnFace + 1), lpszClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(1410) {
		return false
	}

	width := int32(460)
	height := int32(184)
	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	x := int32((int(screenW) - int(width)) / 2)
	y := int32((int(screenH) - int(height)) / 2)
	if x < 0 {
		x = 60
	}
	if y < 0 {
		y = 60
	}
	hwnd, _, _ := procCreateWindowExW.Call(
		wsExDlgModalFrame, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)),
		wsCaption|wsSysMenu|wsVisible, uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return false
	}
	d.hwnd = hwnd
	createConfirmChildren(hwnd, hInstance, message)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWnd.Call(hwnd)

	var m msg
	for {
		select {
		case <-d.closed:
			return d.ok
		default:
		}
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return false
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createConfirmChildren(hwnd, hInstance uintptr, message string) {
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	msg, _ := syscall.UTF16PtrFromString(message)
	okText, _ := syscall.UTF16PtrFromString("确定")
	cancelText, _ := syscall.UTF16PtrFromString("取消")
	font, _, _ := procGetStockObject.Call(defaultGuiFont)

	iconCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), 0, wsChild|wsVisible|ssIcon,
		24, 26, 36, 36, hwnd, 0, hInstance, 0)
	if iconCtl != 0 {
		hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiQuestion))
		procPromptSendMessageW.Call(iconCtl, stmSetIcon, hIcon, 0)
	}
	msgCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(msg)), wsChild|wsVisible|ssLeft,
		72, 28, 360, 42, hwnd, 0, hInstance, 0)
	okBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(okText)), wsChild|wsVisible|wsTabStop|bsDefPushButton,
		250, 106, 84, 30, hwnd, promptIDOK, hInstance, 0)
	cancelBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(cancelText)), wsChild|wsVisible|wsTabStop,
		348, 106, 84, 30, hwnd, promptIDCancel, hInstance, 0)
	for _, ctl := range []uintptr{msgCtl, okBtn, cancelBtn} {
		if ctl != 0 && font != 0 {
			procPromptSendMessageW.Call(ctl, wmSetFont, font, 1)
		}
	}
}

func confirmDialogWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		id := uint16(wParam & 0xffff)
		if id == promptIDOK || id == promptIDCancel {
			confirmFinish(hwnd, id == promptIDOK)
			return 0
		}
	case wmClose:
		confirmFinish(hwnd, false)
		return 0
	case wmDestroy:
		confirmClose(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func confirmFinish(hwnd uintptr, ok bool) {
	confirmMu.Lock()
	if activeConfirm != nil && activeConfirm.hwnd == hwnd {
		activeConfirm.ok = ok
	}
	confirmMu.Unlock()
	procDestroyWindow.Call(hwnd)
}

func confirmClose(hwnd uintptr) {
	confirmMu.Lock()
	if activeConfirm != nil && activeConfirm.hwnd == hwnd && !activeConfirm.done {
		activeConfirm.done = true
		close(activeConfirm.closed)
	}
	confirmMu.Unlock()
}

type infoDialog struct {
	hwnd   uintptr
	done   bool
	closed chan struct{}
}

var (
	infoMu     sync.Mutex
	activeInfo *infoDialog
)

func showStyledInfoDialog(title, message string) {
	infoMu.Lock()
	if activeInfo != nil && !activeInfo.done {
		if activeInfo.hwnd != 0 {
			procShowWindow.Call(activeInfo.hwnd, swShow)
			procSetForegroundWnd.Call(activeInfo.hwnd)
		}
		infoMu.Unlock()
		return
	}
	d := &infoDialog{closed: make(chan struct{})}
	activeInfo = d
	infoMu.Unlock()
	defer func() {
		infoMu.Lock()
		if activeInfo == d {
			activeInfo = nil
		}
		infoMu.Unlock()
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("BiliQueueInfoDialogWindow")
	windowName, _ := syscall.UTF16PtrFromString(title)
	hInstance, _, _ := procPromptGetModuleHandle.Call(0)

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(infoDialogWndProc),
		hInstance:     hInstance,
		hbrBackground: uintptr(colorBtnFace + 1),
		lpszClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(1410) {
		return
	}

	width := int32(460)
	height := int32(166)
	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	x := int32((int(screenW) - int(width)) / 2)
	y := int32((int(screenH) - int(height)) / 2)
	if x < 0 {
		x = 60
	}
	if y < 0 {
		y = 60
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExDlgModalFrame,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		wsCaption|wsSysMenu|wsVisible,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}
	d.hwnd = hwnd
	createInfoChildren(hwnd, hInstance, message)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWnd.Call(hwnd)

	var m msg
	for {
		select {
		case <-d.closed:
			return
		default:
		}
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createInfoChildren(hwnd, hInstance uintptr, message string) {
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	msg, _ := syscall.UTF16PtrFromString(message)
	okText, _ := syscall.UTF16PtrFromString("确定")
	font, _, _ := procGetStockObject.Call(defaultGuiFont)

	iconCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), 0, wsChild|wsVisible|ssIcon,
		24, 26, 36, 36, hwnd, 0, hInstance, 0)
	if iconCtl != 0 {
		hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiInformation))
		procPromptSendMessageW.Call(iconCtl, stmSetIcon, hIcon, 0)
	}
	msgCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(msg)), wsChild|wsVisible|ssLeft,
		72, 28, 360, 42, hwnd, 0, hInstance, 0)
	okBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(okText)), wsChild|wsVisible|wsTabStop|bsDefPushButton,
		348, 92, 84, 30, hwnd, promptIDOK, hInstance, 0)
	for _, ctl := range []uintptr{msgCtl, okBtn} {
		if ctl != 0 && font != 0 {
			procPromptSendMessageW.Call(ctl, wmSetFont, font, 1)
		}
	}
}

func infoDialogWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		id := uint16(wParam & 0xffff)
		if id == promptIDOK {
			infoFinish(hwnd)
			return 0
		}
	case wmClose:
		infoFinish(hwnd)
		return 0
	case wmDestroy:
		infoClose(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func infoFinish(hwnd uintptr) {
	procDestroyWindow.Call(hwnd)
}

func infoClose(hwnd uintptr) {
	infoMu.Lock()
	d := activeInfo
	if d != nil && d.hwnd == hwnd && !d.done {
		d.done = true
		close(d.closed)
	}
	infoMu.Unlock()
}

func promptListenAddressWithRetry(current string, message string) (string, bool) {
	defaultValue := "127.0.0.1:" + listenPort(current)
	if strings.Contains(current, ":") {
		defaultValue = current
	}
	for i := 0; i < 5; i++ {
		value, ok := promptListenAddress("啊哦！", message, defaultValue)
		if !ok {
			return "", false
		}
		value = strings.TrimSpace(value)
		if value == "" {
			message = "端口不能为空，请输入一个新的端口。"
			continue
		}
		return value, true
	}
	return "", false
}
