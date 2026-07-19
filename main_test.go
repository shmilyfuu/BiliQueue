package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionFileMatchesBinaryVersion(t *testing.T) {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if fileVersion := strings.TrimSpace(string(data)); fileVersion != version {
		t.Fatalf("VERSION mismatch: file=%q binary=%q", fileVersion, version)
	}
}

func TestQueueCommands(t *testing.T) {
	a := newApp(t.TempDir())
	if a.state().Queue == nil {
		t.Fatal("empty queue must encode as []")
	}

	a.processMessage(ChatMessage{UID: 1, Username: "甲", Text: "我要排队"})
	if got := len(a.state().Queue); got != 0 {
		t.Fatalf("partial command added user: %d", got)
	}

	a.processMessage(ChatMessage{UID: 1, Username: "甲", Text: "排队"})
	a.processMessage(ChatMessage{UID: 1, Username: "甲", Text: "排队"})
	if got := len(a.state().Queue); got != 1 {
		t.Fatalf("duplicate check failed: %d", got)
	}

	a.processMessage(ChatMessage{UID: 1, Username: "甲", Text: "取消排队"})
	if got := len(a.state().Queue); got != 0 {
		t.Fatalf("cancel failed: %d", got)
	}
}

func TestDanmuParser(t *testing.T) {
	obj := map[string]any{
		"cmd": "DANMU_MSG:4:0:2:2:2:0",
		"info": []any{
			[]any{},
			"排队",
			[]any{float64(12345), "测试用户"},
			[]any{float64(18), "牌子"},
		},
	}
	msg, ok := parseDanmuMessage(obj)
	if !ok {
		t.Fatal("danmu was not parsed")
	}
	if msg.UID != 12345 || msg.Username != "测试用户" || msg.Text != "排队" || msg.MedalLevel != 18 {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestDuplicateJoinRefreshesAvatar(t *testing.T) {
	a := newApp(t.TempDir())
	a.processMessage(ChatMessage{UID: 9, Username: "用户", Text: "排队"})
	a.processMessage(ChatMessage{UID: 9, Username: "用户", Avatar: "https://i0.hdslb.com/avatar.jpg", Text: "排队"})
	queue := a.state().Queue
	if len(queue) != 1 || queue[0].Avatar != "https://i0.hdslb.com/avatar.jpg" {
		t.Fatalf("duplicate join did not refresh avatar: %#v", queue)
	}
}

func TestDanmuAvatarParser(t *testing.T) {
	meta := make([]any, 16)
	meta[15] = map[string]any{
		"user": map[string]any{
			"base": map[string]any{"face": "//i0.hdslb.com/bfs/face/test-avatar.jpg"},
		},
	}
	obj := map[string]any{
		"cmd": "DANMU_MSG",
		"info": []any{
			meta,
			"排队",
			[]any{float64(12345), "头像用户"},
		},
	}
	msg, ok := parseDanmuMessage(obj)
	if !ok {
		t.Fatal("danmu with avatar was not parsed")
	}
	if msg.Avatar != "https://i0.hdslb.com/bfs/face/test-avatar.jpg" {
		t.Fatalf("unexpected avatar: %q", msg.Avatar)
	}
}

func TestConfigImportCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir)
	a.mu.Lock()
	a.config.JoinCommand = "旧指令"
	a.mu.Unlock()
	a.saveConfig()
	ts := httptest.NewServer(a.routes())
	defer ts.Close()

	payload := bytes.NewBufferString(`{
  "schemaVersion": 4,
  "roomId": "123",
  "joinCommand": "新排队",
  "cancelCommand": "新取消",
  "maxQueue": 88,
  "giftPriority": {"enabled": true, "thresholdBattery": 200, "sortByValue": false},
  "overlay": {"height": 120, "fontSize": 24, "currentFontSize": 24, "queueFontSize": 24, "infoFontSize": 18, "background": "#000000", "opacity": 0.45, "scrollMode": "continuous", "shortAlign": "center", "currentWidth": 300, "queueWidth": 1220, "infoWidth": 400, "emptyText": "排队空闲中", "queueEmptyText": "空"}
}`)
	resp, err := http.Post(ts.URL+"/api/config/import", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("import status %d: %s", resp.StatusCode, body)
	}
	if got := a.state().Config.JoinCommand; got != "新排队" {
		t.Fatalf("config not imported: %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup not created: entries=%d err=%v", len(entries), err)
	}
}

func TestImageProxyServesCache(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir)
	raw := "https://i0.hdslb.com/bfs/face/cache-test.png"
	cacheDir := filepath.Join(dir, "avatars")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// PNG signature plus IHDR marker is enough for http.DetectContentType.
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	if err := os.WriteFile(filepath.Join(cacheDir, imageCacheName(raw)), png, 0o600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.routes())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/media/image?url=" + url.QueryEscape(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected image response: status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestImageProxyHostValidation(t *testing.T) {
	for _, host := range []string{"i0.hdslb.com", "s1.hdslb.com", "api.bilibili.com"} {
		if !allowedBiliImageHost(host) {
			t.Fatalf("expected allowed host: %s", host)
		}
	}
	if allowedBiliImageHost("example.com") {
		t.Fatal("unexpected external image host allowed")
	}
}

func TestBiliPacketDecodeZlib(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"cmd":  "DANMU_MSG",
		"info": []any{[]any{}, "排队", []any{float64(88), "用户"}},
	})
	inner := encodeBiliPacket(5, 0, body)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(inner)
	_ = zw.Close()
	outer := encodeBiliPacket(5, 2, compressed.Bytes())

	called := 0
	err := decodeBiliPackets(outer, func(operation, protocol int, got []byte) error {
		called++
		if operation != 5 || protocol != 0 || !bytes.Equal(got, body) {
			t.Fatalf("unexpected decoded packet: op=%d protocol=%d body=%q", operation, protocol, got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("handler calls: %d", called)
	}
}

func TestBiliPacketHeader(t *testing.T) {
	p := encodeBiliPacket(7, 1, []byte("{}"))
	if int(binary.BigEndian.Uint32(p[0:4])) != len(p) {
		t.Fatal("wrong packet length")
	}
	if binary.BigEndian.Uint16(p[4:6]) != 16 || binary.BigEndian.Uint32(p[8:12]) != 7 {
		t.Fatal("wrong packet header")
	}
}

func TestHTTPDebugFlow(t *testing.T) {
	a := newApp(t.TempDir())
	ts := httptest.NewServer(a.routes())
	defer ts.Close()

	payload := bytes.NewBufferString(`{"uid":101,"username":"测试甲","text":"排队"}`)
	resp, err := http.Post(ts.URL+"/api/debug/message", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("debug endpoint status: %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var state PublicState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if len(state.Queue) != 1 || state.Queue[0].Username != "测试甲" {
		t.Fatalf("unexpected state: %#v", state.Queue)
	}
}

func TestMiniControlWindowEndpointsWithoutActiveWindow(t *testing.T) {
	a := newApp(t.TempDir())
	handler := a.routes()

	stateResponse := httptest.NewRecorder()
	handler.ServeHTTP(stateResponse, httptest.NewRequest(http.MethodGet, "/api/window/mini-control", nil))
	if stateResponse.Code != http.StatusOK {
		t.Fatalf("window state status: %d", stateResponse.Code)
	}
	var windowState MiniControlWindowState
	if err := json.NewDecoder(stateResponse.Body).Decode(&windowState); err != nil {
		t.Fatal(err)
	}
	if windowState.Supported != (runtime.GOOS == "windows") || windowState.Active || windowState.Opening {
		t.Fatalf("unexpected inactive window state: %#v", windowState)
	}

	topmostResponse := httptest.NewRecorder()
	topmostRequest := httptest.NewRequest(http.MethodPost, "/api/window/mini-control/topmost", bytes.NewBufferString(`{"topmost":true}`))
	handler.ServeHTTP(topmostResponse, topmostRequest)
	wantStatus := http.StatusConflict
	if topmostResponse.Code != wantStatus {
		t.Fatalf("inactive topmost status: got %d want %d", topmostResponse.Code, wantStatus)
	}
}

func TestWBIHelpers(t *testing.T) {
	img := wbiFilenameKey("https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png")
	if img != "7cd084941338484aae1ad9425b84077c" {
		t.Fatalf("unexpected WBI filename key: %q", img)
	}
	if got := sanitizeWBIValue("a!'()*b"); got != "ab" {
		t.Fatalf("unexpected sanitized WBI value: %q", got)
	}
}

func TestCookieHelpers(t *testing.T) {
	cookie := cookiesToHeader([]*http.Cookie{
		{Name: "SESSDATA", Value: "abc"},
		{Name: "DedeUserID", Value: "12345"},
	})
	if cookieInt64(cookie, "DedeUserID") != 12345 {
		t.Fatalf("uid cookie parse failed: %q", cookie)
	}
	merged := mergeCookie(cookie, map[string]string{"buvid3": "B3"})
	if cookieMap(merged)["SESSDATA"] != "abc" || cookieMap(merged)["buvid3"] != "B3" {
		t.Fatalf("cookie merge failed: %q", merged)
	}
}

func TestConnectRequiresLogin(t *testing.T) {
	a := newApp(t.TempDir())
	if err := a.connect("5050"); err == nil {
		t.Fatal("connection should require login")
	}
}

func TestGiftParser(t *testing.T) {
	obj := map[string]any{
		"cmd": "SEND_GIFT",
		"data": map[string]any{
			"uid": float64(2468), "uname": "送礼用户", "giftId": float64(99),
			"giftName": "测试礼物", "num": float64(2), "coin_type": "gold",
			"price": float64(5000), "total_coin": float64(10000),
			"gift_info":    map[string]any{"img_basic": "https://example.com/gift.png"},
			"sender_uinfo": map[string]any{"base": map[string]any{"face": "https://example.com/avatar.png"}},
		},
	}
	gift, ok := parseGiftMessage(obj)
	if !ok {
		t.Fatal("gift was not parsed")
	}
	if gift.UID != 2468 || gift.GiftName != "测试礼物" || gift.Battery != 100 || gift.GiftIcon == "" || gift.Avatar == "" {
		t.Fatalf("unexpected gift: %#v", gift)
	}
}

func TestGiftPriority(t *testing.T) {
	a := newApp(t.TempDir())
	a.processMessage(ChatMessage{UID: 1, Username: "当前", Text: "排队"})
	a.processMessage(ChatMessage{UID: 2, Username: "第二", Text: "排队"})
	a.processMessage(ChatMessage{UID: 3, Username: "第三", Text: "排队"})
	a.processMessage(ChatMessage{UID: 4, Username: "第四", Text: "排队"})

	// 关闭价值排序：后触发的人追加到已有礼物优先区末尾。
	a.processGift(GiftMessage{EventID: "gift-1", UID: 4, Username: "第四", GiftName: "礼物", CoinType: "gold", Battery: 100})
	a.processGift(GiftMessage{EventID: "gift-2", UID: 3, Username: "第三", GiftName: "礼物", CoinType: "gold", Battery: 300})
	queue := a.state().Queue
	if len(queue) != 4 || queue[0].UID != 1 || queue[1].UID != 4 || queue[2].UID != 3 || queue[3].UID != 2 {
		t.Fatalf("gift priority sequence failed: %#v", queue)
	}

	// 已在礼物优先区的人再次送礼，关闭排序时位置不变。
	a.processGift(GiftMessage{EventID: "gift-3", UID: 4, Username: "第四", GiftName: "大礼物", CoinType: "gold", Battery: 500})
	queue = a.state().Queue
	if queue[1].UID != 4 || queue[2].UID != 3 {
		t.Fatalf("existing priority user moved unexpectedly: %#v", queue)
	}

	// 未排队用户送礼只记录最近礼物，不加入队列。
	a.processGift(GiftMessage{EventID: "gift-4", UID: 5, Username: "新用户", GiftName: "礼物", CoinType: "gold", Battery: 100})
	queue = a.state().Queue
	if len(queue) != 4 || queue[1].UID != 4 || queue[2].UID != 3 || queue[3].UID != 2 {
		t.Fatalf("non-queued gift user should not be appended: %#v", queue)
	}

	// 免费礼物不进入队列。
	a.processGift(GiftMessage{EventID: "gift-5", UID: 6, Username: "免费礼物", GiftName: "免费礼物", CoinType: "silver", Battery: 999})
	if len(a.state().Queue) != 4 {
		t.Fatal("free gift entered queue")
	}
}

func TestGiftPrioritySortBySingleGiftValue(t *testing.T) {
	a := newApp(t.TempDir())
	a.mu.Lock()
	a.config.GiftPriority.SortByValue = true
	a.config.GiftPriority.ThresholdBattery = 100
	a.mu.Unlock()

	a.processMessage(ChatMessage{UID: 1, Username: "当前", Text: "排队"})
	a.processMessage(ChatMessage{UID: 2, Username: "普通", Text: "排队"})
	a.processMessage(ChatMessage{UID: 3, Username: "一百", Text: "排队"})
	a.processMessage(ChatMessage{UID: 4, Username: "三百", Text: "排队"})
	a.processMessage(ChatMessage{UID: 5, Username: "二百", Text: "排队"})
	a.processGift(GiftMessage{EventID: "sort-1", UID: 3, Username: "一百", GiftName: "礼物", CoinType: "gold", Battery: 100})
	a.processGift(GiftMessage{EventID: "sort-2", UID: 4, Username: "三百", GiftName: "礼物", CoinType: "gold", Battery: 300})
	a.processGift(GiftMessage{EventID: "sort-3", UID: 5, Username: "二百", GiftName: "礼物", CoinType: "gold", Battery: 200})

	queue := a.state().Queue
	want := []int64{1, 4, 5, 3, 2}
	if len(queue) != len(want) {
		t.Fatalf("unexpected queue length: %#v", queue)
	}
	for i, uid := range want {
		if queue[i].UID != uid {
			t.Fatalf("sort order at %d: got %d want %d; queue=%#v", i, queue[i].UID, uid, queue)
		}
	}

	// 比较最近一次达到门槛的单次礼物价值，不做累计。
	a.processGift(GiftMessage{EventID: "sort-4", UID: 3, Username: "一百", GiftName: "大礼物", CoinType: "gold", Battery: 500})
	queue = a.state().Queue
	if queue[1].UID != 3 || queue[1].GiftBattery != 500 {
		t.Fatalf("single gift value reorder failed: %#v", queue)
	}
}

func TestGiftThresholdDoesNotAccumulate(t *testing.T) {
	a := newApp(t.TempDir())
	a.mu.Lock()
	a.config.GiftPriority.ThresholdBattery = 100
	a.mu.Unlock()
	a.processMessage(ChatMessage{UID: 1, Username: "当前", Text: "排队"})

	a.processGift(GiftMessage{EventID: "small-1", UID: 2, Username: "小礼物", GiftName: "礼物", CoinType: "gold", Battery: 60})
	a.processGift(GiftMessage{EventID: "small-2", UID: 2, Username: "小礼物", GiftName: "礼物", CoinType: "gold", Battery: 50})
	if len(a.state().Queue) != 1 {
		t.Fatalf("small gifts accumulated unexpectedly: %#v", a.state().Queue)
	}
}

func TestLegacyConfigMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "roomId":"",
  "joinCommand":"排队",
  "cancelCommand":"取消排队",
  "maxQueue":100,
  "giftPriority":{"enabled":false,"thresholdBattery":100},
  "overlay":{
    "height":120,"fontSize":24,"speed":40,"background":"#000000","opacity":0.45,"radius":16,
    "showAvatar":true,"showCount":true,"showRules":true,"showGiftIcon":true,
    "scrollMode":"continuous","shortAlign":"center","currentWidth":300,"countWidth":520
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newApp(dir)
	cfg := a.state().Config
	if cfg.SchemaVersion != defaultConfig().SchemaVersion || cfg.Overlay.InfoWidth != 520 || cfg.Overlay.QueueWidth != 1220 {
		t.Fatalf("legacy widths not migrated: %#v", cfg.Overlay)
	}
	if cfg.Overlay.QueueLineGap != 8 || cfg.Overlay.InfoLineGap != 4 || cfg.Overlay.QueuePageSize != 5 || !cfg.Overlay.DoubleLineEnabled {
		t.Fatalf("legacy layout defaults not applied: %#v", cfg.Overlay)
	}
	if cfg.GiftPriority.Enabled {
		t.Fatal("disabled gift priority should remain disabled during migration")
	}
	if cfg.Overlay.GradientTopOpacity != .45 || cfg.Overlay.GradientBottomOpacity != .45 {
		t.Fatalf("legacy background opacity not migrated: %#v", cfg.Overlay)
	}
	if cfg.Overlay.CurrentTextOpacity != 1 || cfg.Overlay.QueueTextOpacity != 1 || cfg.Overlay.InfoTextOpacity != 1 {
		t.Fatalf("legacy text opacity not migrated: %#v", cfg.Overlay)
	}
	if cfg.Overlay.GradientStart != 0 || cfg.Overlay.GradientEnd != 100 || cfg.Overlay.AvatarSize != 32 {
		t.Fatalf("legacy gradient/avatar defaults not migrated: %#v", cfg.Overlay)
	}
}

func TestNewConfigAllowsZeroLineGaps(t *testing.T) {
	cfg := defaultConfig()
	cfg.Overlay.QueueLineGap = 0
	cfg.Overlay.InfoLineGap = 0
	applyConfigDefaults(&cfg)
	if cfg.Overlay.QueueLineGap != 0 || cfg.Overlay.InfoLineGap != 0 {
		t.Fatalf("zero line gaps were overwritten: %#v", cfg.Overlay)
	}
}
func TestNewConfigAllowsZeroHeight(t *testing.T) {
	cfg := defaultConfig()
	cfg.Overlay.Height = 0
	applyConfigDefaults(&cfg)
	if cfg.Overlay.Height != 0 {
		t.Fatalf("zero height was overwritten: %#v", cfg.Overlay)
	}
}

func TestHotkeyConfigPersists(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir)
	body := bytes.NewBufferString(`{"openControl":"Ctrl+Alt+C","openMiniControl":"F8","nextQueue":"Ctrl+Alt+N","clearQueue":"Ctrl+Alt+X"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/hotkeys", body)
	rr := httptest.NewRecorder()
	a.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hotkey response: code=%d body=%s", rr.Code, rr.Body.String())
	}
	want := HotkeyConfig{OpenControl: "Ctrl+Alt+C", OpenMiniControl: "F8", NextQueue: "Ctrl+Alt+N", ClearQueue: "Ctrl+Alt+X"}
	if got := a.state().Config.Hotkeys; got != want {
		t.Fatalf("hotkeys not applied: got %#v want %#v", got, want)
	}
	reloaded := newApp(dir)
	if got := reloaded.state().Config.Hotkeys; got != want {
		t.Fatalf("hotkeys not persisted: got %#v want %#v", got, want)
	}
}

func TestOverlayAreaTextDefaults(t *testing.T) {
	cfg := defaultConfig()
	o := cfg.Overlay
	if o.CurrentFontSize != 24 || o.QueueFontSize != 24 || o.InfoFontSize != 18 {
		t.Fatalf("unexpected area font defaults: %#v", o)
	}
	if o.CurrentTextColor != "#ffffff" || o.QueueTextColor != "#ffffff" || o.InfoTextColor != "#ffffff" {
		t.Fatalf("unexpected area color defaults: %#v", o)
	}
	if o.QueueEmptyText != "空" {
		t.Fatalf("unexpected queue empty text: %q", o.QueueEmptyText)
	}
	if o.CurrentTextAlign != "left" || o.QueueTextAlign != "left" || o.InfoTextAlign != "left" {
		t.Fatalf("unexpected area text alignment defaults: %#v", o)
	}
	if o.CurrentTextLineGap != 0 || o.QueueTextLineGap != 0 {
		t.Fatalf("unexpected empty text line gap defaults: %#v", o)
	}
	if o.GradientStart != 0 || o.GradientEnd != 100 || o.AvatarSize != 32 {
		t.Fatalf("unexpected gradient/avatar defaults: %#v", o)
	}
}

func TestV2ConfigMigratesAreaTextStyles(t *testing.T) {
	cfg := Config{SchemaVersion: 2, JoinCommand: "排队", CancelCommand: "取消排队", MaxQueue: 100}
	cfg.Overlay.FontSize = 30
	cfg.Overlay.Height = 120
	cfg.Overlay.Background = "#000000"
	cfg.Overlay.Opacity = .45
	cfg.Overlay.CurrentWidth = 300
	cfg.Overlay.QueueWidth = 1220
	cfg.Overlay.InfoWidth = 400
	cfg.Overlay.ScrollMode = "continuous"
	cfg.Overlay.ShortAlign = "center"
	cfg.Overlay.EmptyText = "排队空闲中"
	applyConfigDefaults(&cfg)
	if cfg.SchemaVersion != defaultConfig().SchemaVersion {
		t.Fatalf("schema version not migrated: %d", cfg.SchemaVersion)
	}
	if cfg.Overlay.CurrentFontSize != 30 || cfg.Overlay.QueueFontSize != 30 {
		t.Fatalf("legacy font size not migrated: %#v", cfg.Overlay)
	}
	if cfg.Overlay.InfoFontSize != 18 || cfg.Overlay.QueueEmptyText != "空" {
		t.Fatalf("new defaults not added: %#v", cfg.Overlay)
	}
}

func TestFontDirectoryListingAndServing(t *testing.T) {
	a := newApp(t.TempDir())
	if err := os.WriteFile(filepath.Join(a.fontsDir, "测试字体.ttf"), []byte("font-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.fontsDir, "ignore.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	fonts := a.listFonts()
	if len(fonts) != 1 || fonts[0].File != "测试字体.ttf" {
		t.Fatalf("unexpected fonts: %#v", fonts)
	}
	req := httptest.NewRequest(http.MethodGet, "/fonts/%E6%B5%8B%E8%AF%95%E5%AD%97%E4%BD%93.ttf", nil)
	rr := httptest.NewRecorder()
	a.handleFontFile(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "font-data" {
		t.Fatalf("font response: code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestV6AllowsZeroOpacity(t *testing.T) {
	cfg := defaultConfig()
	cfg.Overlay.CurrentTextOpacity = 0
	cfg.Overlay.QueueTextOpacity = 0
	cfg.Overlay.InfoTextOpacity = 0
	cfg.Overlay.GradientTopOpacity = 0
	cfg.Overlay.GradientBottomOpacity = 0
	cfg.Overlay.CurrentBackgroundOpacity = 0
	cfg.Overlay.QueueBackgroundOpacity = 0
	cfg.Overlay.InfoBackgroundOpacity = 0
	cfg.Overlay.GradientStart = 40
	cfg.Overlay.GradientEnd = 80
	cfg.Overlay.AvatarSize = 44
	applyConfigDefaults(&cfg)
	if cfg.Overlay.CurrentTextOpacity != 0 || cfg.Overlay.QueueTextOpacity != 0 || cfg.Overlay.InfoTextOpacity != 0 {
		t.Fatalf("zero text opacity was overwritten: %#v", cfg.Overlay)
	}
	if cfg.Overlay.GradientTopOpacity != 0 || cfg.Overlay.GradientBottomOpacity != 0 {
		t.Fatalf("zero gradient opacity was overwritten: %#v", cfg.Overlay)
	}
	if cfg.Overlay.GradientStart != 40 || cfg.Overlay.GradientEnd != 80 || cfg.Overlay.AvatarSize != 44 {
		t.Fatalf("gradient bounds or avatar size was overwritten: %#v", cfg.Overlay)
	}
}

func TestV6GradientRangeMigratesToStartEnd(t *testing.T) {
	cfg := defaultConfig()
	cfg.SchemaVersion = 6
	cfg.Overlay.GradientRange = 50
	cfg.Overlay.GradientStart = 0
	cfg.Overlay.GradientEnd = 0
	applyConfigDefaults(&cfg)
	if cfg.SchemaVersion != defaultConfig().SchemaVersion {
		t.Fatalf("schema version not migrated: %d", cfg.SchemaVersion)
	}
	if cfg.Overlay.GradientStart != 50 || cfg.Overlay.GradientEnd != 100 {
		t.Fatalf("legacy gradient range not migrated: %#v", cfg.Overlay)
	}
}
