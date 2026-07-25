package nativeui

import (
	"math"
	"testing"
)

func TestEmbeddedDesign(t *testing.T) {
	design, err := LoadDesign()
	if err != nil {
		t.Fatal(err)
	}
	if design.Theme.SchemaVersion != 16 {
		t.Fatalf("schema = %d", design.Theme.SchemaVersion)
	}
	if design.Layout.Window.Width != 420 || design.Layout.Window.Height != 560 {
		t.Fatalf("mini size = %dx%d", design.Layout.Window.Width, design.Layout.Window.Height)
	}
	for _, target := range []string{"portChange", "clearQueue", "updateProgress", "updateComplete"} {
		layout := design.TargetLayout(target, TargetLayout{})
		if layout.Width == 0 || layout.Height == 0 {
			t.Fatalf("missing layout for %s", target)
		}
		if _, ok := design.Theme.TargetMaterials[target]; !ok {
			t.Fatalf("missing material for %s", target)
		}
	}
}

func TestEmbeddedDesignUsesSolidWindowBackgrounds(t *testing.T) {
	design, err := LoadDesign()
	if err != nil {
		t.Fatal(err)
	}
	assertSolid := func(name string, material WindowMaterial, style ElementVisual) {
		t.Helper()
		if material.Backdrop != "none" {
			t.Fatalf("%s backdrop = %q, want none", name, material.Backdrop)
		}
		if material.TintColor != "#202020" || material.TitleBarColor != "#202020" || material.TitleBarBorderColor != "#202020" {
			t.Fatalf("%s colors = tint %q, title %q, border %q", name, material.TintColor, material.TitleBarColor, material.TitleBarBorderColor)
		}
		if style.Material != "normal" || style.FillDisabled || style.Fill.Color1 != "#202020" || style.Fill.Color2 != "#202020" || style.Fill.Opacity != 100 {
			t.Fatalf("%s background style is not opaque #202020: %#v", name, style)
		}
	}
	assertSolid("miniControl", design.MaterialFor("miniControl"), design.Style("window.background"))
	for name, material := range design.Theme.TargetMaterials {
		assertSolid(name, material, design.Style("targets."+name+".window.background"))
	}
}

func TestLinearGradientAnglesMatchDesignCoordinates(t *testing.T) {
	dx, dy := linearGradientHalfVector(100, 40, 0)
	if dx <= 0 || math.Abs(dy) > 0.0001 {
		t.Fatalf("0 degree vector = (%f, %f), want left to right", dx, dy)
	}
	dx, dy = linearGradientHalfVector(100, 40, 90)
	if math.Abs(dx) > 0.0001 || dy <= 0 {
		t.Fatalf("90 degree vector = (%f, %f), want top to bottom", dx, dy)
	}
}

func TestScrollbarThumbGeometry(t *testing.T) {
	thumb, offset, maxScroll := scrollbarThumbGeometry(300, 120, 600, 28, 240)
	if maxScroll != 480 || thumb != 60 || offset != 120 {
		t.Fatalf("overflow geometry = %d, %d, %d", thumb, offset, maxScroll)
	}
	thumb, offset, maxScroll = scrollbarThumbGeometry(300, 400, 200, 28, 999)
	if thumb != 300 || offset != 0 || maxScroll != 0 {
		t.Fatalf("non-overflow geometry = %d, %d, %d", thumb, offset, maxScroll)
	}
}

func TestTargetDefaultsPreserveBusinessDetails(t *testing.T) {
	request := applyTargetDefaults("portChange", DialogRequest{})
	if request.Title != "修改监听地址" || request.Message != "修改后程序会重新启动本地服务。" {
		t.Fatalf("port defaults = %#v", request)
	}
	request = applyTargetDefaults("updateFailed", DialogRequest{Message: "下载校验失败：测试详情"})
	if request.Message != "下载校验失败：测试详情" || request.Title != "更新失败" {
		t.Fatalf("dynamic details were not preserved: %#v", request)
	}
}
