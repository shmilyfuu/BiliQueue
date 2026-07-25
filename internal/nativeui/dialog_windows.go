//go:build windows

package nativeui

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	emSetSel       = 0x00B1
	emSetMargins   = 0x00D3
	emSetCueBanner = 0x1501
	wmSetFont      = 0x0030
	defaultGUIFont = 17
)

type hitElement struct {
	rect   Rect
	action string
}

type window struct {
	host          *Host
	hwnd          uintptr
	target        string
	kind          DialogKind
	request       DialogRequest
	logicalWidth  int
	logicalHeight int
	padding       int
	dpi           uint32
	renderer      *direct2DRenderer
	dwmActive     bool
	elements      []hitElement
	hovered       string
	pressed       string
	result        chan DialogResult
	progress      bool
	hidden        bool
	hostEdit      uintptr
	portEdit      uintptr
	font          uintptr
	closed        sync.Once
}

type progressHandle struct {
	host *Host
	win  *window
}

func (h *Host) ShowDialog(request DialogRequest) (DialogResult, error) {
	if h == nil {
		return DialogResult{}, ErrUnavailable
	}
	h.dialogMu.Lock()
	defer h.dialogMu.Unlock()
	result := make(chan struct {
		win *window
		err error
	}, 1)
	if err := h.dispatch(func() {
		win, err := h.createDialog(request, false)
		result <- struct {
			win *window
			err error
		}{win, err}
	}); err != nil {
		return DialogResult{}, err
	}
	created := <-result
	if created.err != nil {
		return DialogResult{}, created.err
	}
	return <-created.win.result, nil
}

func (h *Host) OpenProgress(request DialogRequest) (ProgressHandle, error) {
	if h == nil {
		return nil, ErrUnavailable
	}
	request.Kind = DialogProgress
	result := make(chan struct {
		win *window
		err error
	}, 1)
	if err := h.dispatch(func() {
		win, err := h.createDialog(request, true)
		result <- struct {
			win *window
			err error
		}{win, err}
	}); err != nil {
		return nil, err
	}
	created := <-result
	if created.err != nil {
		return nil, created.err
	}
	return &progressHandle{host: h, win: created.win}, nil
}

func (p *progressHandle) Update(stage string, percent int) {
	if p == nil || p.host == nil || p.win == nil {
		return
	}
	_ = p.host.dispatch(func() {
		if p.win.hwnd == 0 {
			return
		}
		p.win.request.Stage = stage
		p.win.request.Progress = clampInt(percent, 0, 100)
		procInvalidateRect.Call(p.win.hwnd, 0, 0)
	})
}

func (p *progressHandle) Close() {
	if p == nil || p.host == nil || p.win == nil {
		return
	}
	_ = p.host.dispatch(func() {
		if p.win.hwnd != 0 {
			procDestroyWindow.Call(p.win.hwnd)
		}
	})
}

func (h *Host) createDialog(request DialogRequest, progress bool) (*window, error) {
	target := request.Target
	if target == "" {
		target = defaultTargetForKind(request.Kind)
	}
	request = applyTargetDefaults(target, request)
	fallback := TargetLayout{Width: 400, Height: 230, Padding: 18}
	layout := h.design.TargetLayout(target, fallback)
	if layout.Width < 320 {
		layout.Width = fallback.Width
	}
	if layout.Height < 180 {
		layout.Height = fallback.Height
	}
	win := &window{
		host:          h,
		target:        target,
		kind:          request.Kind,
		request:       request,
		logicalWidth:  layout.Width,
		logicalHeight: layout.Height,
		padding:       maxInt(12, layout.Padding),
		result:        make(chan DialogResult, 1),
		progress:      progress,
		dpi:           96,
	}
	if win.request.Title == "" {
		win.request.Title = "BiliQueue"
	}
	if win.request.ConfirmText == "" {
		win.request.ConfirmText = "确认"
	}
	if win.request.CancelText == "" {
		win.request.CancelText = "取消"
	}

	className, _ := syscall.UTF16PtrFromString(nativeClassName)
	title, _ := syscall.UTF16PtrFromString(win.request.Title)
	instance, _, _ := procGetModuleHandleW.Call(0)
	screenWidth, _, _ := procGetSystemMetrics.Call(0)
	screenHeight, _, _ := procGetSystemMetrics.Call(1)
	x := maxInt(0, (int(screenWidth)-layout.Width)/2)
	y := maxInt(0, (int(screenHeight)-layout.Height)/2)
	hwnd, _, callErr := procCreateWindowExW.Call(
		wsExAppWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		uintptr(x), uintptr(y), uintptr(layout.Width), uintptr(layout.Height),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("create native dialog: %w", callErr)
	}
	win.hwnd = hwnd
	windowRegistry.Store(hwnd, win)
	applyNativeWindowIcons(hwnd)
	win.dpi = windowDPI(hwnd)
	width := scaleForDPI(win.logicalWidth, win.dpi)
	height := scaleForDPI(win.logicalHeight, win.dpi)
	x = maxInt(0, (int(screenWidth)-width)/2)
	y = maxInt(0, (int(screenHeight)-height)/2)
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpNoActivate)

	renderer, err := newDirect2DRenderer(h.renderer, hwnd, width, height, win.dpi)
	if err != nil {
		windowRegistry.Delete(hwnd)
		procDestroyWindow.Call(hwnd)
		return nil, err
	}
	win.renderer = renderer
	material := h.design.MaterialFor(target)
	win.dwmActive = applyDWM(hwnd, material, true)
	fieldStyle := win.style("field.host")
	win.font = createUIFont(h.design.Theme.Typography.Family, maxInt(12, fieldStyle.FontSize), win.dpi)
	if win.kind == DialogPort {
		win.createPortEditors()
	}
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	return win, nil
}

func defaultTargetForKind(kind DialogKind) string {
	switch kind {
	case DialogError:
		return "genericError"
	case DialogConfirm:
		return "genericConfirm"
	case DialogDanger:
		return "clearQueue"
	case DialogPort:
		return "portChange"
	case DialogProgress:
		return "updateProgress"
	default:
		return "genericInfo"
	}
}

func (w *window) style(role string) ElementVisual {
	return w.host.design.Style("targets." + w.target + "." + role)
}

type dialogGeometry struct {
	panel, body, footer, divider, title, message Rect
	hostField, portField                         Rect
	progressTrack, progressText                  Rect
	buttons                                      []hitElement
}

func (w *window) geometry() dialogGeometry {
	width, height := w.logicalWidth, w.logicalHeight
	prefix := "targets." + w.target + "."
	footerHeight := clampInt(68, 54, maxInt(54, height/2))
	footerY := height - footerHeight
	bodyStyle := w.style("body")
	footerStyle := w.style("footer")
	bodyOverlap := maxInt(0, bodyStyle.Radius)
	footerOverlap := maxInt(0, footerStyle.Radius)
	geometry := dialogGeometry{
		body:    w.host.design.LayoutRect(prefix+"body", Rect{0, 0, width, minInt(height, footerY+bodyOverlap)}),
		footer:  w.host.design.LayoutRect(prefix+"footer", Rect{0, maxInt(0, footerY-footerOverlap), width, height - maxInt(0, footerY-footerOverlap)}),
		divider: w.host.design.LayoutRect(prefix+"divider", Rect{0, maxInt(0, footerY-footerOverlap), width, 1}),
	}
	geometry.panel = Rect{w.padding, w.padding, width - 2*w.padding, height - 2*w.padding}
	icon := Rect{geometry.panel.X + 22, geometry.panel.Y + 24, 34, 34}
	geometry.title = w.host.design.LayoutRect(prefix+"title", Rect{icon.X + icon.W + 14, geometry.panel.Y + 20, geometry.panel.W - 92, 32})
	messageY := geometry.panel.Y + 72
	messageHeight := minInt(54, maxInt(32, footerY-messageY-18))
	geometry.message = w.host.design.LayoutRect(prefix+"message", Rect{geometry.panel.X + 24, messageY, geometry.panel.W - 48, messageHeight})
	if w.kind == DialogPort {
		fieldY := messageY + messageHeight + 8
		geometry.hostField = w.host.design.LayoutRect(prefix+"field.host", Rect{geometry.panel.X + 24, fieldY, geometry.panel.W - 190, 38})
		geometry.portField = w.host.design.LayoutRect(prefix+"field.port", Rect{geometry.hostField.X + geometry.hostField.W + 10, fieldY, 132, 38})
	}
	if w.kind == DialogProgress {
		geometry.progressTrack = w.host.design.LayoutRect(prefix+"progress.track", Rect{geometry.panel.X + 24, messageY + messageHeight + 12, geometry.panel.W - 48, 12})
		geometry.progressText = w.host.design.LayoutRect(prefix+"progress.text", Rect{geometry.progressTrack.X, geometry.progressTrack.Y + 18, geometry.progressTrack.W, 24})
	}

	buttonHeight, buttonWidth, gap := 34, 128, 10
	buttonY := geometry.panel.Y + geometry.panel.H - 54
	type button struct{ role, action string }
	var buttons []button
	switch w.kind {
	case DialogConfirm, DialogDanger, DialogPort:
		buttons = []button{{"button.secondary", "cancel"}, {"button.primary", "confirm"}}
	case DialogProgress:
		buttons = []button{{"button.secondary", "hide"}}
	default:
		buttons = []button{{"button.primary", "confirm"}}
	}
	if len(buttons) == 1 {
		buttonWidth = maxInt(128, geometry.panel.W-48)
	}
	total := len(buttons)*buttonWidth + maxInt(0, len(buttons)-1)*gap
	x := geometry.panel.X + geometry.panel.W - 24 - total
	for _, button := range buttons {
		path := prefix + button.role
		geometry.buttons = append(geometry.buttons, hitElement{
			rect:   w.host.design.LayoutRect(path, Rect{x, buttonY, buttonWidth, buttonHeight}),
			action: button.action,
		})
		x += buttonWidth + gap
	}
	return geometry
}

func (w *window) createPortEditors() {
	className, _ := syscall.UTF16PtrFromString("EDIT")
	instance, _, _ := procGetModuleHandleW.Call(0)
	geometry := w.geometry()
	create := func(value string, rect Rect, role string, id uintptr) uintptr {
		text, _ := syscall.UTF16PtrFromString(value)
		style := w.style(role)
		editor := editOverlayRect(rect, 52, 8, style.FontSize)
		x := scaleForDPI(editor.X, w.dpi)
		y := scaleForDPI(editor.Y, w.dpi)
		width := scaleForDPI(editor.W, w.dpi)
		height := scaleForDPI(editor.H, w.dpi)
		hwnd, _, _ := procCreateWindowExW.Call(
			wsExTransparent, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(text)),
			wsChild|wsVisible|wsTabStop|esAutoHScroll,
			uintptr(x), uintptr(y), uintptr(width), uintptr(height),
			w.hwnd, id, instance, 0,
		)
		if hwnd != 0 {
			procSendMessageW.Call(hwnd, wmSetFont, w.font, 1)
			procSendMessageW.Call(hwnd, emSetMargins, 3, 0)
		}
		return hwnd
	}
	w.hostEdit = create(w.request.Host, geometry.hostField, "field.host", 1001)
	w.portEdit = create(w.request.Port, geometry.portField, "field.port", 1002)
	if w.portEdit != 0 {
		procSetFocus.Call(w.portEdit)
		procSendMessageW.Call(w.portEdit, emSetSel, 0, ^uintptr(0))
	}
}

func (w *window) repositionEditors() {
	if w.kind != DialogPort {
		return
	}
	geometry := w.geometry()
	position := func(hwnd uintptr, rect Rect, role string) {
		if hwnd == 0 {
			return
		}
		style := w.style(role)
		editor := editOverlayRect(rect, 52, 8, style.FontSize)
		procSetWindowPos.Call(
			hwnd, 0,
			uintptr(scaleForDPI(editor.X, w.dpi)),
			uintptr(scaleForDPI(editor.Y, w.dpi)),
			uintptr(scaleForDPI(editor.W, w.dpi)),
			uintptr(scaleForDPI(editor.H, w.dpi)),
			swpNoZOrder|swpNoActivate,
		)
	}
	position(w.hostEdit, geometry.hostField, "field.host")
	position(w.portEdit, geometry.portField, "field.port")
}

func (w *window) proc(message uint32, wParam, lParam uintptr) uintptr {
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
		}
		if lParam != 0 {
			var rect nativeRect
			procCopyMemory.Call(uintptr(unsafe.Pointer(&rect)), lParam, unsafe.Sizeof(rect))
			procSetWindowPos.Call(
				w.hwnd, 0,
				uintptr(rect.Left), uintptr(rect.Top),
				uintptr(rect.Right-rect.Left), uintptr(rect.Bottom-rect.Top),
				swpNoZOrder|swpNoActivate,
			)
		}
		w.repositionEditors()
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmCtlColorEdit:
		procSetTextColor.Call(wParam, uintptr(colorRef(w.host.design.Theme.Colors.TextPrimary)))
		procSetBkMode.Call(wParam, 1)
		brush, _, _ := procGetStockObject.Call(5)
		return brush
	case wmCommand:
		id := uint16(wParam)
		code := uint16(wParam >> 16)
		if (id == 1001 || id == 1002) && code == 0x0300 {
			procInvalidateRect.Call(w.hwnd, 0, 0)
			return 0
		}
	case wmMouseMove:
		trackMouseLeave(w.hwnd)
		x, y := w.logicalPoint(lParam)
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
		w.pressed = w.actionAt(x, y)
		if w.pressed != "" {
			procSetCapture.Call(w.hwnd)
			procInvalidateRect.Call(w.hwnd, 0, 0)
		}
		return 0
	case wmLButtonUp:
		x, y := w.logicalPoint(lParam)
		action := w.actionAt(x, y)
		pressed := w.pressed
		w.pressed = ""
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
		w.pressed = ""
		procInvalidateRect.Call(w.hwnd, 0, 0)
		return 0
	case wmSetCursor:
		if w.hovered != "" {
			cursor, _, _ := procLoadCursorW.Call(0, idcHand)
			procSetCursor.Call(cursor)
			return 1
		}
	case wmKeyDown:
		switch wParam {
		case vkEscape:
			if w.progress {
				w.hide()
			} else {
				w.resolve(false)
			}
			return 0
		case vkReturn:
			if !w.progress {
				w.resolve(true)
				return 0
			}
		}
	case wmNCHitTest:
		point := nativePoint{X: int32(int16(lParam)), Y: int32(int16(lParam >> 16))}
		procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&point)))
		x := unscaleForDPI(int(point.X), w.dpi)
		y := unscaleForDPI(int(point.Y), w.dpi)
		if w.actionAt(x, y) != "" || w.fieldAt(x, y) {
			return htClient
		}
		return htCaption
	case wmClose:
		if w.progress {
			w.hide()
		} else {
			w.resolve(false)
		}
		return 0
	case wmDestroy:
		w.cleanup()
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(w.hwnd, uintptr(message), wParam, lParam)
	return result
}

func (w *window) logicalPoint(lParam uintptr) (int, int) {
	x := int(int16(lParam))
	y := int(int16(lParam >> 16))
	return unscaleForDPI(x, w.dpi), unscaleForDPI(y, w.dpi)
}

func (w *window) actionAt(x, y int) string {
	for _, element := range w.elements {
		if element.rect.Contains(x, y) {
			return element.action
		}
	}
	return ""
}

func (w *window) fieldAt(x, y int) bool {
	if w.kind != DialogPort {
		return false
	}
	geometry := w.geometry()
	return geometry.hostField.Contains(x, y) || geometry.portField.Contains(x, y)
}

func (w *window) activate(action string) {
	switch action {
	case "confirm":
		w.resolve(true)
	case "cancel":
		w.resolve(false)
	case "hide":
		w.hide()
	}
}

func (w *window) hide() {
	w.hidden = true
	procShowWindow.Call(w.hwnd, swHide)
}

func (w *window) resolve(accepted bool) {
	result := DialogResult{Accepted: accepted}
	if accepted && w.kind == DialogPort {
		result.Host = readWindowText(w.hostEdit)
		result.Port = readWindowText(w.portEdit)
	}
	w.closed.Do(func() {
		w.result <- result
		close(w.result)
	})
	procDestroyWindow.Call(w.hwnd)
}

func (w *window) cleanup() {
	if w.renderer != nil {
		w.renderer.release()
		w.renderer = nil
	}
	if w.font != 0 {
		procDeleteObject.Call(w.font)
		w.font = 0
	}
	windowRegistry.Delete(w.hwnd)
	w.hwnd = 0
	if !w.progress {
		w.closed.Do(func() {
			w.result <- DialogResult{}
			close(w.result)
		})
	}
}

func readWindowText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	length, _, _ := procGetWindowTextLengthW.Call(hwnd)
	buffer := make([]uint16, int(length)+1)
	if len(buffer) == 0 {
		return ""
	}
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func (w *window) paint() {
	var paint nativePaintStruct
	procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&paint)))
	defer procEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&paint)))
	if w.renderer == nil {
		return
	}
	var client nativeRect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&client)))
	pixelWidth := int(client.Right - client.Left)
	pixelHeight := int(client.Bottom - client.Top)
	if pixelWidth <= 0 || pixelHeight <= 0 {
		return
	}
	w.renderer.resize(pixelWidth, pixelHeight)
	w.draw()
}

func (w *window) draw() {
	renderer := w.renderer
	design := w.host.design
	geometry := w.geometry()
	w.elements = nil
	renderer.ensureBackground(design.Theme)
	renderer.begin()
	defer func() { _ = renderer.end() }()

	full := Rect{0, 0, w.logicalWidth, w.logicalHeight}
	material := design.MaterialFor(w.target)
	if !w.dwmActive {
		base := renderer.solidBrush(material.TintColor, float32(clampInt(material.TintOpacity, 0, 100))/100)
		renderer.fillRounded(full, 0, base)
		releaseCOM(base)
	}
	renderer.drawBitmap(renderer.backgroundBitmap, full, float32(clampInt(material.BackgroundOpacity, 0, 100))/100)
	if material.NoiseOpacity > 0 && renderer.noiseBitmap != 0 {
		renderer.fillRoundedBitmap(full, 0, renderer.noiseBitmap, float32(material.NoiseOpacity)/20)
	}
	renderer.drawVisual(full, w.style("window.background"))
	renderer.drawVisual(geometry.body, w.style("body"))
	renderer.drawVisual(geometry.footer, w.style("footer"))
	renderer.drawVisual(geometry.divider, w.style("divider"))

	titleStyle := w.style("title")
	renderer.drawText(w.request.Title, geometry.title, design.Theme.Typography.Family, titleStyle.FontSize, visualWeight(titleStyle), titleStyle.TextColor, 1, 0, 1)
	messageStyle := w.style("message")
	drawWrapped(renderer, w.request.Message, geometry.message, design.Theme.Typography.Family, messageStyle, 0)

	if w.kind == DialogPort {
		w.drawField("field.host", geometry.hostField, "地址")
		w.drawField("field.port", geometry.portField, "端口")
	}
	if w.kind == DialogProgress {
		trackStyle := w.style("progress.track")
		fillStyle := w.style("progress.fill")
		renderer.drawVisual(geometry.progressTrack, trackStyle)
		fill := geometry.progressTrack
		fill.W = fill.W * clampInt(w.request.Progress, 0, 100) / 100
		renderer.drawVisual(fill, fillStyle)
		progressStyle := w.style("progress.text")
		text := fmt.Sprintf("%d%%", clampInt(w.request.Progress, 0, 100))
		if strings.TrimSpace(w.request.Stage) != "" {
			text += " · " + w.request.Stage
		}
		renderer.drawText(text, geometry.progressText, design.Theme.Typography.Family, progressStyle.FontSize, visualWeight(progressStyle), progressStyle.TextColor, 1, 0, 1)
	}

	for _, element := range geometry.buttons {
		role := "button.primary"
		label := w.request.ConfirmText
		if element.action == "cancel" || element.action == "hide" {
			role = "button.secondary"
			label = w.request.CancelText
			if element.action == "hide" {
				label = w.request.ConfirmText
			}
		}
		state := buttonState(element.action == w.hovered, element.action == w.pressed)
		style := buttonVisual(w.style(role), state)
		if w.kind == DialogDanger && element.action == "confirm" {
			style = dangerButtonVisual(w.style(role), state)
		}
		drawRect := buttonContentRect(element.rect, state)
		renderer.drawVisual(drawRect, style)
		renderer.drawText(label, drawRect, design.Theme.Typography.Family, style.FontSize, visualWeight(style), style.TextColor, 1, 1, 1)
		w.elements = append(w.elements, element)
	}
	if w.hostEdit != 0 {
		procInvalidateRect.Call(w.hostEdit, 0, 0)
	}
	if w.portEdit != 0 {
		procInvalidateRect.Call(w.portEdit, 0, 0)
	}
}

func (w *window) drawField(role string, rect Rect, label string) {
	style := w.style(role)
	w.renderer.drawVisual(rect, style)
	labelRect := Rect{rect.X + 12, rect.Y, 42, rect.H}
	w.renderer.drawText(label, labelRect, w.host.design.Theme.Typography.Family, style.FontSize, 400, w.host.design.Theme.Colors.TextSecondary, 1, 0, 1)
}

func visualWeight(style ElementVisual) int {
	if style.FontWeight > 0 {
		return style.FontWeight
	}
	return 400
}

func drawWrapped(renderer *direct2DRenderer, text string, rect Rect, family string, style ElementVisual, hAlign int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	fontSize := maxInt(10, style.FontSize)
	maxRunes := maxInt(4, rect.W*2/maxInt(1, fontSize))
	lines := wrapText(text, maxRunes)
	lineHeight := minInt(maxInt(fontSize, fontSize+7), maxInt(fontSize, rect.H))
	for index, line := range lines {
		y := rect.Y + index*lineHeight
		if y >= rect.Y+rect.H {
			break
		}
		height := minInt(lineHeight, rect.Y+rect.H-y)
		renderer.drawText(line, Rect{rect.X, y, rect.W, height}, family, fontSize, visualWeight(style), style.TextColor, 1, hAlign, 0)
	}
}

func wrapText(text string, maxRunes int) []string {
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		runes := []rune(paragraph)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > maxRunes {
			lines = append(lines, string(runes[:maxRunes]))
			runes = runes[maxRunes:]
		}
		lines = append(lines, string(runes))
	}
	return lines
}
