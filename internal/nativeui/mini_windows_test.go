//go:build windows

package nativeui

import (
	"image"
	"image/color"
	"testing"
)

func TestReorderMiniUsersSupportsFirstAndLastSlots(t *testing.T) {
	users := []MiniUser{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, changed := reorderMiniUsers(users, 0, 3)
	if !changed || got[0].ID != "b" || got[1].ID != "c" || got[2].ID != "a" {
		t.Fatalf("move first to end: %#v, changed=%v", got, changed)
	}
	got, changed = reorderMiniUsers(users, 2, 0)
	if !changed || got[0].ID != "c" || got[1].ID != "a" || got[2].ID != "b" {
		t.Fatalf("move last to first: %#v, changed=%v", got, changed)
	}
}

func TestImageConversionDoesNotDoublePremultiplyAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	pixels := imageToCirclePixels(source, 1, true)
	if len(pixels) != 1 {
		t.Fatalf("pixels = %d", len(pixels))
	}
	alpha := byte(pixels[0] >> 24)
	red := byte(pixels[0] >> 16)
	if alpha < 127 || alpha > 129 || red < 127 || red > 129 {
		t.Fatalf("premultiplied pixel = 0x%08x", pixels[0])
	}
}

func TestDialogGeometryStaysInsideFixedTarget(t *testing.T) {
	design, err := LoadDesign()
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{design: design}
	layout := design.TargetLayout("updateReady", TargetLayout{Width: 440, Height: 250, Padding: 18})
	win := &window{
		host:          host,
		target:        "updateReady",
		kind:          DialogConfirm,
		logicalWidth:  layout.Width,
		logicalHeight: layout.Height,
		padding:       layout.Padding,
	}
	geometry := win.geometry()
	if len(geometry.buttons) != 2 {
		t.Fatalf("buttons = %d", len(geometry.buttons))
	}
	for _, button := range geometry.buttons {
		if button.rect.X < 0 || button.rect.Y < 0 || button.rect.X+button.rect.W > layout.Width || button.rect.Y+button.rect.H > layout.Height {
			t.Fatalf("button outside target: %#v in %dx%d", button.rect, layout.Width, layout.Height)
		}
	}
}
