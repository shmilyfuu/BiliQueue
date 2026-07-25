//go:build windows

package nativeui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// This renderer is the production extraction of the experiment's reusable
// Direct2D/DirectWrite layer. Window policy and BiliQueue state live elsewhere.

var (
	d2d1DLL                 = syscall.NewLazyDLL("d2d1.dll")
	dwriteDLL               = syscall.NewLazyDLL("dwrite.dll")
	procD2D1CreateFactory   = d2d1DLL.NewProc("D2D1CreateFactory")
	procDWriteCreateFactory = dwriteDLL.NewProc("DWriteCreateFactory")
)

type d2dGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	iidID2D1Factory   = d2dGUID{0x06152247, 0x6f50, 0x465a, [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidIDWriteFactory = d2dGUID{0xb859ee5a, 0xd838, 0x4b5b, [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
)

const (
	d2dFactoryTypeSingleThreaded = 0
	d2dRenderTargetTypeDefault   = 0
	d2dAlphaModePremultiplied    = 1
	dxgiFormatB8G8R8A8UNorm      = 87
	d2dPresentOptionsNone        = 0
	d2dGamma22                   = 0
	d2dExtendModeClamp           = 0
	d2dBitmapInterpolationLinear = 1
	d2dAntialiasPerPrimitive     = 0
	d2dTextAntialiasGrayscale    = 2

	dwriteFactoryShared        = 0
	dwriteFontWeightNormal     = 400
	dwriteFontWeightSemiBold   = 600
	dwriteFontWeightBold       = 700
	dwriteFontStyleNormal      = 0
	dwriteFontStretchNormal    = 5
	dwriteTextAlignLeading     = 0
	dwriteTextAlignTrailing    = 1
	dwriteTextAlignCenter      = 2
	dwriteParagraphAlignNear   = 0
	dwriteParagraphAlignFar    = 1
	dwriteParagraphAlignCenter = 2
	dwriteWordWrappingNoWrap   = 1
	dwriteMeasuringModeNatural = 0
)

type d2dColorF struct{ R, G, B, A float32 }
type d2dPoint2F struct{ X, Y float32 }
type d2dRectF struct{ Left, Top, Right, Bottom float32 }
type d2dSizeU struct{ Width, Height uint32 }
type d2dPixelFormat struct{ Format, AlphaMode uint32 }
type d2dMatrix3x2F struct{ M11, M12, M21, M22, Dx, Dy float32 }
type d2dRenderTargetProperties struct {
	Type        uint32
	PixelFormat d2dPixelFormat
	DpiX, DpiY  float32
	Usage       uint32
	MinLevel    uint32
}
type d2dHwndRenderTargetProperties struct {
	Hwnd           uintptr
	PixelSize      d2dSizeU
	PresentOptions uint32
	_              uint32
}
type d2dBrushProperties struct {
	Opacity   float32
	Transform d2dMatrix3x2F
}
type d2dRoundedRect struct {
	Rect             d2dRectF
	RadiusX, RadiusY float32
}
type d2dEllipse struct {
	Point            d2dPoint2F
	RadiusX, RadiusY float32
}
type d2dGradientStop struct {
	Position float32
	Color    d2dColorF
}
type d2dLinearGradientBrushProperties struct{ StartPoint, EndPoint d2dPoint2F }
type d2dRadialGradientBrushProperties struct {
	Center, GradientOriginOffset d2dPoint2F
	RadiusX, RadiusY             float32
}
type d2dBitmapProperties struct {
	PixelFormat d2dPixelFormat
	DpiX, DpiY  float32
}
type d2dBitmapBrushProperties struct{ ExtendModeX, ExtendModeY, InterpolationMode uint32 }

type textFormatKey struct {
	family                       string
	size, weight, hAlign, vAlign int
}

type direct2DRenderer struct {
	factory          uintptr
	writeFactory     uintptr
	target           uintptr
	hwnd             uintptr
	width, height    int
	textFormats      map[textFormatKey]uintptr
	backgroundBitmap uintptr
	noiseBitmap      uintptr
	backgroundPixels []uint32
	blurBitmaps      map[int]uintptr
	bgKey            string
	backgroundSolid  bool
	dpi              float32
	generation       uint64
}

type rendererResources struct {
	factory      uintptr
	writeFactory uintptr
}

func comMethod(obj uintptr, index int) uintptr {
	if obj == 0 {
		return 0
	}
	vtbl := readUintptr(obj)
	return readUintptr(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0)))
}

func readUintptr(address uintptr) uintptr {
	if address == 0 {
		return 0
	}
	var value uintptr
	procCopyMemory.Call(uintptr(unsafe.Pointer(&value)), address, unsafe.Sizeof(value))
	return value
}

func comCall(obj uintptr, index int, args ...uintptr) uintptr {
	fn := comMethod(obj, index)
	if fn == 0 {
		return uintptr(^uint32(0))
	}
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, obj)
	callArgs = append(callArgs, args...)
	r, _, _ := syscall.SyscallN(fn, callArgs...)
	return r
}

func hresultFailed(v uintptr) bool { return int32(uint32(v)) < 0 }
func releaseCOM(p uintptr) {
	if p != 0 {
		comCall(p, 2)
	}
}

func newRendererResources() (*rendererResources, error) {
	resources := &rendererResources{}
	hr, _, callErr := procD2D1CreateFactory.Call(d2dFactoryTypeSingleThreaded, uintptr(unsafe.Pointer(&iidID2D1Factory)), 0, uintptr(unsafe.Pointer(&resources.factory)))
	if hresultFailed(hr) || resources.factory == 0 {
		return nil, fmt.Errorf("D2D1CreateFactory HRESULT=0x%08X: %v", uint32(hr), callErr)
	}
	hr, _, callErr = procDWriteCreateFactory.Call(dwriteFactoryShared, uintptr(unsafe.Pointer(&iidIDWriteFactory)), uintptr(unsafe.Pointer(&resources.writeFactory)))
	if hresultFailed(hr) || resources.writeFactory == 0 {
		releaseCOM(resources.factory)
		return nil, fmt.Errorf("DWriteCreateFactory HRESULT=0x%08X: %v", uint32(hr), callErr)
	}
	return resources, nil
}

func (resources *rendererResources) release() {
	if resources == nil {
		return
	}
	releaseCOM(resources.writeFactory)
	resources.writeFactory = 0
	releaseCOM(resources.factory)
	resources.factory = 0
}

func newDirect2DRenderer(resources *rendererResources, hwnd uintptr, w, h int, dpi uint32) (*direct2DRenderer, error) {
	if resources == nil || resources.factory == 0 || resources.writeFactory == 0 {
		return nil, fmt.Errorf("Direct2D resources are unavailable")
	}
	if dpi == 0 {
		dpi = 96
	}
	r := &direct2DRenderer{
		factory:      resources.factory,
		writeFactory: resources.writeFactory,
		hwnd:         hwnd,
		width:        maxInt(1, w),
		height:       maxInt(1, h),
		dpi:          float32(dpi),
		textFormats:  map[textFormatKey]uintptr{},
		blurBitmaps:  map[int]uintptr{},
	}
	if err := r.createTarget(); err != nil {
		r.release()
		return nil, err
	}
	return r, nil
}

func (r *direct2DRenderer) createTarget() error {
	if r.target != 0 {
		releaseCOM(r.target)
		r.target = 0
	}
	props := d2dRenderTargetProperties{Type: d2dRenderTargetTypeDefault, PixelFormat: d2dPixelFormat{Format: dxgiFormatB8G8R8A8UNorm, AlphaMode: d2dAlphaModePremultiplied}, DpiX: r.dpi, DpiY: r.dpi}
	hp := d2dHwndRenderTargetProperties{Hwnd: r.hwnd, PixelSize: d2dSizeU{uint32(maxInt(1, r.width)), uint32(maxInt(1, r.height))}, PresentOptions: d2dPresentOptionsNone}
	hr := comCall(r.factory, 14, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&hp)), uintptr(unsafe.Pointer(&r.target)))
	if hresultFailed(hr) || r.target == 0 {
		return fmt.Errorf("CreateHwndRenderTarget HRESULT=0x%08X", uint32(hr))
	}
	comCall(r.target, 32, d2dAntialiasPerPrimitive)
	comCall(r.target, 34, d2dTextAntialiasGrayscale)
	r.generation++
	return nil
}

func (r *direct2DRenderer) setDPI(dpi uint32) {
	if dpi == 0 {
		dpi = 96
	}
	if r.dpi == float32(dpi) {
		return
	}
	r.dpi = float32(dpi)
	r.bgKey = ""
	r.releaseBitmaps()
	_ = r.createTarget()
}

func (r *direct2DRenderer) releaseBitmaps() {
	releaseCOM(r.backgroundBitmap)
	r.backgroundBitmap = 0
	releaseCOM(r.noiseBitmap)
	r.noiseBitmap = 0
	for _, b := range r.blurBitmaps {
		releaseCOM(b)
	}
	r.blurBitmaps = map[int]uintptr{}
	r.backgroundPixels = nil
	r.backgroundSolid = false
}

func (r *direct2DRenderer) release() {
	r.releaseBitmaps()
	for _, f := range r.textFormats {
		releaseCOM(f)
	}
	r.textFormats = nil
	releaseCOM(r.target)
	r.target = 0
	r.writeFactory = 0
	r.factory = 0
}

func (r *direct2DRenderer) resize(w, h int) {
	w, h = maxInt(1, w), maxInt(1, h)
	if r.width == w && r.height == h {
		return
	}
	r.width, r.height = w, h
	r.bgKey = ""
	r.releaseBitmaps()
	if r.target != 0 {
		sz := d2dSizeU{uint32(w), uint32(h)}
		hr := comCall(r.target, 58, uintptr(unsafe.Pointer(&sz)))
		if hresultFailed(hr) {
			_ = r.createTarget()
		}
	}
}

func (r *direct2DRenderer) begin() {
	comCall(r.target, 48)
	clear := d2dColorF{0, 0, 0, 0}
	comCall(r.target, 47, uintptr(unsafe.Pointer(&clear)))
}

func (r *direct2DRenderer) end() error {
	hr := comCall(r.target, 49, 0, 0)
	// D2DERR_RECREATE_TARGET
	if uint32(hr) == 0x8899000C {
		if err := r.createTarget(); err != nil {
			return err
		}
		r.bgKey = ""
		r.releaseBitmaps()
		return nil
	}
	if hresultFailed(hr) {
		return fmt.Errorf("Direct2D EndDraw HRESULT=0x%08X", uint32(hr))
	}
	return nil
}

func packSizeU(w, h uint32) uintptr { return uintptr(uint64(h)<<32 | uint64(w)) }
func packPointF(x, y float32) uintptr {
	return uintptr(uint64(math.Float32bits(y))<<32 | uint64(math.Float32bits(x)))
}

func rectF(v Rect) d2dRectF {
	return d2dRectF{float32(v.X), float32(v.Y), float32(v.X + v.W), float32(v.Y + v.H)}
}
func roundedF(v Rect, radius int) d2dRoundedRect {
	rr := float32(clampInt(radius, 0, minInt(v.W, v.H)/2))
	return d2dRoundedRect{Rect: rectF(v), RadiusX: rr, RadiusY: rr}
}

func parseColorF(hex string, opacity float32) d2dColorF {
	v := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(v) != 6 {
		v = "000000"
	}
	n, _ := strconv.ParseUint(v, 16, 32)
	return d2dColorF{float32((n>>16)&255) / 255, float32((n>>8)&255) / 255, float32(n&255) / 255, float32(math.Max(0, math.Min(1, float64(opacity))))}
}

func identityMatrix() d2dMatrix3x2F { return d2dMatrix3x2F{M11: 1, M22: 1} }

func (r *direct2DRenderer) solidBrush(color string, opacity float32) uintptr {
	c := parseColorF(color, opacity)
	var b uintptr
	hr := comCall(r.target, 8, uintptr(unsafe.Pointer(&c)), 0, uintptr(unsafe.Pointer(&b)))
	if hresultFailed(hr) {
		return 0
	}
	return b
}

func brushStops(b BrushSpec) []GradientStop {
	stops := append([]GradientStop(nil), b.Stops...)
	if len(stops) == 0 {
		stops = []GradientStop{{0, b.Color1, 100}, {100, b.Color2, 100}}
	}
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].Position < stops[j].Position })
	return stops
}

func (r *direct2DRenderer) brushFor(b BrushSpec, bounds Rect) uintptr {
	overall := float32(clampInt(b.Opacity, 0, 100)) / 100
	stops := brushStops(b)
	if strings.EqualFold(b.Type, "solid") || len(stops) < 2 {
		return r.solidBrush(stops[0].Color, overall*float32(clampInt(stops[0].Opacity, 0, 100))/100)
	}
	ds := make([]d2dGradientStop, len(stops))
	for i, s := range stops {
		ds[i] = d2dGradientStop{Position: float32(clampInt(s.Position, 0, 100)) / 100, Color: parseColorF(s.Color, overall*float32(clampInt(s.Opacity, 0, 100))/100)}
	}
	var coll uintptr
	hr := comCall(r.target, 9, uintptr(unsafe.Pointer(&ds[0])), uintptr(len(ds)), d2dGamma22, d2dExtendModeClamp, uintptr(unsafe.Pointer(&coll)))
	if hresultFailed(hr) || coll == 0 {
		return r.solidBrush(stops[0].Color, overall*float32(clampInt(stops[0].Opacity, 0, 100))/100)
	}
	defer releaseCOM(coll)
	bp := d2dBrushProperties{Opacity: 1, Transform: identityMatrix()}
	var br uintptr
	if strings.EqualFold(b.Type, "radial") {
		cx0, cy0, rx0, ry0 := radialGradientGeometry(float64(bounds.W), float64(bounds.H), b.CenterX, b.CenterY, b.Radius, b.Shape)
		cx := float32(bounds.X) + float32(cx0)
		cy := float32(bounds.Y) + float32(cy0)
		rp := d2dRadialGradientBrushProperties{Center: d2dPoint2F{cx, cy}, RadiusX: float32(rx0), RadiusY: float32(ry0)}
		hr = comCall(r.target, 11, uintptr(unsafe.Pointer(&rp)), uintptr(unsafe.Pointer(&bp)), coll, uintptr(unsafe.Pointer(&br)))
	} else {
		cx := float64(bounds.X) + float64(bounds.W)/2
		cy := float64(bounds.Y) + float64(bounds.H)/2
		dx, dy := linearGradientHalfVector(float64(bounds.W), float64(bounds.H), b.Angle)
		lp := d2dLinearGradientBrushProperties{StartPoint: d2dPoint2F{float32(cx - dx), float32(cy - dy)}, EndPoint: d2dPoint2F{float32(cx + dx), float32(cy + dy)}}
		hr = comCall(r.target, 10, uintptr(unsafe.Pointer(&lp)), uintptr(unsafe.Pointer(&bp)), coll, uintptr(unsafe.Pointer(&br)))
	}
	if hresultFailed(hr) || br == 0 {
		return r.solidBrush(stops[0].Color, overall)
	}
	return br
}

func (r *direct2DRenderer) fillRounded(v Rect, radius int, brush uintptr) {
	if brush == 0 {
		return
	}
	rr := roundedF(v, radius)
	comCall(r.target, 19, uintptr(unsafe.Pointer(&rr)), brush)
}
func (r *direct2DRenderer) strokeRounded(v Rect, radius int, brush uintptr, width float32) {
	if brush == 0 || width <= 0 {
		return
	}
	rr := roundedF(v, radius)
	comCall(r.target, 18, uintptr(unsafe.Pointer(&rr)), brush, uintptr(math.Float32bits(width)), 0)
}
func (r *direct2DRenderer) fillEllipse(v Rect, brush uintptr) {
	if brush == 0 {
		return
	}
	e := d2dEllipse{Point: d2dPoint2F{float32(v.X) + float32(v.W)/2, float32(v.Y) + float32(v.H)/2}, RadiusX: float32(v.W) / 2, RadiusY: float32(v.H) / 2}
	comCall(r.target, 21, uintptr(unsafe.Pointer(&e)), brush)
}
func (r *direct2DRenderer) line(x1, y1, x2, y2 int, brush uintptr, width float32) {
	if brush == 0 {
		return
	}
	p1, p2 := d2dPoint2F{float32(x1), float32(y1)}, d2dPoint2F{float32(x2), float32(y2)}
	comCall(r.target, 15, packPointF(p1.X, p1.Y), packPointF(p2.X, p2.Y), brush, uintptr(math.Float32bits(width)), 0)
}
func (r *direct2DRenderer) pushClip(v Rect) {
	rf := rectF(v)
	comCall(r.target, 45, uintptr(unsafe.Pointer(&rf)), d2dAntialiasPerPrimitive)
}
func (r *direct2DRenderer) popClip() { comCall(r.target, 46) }

func (r *direct2DRenderer) textFormat(family string, size, weight, hAlign, vAlign int) uintptr {
	if family == "" {
		family = "Segoe UI"
	}
	key := textFormatKey{family, size, weight, hAlign, vAlign}
	if p := r.textFormats[key]; p != 0 {
		return p
	}
	fp, _ := syscall.UTF16PtrFromString(family)
	loc, _ := syscall.UTF16PtrFromString("zh-CN")
	var f uintptr
	hr := comCall(r.writeFactory, 15, uintptr(unsafe.Pointer(fp)), 0, uintptr(weight), dwriteFontStyleNormal, dwriteFontStretchNormal, uintptr(math.Float32bits(float32(size))), uintptr(unsafe.Pointer(loc)), uintptr(unsafe.Pointer(&f)))
	if hresultFailed(hr) || f == 0 {
		return 0
	}
	align := dwriteTextAlignLeading
	if hAlign == 1 {
		align = dwriteTextAlignCenter
	} else if hAlign == 2 {
		align = dwriteTextAlignTrailing
	}
	para := dwriteParagraphAlignNear
	if vAlign == 1 {
		para = dwriteParagraphAlignCenter
	} else if vAlign == 2 {
		para = dwriteParagraphAlignFar
	}
	comCall(f, 3, uintptr(align))
	comCall(f, 4, uintptr(para))
	comCall(f, 5, dwriteWordWrappingNoWrap)
	r.textFormats[key] = f
	return f
}

func (r *direct2DRenderer) drawText(text string, v Rect, family string, size, weight int, color string, opacity float32, hAlign, vAlign int) {
	if text == "" || v.W <= 0 || v.H <= 0 {
		return
	}
	fmtp := r.textFormat(family, size, weight, hAlign, vAlign)
	if fmtp == 0 {
		return
	}
	br := r.solidBrush(color, opacity)
	if br == 0 {
		return
	}
	defer releaseCOM(br)
	u, _ := syscall.UTF16FromString(text)
	if len(u) > 0 {
		u = u[:len(u)-1]
	}
	rf := rectF(v)
	if len(u) > 0 {
		comCall(r.target, 27, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), fmtp, uintptr(unsafe.Pointer(&rf)), br, 0, dwriteMeasuringModeNatural)
	}
}

func (r *direct2DRenderer) createBitmap(pixels []uint32, w, h int) uintptr {
	if len(pixels) < w*h || w <= 0 || h <= 0 {
		return 0
	}
	props := d2dBitmapProperties{PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8UNorm, d2dAlphaModePremultiplied}, DpiX: 96, DpiY: 96}
	sz := d2dSizeU{uint32(w), uint32(h)}
	var bmp uintptr
	hr := comCall(r.target, 4, packSizeU(sz.Width, sz.Height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(w*4), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&bmp)))
	if hresultFailed(hr) {
		return 0
	}
	return bmp
}

func (r *direct2DRenderer) drawBitmap(bitmap uintptr, dest Rect, opacity float32) {
	if bitmap == 0 {
		return
	}
	dr := rectF(dest)
	comCall(r.target, 26, bitmap, uintptr(unsafe.Pointer(&dr)), uintptr(math.Float32bits(opacity)), d2dBitmapInterpolationLinear, 0)
}

func (r *direct2DRenderer) bitmapBrush(bitmap uintptr, opacity float32) uintptr {
	if bitmap == 0 {
		return 0
	}
	bbp := d2dBitmapBrushProperties{d2dExtendModeClamp, d2dExtendModeClamp, d2dBitmapInterpolationLinear}
	bp := d2dBrushProperties{Opacity: opacity, Transform: identityMatrix()}
	var br uintptr
	hr := comCall(r.target, 7, bitmap, uintptr(unsafe.Pointer(&bbp)), uintptr(unsafe.Pointer(&bp)), uintptr(unsafe.Pointer(&br)))
	if hresultFailed(hr) {
		return 0
	}
	return br
}

func (r *direct2DRenderer) fillRoundedBitmap(v Rect, radius int, bitmap uintptr, opacity float32) {
	br := r.bitmapBrush(bitmap, opacity)
	if br == 0 {
		return
	}
	defer releaseCOM(br)
	rr := roundedF(v, radius)
	var geom uintptr
	hr := comCall(r.factory, 6, uintptr(unsafe.Pointer(&rr)), uintptr(unsafe.Pointer(&geom)))
	if hresultFailed(hr) || geom == 0 {
		return
	}
	defer releaseCOM(geom)
	comCall(r.target, 23, geom, br, 0)
}

func premulBGRA(r, g, b, a int) uint32 {
	a = clampInt(a, 0, 255)
	r = r * a / 255
	g = g * a / 255
	b = b * a / 255
	return uint32(b) | uint32(g)<<8 | uint32(r)<<16 | uint32(a)<<24
}

func blendPremul(dst, src uint32) uint32 {
	sa := int(src >> 24)
	inv := 255 - sa
	db := int(dst & 255)
	dg := int((dst >> 8) & 255)
	dr := int((dst >> 16) & 255)
	da := int(dst >> 24)
	sb := int(src & 255)
	sg := int((src >> 8) & 255)
	sr := int((src >> 16) & 255)
	b := sb + db*inv/255
	g := sg + dg*inv/255
	rr := sr + dr*inv/255
	a := sa + da*inv/255
	return uint32(clampInt(b, 0, 255)) | uint32(clampInt(g, 0, 255))<<8 | uint32(clampInt(rr, 0, 255))<<16 | uint32(clampInt(a, 0, 255))<<24
}

func colorInts(hex string) (int, int, int) {
	c := parseColorF(hex, 1)
	return int(c.R * 255), int(c.G * 255), int(c.B * 255)
}

func generateAcrylicBackground(w, h int, theme Theme) ([]uint32, []uint32) {
	px := make([]uint32, w*h)
	// Low-opacity dark wash lets the real DWM backdrop remain visible.
	baseR, baseG, baseB := colorInts(theme.Material.TintColor)
	baseA := clampInt(theme.Material.TintOpacity*255/100, 0, 255)
	for i := range px {
		px[i] = premulBGRA(baseR, baseG, baseB, baseA)
	}
	type blob struct {
		x, y, rad float64
		color     string
		alpha     int
	}
	blobs := []blob{
		{0.12, 0.08, 0.62, "#48D8FF", 78},
		{0.93, 0.22, 0.58, "#C546FF", 72},
		{0.20, 0.86, 0.54, "#6657FF", 58},
		{0.86, 0.92, 0.44, "#E6C54A", 45},
	}
	if st, ok := theme.Styles["window.background"]; ok && len(st.Fill.Stops) > 0 {
		for i := range blobs {
			blobs[i].color = st.Fill.Stops[i%len(st.Fill.Stops)].Color
		}
	}
	for _, bl := range blobs {
		cr, cg, cb := colorInts(bl.color)
		cx, cy := bl.x*float64(w), bl.y*float64(h)
		rad := bl.rad * float64(maxInt(w, h))
		minX := maxInt(0, int(cx-rad))
		maxX := minInt(w-1, int(cx+rad))
		minY := maxInt(0, int(cy-rad))
		maxY := minInt(h-1, int(cy+rad))
		for y := minY; y <= maxY; y++ {
			for x := minX; x <= maxX; x++ {
				dx, dy := float64(x)-cx, float64(y)-cy
				d := math.Sqrt(dx*dx+dy*dy) / rad
				if d >= 1 {
					continue
				}
				fall := math.Pow(1-d, 2.15)
				a := int(float64(bl.alpha) * fall)
				px[y*w+x] = blendPremul(px[y*w+x], premulBGRA(cr, cg, cb, a))
			}
		}
	}
	// A subtle vertical darkening keeps text legible.
	for y := 0; y < h; y++ {
		a := int(38 * float64(y) / float64(maxInt(1, h-1)))
		for x := 0; x < w; x++ {
			px[y*w+x] = blendPremul(px[y*w+x], premulBGRA(4, 6, 12, a))
		}
	}
	return px, nil
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func blurPremul(src []uint32, w, h, radius int) []uint32 {
	if radius <= 0 {
		return append([]uint32(nil), src...)
	}
	// Three box passes approximate Gaussian blur and are fast enough for the
	// small design preview. Radius is divided across the passes.
	out := append([]uint32(nil), src...)
	tmp := make([]uint32, len(src))
	rad := maxInt(1, radius/3)
	for pass := 0; pass < 3; pass++ {
		boxBlurHorizontal(out, tmp, w, h, rad)
		boxBlurVertical(tmp, out, w, h, rad)
	}
	return out
}

func boxBlurHorizontal(src, dst []uint32, w, h, r int) {
	for y := 0; y < h; y++ {
		var sb, sg, sr, sa int64
		count := 0
		for x := -r; x <= r; x++ {
			xx := clampInt(x, 0, w-1)
			p := src[y*w+xx]
			sb += int64(p & 255)
			sg += int64((p >> 8) & 255)
			sr += int64((p >> 16) & 255)
			sa += int64(p >> 24)
			count++
		}
		for x := 0; x < w; x++ {
			dst[y*w+x] = uint32(sb/int64(count)) | uint32(sg/int64(count))<<8 | uint32(sr/int64(count))<<16 | uint32(sa/int64(count))<<24
			old := clampInt(x-r, 0, w-1)
			neu := clampInt(x+r+1, 0, w-1)
			po, pn := src[y*w+old], src[y*w+neu]
			sb += int64(pn&255) - int64(po&255)
			sg += int64((pn>>8)&255) - int64((po>>8)&255)
			sr += int64((pn>>16)&255) - int64((po>>16)&255)
			sa += int64(pn>>24) - int64(po>>24)
		}
	}
}
func boxBlurVertical(src, dst []uint32, w, h, r int) {
	for x := 0; x < w; x++ {
		var sb, sg, sr, sa int64
		count := 0
		for y := -r; y <= r; y++ {
			yy := clampInt(y, 0, h-1)
			p := src[yy*w+x]
			sb += int64(p & 255)
			sg += int64((p >> 8) & 255)
			sr += int64((p >> 16) & 255)
			sa += int64(p >> 24)
			count++
		}
		for y := 0; y < h; y++ {
			dst[y*w+x] = uint32(sb/int64(count)) | uint32(sg/int64(count))<<8 | uint32(sr/int64(count))<<16 | uint32(sa/int64(count))<<24
			old := clampInt(y-r, 0, h-1)
			neu := clampInt(y+r+1, 0, h-1)
			po, pn := src[old*w+x], src[neu*w+x]
			sb += int64(pn&255) - int64(po&255)
			sg += int64((pn>>8)&255) - int64((po>>8)&255)
			sr += int64((pn>>16)&255) - int64((po>>16)&255)
			sa += int64(pn>>24) - int64(po>>24)
		}
	}
}

func generateNoiseBitmap(w, h int) []uint32 {
	px := make([]uint32, w*h)
	seed := uint32(0x91E10DA5)
	for i := range px {
		seed = seed*1664525 + 1013904223
		v := int((seed >> 24) & 255)
		a := 20 + v*18/255
		px[i] = premulBGRA(v, v, v, a)
	}
	return px
}

func (r *direct2DRenderer) ensureBackground(theme Theme) {
	key := fmt.Sprintf("%dx%d|%s|%s|%d|%d|%d|%d|%v", r.width, r.height, theme.Material.Backdrop, theme.Material.TintColor, theme.Material.TintOpacity, theme.Material.BackgroundOpacity, theme.Material.BlurStrength, theme.Material.NoiseOpacity, theme.Styles["window.background"].Fill.Stops)
	if key == r.bgKey && r.backgroundBitmap != 0 {
		return
	}
	r.releaseBitmaps()
	if strings.EqualFold(theme.Material.Backdrop, "none") {
		red, green, blue := colorInts(theme.Material.TintColor)
		pixel := premulBGRA(red, green, blue, clampInt(theme.Material.TintOpacity*255/100, 0, 255))
		r.backgroundPixels = make([]uint32, r.width*r.height)
		for i := range r.backgroundPixels {
			r.backgroundPixels[i] = pixel
		}
		r.backgroundBitmap = r.createBitmap(r.backgroundPixels, r.width, r.height)
		r.backgroundSolid = true
		r.bgKey = key
		return
	}
	base, _ := generateAcrylicBackground(r.width, r.height, theme)
	diffused := blurPremul(base, r.width, r.height, maxInt(1, theme.Material.BlurStrength/4))
	r.backgroundPixels = diffused
	r.backgroundBitmap = r.createBitmap(diffused, r.width, r.height)
	r.noiseBitmap = r.createBitmap(generateNoiseBitmap(r.width, r.height), r.width, r.height)
	r.bgKey = key
}

func (r *direct2DRenderer) bitmapForBlur(radius int) uintptr {
	if r.backgroundSolid {
		return r.backgroundBitmap
	}
	radius = clampInt(radius, 1, 64)
	if b := r.blurBitmaps[radius]; b != 0 {
		return b
	}
	if len(r.backgroundPixels) == 0 {
		return 0
	}
	// A handful of blur radii is enough for a small design preview. Bounding the
	// cache prevents repeated slider experiments at large window sizes from
	// retaining dozens of full-window bitmaps.
	if len(r.blurBitmaps) >= 8 {
		for _, cached := range r.blurBitmaps {
			releaseCOM(cached)
		}
		r.blurBitmaps = map[int]uintptr{}
	}
	px := blurPremul(r.backgroundPixels, r.width, r.height, radius)
	b := r.createBitmap(px, r.width, r.height)
	if b != 0 {
		r.blurBitmaps[radius] = b
	}
	return b
}

func (r *direct2DRenderer) drawVisual(v Rect, st ElementVisual) {
	if st.Hidden || v.W <= 0 || v.H <= 0 {
		return
	}
	if strings.EqualFold(st.Material, "acrylic") {
		blurBitmap := r.bitmapForBlur(st.Acrylic.Blur)
		if blurBitmap != 0 {
			r.fillRoundedBitmap(v, st.Radius, blurBitmap, 1)
		}
		tint := st.Acrylic.TintColor
		if tint == "" {
			tint = "#111827"
		}
		br := r.solidBrush(tint, float32(clampInt(st.Acrylic.TintOpacity, 0, 100))/100)
		r.fillRounded(v, st.Radius, br)
		releaseCOM(br)
		if st.Acrylic.LuminosityOpacity > 0 {
			lb := r.solidBrush("#FFFFFF", float32(st.Acrylic.LuminosityOpacity)/100)
			r.fillRounded(v, st.Radius, lb)
			releaseCOM(lb)
		}
		if st.Acrylic.NoiseOpacity > 0 && r.noiseBitmap != 0 {
			r.fillRoundedBitmap(v, st.Radius, r.noiseBitmap, float32(st.Acrylic.NoiseOpacity)/20)
		}
	}
	if !st.FillDisabled && st.Fill.Opacity > 0 {
		br := r.brushFor(st.Fill, v)
		r.fillRounded(v, st.Radius, br)
		releaseCOM(br)
	}
	if !st.BorderDisabled && st.BorderWidth > 0 {
		rr := v
		rad := st.Radius
		half := st.BorderWidth / 2
		switch st.BorderPosition {
		case "inside":
			rr = rr.inset(half)
			rad = maxInt(0, rad-half)
		case "outside":
			rr = Rect{rr.X - half, rr.Y - half, rr.W + 2*half, rr.H + 2*half}
			rad += half
		}
		// Figma maps stroke paint to the element bounds, independent of whether
		// the stroke itself is inside, centered or outside.
		br := r.brushFor(st.Border, v)
		r.strokeRounded(rr, rad, br, float32(st.BorderWidth))
		releaseCOM(br)
	}
}
