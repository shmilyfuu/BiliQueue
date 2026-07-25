package nativeui

import "testing"

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
