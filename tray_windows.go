//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmNull             = 0x0000
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmContextMenu      = 0x007B
	wmCommand          = 0x0111
	wmHotkey           = 0x0312
	wmUser             = 0x0400
	wmAppTray          = wmUser + 1
	wmAppShowMenu      = wmUser + 2
	wmAppReloadHotkeys = wmUser + 3
	wmRButtonDown      = 0x0204
	wmRButtonUp        = 0x0205
	wmLButtonDblClk    = 0x0203

	mfString    = 0x0000
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	nimAdd    = 0x00000000
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	idiApplication = 32512

	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040

	mbOK              = 0x00000000
	mbIconError       = 0x00000010
	mbIconInfo        = 0x00000040
	mbSetForeground   = 0x00010000
	cfUnicodeText     = 13
	gmemMoveable      = 0x0002
	gmemZeroInit      = 0x0040
	clipboardRetryNum = 8
	modAlt            = 0x0001
	modControl        = 0x0002
	modShift          = 0x0004
	modWin            = 0x0008
	modNoRepeat       = 0x4000
)

const (
	menuOpenControl     = 1001
	menuOpenMiniControl = 1003
	menuCopyOverlay     = 1004
	menuChangePort      = 1006
	menuClearQueue      = 1007
	menuOpenDataDir     = 1008
	menuOpenLog         = 1009
	menuExit            = 1010
	menuNextQueue       = 1011

	hotkeyOpenControl     = 2001
	hotkeyOpenMiniControl = 2002
	hotkeyNextQueue       = 2003
	hotkeyClearQueue      = 2004
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procMoveMemory       = kernel32.NewProc("RtlMoveMemory")
)

type wndClassEx struct {
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

type point struct {
	x int32
	y int32
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
}

type trayApp struct {
	app               *App
	controller        *ServerController
	dataDir           string
	hwnd              uintptr
	hIcon             uintptr
	customIcon        bool
	menuOpen          bool
	menuPending       bool
	exiting           atomic.Bool
	hotkeyRequests    chan hotkeyReloadRequest
	registeredHotkeys []int32
}

type hotkeyReloadRequest struct {
	config HotkeyConfig
	done   chan map[string]string
}

var (
	activeTrayMu sync.RWMutex
	activeTray   *trayApp
)

func setActiveTray(t *trayApp) {
	activeTrayMu.Lock()
	activeTray = t
	activeTrayMu.Unlock()
}

func getActiveTray() *trayApp {
	activeTrayMu.RLock()
	t := activeTray
	activeTrayMu.RUnlock()
	return t
}

func runTray(app *App, controller *ServerController, dataDir string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className, _ := syscall.UTF16PtrFromString("BiliQueueTrayWindow")
	windowName, _ := syscall.UTF16PtrFromString("BiliQueue")
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	hIcon, customIcon := loadTrayIcon()

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(trayWndProc),
		hInstance:     hInstance,
		hIcon:         hIcon,
		lpszClassName: className,
		hIconSm:       hIcon,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}

	t := &trayApp{
		app: app, controller: controller, dataDir: dataDir, hwnd: hwnd,
		hIcon: hIcon, customIcon: customIcon,
		hotkeyRequests: make(chan hotkeyReloadRequest, 8),
	}
	setActiveTray(t)
	if err := t.addIcon(); err != nil {
		setActiveTray(nil)
		_, _, _ = procDestroyWindow.Call(hwnd)
		return err
	}
	app.mu.RLock()
	hotkeys := app.config.Hotkeys
	app.mu.RUnlock()
	app.setHotkeyStatus(t.applyHotkeys(hotkeys))
	app.broadcast()
	log.Printf("tray ready")

	var m msg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("GetMessageW: %w", err)
		}
		if r == 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func trayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	t := getActiveTray()
	switch message {
	case wmAppTray:
		if t == nil {
			break
		}
		trayMessage := uint32(lParam)
		log.Printf("tray callback message: 0x%x", trayMessage)
		switch trayMessage {
		case wmRButtonDown, wmRButtonUp, wmContextMenu:
			t.requestMenu()
		case wmLButtonDblClk:
			go t.openControl()
		}
		return 0
	case wmContextMenu:
		if t != nil {
			log.Printf("tray window context menu message")
			t.requestMenu()
		}
		return 0
	case wmAppShowMenu:
		if t != nil {
			t.menuPending = false
			t.showMenu()
		}
		return 0
	case wmAppReloadHotkeys:
		if t != nil {
			t.processHotkeyReloads()
		}
		return 0
	case wmHotkey:
		if t != nil {
			go t.handleHotkey(int32(wParam))
		}
		return 0
	case wmCommand:
		if t != nil {
			id := uint16(wParam & 0xffff)
			go t.handleMenu(id)
		}
		return 0
	case wmClose:
		closeActivePrompt()
		if t != nil {
			t.exiting.Store(true)
			t.shutdownServer()
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if t != nil {
			t.removeIcon()
			setActiveTray(nil)
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func loadTrayIcon() (uintptr, bool) {
	if executable, err := os.Executable(); err == nil {
		iconPath := filepath.Join(filepath.Dir(executable), "assets", "biliqueue.ico")
		if _, err := os.Stat(iconPath); err == nil {
			path, _ := syscall.UTF16PtrFromString(iconPath)
			if hIcon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(path)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize); hIcon != 0 {
				return hIcon, true
			}
		}
	}
	hIcon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return hIcon, false
}

func (t *trayApp) addIcon() error {
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.hwnd
	nid.uID = 1
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmAppTray
	nid.hIcon = t.hIcon
	copy(nid.szTip[:], syscall.StringToUTF16("BiliQueue v"+version))
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return fmt.Errorf("Shell_NotifyIconW add: %w", err)
	}
	return nil
}

func (t *trayApp) removeIcon() {
	t.unregisterHotkeys()
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = t.hwnd
	nid.uID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if t.customIcon && t.hIcon != 0 {
		procDestroyIcon.Call(t.hIcon)
		t.hIcon = 0
	}
}

func (t *trayApp) requestMenu() {
	if t == nil || t.exiting.Load() || t.menuOpen || t.menuPending {
		return
	}
	t.menuPending = true
	procPostMessageW.Call(t.hwnd, wmAppShowMenu, 0, 0)
}

func (t *trayApp) showMenu() {
	if t == nil || t.exiting.Load() || t.menuOpen {
		return
	}
	t.menuOpen = true
	defer func() { t.menuOpen = false }()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, mfString, menuOpenControl, "打开控制台")
	appendMenu(menu, mfString, menuOpenMiniControl, "打开简易控制页")
	appendMenu(menu, mfString, menuCopyOverlay, "复制浏览器源地址")
	appendMenu(menu, mfString, menuChangePort, "修改端口")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuNextQueue, "下一位")
	appendMenu(menu, mfString, menuClearQueue, "清空队列")
	appendMenu(menu, mfString, menuOpenDataDir, "打开数据文件夹")
	appendMenu(menu, mfString, menuOpenLog, "打开日志文件")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuExit, "退出")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWnd.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	if cmd != 0 {
		go t.handleMenu(uint16(cmd))
	}
}

func appendMenu(menu uintptr, flags uint32, id uint16, text string) {
	if flags&mfSeparator != 0 {
		procAppendMenuW.Call(menu, uintptr(flags), 0, 0)
		return
	}
	label, _ := syscall.UTF16PtrFromString(text)
	procAppendMenuW.Call(menu, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(label)))
}

func (t *trayApp) handleMenu(id uint16) {
	log.Printf("tray menu selected: %d", id)
	switch id {
	case menuOpenControl:
		t.openControl()
	case menuOpenMiniControl:
		t.openMiniControl()
	case menuCopyOverlay:
		if err := copyTextToClipboard(t.overlayURL()); err != nil {
			log.Printf("copy source url: %v", err)
			messageBox("BiliQueue", "复制失败："+err.Error(), mbOK|mbIconError|mbSetForeground)
		}
	case menuChangePort:
		t.changePort()
	case menuNextQueue:
		t.app.advanceQueue()
	case menuClearQueue:
		if showStyledConfirmDialog("清空队列", "确定清空当前队列吗？此操作无法撤销。") {
			t.app.clearQueue()
		}
	case menuOpenDataDir:
		openPath(t.dataDir)
	case menuOpenLog:
		openPath(filepath.Join(t.dataDir, "logs", "biliqueue.log"))
	case menuExit:
		log.Printf("tray exit requested")
		if t.exiting.CompareAndSwap(false, true) {
			closeActivePrompt()
			procPostMessageW.Call(t.hwnd, wmClose, 0, 0)
		}
	}
}

func reloadGlobalHotkeys(cfg HotkeyConfig) map[string]string {
	t := getActiveTray()
	if t == nil || t.hwnd == 0 || t.exiting.Load() {
		return defaultHotkeyStatus("托盘模式未启用")
	}
	req := hotkeyReloadRequest{config: cfg, done: make(chan map[string]string, 1)}
	select {
	case t.hotkeyRequests <- req:
		procPostMessageW.Call(t.hwnd, wmAppReloadHotkeys, 0, 0)
	case <-time.After(2 * time.Second):
		return defaultHotkeyStatus("快捷键更新队列繁忙")
	}
	select {
	case status := <-req.done:
		return status
	case <-time.After(3 * time.Second):
		return defaultHotkeyStatus("快捷键注册超时")
	}
}

func (t *trayApp) processHotkeyReloads() {
	for {
		select {
		case req := <-t.hotkeyRequests:
			req.done <- t.applyHotkeys(req.config)
		default:
			return
		}
	}
}

func (t *trayApp) unregisterHotkeys() {
	for _, id := range t.registeredHotkeys {
		procUnregisterHotKey.Call(t.hwnd, uintptr(id))
	}
	t.registeredHotkeys = nil
}

func (t *trayApp) applyHotkeys(cfg HotkeyConfig) map[string]string {
	t.unregisterHotkeys()
	type binding struct {
		key   string
		label string
		value string
		id    int32
	}
	bindings := []binding{
		{key: "openControl", label: "打开控制台网页", value: cfg.OpenControl, id: hotkeyOpenControl},
		{key: "openMiniControl", label: "打开简易控制页", value: cfg.OpenMiniControl, id: hotkeyOpenMiniControl},
		{key: "nextQueue", label: "下一位", value: cfg.NextQueue, id: hotkeyNextQueue},
		{key: "clearQueue", label: "清空队列", value: cfg.ClearQueue, id: hotkeyClearQueue},
	}
	status := make(map[string]string, len(bindings))
	seen := make(map[string]string, len(bindings))
	for _, item := range bindings {
		value := strings.TrimSpace(item.value)
		if value == "" {
			status[item.key] = "未设置"
			continue
		}
		modifiers, virtualKey, canonical, err := parseWindowsHotkey(value)
		if err != nil {
			status[item.key] = "无效快捷键：" + err.Error()
			continue
		}
		duplicateKey := strings.ToLower(canonical)
		if label, exists := seen[duplicateKey]; exists {
			status[item.key] = "与“" + label + "”重复"
			continue
		}
		seen[duplicateKey] = item.label
		result, _, _ := procRegisterHotKey.Call(t.hwnd, uintptr(item.id), uintptr(modifiers|modNoRepeat), uintptr(virtualKey))
		if result == 0 {
			status[item.key] = "注册失败，快捷键可能已被占用"
			continue
		}
		t.registeredHotkeys = append(t.registeredHotkeys, item.id)
		status[item.key] = "已启用"
	}
	return status
}

func parseWindowsHotkey(value string) (uint32, uint32, string, error) {
	parts := strings.Split(value, "+")
	if len(parts) == 0 {
		return 0, 0, "", fmt.Errorf("格式为空")
	}
	var modifiers uint32
	canonical := make([]string, 0, len(parts))
	for _, raw := range parts[:len(parts)-1] {
		part := strings.ToLower(strings.TrimSpace(raw))
		switch part {
		case "ctrl", "control":
			modifiers |= modControl
			canonical = append(canonical, "Ctrl")
		case "alt":
			modifiers |= modAlt
			canonical = append(canonical, "Alt")
		case "shift":
			modifiers |= modShift
			canonical = append(canonical, "Shift")
		case "win", "meta":
			modifiers |= modWin
			canonical = append(canonical, "Win")
		default:
			return 0, 0, "", fmt.Errorf("无法识别修饰键 %q", raw)
		}
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	virtualKey, keyLabel, err := windowsVirtualKey(key)
	if err != nil {
		return 0, 0, "", err
	}
	canonical = append(canonical, keyLabel)
	return modifiers, virtualKey, strings.Join(canonical, "+"), nil
}

func windowsVirtualKey(key string) (uint32, string, error) {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if len(upper) == 1 {
		ch := upper[0]
		if ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			return uint32(ch), upper, nil
		}
	}
	if strings.HasPrefix(upper, "F") {
		if number, err := strconv.Atoi(strings.TrimPrefix(upper, "F")); err == nil && number >= 1 && number <= 24 {
			return uint32(0x70 + number - 1), "F" + strconv.Itoa(number), nil
		}
	}
	keys := map[string]struct {
		code  uint32
		label string
	}{
		"SPACE": {0x20, "Space"}, "ENTER": {0x0D, "Enter"}, "TAB": {0x09, "Tab"},
		"BACKSPACE": {0x08, "Backspace"}, "DELETE": {0x2E, "Delete"}, "INSERT": {0x2D, "Insert"},
		"HOME": {0x24, "Home"}, "END": {0x23, "End"}, "PAGEUP": {0x21, "PageUp"}, "PAGEDOWN": {0x22, "PageDown"},
		"ARROWUP": {0x26, "ArrowUp"}, "ARROWDOWN": {0x28, "ArrowDown"}, "ARROWLEFT": {0x25, "ArrowLeft"}, "ARROWRIGHT": {0x27, "ArrowRight"},
	}
	if item, ok := keys[upper]; ok {
		return item.code, item.label, nil
	}
	return 0, "", fmt.Errorf("无法识别按键 %q", key)
}

func (t *trayApp) handleHotkey(id int32) {
	switch id {
	case hotkeyOpenControl:
		t.openControl()
	case hotkeyOpenMiniControl:
		t.openMiniControl()
	case hotkeyNextQueue:
		t.app.advanceQueue()
	case hotkeyClearQueue:
		if showStyledConfirmDialog("清空队列", "确定清空当前队列吗？此操作无法撤销。") {
			t.app.clearQueue()
		}
	}
}

func (t *trayApp) listenAddress() string  { return t.app.currentListenAddress() }
func (t *trayApp) controlURL() string     { return urlForListen(t.listenAddress(), "/control") }
func (t *trayApp) overlayURL() string     { return urlForListen(t.listenAddress(), "/overlay") }
func (t *trayApp) miniControlURL() string { return urlForListen(t.listenAddress(), "/mini-control") }

func (t *trayApp) openControl() {
	if err := openBrowser(freshOpenURL(t.controlURL())); err != nil {
		log.Printf("open control: %v", err)
	}
}

func (t *trayApp) openMiniControl() {
	if err := openMiniControlWindow(t.app); err != nil {
		log.Printf("open mini control: %v", err)
	}
}

func (t *trayApp) changePort() {
	current := t.listenAddress()
	input, ok := promptListenAddress("修改端口", "请输入新的本机服务地址和端口。", current)
	if !ok {
		return
	}
	state, err := t.controller.ChangeListenAddress(input)
	if err != nil {
		messageBox("啊哦！", "端口修改失败："+err.Error(), mbOK|mbIconError|mbSetForeground)
		return
	}
	_ = copyTextToClipboard(state.OverlayURL)
	messageBox("BiliQueue", "端口已修改。浏览器源地址已复制：\n"+state.OverlayURL, mbOK|mbSetForeground)
}

func (t *trayApp) shutdownServer() {
	closeMiniControlWindow()
	if t.app != nil {
		t.app.prepareExit()
	}
	if t.controller != nil {
		_ = t.controller.Close()
	}
}

func openPath(path string) {
	if err := exec.Command("explorer", path).Start(); err != nil {
		log.Printf("open path %s: %v", path, err)
	}
}

func copyTextToClipboard(text string) error {
	utf16 := syscall.StringToUTF16(text)
	bytes := uintptr(len(utf16) * 2)
	hMem, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, bytes)
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	ptr, _, err := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock: %w", err)
	}
	procMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), bytes)
	runtime.KeepAlive(utf16)
	procGlobalUnlock.Call(hMem)

	var opened bool
	for i := 0; i < clipboardRetryNum; i++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			opened = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !opened {
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}

func messageBox(title, text string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return int(r)
}

func showErrorDialog(title, message string) {
	messageBox(title, message, mbOK|mbIconError|mbSetForeground)
}

func showInfoDialog(title, message string) {
	showStyledInfoDialog(title, message)
}
