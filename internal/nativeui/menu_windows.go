//go:build windows

package nativeui

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	wmActivate    = 0x0006
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	vkUp          = 0x26
	vkDown        = 0x28
)

type menuEntry struct {
	item MenuItem
	rect Rect
}

type menuWindow struct {
	host     *Host
	hwnd     uintptr
	items    []MenuItem
	entries  []menuEntry
	width    int
	height   int
	dpi      uint32
	renderer *direct2DRenderer
	hover    int
	selected int
	result   chan int
	resolved bool
}

func (h *Host) ShowMenu(items []MenuItem, screenX, screenY int) (int, error) {
	if h == nil {
		return 0, ErrUnavailable
	}
	result := make(chan struct {
		menu *menuWindow
		err  error
	}, 1)
	if err := h.dispatch(func() {
		menu, err := h.createMenu(items, screenX, screenY)
		result <- struct {
			menu *menuWindow
			err  error
		}{menu, err}
	}); err != nil {
		return 0, err
	}
	created := <-result
	if created.err != nil {
		return 0, created.err
	}
	return <-created.menu.result, nil
}

func (h *Host) createMenu(items []MenuItem, screenX, screenY int) (*menuWindow, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("native menu has no items")
	}
	menu := &menuWindow{
		host:     h,
		items:    append([]MenuItem(nil), items...),
		width:    232,
		dpi:      96,
		hover:    -1,
		selected: -1,
		result:   make(chan int, 1),
	}
	menu.height = 16
	for _, item := range items {
		if item.Separator {
			menu.height += 9
		} else {
			menu.height += 36
		}
	}
	className, _ := syscall.UTF16PtrFromString(nativeClassName)
	title, _ := syscall.UTF16PtrFromString("BiliQueue")
	instance, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, callErr := procCreateWindowExW.Call(
		wsExToolWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		uintptr(screenX), uintptr(screenY), uintptr(menu.width), uintptr(menu.height),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("create native tray menu: %w", callErr)
	}
	menu.hwnd = hwnd
	windowRegistry.Store(hwnd, menu)
	menu.dpi = windowDPI(hwnd)
	width := scaleForDPI(menu.width, menu.dpi)
	height := scaleForDPI(menu.height, menu.dpi)
	work := workAreaForPoint(screenX, screenY)
	if screenX+width > int(work.Right) {
		screenX = maxInt(int(work.Left), int(work.Right)-width)
	}
	if screenX < int(work.Left) {
		screenX = int(work.Left)
	}
	if screenY+height > int(work.Bottom) {
		screenY = maxInt(int(work.Top), screenY-height)
	}
	if screenY < int(work.Top) {
		screenY = int(work.Top)
	}
	procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(screenX), uintptr(screenY), uintptr(width), uintptr(height), 0)
	renderer, err := newDirect2DRenderer(h.renderer, hwnd, width, height, menu.dpi)
	if err != nil {
		windowRegistry.Delete(hwnd)
		procDestroyWindow.Call(hwnd)
		return nil, err
	}
	menu.renderer = renderer
	applyDWM(hwnd, h.design.MaterialFor("genericConfirm"), true)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSetCapture.Call(hwnd)
	return menu, nil
}

func (m *menuWindow) layoutEntries() []menuEntry {
	entries := make([]menuEntry, 0, len(m.items))
	y := 8
	for _, item := range m.items {
		height := 36
		if item.Separator {
			height = 9
		}
		entries = append(entries, menuEntry{item: item, rect: Rect{8, y, m.width - 16, height}})
		y += height
	}
	return entries
}

func (m *menuWindow) proc(message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		m.paint()
		return 0
	case wmMouseMove:
		x, y := m.logicalPoint(lParam)
		hover := m.indexAt(x, y)
		if hover != m.hover {
			m.hover = hover
			procInvalidateRect.Call(m.hwnd, 0, 0)
		}
		return 0
	case wmLButtonDown, wmRButtonDown:
		x, y := m.logicalPoint(lParam)
		index := m.indexAt(x, y)
		if index < 0 {
			m.resolve(0)
		} else {
			m.selected = index
			procInvalidateRect.Call(m.hwnd, 0, 0)
		}
		return 0
	case wmLButtonUp, wmRButtonUp:
		x, y := m.logicalPoint(lParam)
		index := m.indexAt(x, y)
		if index >= 0 && index == m.selected && !m.entries[index].item.Separator {
			m.resolve(m.entries[index].item.ID)
		} else if index < 0 {
			m.resolve(0)
		}
		return 0
	case wmKeyDown:
		switch wParam {
		case vkEscape:
			m.resolve(0)
		case vkUp:
			m.moveSelection(-1)
		case vkDown:
			m.moveSelection(1)
		case vkReturn:
			index := m.selected
			if index < 0 {
				index = m.hover
			}
			if index >= 0 && !m.entries[index].item.Separator {
				m.resolve(m.entries[index].item.ID)
			}
		}
		return 0
	case wmActivate:
		if wParam&0xFFFF == 0 {
			m.resolve(0)
		}
		return 0
	case wmClose:
		m.resolve(0)
		return 0
	case wmDestroy:
		m.cleanup()
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(m.hwnd, uintptr(message), wParam, lParam)
	return result
}

func (m *menuWindow) logicalPoint(lParam uintptr) (int, int) {
	return unscaleForDPI(int(int16(lParam)), m.dpi), unscaleForDPI(int(int16(lParam>>16)), m.dpi)
}

func (m *menuWindow) indexAt(x, y int) int {
	for index, entry := range m.entries {
		if !entry.item.Separator && entry.rect.Contains(x, y) {
			return index
		}
	}
	return -1
}

func (m *menuWindow) moveSelection(direction int) {
	if len(m.entries) == 0 {
		return
	}
	index := m.selected
	for step := 0; step < len(m.entries); step++ {
		index = (index + direction + len(m.entries)) % len(m.entries)
		if !m.entries[index].item.Separator {
			m.selected = index
			m.hover = index
			procInvalidateRect.Call(m.hwnd, 0, 0)
			return
		}
	}
}

func (m *menuWindow) resolve(id int) {
	if m.resolved {
		return
	}
	m.resolved = true
	procReleaseCapture.Call()
	m.result <- id
	close(m.result)
	procDestroyWindow.Call(m.hwnd)
}

func (m *menuWindow) cleanup() {
	if m.renderer != nil {
		m.renderer.release()
		m.renderer = nil
	}
	windowRegistry.Delete(m.hwnd)
	m.hwnd = 0
	if !m.resolved {
		m.resolved = true
		m.result <- 0
		close(m.result)
	}
}

func (m *menuWindow) paint() {
	var paint nativePaintStruct
	procBeginPaint.Call(m.hwnd, uintptr(unsafe.Pointer(&paint)))
	defer procEndPaint.Call(m.hwnd, uintptr(unsafe.Pointer(&paint)))
	if m.renderer == nil {
		return
	}
	m.entries = m.layoutEntries()
	m.renderer.ensureBackground(m.host.design.Theme)
	m.renderer.begin()
	defer func() { _ = m.renderer.end() }()
	full := Rect{0, 0, m.width, m.height}
	material := m.host.design.MaterialFor("genericConfirm")
	m.renderer.drawBitmap(m.renderer.backgroundBitmap, full, float32(clampInt(material.BackgroundOpacity, 0, 100))/100)
	m.renderer.drawVisual(full, m.host.design.Style("targets.genericConfirm.window.background"))
	panel := m.host.design.Style("targets.genericConfirm.body")
	panel.Radius = 8
	m.renderer.drawVisual(full.Inset(1), panel)
	for index, entry := range m.entries {
		if entry.item.Separator {
			divider := Rect{entry.rect.X + 8, entry.rect.Y + entry.rect.H/2, entry.rect.W - 16, 1}
			m.renderer.drawVisual(divider, m.host.design.Style("targets.genericConfirm.divider"))
			continue
		}
		active := index == m.hover || index == m.selected
		if active {
			itemStyle := m.host.design.Style("targets.genericConfirm.button.secondary")
			setSolidFill(&itemStyle, "#FFFFFF", 8)
			itemStyle.BorderDisabled = true
			itemStyle.Radius = 5
			m.renderer.drawVisual(entry.rect, itemStyle)
		}
		if entry.item.Checked {
			m.renderer.drawText("✓", Rect{entry.rect.X + 10, entry.rect.Y, 20, entry.rect.H}, m.host.design.Theme.Typography.Family, 13, 600, "#60CDFF", 1, 1, 1)
		}
		m.renderer.drawText(entry.item.Label, Rect{entry.rect.X + 38, entry.rect.Y, entry.rect.W - 48, entry.rect.H}, m.host.design.Theme.Typography.Family, m.host.design.Theme.Typography.BodySize, 400, m.host.design.Theme.Colors.TextPrimary, 1, 0, 1)
	}
}
