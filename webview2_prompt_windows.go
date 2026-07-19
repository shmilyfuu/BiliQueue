//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type webView2MissingChoice int

const (
	webView2MissingCancel webView2MissingChoice = iota
	webView2MissingDownload
	webView2MissingBrowser

	webView2PromptDownloadID = 21
	webView2PromptBrowserID  = 22
)

type webView2PromptDialog struct {
	hwnd   uintptr
	done   bool
	choice webView2MissingChoice
	closed chan struct{}
}

var (
	webView2PromptMu     sync.Mutex
	activeWebView2Prompt *webView2PromptDialog
)

func showWebView2MissingDialog() webView2MissingChoice {
	webView2PromptMu.Lock()
	if activeWebView2Prompt != nil && !activeWebView2Prompt.done {
		if activeWebView2Prompt.hwnd != 0 {
			procShowWindow.Call(activeWebView2Prompt.hwnd, swShow)
			procSetForegroundWnd.Call(activeWebView2Prompt.hwnd)
		}
		webView2PromptMu.Unlock()
		return webView2MissingCancel
	}
	d := &webView2PromptDialog{closed: make(chan struct{})}
	activeWebView2Prompt = d
	webView2PromptMu.Unlock()
	defer func() {
		webView2PromptMu.Lock()
		if activeWebView2Prompt == d {
			activeWebView2Prompt = nil
		}
		webView2PromptMu.Unlock()
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString("BiliQueueWebView2PromptWindow")
	windowName, _ := syscall.UTF16PtrFromString("需要 WebView2 Runtime")
	hInstance, _, _ := procPromptGetModuleHandle.Call(0)
	wc := wndClassEx{
		cbSize: uint32(unsafe.Sizeof(wndClassEx{})), lpfnWndProc: syscall.NewCallback(webView2PromptWndProc),
		hInstance: hInstance, hbrBackground: uintptr(colorBtnFace + 1), lpszClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(1410) {
		return webView2MissingCancel
	}

	width := int32(560)
	height := int32(214)
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
		return webView2MissingCancel
	}
	d.hwnd = hwnd
	createWebView2PromptChildren(hwnd, hInstance)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWnd.Call(hwnd)

	var m msg
	for {
		select {
		case <-d.closed:
			return d.choice
		default:
		}
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return webView2MissingCancel
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createWebView2PromptChildren(hwnd, hInstance uintptr) {
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	buttonClass, _ := syscall.UTF16PtrFromString("BUTTON")
	message, _ := syscall.UTF16PtrFromString("简易控制窗口需要 Microsoft Edge WebView2 Runtime。\n请选择下载运行时、改用默认浏览器打开，或取消。")
	downloadText, _ := syscall.UTF16PtrFromString("下载 WebView2")
	browserText, _ := syscall.UTF16PtrFromString("默认浏览器打开")
	cancelText, _ := syscall.UTF16PtrFromString("取消")
	font, _, _ := procGetStockObject.Call(defaultGuiFont)

	iconCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), 0, wsChild|wsVisible|ssIcon,
		24, 28, 36, 36, hwnd, 0, hInstance, 0)
	if iconCtl != 0 {
		hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiInformation))
		procPromptSendMessageW.Call(iconCtl, stmSetIcon, hIcon, 0)
	}
	msgCtl, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(message)), wsChild|wsVisible|ssLeft,
		72, 26, 456, 54, hwnd, 0, hInstance, 0)
	downloadBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(downloadText)), wsChild|wsVisible|wsTabStop|bsDefPushButton,
		160, 116, 112, 32, hwnd, webView2PromptDownloadID, hInstance, 0)
	browserBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(browserText)), wsChild|wsVisible|wsTabStop,
		282, 116, 126, 32, hwnd, webView2PromptBrowserID, hInstance, 0)
	cancelBtn, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(cancelText)), wsChild|wsVisible|wsTabStop,
		418, 116, 84, 32, hwnd, promptIDCancel, hInstance, 0)
	for _, ctl := range []uintptr{msgCtl, downloadBtn, browserBtn, cancelBtn} {
		if ctl != 0 && font != 0 {
			procPromptSendMessageW.Call(ctl, wmSetFont, font, 1)
		}
	}
}

func webView2PromptWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		switch uint16(wParam & 0xffff) {
		case webView2PromptDownloadID:
			finishWebView2Prompt(hwnd, webView2MissingDownload)
			return 0
		case webView2PromptBrowserID:
			finishWebView2Prompt(hwnd, webView2MissingBrowser)
			return 0
		case promptIDCancel:
			finishWebView2Prompt(hwnd, webView2MissingCancel)
			return 0
		}
	case wmClose:
		finishWebView2Prompt(hwnd, webView2MissingCancel)
		return 0
	case wmDestroy:
		closeWebView2Prompt(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func finishWebView2Prompt(hwnd uintptr, choice webView2MissingChoice) {
	webView2PromptMu.Lock()
	if activeWebView2Prompt != nil && activeWebView2Prompt.hwnd == hwnd {
		activeWebView2Prompt.choice = choice
	}
	webView2PromptMu.Unlock()
	procDestroyWindow.Call(hwnd)
}

func closeWebView2Prompt(hwnd uintptr) {
	webView2PromptMu.Lock()
	if activeWebView2Prompt != nil && activeWebView2Prompt.hwnd == hwnd && !activeWebView2Prompt.done {
		activeWebView2Prompt.done = true
		close(activeWebView2Prompt.closed)
	}
	webView2PromptMu.Unlock()
}

func closeWebView2MissingDialog() {
	webView2PromptMu.Lock()
	if activeWebView2Prompt != nil && activeWebView2Prompt.hwnd != 0 && !activeWebView2Prompt.done {
		procPostMessageW.Call(activeWebView2Prompt.hwnd, wmClose, 0, 0)
	}
	webView2PromptMu.Unlock()
}
