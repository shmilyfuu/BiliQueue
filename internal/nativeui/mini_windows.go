//go:build windows

package nativeui

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type miniBitmap struct {
	pixels []uint32
	width  int
	height int
	handle uintptr
}

type miniWindow struct {
	host       *Host
	hwnd       uintptr
	controller MiniController
	state      MiniState
	topmost    bool
	visible    bool
	dpi        uint32
	renderer   *direct2DRenderer
	dwmActive  bool
	cancel     func()

	input          uintptr
	font           uintptr
	elements       []hitElement
	queueViewport  Rect
	hovered        string
	pressed        string
	scrollY        int
	scrollbarUntil time.Time
	scrollbarDrag  bool
	scrollbarGrabY int
	modal          bool
	dragging       bool
	dragIndex      int
	dragTarget     int
	dragStartY     int
	dragMoved      bool
	toast          string
	toastUntil     time.Time

	imageMu          sync.Mutex
	images           map[string]*miniBitmap
	loading          map[string]bool
	bitmapGeneration uint64
	oldInputProc     uintptr
}

var (
	miniInputCallback = syscall.NewCallback(miniInputWindowProc)
	miniInputRegistry sync.Map
)

const miniScrollbarTimerID = 2

func (h *Host) PreloadMini(controller MiniController, topmost bool) error {
	return h.ensureMini(controller, topmost, false)
}

func (h *Host) OpenMini(controller MiniController, topmost bool) error {
	return h.ensureMini(controller, topmost, true)
}

func (h *Host) ensureMini(controller MiniController, topmost, visible bool) error {
	if h == nil {
		return ErrUnavailable
	}
	result := make(chan error, 1)
	if err := h.dispatch(func() {
		if h.mini == nil || h.mini.hwnd == 0 {
			win, err := h.createMini(controller, topmost)
			if err != nil {
				result <- err
				return
			}
			h.mini = win
		} else {
			h.mini.controller = controller
			h.mini.setTopmost(topmost, false)
		}
		if visible {
			h.mini.show()
		}
		result <- nil
	}); err != nil {
		return err
	}
	return <-result
}

func (h *Host) ToggleMini(controller MiniController, topmost bool) error {
	if h == nil {
		return ErrUnavailable
	}
	result := make(chan error, 1)
	if err := h.dispatch(func() {
		if h.mini == nil || h.mini.hwnd == 0 {
			win, err := h.createMini(controller, topmost)
			if err != nil {
				result <- err
				return
			}
			h.mini = win
			h.mini.show()
		} else if h.mini.visible {
			h.mini.hide()
		} else {
			h.mini.show()
		}
		result <- nil
	}); err != nil {
		return err
	}
	return <-result
}

func (h *Host) SetMiniTopmost(topmost bool) (MiniWindowState, error) {
	if h == nil {
		return MiniWindowState{}, ErrUnavailable
	}
	result := make(chan MiniWindowState, 1)
	if err := h.dispatch(func() {
		if h.mini == nil || h.mini.hwnd == 0 {
			result <- MiniWindowState{}
			return
		}
		h.mini.setTopmost(topmost, true)
		result <- h.mini.windowState()
	}); err != nil {
		return MiniWindowState{}, err
	}
	state := <-result
	if !state.Active {
		return state, ErrUnavailable
	}
	return state, nil
}

func (h *Host) MiniState() MiniWindowState {
	if h == nil || h.threadID == 0 {
		return MiniWindowState{}
	}
	result := make(chan MiniWindowState, 1)
	if h.dispatch(func() {
		if h.mini == nil {
			result <- MiniWindowState{}
			return
		}
		result <- h.mini.windowState()
	}) != nil {
		return MiniWindowState{}
	}
	return <-result
}

func (h *Host) CloseMini() {
	if h == nil {
		return
	}
	_ = h.dispatch(func() {
		if h.mini != nil && h.mini.hwnd != 0 {
			procDestroyWindow.Call(h.mini.hwnd)
		}
		h.mini = nil
	})
}

func (h *Host) createMini(controller MiniController, topmost bool) (*miniWindow, error) {
	win := &miniWindow{
		host:       h,
		controller: controller,
		topmost:    topmost,
		dpi:        96,
		dragIndex:  -1,
		dragTarget: -1,
		images:     make(map[string]*miniBitmap),
		loading:    make(map[string]bool),
	}
	className, _ := syscall.UTF16PtrFromString(nativeClassName)
	title, _ := syscall.UTF16PtrFromString("BiliQueue 简易控制")
	instance, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, callErr := procCreateWindowExW.Call(
		wsExDlgModalFrame,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsFixedWindow,
		0, 0, 420, 560,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("create native mini control: %w", callErr)
	}
	win.hwnd = hwnd
	windowRegistry.Store(hwnd, win)
	applyNativeWindowIcons(hwnd)
	win.dpi = windowDPI(hwnd)
	win.resizeClient(true)
	material := h.design.MaterialFor("miniControl")
	win.dwmActive = applyDWM(hwnd, material, false)
	var client nativeRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	renderer, err := newDirect2DRenderer(h.renderer, hwnd, int(client.Right-client.Left), int(client.Bottom-client.Top), win.dpi)
	if err != nil {
		windowRegistry.Delete(hwnd)
		procDestroyWindow.Call(hwnd)
		return nil, err
	}
	win.renderer = renderer
	win.bitmapGeneration = renderer.generation
	inputStyle := h.design.Style("manual.input")
	win.font = createUIFont(h.design.Theme.Typography.Family, maxInt(12, inputStyle.FontSize), win.dpi)
	win.createInput()
	win.setTopmost(topmost, false)
	win.startSubscription()
	win.loadGuardIcons()
	procInvalidateRect.Call(hwnd, 0, 0)
	return win, nil
}

func (w *miniWindow) resizeClient(center bool) {
	targetWidth := scaleForDPI(420, w.dpi)
	targetHeight := scaleForDPI(560, w.dpi)
	var client, outer nativeRect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&client)))
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&outer)))
	currentClientWidth := int(client.Right - client.Left)
	currentClientHeight := int(client.Bottom - client.Top)
	currentOuterWidth := int(outer.Right - outer.Left)
	currentOuterHeight := int(outer.Bottom - outer.Top)
	width := currentOuterWidth + targetWidth - currentClientWidth
	height := currentOuterHeight + targetHeight - currentClientHeight
	x, y := int(outer.Left), int(outer.Top)
	if center {
		screenWidth, _, _ := procGetSystemMetrics.Call(0)
		screenHeight, _, _ := procGetSystemMetrics.Call(1)
		x = maxInt(0, (int(screenWidth)-width)/2)
		y = maxInt(0, (int(screenHeight)-height)/2)
	}
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpNoActivate)
}

func (w *miniWindow) createInput() {
	className, _ := syscall.UTF16PtrFromString("EDIT")
	empty, _ := syscall.UTF16PtrFromString("")
	instance, _, _ := procGetModuleHandleW.Call(0)
	rect := w.miniGeometry().input
	style := w.host.design.Style("manual.input")
	editor := editOverlayRect(rect, 10, 10, style.FontSize)
	w.input, _, _ = procCreateWindowExW.Call(
		wsExTransparent,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(empty)),
		wsChild|wsVisible|wsTabStop|esAutoHScroll,
		uintptr(scaleForDPI(editor.X, w.dpi)),
		uintptr(scaleForDPI(editor.Y, w.dpi)),
		uintptr(scaleForDPI(editor.W, w.dpi)),
		uintptr(scaleForDPI(editor.H, w.dpi)),
		w.hwnd, 2001, instance, 0,
	)
	if w.input != 0 {
		procSendMessageW.Call(w.input, wmSetFont, w.font, 1)
		procSendMessageW.Call(w.input, emSetMargins, 3, 0)
		cue, _ := syscall.UTF16PtrFromString("手动添加用户名")
		procSendMessageW.Call(w.input, emSetCueBanner, 1, uintptr(unsafe.Pointer(cue)))
		w.oldInputProc, _, _ = procSetWindowLongPtrW.Call(w.input, gwlpWndProc, miniInputCallback)
		miniInputRegistry.Store(w.input, w)
	}
}

func miniInputWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if value, ok := miniInputRegistry.Load(hwnd); ok {
		win := value.(*miniWindow)
		if message == wmKeyDown && wParam == vkReturn {
			win.addManual()
			return 0
		}
		if win.oldInputProc != 0 {
			result, _, _ := procCallWindowProcW.Call(win.oldInputProc, hwnd, uintptr(message), wParam, lParam)
			return result
		}
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (w *miniWindow) repositionInput() {
	if w.input == 0 {
		return
	}
	rect := w.miniGeometry().input
	style := w.host.design.Style("manual.input")
	editor := editOverlayRect(rect, 10, 10, style.FontSize)
	procSetWindowPos.Call(
		w.input, 0,
		uintptr(scaleForDPI(editor.X, w.dpi)),
		uintptr(scaleForDPI(editor.Y, w.dpi)),
		uintptr(scaleForDPI(editor.W, w.dpi)),
		uintptr(scaleForDPI(editor.H, w.dpi)),
		swpNoZOrder|swpNoActivate,
	)
}

func (w *miniWindow) startSubscription() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.controller.Subscribe == nil {
		return
	}
	states, cancel := w.controller.Subscribe()
	w.cancel = cancel
	go func(win *miniWindow) {
		for state := range states {
			state := state
			_ = win.host.dispatch(func() {
				if win.hwnd == 0 {
					return
				}
				win.state = state
				win.clampScroll()
				win.requestImages()
				procInvalidateRect.Call(win.hwnd, 0, 0)
			})
		}
	}(w)
}

func (w *miniWindow) show() {
	w.visible = true
	procShowWindow.Call(w.hwnd, swRestore)
	procSetForegroundWindow.Call(w.hwnd)
}

func (w *miniWindow) hide() {
	w.visible = false
	procShowWindow.Call(w.hwnd, swHide)
}

func (w *miniWindow) setTopmost(topmost, notify bool) {
	w.topmost = topmost
	insertAfter := hwndNotTopmost
	if topmost {
		insertAfter = hwndTopmost
	}
	procSetWindowPos.Call(w.hwnd, insertAfter, 0, 0, 0, 0, 0x0001|0x0002|swpNoActivate)
	if notify && w.controller.TopmostChanged != nil {
		go w.controller.TopmostChanged(topmost)
	}
	procInvalidateRect.Call(w.hwnd, 0, 0)
}

func (w *miniWindow) windowState() MiniWindowState {
	return MiniWindowState{Active: w.hwnd != 0, Visible: w.visible, Topmost: w.topmost}
}

type miniGeometry struct {
	top, brand, topmost, status      Rect
	card, header, next, pause, clear Rect
	input, add, queue                Rect
}

func (w *miniWindow) miniGeometry() miniGeometry {
	design := w.host.design
	layout := design.Layout
	width, height := 420, 560
	padding := layout.Window.Padding
	top := design.LayoutRect("topbar", Rect{padding, padding, width - 2*padding, layout.TopBar.Height})
	status := design.LayoutRect("topbar.status", Rect{top.X + top.W - layout.TopBar.StatusWidth, top.Y, layout.TopBar.StatusWidth, top.H})
	topmost := design.LayoutRect("topbar.topmost", Rect{status.X - layout.TopBar.ActionGap - layout.TopBar.StatusWidth, status.Y, layout.TopBar.StatusWidth, status.H})
	brand := design.LayoutRect("topbar.brand", Rect{top.X, top.Y, maxInt(20, topmost.X-top.X-layout.TopBar.ActionGap), top.H})
	cardY := top.Y + top.H + layout.TopBar.MarginBottom
	card := design.LayoutRect("card", Rect{padding, cardY, width - 2*padding, height - cardY - padding})
	header := design.LayoutRect("card.header", Rect{card.X, card.Y, card.W, layout.Card.HeaderHeight})
	body := Rect{card.X + layout.Card.BodyPadding, header.Y + header.H + layout.Card.BodyPadding, card.W - 2*layout.Card.BodyPadding, card.H - header.H - 2*layout.Card.BodyPadding}
	toolbar := Rect{body.X, body.Y, body.W, layout.Toolbar.Height}
	next := design.LayoutRect("toolbar.next", Rect{toolbar.X, toolbar.Y, layout.Toolbar.ButtonWidth, toolbar.H})
	clear := design.LayoutRect("toolbar.clear", Rect{toolbar.X + toolbar.W - layout.Toolbar.ButtonWidth, toolbar.Y, layout.Toolbar.ButtonWidth, toolbar.H})
	pause := design.LayoutRect("toolbar.pause", Rect{clear.X - layout.Toolbar.Gap - layout.Toolbar.ButtonWidth, toolbar.Y, layout.Toolbar.ButtonWidth, toolbar.H})
	manualY := toolbar.Y + toolbar.H + layout.Toolbar.MarginBottom
	manualWidth := body.W - layout.Manual.Gap - layout.Manual.ButtonWidth
	input := design.LayoutRect("manual.input", Rect{body.X, manualY, manualWidth, layout.Manual.Height})
	add := design.LayoutRect("manual.add", Rect{body.X + manualWidth + layout.Manual.Gap, manualY, layout.Manual.ButtonWidth, layout.Manual.Height})
	dividerY := manualY + layout.Manual.Height + layout.Manual.PaddingBottom
	queueY := dividerY + layout.Manual.MarginBottom
	queue := design.LayoutRect("queue.viewport", Rect{body.X, queueY, body.W, maxInt(0, card.Y+card.H-layout.Card.BodyPadding-queueY)})
	return miniGeometry{top, brand, topmost, status, card, header, next, pause, clear, input, add, queue}
}

func (w *miniWindow) proc(message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		w.paint()
		return 0
	case wmSize:
		width := int(uint16(lParam))
		height := int(uint16(lParam >> 16))
		if w.renderer != nil && width > 0 && height > 0 {
			w.renderer.resize(width, height)
		}
		return 0
	case wmDPIChanged:
		w.dpi = uint32(wParam & 0xFFFF)
		if w.renderer != nil {
			w.renderer.setDPI(w.dpi)
			w.bitmapGeneration = 0
		}
		if lParam != 0 {
			var rect nativeRect
			procCopyMemory.Call(uintptr(unsafe.Pointer(&rect)), lParam, unsafe.Sizeof(rect))
			procSetWindowPos.Call(
				w.hwnd, 0, uintptr(rect.Left), uintptr(rect.Top),
				uintptr(rect.Right-rect.Left), uintptr(rect.Bottom-rect.Top),
				swpNoZOrder|swpNoActivate,
			)
		}
		w.resizeClient(false)
		w.repositionInput()
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmCtlColorEdit:
		if lParam == w.input {
			procSetTextColor.Call(wParam, uintptr(colorRef(w.host.design.Theme.Colors.TextPrimary)))
			procSetBkMode.Call(wParam, 1)
			brush, _, _ := procGetStockObject.Call(5)
			return brush
		}
	case wmCommand:
		if uint16(wParam) == 2001 && uint16(wParam>>16) == 0x0300 {
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
	case wmMouseWheel:
		delta := int(int16(wParam >> 16))
		step := w.host.design.Layout.Queue.RowHeight + w.host.design.Layout.Queue.RowGap
		if delta > 0 {
			w.scrollY -= step * 3
		} else {
			w.scrollY += step * 3
		}
		w.clampScroll()
		w.showScrollbar()
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmTimer:
		if wParam == miniScrollbarTimerID {
			if w.scrollbarOpacity() <= 0 {
				procKillTimer.Call(w.hwnd, miniScrollbarTimerID)
			}
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
	case wmMouseMove:
		trackMouseLeave(w.hwnd)
		x, y := w.logicalPoint(lParam)
		if w.scrollbarDrag {
			w.scrollScrollbarTo(y)
			w.showScrollbar()
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
		if w.dragging {
			if absInt(y-w.dragStartY) > 3 {
				w.dragMoved = true
			}
			scrollStep := maxInt(1, w.host.design.Layout.Queue.RowHeight/3)
			if y < w.queueViewport.Y+20 {
				w.scrollY -= scrollStep
			} else if y > w.queueViewport.Y+w.queueViewport.H-20 {
				w.scrollY += scrollStep
			}
			w.clampScroll()
			w.showScrollbar()
			w.dragTarget = w.dropIndex(y)
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
		hovered := w.actionAt(x, y)
		if w.pressed != "" && hovered != w.pressed {
			w.pressed = ""
		}
		if hovered != w.hovered {
			w.hovered = hovered
			procInvalidateRect.Call(w.hwnd, 0, 0)
		}
		return 0
	case wmLButtonDown:
		x, y := w.logicalPoint(lParam)
		action := w.actionAt(x, y)
		w.pressed = action
		if action == "scroll.thumb" {
			_, thumb := w.scrollbarRects(w.queueViewport)
			w.scrollbarDrag = true
			w.scrollbarGrabY = y - thumb.Y
			w.showScrollbar()
			procSetCapture.Call(w.hwnd)
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
		if action == "scroll.track" {
			_, thumb := w.scrollbarRects(w.queueViewport)
			if y < thumb.Y {
				w.scrollY -= w.queueViewport.H
			} else if y > thumb.Y+thumb.H {
				w.scrollY += w.queueViewport.H
			}
			w.clampScroll()
			w.showScrollbar()
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
		if strings.HasPrefix(action, "drag:") {
			index, _ := strconv.Atoi(strings.TrimPrefix(action, "drag:"))
			w.dragging = true
			w.dragIndex = index
			w.dragTarget = index
			w.dragStartY = y
			w.dragMoved = false
			procSetCapture.Call(w.hwnd)
		} else if action != "" {
			procSetCapture.Call(w.hwnd)
		}
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		x, y := w.logicalPoint(lParam)
		action := w.actionAt(x, y)
		pressed := w.pressed
		w.pressed = ""
		if w.scrollbarDrag {
			w.scrollbarDrag = false
			procReleaseCapture.Call()
			w.showScrollbar()
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
		if w.dragging {
			procReleaseCapture.Call()
			w.finishDrag()
			return 0
		}
		procReleaseCapture.Call()
		if action != "" && action == pressed {
			w.activate(action)
		}
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmMouseLeave:
		w.hovered = ""
		w.pressed = ""
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmCaptureChanged:
		if !w.dragging && !w.scrollbarDrag {
			w.pressed = ""
			procInvalidateRect.Call(w.hwnd, 0, 0)
		}
		return 0
	case wmSetCursor:
		if w.hovered != "" {
			cursor, _, _ := procLoadCursorW.Call(0, idcHand)
			procSetCursor.Call(cursor)
			return 1
		}
	case wmKeyDown:
		if wParam == vkEscape && w.modal {
			w.modal = false
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
	case wmClose:
		w.hide()
		return 0
	case wmDestroy:
		w.cleanup()
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(w.hwnd, uintptr(message), wParam, lParam)
	return result
}

func (w *miniWindow) logicalPoint(lParam uintptr) (int, int) {
	return unscaleForDPI(int(int16(lParam)), w.dpi), unscaleForDPI(int(int16(lParam>>16)), w.dpi)
}

func (w *miniWindow) actionAt(x, y int) string {
	for index := len(w.elements) - 1; index >= 0; index-- {
		element := w.elements[index]
		if element.rect.Contains(x, y) {
			return element.action
		}
	}
	return ""
}

func (w *miniWindow) activate(action string) {
	if w.modal {
		switch action {
		case "modal.cancel":
			w.modal = false
		case "modal.confirm":
			w.modal = false
			if w.controller.Clear != nil {
				go w.controller.Clear()
			}
		}
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return
	}
	switch {
	case action == "topmost":
		w.setTopmost(!w.topmost, true)
	case action == "next":
		if w.controller.Next != nil && len(w.state.Queue) > 0 {
			go w.controller.Next()
		}
	case action == "pause":
		if w.controller.SetPaused != nil {
			go w.controller.SetPaused(!w.state.Paused)
		}
	case action == "clear":
		w.modal = true
		procInvalidateRect.Call(w.hwnd, 0, 0)
	case action == "manual.add":
		w.addManual()
	case strings.HasPrefix(action, "remove:"):
		if w.controller.Remove != nil {
			id := strings.TrimPrefix(action, "remove:")
			go w.controller.Remove(id)
		}
	}
}

func (w *miniWindow) addManual() {
	username := strings.TrimSpace(readWindowText(w.input))
	if username == "" || w.controller.Add == nil {
		w.showToast("请输入用户名")
		return
	}
	go func() {
		ok, detail := w.controller.Add(username)
		_ = w.host.dispatch(func() {
			if w.hwnd == 0 {
				return
			}
			if ok {
				empty, _ := syscall.UTF16PtrFromString("")
				procSetWindowTextW.Call(w.input, uintptr(unsafe.Pointer(empty)))
			} else {
				w.showToast(detail)
			}
		})
	}()
}

func (w *miniWindow) showToast(message string) {
	w.toast = message
	w.toastUntil = time.Now().Add(2200 * time.Millisecond)
	procInvalidateRect.Call(w.hwnd, 0, 0)
	go func(until time.Time) {
		time.Sleep(time.Until(until))
		_ = w.host.dispatch(func() {
			if w.hwnd != 0 && !time.Now().Before(w.toastUntil) {
				procInvalidateRect.Call(w.hwnd, 0, 0)
			}
		})
	}(w.toastUntil)
}

func (w *miniWindow) dropIndex(y int) int {
	if len(w.state.Queue) == 0 {
		return -1
	}
	layout := w.host.design.Layout.Queue
	step := layout.RowHeight + layout.RowGap
	index := (y - w.queueViewport.Y + w.scrollY + step/2) / maxInt(1, step)
	return clampInt(index, 0, len(w.state.Queue))
}

func (w *miniWindow) finishDrag() {
	from, slot := w.dragIndex, w.dragTarget
	moved := w.dragMoved
	w.dragging = false
	w.dragIndex = -1
	w.dragTarget = -1
	w.dragMoved = false
	if !moved || from < 0 || slot < 0 || from >= len(w.state.Queue) || slot > len(w.state.Queue) || slot == from || slot == from+1 {
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return
	}
	users, changed := reorderMiniUsers(w.state.Queue, from, slot)
	if !changed {
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return
	}
	ids := make([]string, len(users))
	for index := range users {
		ids[index] = users[index].ID
	}
	if w.controller.Reorder != nil {
		go w.controller.Reorder(ids)
	}
	procInvalidateRect.Call(w.hwnd, 0, 0)
}

func reorderMiniUsers(source []MiniUser, from, slot int) ([]MiniUser, bool) {
	if from < 0 || from >= len(source) || slot < 0 || slot > len(source) || slot == from || slot == from+1 {
		return append([]MiniUser(nil), source...), false
	}
	users := append([]MiniUser(nil), source...)
	user := users[from]
	users = append(users[:from], users[from+1:]...)
	if from < slot {
		slot--
	}
	slot = clampInt(slot, 0, len(users))
	users = append(users, MiniUser{})
	copy(users[slot+1:], users[slot:])
	users[slot] = user
	return users, true
}

func (w *miniWindow) maxScroll() int {
	layout := w.host.design.Layout.Queue
	content := len(w.state.Queue)*(layout.RowHeight+layout.RowGap) - layout.RowGap
	return maxInt(0, content-w.queueViewport.H)
}

func (w *miniWindow) clampScroll() {
	w.scrollY = clampInt(w.scrollY, 0, w.maxScroll())
}

func (w *miniWindow) cleanup() {
	procKillTimer.Call(w.hwnd, miniScrollbarTimerID)
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.renderer != nil {
		for _, bitmap := range w.images {
			releaseCOM(bitmap.handle)
			bitmap.handle = 0
		}
		w.renderer.release()
		w.renderer = nil
	}
	if w.font != 0 {
		procDeleteObject.Call(w.font)
		w.font = 0
	}
	if w.input != 0 {
		miniInputRegistry.Delete(w.input)
		w.input = 0
	}
	windowRegistry.Delete(w.hwnd)
	w.hwnd = 0
	w.visible = false
}

func (w *miniWindow) paint() {
	var paint nativePaintStruct
	procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&paint)))
	defer procEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&paint)))
	if w.renderer == nil {
		return
	}
	var client nativeRect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&client)))
	w.renderer.resize(int(client.Right-client.Left), int(client.Bottom-client.Top))
	w.draw()
}

func (w *miniWindow) draw() {
	renderer := w.renderer
	design := w.host.design
	layout := design.Layout
	geometry := w.miniGeometry()
	w.queueViewport = geometry.queue
	w.clampScroll()
	w.elements = nil
	w.syncBitmapGeneration()
	renderer.ensureBackground(design.Theme)
	renderer.begin()
	defer func() { _ = renderer.end() }()

	full := Rect{0, 0, 420, 560}
	material := design.MaterialFor("miniControl")
	if !w.dwmActive {
		base := renderer.solidBrush(material.TintColor, float32(clampInt(material.TintOpacity, 0, 100))/100)
		renderer.fillRounded(full, 0, base)
		releaseCOM(base)
	}
	renderer.drawBitmap(renderer.backgroundBitmap, full, float32(clampInt(material.BackgroundOpacity, 0, 100))/100)
	renderer.drawVisual(full, design.Style("window.background"))
	w.drawBrand(geometry.brand)
	w.drawButton("topbar.topmost", geometry.topmost, map[bool]string{true: "取消置顶", false: "置顶"}[w.topmost], "topmost", false)
	w.drawStatus(geometry.status)
	renderer.drawVisual(geometry.card, design.Style("card"))
	w.drawHeader(geometry.header)
	w.drawButton("toolbar.next", geometry.next, "下一位", "next", len(w.state.Queue) == 0)
	pauseText := "暂停排队"
	if w.state.Paused {
		pauseText = "继续排队"
	}
	w.drawButton("toolbar.pause", geometry.pause, pauseText, "pause", false)
	w.drawButton("toolbar.clear", geometry.clear, "清空队列", "clear", len(w.state.Queue) == 0)
	renderer.drawVisual(geometry.input, design.Style("manual.input"))
	w.drawButton("manual.add", geometry.add, "添加", "manual.add", false)
	w.elements = append(w.elements, hitElement{geometry.add, "manual.add"})
	w.drawQueue(geometry.queue)
	if w.modal {
		w.drawModal()
	}
	if w.toast != "" && time.Now().Before(w.toastUntil) {
		w.drawToast()
	}
	if w.input != 0 {
		procInvalidateRect.Call(w.input, 0, 0)
	}
	_ = layout
}

func (w *miniWindow) drawBrand(rect Rect) {
	design := w.host.design
	title := design.LayoutRect("topbar.brand.title", Rect{rect.X, rect.Y, rect.W, 18})
	subtitle := design.LayoutRect("topbar.brand.subtitle", Rect{rect.X, rect.Y + 18, rect.W, 12})
	titleStyle := design.Style("topbar.brand.title")
	subtitleStyle := design.Style("topbar.brand.subtitle")
	w.renderer.drawText("BiliQueue", title, design.Theme.Typography.Family, titleStyle.FontSize, visualWeight(titleStyle), titleStyle.TextColor, 1, 0, 1)
	w.renderer.drawText("简易控制页 · 队列管理", subtitle, design.Theme.Typography.Family, subtitleStyle.FontSize, visualWeight(subtitleStyle), subtitleStyle.TextColor, 1, 0, 1)
}

func (w *miniWindow) drawStatus(rect Rect) {
	design := w.host.design
	style := design.Style("topbar.status")
	w.renderer.drawVisual(rect, style)
	connected := w.state.Connected
	label := "未连接"
	if connected {
		label = "已连接"
	}
	dot := design.LayoutRect("topbar.status.dot", Rect{rect.X + 8, rect.Y + (rect.H-8)/2, 8, 8})
	dotStyle := design.Style("topbar.status.dot")
	if !connected {
		dotStyle.Fill = BrushSpec{Type: "solid", Color1: design.Theme.Colors.StatusDisconnected, Opacity: 100}
	}
	brush := w.renderer.brushFor(dotStyle.Fill, dot)
	w.renderer.fillEllipse(dot, brush)
	releaseCOM(brush)
	text := design.LayoutRect("topbar.status.text", Rect{rect.X + 20, rect.Y, rect.W - 24, rect.H})
	textStyle := design.Style("topbar.status.text")
	w.renderer.drawText(label, text, design.Theme.Typography.Family, textStyle.FontSize, visualWeight(textStyle), textStyle.TextColor, 1, 0, 1)
}

func (w *miniWindow) drawHeader(rect Rect) {
	design := w.host.design
	title := design.LayoutRect("card.header.title", Rect{rect.X + 11, rect.Y, 76, rect.H})
	count := design.LayoutRect("card.header.count", Rect{rect.X + 88, rect.Y, rect.W - 99, rect.H})
	titleStyle := design.Style("card.header.title")
	countStyle := design.Style("card.header.count")
	w.renderer.drawText("队列管理", title, design.Theme.Typography.Family, titleStyle.FontSize, visualWeight(titleStyle), titleStyle.TextColor, 1, 0, 1)
	w.renderer.drawText(fmt.Sprintf("%d 人", len(w.state.Queue)), count, design.Theme.Typography.Family, countStyle.FontSize, visualWeight(countStyle), countStyle.TextColor, 1, 0, 1)
}

func (w *miniWindow) drawButton(path string, rect Rect, label, action string, disabled bool) {
	state := buttonState(action == w.hovered, action == w.pressed)
	style := buttonVisual(w.host.design.Style(path), state)
	if action == "clear" || action == "modal.confirm" {
		style = dangerButtonVisual(w.host.design.Style(path), state)
	}
	if disabled {
		style.Fill.Opacity = style.Fill.Opacity * 45 / 100
		style.TextColor = w.host.design.Theme.Colors.TextMuted
	}
	drawRect := buttonContentRect(rect, state)
	w.renderer.drawVisual(drawRect, style)
	w.renderer.drawText(label, drawRect, w.host.design.Theme.Typography.Family, style.FontSize, visualWeight(style), style.TextColor, 1, 1, 1)
	if !disabled {
		w.elements = append(w.elements, hitElement{rect, action})
	}
}

func (w *miniWindow) drawQueue(view Rect) {
	design := w.host.design
	layout := design.Layout.Queue
	renderer := w.renderer
	renderer.pushClip(view)
	if len(w.state.Queue) == 0 {
		style := design.Style("queue.viewport")
		renderer.drawText("当前没有排队用户", view, design.Theme.Typography.Family, style.FontSize, visualWeight(style), style.TextColor, 1, 1, 1)
		renderer.popClip()
		return
	}
	step := layout.RowHeight + layout.RowGap
	for index, user := range w.state.Queue {
		y := view.Y + index*step - w.scrollY
		if y+layout.RowHeight < view.Y || y > view.Y+view.H {
			continue
		}
		path := "queue.row"
		if user.GiftBattery > 0 {
			path = "queue.row.gifted"
		}
		rowAction := "drag:" + strconv.Itoa(index)
		row := design.LayoutRect(path, Rect{view.X, y, view.W, layout.RowHeight})
		rowStyle := design.Style(path)
		if w.hovered == rowAction && !w.dragging {
			rowStyle.Fill.Opacity = clampInt(rowStyle.Fill.Opacity+5, 0, 100)
		}
		renderer.drawVisual(row, rowStyle)
		if w.dragging && w.dragTarget == index {
			indicator := design.Style("queue.drag.indicator")
			renderer.drawVisual(design.LayoutRect("queue.drag.indicator", Rect{row.X, row.Y, row.W, 2}), indicator)
		}
		padding, gap := layout.RowPadding, layout.CellGap
		x := row.X + padding
		drag := design.LayoutRect("queue.row.drag", Rect{x, row.Y + padding, layout.DragWidth, row.H - 2*padding})
		x += drag.W + gap
		position := design.LayoutRect("queue.row.position", Rect{x, row.Y + padding, layout.PositionWidth, row.H - 2*padding})
		x += position.W + gap
		avatar := design.LayoutRect("queue.row.avatar", Rect{x, row.Y + (row.H-layout.AvatarSize)/2, layout.AvatarSize, layout.AvatarSize})
		x += avatar.W + gap
		remove := design.LayoutRect("queue.row.remove", Rect{row.X + row.W - padding - layout.RemoveWidth, row.Y + (row.H-24)/2, layout.RemoveWidth, 24})
		giftWidth := 0
		if user.GiftBattery > 0 {
			giftWidth = minInt(layout.GiftMaxWidth, 70)
		}
		gift := Rect{remove.X - gap - giftWidth, row.Y + (row.H-24)/2, giftWidth, 24}
		if giftWidth > 0 {
			gift = design.LayoutRect("queue.row.gift", gift)
		}
		nameRight := remove.X - gap
		if giftWidth > 0 {
			nameRight = gift.X - gap
		}
		name := design.LayoutRect("queue.row.name", Rect{x, row.Y + padding, maxInt(10, nameRight-x), row.H - 2*padding})
		dragStyle := design.Style("queue.row.drag")
		positionStyle := design.Style("queue.row.position")
		nameStyle := design.Style("queue.row.name")
		renderer.drawText("≡", drag, design.Theme.Typography.Family, dragStyle.FontSize, 400, dragStyle.TextColor, 1, 1, 1)
		renderer.drawText(strconv.Itoa(index+1), position, design.Theme.Typography.Family, positionStyle.FontSize, 400, positionStyle.TextColor, 1, 1, 1)
		w.drawAvatar(user, avatar)
		renderer.drawText(user.Username, name, design.Theme.Typography.Family, nameStyle.FontSize, visualWeight(nameStyle), nameStyle.TextColor, 1, 0, 1)
		if giftWidth > 0 {
			giftStyle := design.Style("queue.row.gift")
			renderer.drawVisual(gift, giftStyle)
			renderer.drawText(fmt.Sprintf("%.0f 电池", user.GiftBattery), gift, design.Theme.Typography.Family, giftStyle.FontSize, visualWeight(giftStyle), giftStyle.TextColor, 1, 1, 1)
		}
		removeAction := "remove:" + user.ID
		removeState := buttonState(w.hovered == removeAction, w.pressed == removeAction)
		removeStyle := buttonVisual(design.Style("queue.row.remove"), removeState)
		removeDrawRect := buttonContentRect(remove, removeState)
		renderer.drawVisual(removeDrawRect, removeStyle)
		renderer.drawText("移除", removeDrawRect, design.Theme.Typography.Family, removeStyle.FontSize, visualWeight(removeStyle), removeStyle.TextColor, 1, 1, 1)
		w.elements = append(w.elements,
			hitElement{row, rowAction},
			hitElement{remove, removeAction},
		)
	}
	if w.dragging && w.dragTarget == len(w.state.Queue) {
		y := view.Y + len(w.state.Queue)*step - layout.RowGap - w.scrollY
		indicator := design.Style("queue.drag.indicator")
		renderer.drawVisual(design.LayoutRect("queue.drag.indicator", Rect{view.X, y, view.W, 2}), indicator)
	}
	renderer.popClip()
	w.drawScrollbar(view)
}

func (w *miniWindow) drawAvatar(user MiniUser, rect Rect) {
	design := w.host.design
	style := design.Style("queue.row.avatar")
	if user.Manual {
		brush := w.renderer.solidBrush(design.Theme.Colors.Primary, 1)
		w.renderer.fillEllipse(rect, brush)
		releaseCOM(brush)
		w.renderer.drawText("⭐", rect, "Segoe UI Emoji", maxInt(13, style.FontSize), 400, "#FFFFFF", 1, 1, 1)
	} else if bitmap := w.bitmap(user.Avatar); bitmap != 0 {
		w.renderer.drawBitmap(bitmap, rect, 1)
	} else {
		brush := w.renderer.brushFor(style.Fill, rect)
		w.renderer.fillEllipse(rect, brush)
		releaseCOM(brush)
		initial := "?"
		if runes := []rune(user.Username); len(runes) > 0 {
			initial = string(runes[0])
		}
		w.renderer.drawText(initial, rect, design.Theme.Typography.Family, style.FontSize, 600, "#FFFFFF", 1, 1, 1)
	}
	if user.GuardLevel > 0 {
		size := maxInt(10, rect.W/2)
		badge := design.LayoutRect("queue.row.avatar.badge", Rect{rect.X - 3, rect.Y - 3, size, size})
		key := fmt.Sprintf("guard:%d", user.GuardLevel)
		if bitmap := w.bitmap(key); bitmap != 0 {
			w.renderer.drawBitmap(bitmap, badge, 1)
		} else {
			badgeStyle := design.Style("queue.row.avatar.badge")
			brush := w.renderer.brushFor(badgeStyle.Fill, badge)
			w.renderer.fillEllipse(badge, brush)
			releaseCOM(brush)
		}
	}
}

func (w *miniWindow) drawScrollbar(view Rect) {
	opacity := w.scrollbarOpacity()
	if opacity <= 0 {
		return
	}
	track, thumb := w.scrollbarRects(view)
	w.renderer.drawVisual(track, fadedVisual(w.host.design.Style("queue.scrollbar.track"), opacity))
	w.renderer.drawVisual(thumb, fadedVisual(w.host.design.Style("queue.scrollbar.thumb"), opacity))
	w.elements = append(w.elements,
		hitElement{rect: track, action: "scroll.track"},
		hitElement{rect: thumb, action: "scroll.thumb"},
	)
}

func (w *miniWindow) showScrollbar() {
	layout := w.host.design.Layout.Queue
	if !layout.ScrollbarAutoHide || w.maxScroll() <= 0 {
		return
	}
	delay := maxInt(1, layout.ScrollbarShowDelayMS)
	w.scrollbarUntil = time.Now().Add(time.Duration(delay) * time.Millisecond)
	procSetTimer.Call(w.hwnd, miniScrollbarTimerID, 33, 0)
}

func (w *miniWindow) scrollbarOpacity() float32 {
	if w.maxScroll() <= 0 {
		return 0
	}
	layout := w.host.design.Layout.Queue
	if !layout.ScrollbarAutoHide || w.scrollbarDrag {
		return 1
	}
	now := time.Now()
	if !now.After(w.scrollbarUntil) {
		return 1
	}
	fade := time.Duration(maxInt(1, layout.ScrollbarFadeDuration)) * time.Millisecond
	elapsed := now.Sub(w.scrollbarUntil)
	if elapsed >= fade {
		return 0
	}
	return 1 - float32(elapsed)/float32(fade)
}

func (w *miniWindow) scrollbarRects(view Rect) (Rect, Rect) {
	layout := w.host.design.Layout.Queue
	width := maxInt(3, layout.ScrollbarWidth)
	inset := maxInt(0, layout.ScrollbarInset)
	base := Rect{view.X + view.W + inset, view.Y, width, view.H}
	base = w.host.design.LayoutRect("queue.scrollbar", base)
	track := w.host.design.LayoutRect("queue.scrollbar.track", base)
	contentHeight := len(w.state.Queue)*(layout.RowHeight+layout.RowGap) - layout.RowGap
	thumbHeight, thumbOffset, _ := scrollbarThumbGeometry(
		track.H,
		view.H,
		contentHeight,
		layout.ScrollbarMinThumb,
		w.scrollY,
	)
	thumb := w.host.design.LayoutRect(
		"queue.scrollbar.thumb",
		Rect{track.X, track.Y + thumbOffset, track.W, thumbHeight},
	)
	return track, thumb
}

func (w *miniWindow) scrollScrollbarTo(y int) {
	track, thumb := w.scrollbarRects(w.queueViewport)
	_, _, maxScroll := scrollbarThumbGeometry(
		track.H,
		w.queueViewport.H,
		w.queueViewport.H+w.maxScroll(),
		w.host.design.Layout.Queue.ScrollbarMinThumb,
		w.scrollY,
	)
	travel := maxInt(1, track.H-thumb.H)
	offset := clampInt(y-w.scrollbarGrabY-track.Y, 0, travel)
	w.scrollY = clampInt(offset*maxScroll/travel, 0, maxScroll)
}

func (w *miniWindow) drawModal() {
	renderer := w.renderer
	design := w.host.design
	card := design.LayoutRect("modal.card", Rect{40, 185, 340, 190})
	renderer.drawVisual(card, design.Style("modal.card"))
	footerY := card.Y + card.H - 62
	body := design.LayoutRect("modal.body", Rect{card.X, card.Y, card.W, footerY - card.Y + 14})
	footer := design.LayoutRect("modal.footer", Rect{card.X, footerY - 14, card.W, card.Y + card.H - (footerY - 14)})
	renderer.drawVisual(body, design.Style("modal.body"))
	renderer.drawVisual(footer, design.Style("modal.footer"))
	renderer.drawVisual(design.LayoutRect("modal.divider", Rect{body.X, footer.Y, body.W, 1}), design.Style("modal.divider"))
	titleStyle := design.Style("modal.title")
	messageStyle := design.Style("modal.message")
	renderer.drawText("清空队列", design.LayoutRect("modal.title", Rect{card.X + 20, card.Y + 18, card.W - 40, 28}), design.Theme.Typography.Family, titleStyle.FontSize, visualWeight(titleStyle), titleStyle.TextColor, 1, 0, 1)
	drawWrapped(renderer, "确定清空当前队列吗？此操作无法撤销。", design.LayoutRect("modal.message", Rect{card.X + 20, card.Y + 50, card.W - 40, 54}), design.Theme.Typography.Family, messageStyle, 0)
	cancel := design.LayoutRect("modal.cancel", Rect{card.X + 18, footerY + 13, (card.W - 48) / 2, 34})
	confirm := design.LayoutRect("modal.confirm", Rect{cancel.X + cancel.W + 12, cancel.Y, cancel.W, cancel.H})
	w.drawButton("modal.cancel", cancel, "取消", "modal.cancel", false)
	w.drawButton("modal.confirm", confirm, "清空队列", "modal.confirm", false)
}

func (w *miniWindow) drawToast() {
	design := w.host.design
	rect := design.LayoutRect("toast", Rect{60, 505, 300, 36})
	w.renderer.drawVisual(rect, design.Style("toast"))
	textRect := design.LayoutRect("toast.text", Rect{rect.X + 10, rect.Y, maxInt(1, rect.W-20), rect.H})
	style := design.Style("toast.text")
	w.renderer.drawText(w.toast, textRect, design.Theme.Typography.Family, style.FontSize, visualWeight(style), style.TextColor, 1, 1, 1)
}

func (w *miniWindow) syncBitmapGeneration() {
	if w.bitmapGeneration == w.renderer.generation {
		return
	}
	w.bitmapGeneration = w.renderer.generation
	for _, bitmap := range w.images {
		bitmap.handle = 0
	}
}

func (w *miniWindow) bitmap(key string) uintptr {
	if key == "" {
		return 0
	}
	w.imageMu.Lock()
	defer w.imageMu.Unlock()
	bitmap := w.images[key]
	if bitmap == nil {
		return 0
	}
	if bitmap.handle == 0 && len(bitmap.pixels) > 0 {
		bitmap.handle = w.renderer.createBitmap(bitmap.pixels, bitmap.width, bitmap.height)
	}
	return bitmap.handle
}

func (w *miniWindow) requestImages() {
	if w.controller.LoadImage != nil {
		for _, user := range w.state.Queue {
			if !user.Manual && strings.TrimSpace(user.Avatar) != "" {
				w.loadImage(user.Avatar, func() (image.Image, error) {
					return w.controller.LoadImage(user.Avatar)
				})
			}
		}
	}
}

func (w *miniWindow) loadGuardIcons() {
	if w.controller.GuardIcon == nil {
		return
	}
	for level := 1; level <= 3; level++ {
		level := level
		key := fmt.Sprintf("guard:%d", level)
		w.loadImage(key, func() (image.Image, error) {
			return w.controller.GuardIcon(level)
		})
	}
}

func (w *miniWindow) loadImage(key string, load func() (image.Image, error)) {
	w.imageMu.Lock()
	if w.images[key] != nil || w.loading[key] {
		w.imageMu.Unlock()
		return
	}
	w.loading[key] = true
	w.imageMu.Unlock()
	go func() {
		source, err := load()
		var bitmap *miniBitmap
		if err == nil && source != nil {
			size := 64
			pixels := imageToCirclePixels(source, size, strings.HasPrefix(key, "guard:"))
			bitmap = &miniBitmap{pixels: pixels, width: size, height: size}
		}
		w.imageMu.Lock()
		delete(w.loading, key)
		if bitmap != nil {
			w.images[key] = bitmap
		}
		w.imageMu.Unlock()
		if bitmap != nil {
			_ = w.host.dispatch(func() {
				if w.hwnd != 0 {
					procInvalidateRect.Call(w.hwnd, 0, 0)
				}
			})
		}
	}()
}

func imageToCirclePixels(source image.Image, size int, keepSquare bool) []uint32 {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || size <= 0 {
		return nil
	}
	pixels := make([]uint32, size*size)
	scale := math.Max(float64(size)/float64(width), float64(size)/float64(height))
	sourceWidth := float64(size) / scale
	sourceHeight := float64(size) / scale
	startX := float64(bounds.Min.X) + (float64(width)-sourceWidth)/2
	startY := float64(bounds.Min.Y) + (float64(height)-sourceHeight)/2
	radius := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !keepSquare {
				dx := float64(x) + .5 - radius
				dy := float64(y) + .5 - radius
				if dx*dx+dy*dy > radius*radius {
					continue
				}
			}
			sourceX := int(startX + (float64(x)+.5)*sourceWidth/float64(size))
			sourceY := int(startY + (float64(y)+.5)*sourceHeight/float64(size))
			sourceX = clampInt(sourceX, bounds.Min.X, bounds.Max.X-1)
			sourceY = clampInt(sourceY, bounds.Min.Y, bounds.Max.Y-1)
			r, g, b, a := source.At(sourceX, sourceY).RGBA()
			pixels[y*size+x] = uint32(b>>8) | uint32(g>>8)<<8 | uint32(r>>8)<<16 | uint32(a>>8)<<24
		}
	}
	return pixels
}
