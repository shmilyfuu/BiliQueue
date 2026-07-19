//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

const (
	miniWindowWidth  = 540
	miniWindowHeight = 720

	miniSWHide      = 0
	miniSWRestore   = 9
	miniWMClose     = 0x0010
	miniWMColorText = 0x0138
	miniGWLWndProc  = ^uintptr(3) // GWLP_WNDPROC (-4)
	miniSWPNoSize   = 0x0001
	miniSWPNoMove   = 0x0002
	miniSWPNoZOrder = 0x0004
	miniSWPNoActive = 0x0010

	miniLoadingWindowClass = "BiliQueueMiniLoadingWindow"
	miniWSExTopmost        = 0x00000008
	miniWSOverlappedWindow = 0x00CF0000
	miniSSCenter           = 0x00000001
	miniBkTransparent      = 1
)

var (
	miniWindowUser32          = syscall.NewLazyDLL("user32.dll")
	miniProcShowWindow        = miniWindowUser32.NewProc("ShowWindow")
	miniProcSetForeground     = miniWindowUser32.NewProc("SetForegroundWindow")
	miniProcSetWindowPosition = miniWindowUser32.NewProc("SetWindowPos")
	miniProcSetWindowLong     = miniWindowUser32.NewProc("SetWindowLongPtrW")
	miniProcCallWindow        = miniWindowUser32.NewProc("CallWindowProcW")
	miniProcDefWindow         = miniWindowUser32.NewProc("DefWindowProcW")
	miniWindowCallback        = syscall.NewCallback(miniControlWindowProc)
	miniLoadingWindowCallback = syscall.NewCallback(miniLoadingWindowProc)
	miniLoadingGDI32          = syscall.NewLazyDLL("gdi32.dll")
	miniProcCreateSolidBrush  = miniLoadingGDI32.NewProc("CreateSolidBrush")
	miniProcSetTextColor      = miniLoadingGDI32.NewProc("SetTextColor")
	miniProcSetBkMode         = miniLoadingGDI32.NewProc("SetBkMode")
	miniLoadingClassOnce      sync.Once
	miniLoadingClassReady     bool
	miniLoadingBackground     uintptr
)

type miniWindowPreferences struct {
	Topmost bool `json:"topmost"`
}

type miniWindowManager struct {
	mu          sync.Mutex
	view        webview2.WebView
	hwnd        uintptr
	opening     bool
	ready       bool
	topmost     bool
	destroying  bool
	oldWndProc  uintptr
	loadingHwnd uintptr
}

var nativeMiniWindow miniWindowManager

func openMiniControlWindow(app *App) error {
	if app == nil {
		return errMiniControlWindowUnavailable
	}
	nativeMiniWindow.mu.Lock()
	if nativeMiniWindow.view != nil && nativeMiniWindow.hwnd != 0 {
		view := nativeMiniWindow.view
		ready := nativeMiniWindow.ready
		nativeMiniWindow.mu.Unlock()
		if ready {
			view.Dispatch(func() {
				hwnd := uintptr(view.Window())
				miniProcShowWindow.Call(hwnd, miniSWRestore)
				miniProcSetForeground.Call(hwnd)
			})
		}
		return nil
	}
	if nativeMiniWindow.opening {
		nativeMiniWindow.mu.Unlock()
		return nil
	}
	nativeMiniWindow.opening = true
	nativeMiniWindow.mu.Unlock()
	go runMiniControlWindow(app)
	return nil
}

func runMiniControlWindow(app *App) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	version, err := webviewloader.GetInstalledVersion()
	if err != nil || strings.TrimSpace(version) == "" {
		if err != nil {
			log.Printf("detect WebView2 runtime: %v", err)
		}
		markMiniWindowClosed()
		handleMissingWebView2(app)
		return
	}

	dataPath := filepath.Join(app.dataDir, "webview2")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		log.Printf("create WebView2 data directory: %v", err)
	}
	windowTitle := "BiliQueue 简易控制"
	loadingHwnd := createMiniLoadingWindow(windowTitle)
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  miniWindowWidth,
			Height: miniWindowHeight,
			Center: true,
		},
	})
	if view == nil {
		destroyMiniLoadingWindow(loadingHwnd)
		markMiniWindowClosed()
		handleMissingWebView2(app)
		return
	}

	hwnd := uintptr(view.Window())
	miniProcShowWindow.Call(hwnd, miniSWHide)
	view.SetSize(miniWindowWidth, miniWindowHeight, webview2.HintFixed)
	prefs := loadMiniWindowPreferences(app)
	applyMiniWindowOuterSize(hwnd)
	oldWndProc, _, _ := miniProcSetWindowLong.Call(hwnd, miniGWLWndProc, miniWindowCallback)
	nativeMiniWindow.mu.Lock()
	nativeMiniWindow.view = view
	nativeMiniWindow.hwnd = hwnd
	nativeMiniWindow.opening = false
	nativeMiniWindow.ready = false
	nativeMiniWindow.topmost = prefs.Topmost
	nativeMiniWindow.destroying = false
	nativeMiniWindow.oldWndProc = oldWndProc
	nativeMiniWindow.loadingHwnd = loadingHwnd
	nativeMiniWindow.mu.Unlock()
	applyMiniWindowTopmost(hwnd, prefs.Topmost)
	if err := view.Bind("__biliqueueMiniReady", func() {
		showReadyMiniControlWindow(hwnd)
	}); err != nil {
		log.Printf("bind mini control ready callback: %v", err)
		showReadyMiniControlWindow(hwnd)
	}
	view.Navigate(freshOpenURL(urlForListen(app.currentListenAddress(), "/mini-control")))
	view.Run()
	markMiniWindowClosed()
}

func createMiniLoadingWindow(title string) uintptr {
	miniLoadingClassOnce.Do(func() {
		className, _ := syscall.UTF16PtrFromString(miniLoadingWindowClass)
		hInstance, _, _ := procPromptGetModuleHandle.Call(0)
		miniLoadingBackground, _, _ = miniProcCreateSolidBrush.Call(0x001B1411) // #11141b
		wc := wndClassEx{
			cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			lpfnWndProc:   miniLoadingWindowCallback,
			hInstance:     hInstance,
			hbrBackground: miniLoadingBackground,
			lpszClassName: className,
		}
		registered, _, registerErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		miniLoadingClassReady = registered != 0 || registerErr == syscall.Errno(1410)
	})
	if !miniLoadingClassReady {
		return 0
	}

	className, _ := syscall.UTF16PtrFromString(miniLoadingWindowClass)
	windowTitle, _ := syscall.UTF16PtrFromString(title)
	hInstance, _, _ := procPromptGetModuleHandle.Call(0)
	screenWidth, _, _ := procGetSystemMetrics.Call(0)
	screenHeight, _, _ := procGetSystemMetrics.Call(1)
	x := (int32(screenWidth) - miniWindowWidth) / 2
	y := (int32(screenHeight) - miniWindowHeight) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	hwnd, _, _ := procCreateWindowExW.Call(
		miniWSExTopmost,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		miniWSOverlappedWindow|wsVisible,
		uintptr(x), uintptr(y), miniWindowWidth, miniWindowHeight,
		0, 0, hInstance, 0,
	)
	if hwnd != 0 {
		staticClass, _ := syscall.UTF16PtrFromString("STATIC")
		loadingText, _ := syscall.UTF16PtrFromString("正在打开简易控制...")
		label, _, _ := procCreateWindowExW.Call(
			0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(loadingText)),
			wsChild|wsVisible|miniSSCenter,
			0, 318, miniWindowWidth, 28,
			hwnd, 0, hInstance, 0,
		)
		if label != 0 {
			font, _, _ := procGetStockObject.Call(defaultGuiFont)
			procPromptSendMessageW.Call(label, wmSetFont, font, 1)
		}
		procShowWindow.Call(hwnd, swShow)
		procUpdateWindow.Call(hwnd)
		procSetForegroundWnd.Call(hwnd)
	}
	return hwnd
}

func miniLoadingWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == miniWMClose {
		return 0
	}
	if message == miniWMColorText {
		miniProcSetTextColor.Call(wParam, 0x009F9993) // #93999f
		miniProcSetBkMode.Call(wParam, miniBkTransparent)
		return miniLoadingBackground
	}
	result, _, _ := miniProcDefWindow.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func destroyMiniLoadingWindow(hwnd uintptr) {
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}
}

func miniControlWindowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	nativeMiniWindow.mu.Lock()
	owned := nativeMiniWindow.hwnd == hwnd
	destroying := nativeMiniWindow.destroying
	oldWndProc := nativeMiniWindow.oldWndProc
	nativeMiniWindow.mu.Unlock()

	if owned && message == miniWMClose && !destroying {
		miniProcShowWindow.Call(hwnd, miniSWHide)
		return 0
	}
	if oldWndProc != 0 {
		result, _, _ := miniProcCallWindow.Call(oldWndProc, hwnd, message, wParam, lParam)
		return result
	}
	result, _, _ := miniProcDefWindow.Call(hwnd, message, wParam, lParam)
	return result
}

func showReadyMiniControlWindow(hwnd uintptr) {
	nativeMiniWindow.mu.Lock()
	if nativeMiniWindow.hwnd != hwnd || nativeMiniWindow.destroying || nativeMiniWindow.ready {
		nativeMiniWindow.mu.Unlock()
		return
	}
	nativeMiniWindow.ready = true
	loadingHwnd := nativeMiniWindow.loadingHwnd
	nativeMiniWindow.loadingHwnd = 0
	nativeMiniWindow.mu.Unlock()
	miniProcShowWindow.Call(hwnd, miniSWRestore)
	destroyMiniLoadingWindow(loadingHwnd)
	miniProcSetForeground.Call(hwnd)
}

func handleMissingWebView2(app *App) {
	choice := showWebView2MissingDialog()
	switch choice {
	case webView2MissingDownload:
		if err := openBrowser("https://go.microsoft.com/fwlink/p/?LinkId=2124703"); err != nil {
			log.Printf("open WebView2 download: %v", err)
		}
	case webView2MissingBrowser:
		if err := openBrowser(freshOpenURL(urlForListen(app.currentListenAddress(), "/mini-control"))); err != nil {
			log.Printf("open mini control in browser: %v", err)
		}
	}
}

func markMiniWindowClosed() {
	nativeMiniWindow.mu.Lock()
	loadingHwnd := nativeMiniWindow.loadingHwnd
	nativeMiniWindow.view = nil
	nativeMiniWindow.hwnd = 0
	nativeMiniWindow.opening = false
	nativeMiniWindow.ready = false
	nativeMiniWindow.destroying = false
	nativeMiniWindow.oldWndProc = 0
	nativeMiniWindow.loadingHwnd = 0
	nativeMiniWindow.mu.Unlock()
	destroyMiniLoadingWindow(loadingHwnd)
}

func miniControlWindowState() MiniControlWindowState {
	nativeMiniWindow.mu.Lock()
	defer nativeMiniWindow.mu.Unlock()
	return MiniControlWindowState{
		Supported: true,
		Active:    nativeMiniWindow.view != nil && nativeMiniWindow.hwnd != 0,
		Opening:   nativeMiniWindow.opening,
		Topmost:   nativeMiniWindow.topmost,
	}
}

func setMiniControlWindowTopmost(app *App, topmost bool) (MiniControlWindowState, error) {
	nativeMiniWindow.mu.Lock()
	view := nativeMiniWindow.view
	hwnd := nativeMiniWindow.hwnd
	if view == nil || hwnd == 0 {
		nativeMiniWindow.mu.Unlock()
		return miniControlWindowState(), errMiniControlWindowUnavailable
	}
	nativeMiniWindow.topmost = topmost
	nativeMiniWindow.mu.Unlock()

	view.Dispatch(func() { applyMiniWindowTopmost(hwnd, topmost) })
	if err := saveMiniWindowPreferences(app, miniWindowPreferences{Topmost: topmost}); err != nil {
		log.Printf("save mini window preferences: %v", err)
	}
	return miniControlWindowState(), nil
}

func applyMiniWindowTopmost(hwnd uintptr, topmost bool) {
	insertAfter := ^uintptr(1) // HWND_NOTOPMOST (-2)
	if topmost {
		insertAfter = ^uintptr(0) // HWND_TOPMOST (-1)
	}
	miniProcSetWindowPosition.Call(hwnd, insertAfter, 0, 0, 0, 0, miniSWPNoMove|miniSWPNoSize|miniSWPNoActive)
}

func applyMiniWindowOuterSize(hwnd uintptr) {
	miniProcSetWindowPosition.Call(
		hwnd, 0, 0, 0, miniWindowWidth, miniWindowHeight,
		miniSWPNoMove|miniSWPNoZOrder|miniSWPNoActive,
	)
}

func refreshMiniControlWindow(app *App) {
	nativeMiniWindow.mu.Lock()
	view := nativeMiniWindow.view
	nativeMiniWindow.mu.Unlock()
	if view == nil || app == nil {
		return
	}
	url := freshOpenURL(urlForListen(app.currentListenAddress(), "/mini-control"))
	view.Dispatch(func() { view.Navigate(url) })
}

func closeMiniControlWindow() {
	nativeMiniWindow.mu.Lock()
	view := nativeMiniWindow.view
	nativeMiniWindow.destroying = true
	loadingHwnd := nativeMiniWindow.loadingHwnd
	nativeMiniWindow.loadingHwnd = 0
	nativeMiniWindow.mu.Unlock()
	destroyMiniLoadingWindow(loadingHwnd)
	if view != nil {
		view.Dispatch(func() { view.Destroy() })
	}
}

func miniWindowPreferencesPath(app *App) string {
	return filepath.Join(app.dataDir, "mini-window.json")
}

func loadMiniWindowPreferences(app *App) miniWindowPreferences {
	var prefs miniWindowPreferences
	data, err := os.ReadFile(miniWindowPreferencesPath(app))
	if err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	return prefs
}

func saveMiniWindowPreferences(app *App, prefs miniWindowPreferences) error {
	if app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	return writeJSONAtomic(miniWindowPreferencesPath(app), prefs)
}
