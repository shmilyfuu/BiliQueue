//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmUser          = 0x0400
	wmAppTray       = wmUser + 1
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203

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
	mbIconQuestion    = 0x00000020
	mbIconInfo        = 0x00000040
	mbYesNo           = 0x00000004
	mbSetForeground   = 0x00010000
	idYes             = 6
	cfUnicodeText     = 13
	gmemMoveable      = 0x0002
	gmemZeroInit      = 0x0040
	clipboardRetryNum = 8
)

const (
	menuOpenControl     = 1001
	menuOpenOverlay     = 1002
	menuOpenMiniControl = 1003
	menuCopyOverlay     = 1004
	menuCopyControl     = 1005
	menuChangePort      = 1006
	menuClearQueue      = 1007
	menuOpenDataDir     = 1008
	menuOpenLog         = 1009
	menuExit            = 1010
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

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
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
	app        *App
	controller *ServerController
	dataDir    string
	hwnd       uintptr
	hIcon      uintptr
	customIcon bool
}

var activeTray *trayApp

func runTray(app *App, controller *ServerController, dataDir string) error {
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

	t := &trayApp{app: app, controller: controller, dataDir: dataDir, hwnd: hwnd, hIcon: hIcon, customIcon: customIcon}
	activeTray = t
	if err := t.addIcon(); err != nil {
		_, _, _ = procDestroyWindow.Call(hwnd)
		return err
	}
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
	t := activeTray
	switch message {
	case wmAppTray:
		if t == nil {
			break
		}
		switch uint32(lParam) {
		case wmRButtonUp:
			t.showMenu()
		case wmLButtonDblClk:
			go t.openControl()
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
			t.shutdownServer()
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if t != nil {
			t.removeIcon()
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

func (t *trayApp) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, mfString, menuOpenControl, "打开控制台")
	appendMenu(menu, mfString, menuOpenOverlay, "打开横条")
	appendMenu(menu, mfString, menuOpenMiniControl, "打开简易控制页")
	appendMenu(menu, mfString, menuCopyOverlay, "复制浏览器源地址")
	appendMenu(menu, mfString, menuCopyControl, "复制控制台地址")
	appendMenu(menu, mfString, menuChangePort, "修改端口")
	appendMenu(menu, mfSeparator, 0, "")
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
	case menuOpenOverlay:
		if err := openBrowser(freshOpenURL(t.overlayURL())); err != nil {
			log.Printf("open overlay: %v", err)
		}
	case menuOpenMiniControl:
		if err := openBrowser(freshOpenURL(t.miniControlURL())); err != nil {
			log.Printf("open mini control: %v", err)
		}
	case menuCopyOverlay:
		if err := copyTextToClipboard(t.overlayURL()); err != nil {
			log.Printf("copy source url: %v", err)
			messageBox("BiliQueue", "复制失败："+err.Error(), mbOK|mbIconError|mbSetForeground)
		}
	case menuCopyControl:
		if err := copyTextToClipboard(t.controlURL()); err != nil {
			log.Printf("copy control url: %v", err)
			messageBox("BiliQueue", "复制失败："+err.Error(), mbOK|mbIconError|mbSetForeground)
		}
	case menuChangePort:
		t.changePort()
	case menuClearQueue:
		if messageBox("BiliQueue", "确定清空当前队列吗？", mbYesNo|mbIconQuestion|mbSetForeground) == idYes {
			t.app.clearQueue()
		}
	case menuOpenDataDir:
		openPath(t.dataDir)
	case menuOpenLog:
		openPath(filepath.Join(t.dataDir, "logs", "biliqueue.log"))
	case menuExit:
		log.Printf("tray exit requested")
		closeActivePrompt()
		procPostMessageW.Call(t.hwnd, wmClose, 0, 0)
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
	for i, v := range utf16 {
		*(*uint16)(unsafe.Pointer(ptr + uintptr(i*2))) = v
	}
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
