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
	"time"
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
	medal := make([]any, 13)
	medal[0] = float64(18)
	medal[1] = "牌子"
	medal[3] = float64(7788)
	medal[12] = float64(9988)
	obj := map[string]any{
		"cmd": "DANMU_MSG:4:0:2:2:2:0",
		"info": []any{
			[]any{},
			"排队",
			[]any{float64(12345), "测试用户"},
			medal,
			nil, nil, nil,
			float64(3),
		},
	}
	msg, ok := parseDanmuMessage(obj)
	if !ok {
		t.Fatal("danmu was not parsed")
	}
	if msg.UID != 12345 || msg.Username != "测试用户" || msg.Text != "排队" || msg.MedalLevel != 18 || msg.MedalRoomID != 7788 || msg.MedalTargetUID != 9988 || msg.GuardLevel != 3 {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestNormalizedDanmuEligibilityParser(t *testing.T) {
	obj := map[string]any{
		"cmd": "DANMU_MSG",
		"data": map[string]any{
			"uid": float64(23456), "uname": "新格式用户", "msg": "排队",
			"fans_medal_level": float64(12), "fans_medal_wearing_status": true,
			"guard_level": float64(2),
		},
	}
	msg, ok := parseDanmuMessage(obj)
	if !ok || msg.MedalLevel != 12 || !msg.MedalCurrentRoom || msg.GuardLevel != 2 {
		t.Fatalf("unexpected normalized message: %#v ok=%v", msg, ok)
	}
}

func TestGuardBuyParser(t *testing.T) {
	guard, ok := parseGuardMessage(map[string]any{
		"cmd": "GUARD_BUY",
		"data": map[string]any{
			"uid": float64(34567), "username": "总督用户", "guard_level": float64(1),
			"uinfo": map[string]any{"base": map[string]any{"face": "//example.com/avatar.png"}},
		},
	})
	if !ok || guard.UID != 34567 || guard.Username != "总督用户" || guard.GuardLevel != 1 || guard.Avatar != "https://example.com/avatar.png" {
		t.Fatalf("unexpected guard message: %#v ok=%v", guard, ok)
	}
}

func TestGiftParserIncludesGuardLevel(t *testing.T) {
	gift, ok := parseGiftMessage(map[string]any{
		"cmd": "SEND_GIFT",
		"data": map[string]any{
			"uid": float64(45678), "uname": "舰长用户", "giftName": "测试礼物",
			"giftId": float64(1), "num": float64(1), "total_coin": float64(10000),
			"coin_type": "gold", "guard_level": float64(3),
		},
	})
	if !ok || gift.GuardLevel != 3 {
		t.Fatalf("gift guard level was not parsed: %#v ok=%v", gift, ok)
	}
}

func TestQueueIdentityEligibility(t *testing.T) {
	a := newApp(t.TempDir())
	a.mu.Lock()
	a.resolvedRoomID = 7788
	a.anchorUID = 9988
	a.config.Eligibility = QueueEligibilityConfig{FanMedalEnabled: true, FanMedalLevel: 10}
	a.mu.Unlock()

	a.processMessage(ChatMessage{UID: 1, Username: "低等级", Text: "排队", MedalLevel: 9, MedalRoomID: 7788, MedalTargetUID: 9988})
	a.processMessage(ChatMessage{UID: 2, Username: "其他直播间", Text: "排队", MedalLevel: 20, MedalRoomID: 8877, MedalTargetUID: 8899})
	a.processMessage(ChatMessage{UID: 3, Username: "当前粉丝牌", Text: "排队", MedalLevel: 10, MedalRoomID: 7788, MedalTargetUID: 9988})
	queue := a.state().Queue
	if len(queue) != 1 || queue[0].UID != 3 {
		t.Fatalf("fan medal eligibility failed: %#v", queue)
	}

	a.mu.Lock()
	a.config.Eligibility.GuardEnabled = true
	a.mu.Unlock()
	a.processMessage(ChatMessage{UID: 4, Username: "大航海", Text: "排队", GuardLevel: 3})
	queue = a.state().Queue
	if len(queue) != 2 || queue[1].UID != 4 {
		t.Fatalf("fan medal or guard eligibility failed: %#v", queue)
	}

	a.mu.Lock()
	a.config.Eligibility = QueueEligibilityConfig{GuardEnabled: true, FanMedalLevel: 1}
	a.mu.Unlock()
	a.processMessage(ChatMessage{UID: 5, Username: "普通用户", Text: "排队", MedalLevel: 30, MedalCurrentRoom: true})
	if len(a.state().Queue) != 2 {
		t.Fatalf("guard-only eligibility accepted a non-guard: %#v", a.state().Queue)
	}
}

func TestGuardPriorityRanksAboveGiftPriority(t *testing.T) {
	a := newApp(t.TempDir())
	a.mu.Lock()
	a.config.Eligibility.GuardPriorityEnabled = true
	a.config.GiftPriority.Enabled = true
	a.config.GiftPriority.ThresholdBattery = 100
	a.mu.Unlock()

	a.addUser(ChatMessage{UID: 1, Username: "当前"})
	a.addUser(ChatMessage{UID: 2, Username: "普通"})
	a.addUser(ChatMessage{UID: 3, Username: "礼物优先"})
	a.processGift(GiftMessage{EventID: "guard-order-gift", UID: 3, Username: "礼物优先", GiftName: "礼物", CoinType: "gold", Battery: 100})
	a.addUser(ChatMessage{UID: 4, Username: "舰长", GuardLevel: 3})
	a.addUser(ChatMessage{UID: 5, Username: "总督", GuardLevel: 1})
	a.addUser(ChatMessage{UID: 6, Username: "提督", GuardLevel: 2})

	queue := a.state().Queue
	want := []int64{1, 5, 6, 4, 3, 2}
	if len(queue) != len(want) {
		t.Fatalf("unexpected queue length: %#v", queue)
	}
	for index, uid := range want {
		if queue[index].UID != uid {
			t.Fatalf("unexpected priority order at %d: got=%d want=%d queue=%#v", index, queue[index].UID, uid, queue)
		}
	}
}

func TestGuardBuyUpgradesQueuedUserWithoutReplacingCurrent(t *testing.T) {
	a := newApp(t.TempDir())
	a.mu.Lock()
	a.config.Eligibility.GuardPriorityEnabled = true
	a.mu.Unlock()
	a.addUser(ChatMessage{UID: 1, Username: "当前"})
	a.addUser(ChatMessage{UID: 2, Username: "普通一"})
	a.addUser(ChatMessage{UID: 3, Username: "普通二"})

	a.processGuard(GuardMessage{UID: 3, Username: "新总督", GuardLevel: 1})
	queue := a.state().Queue
	if len(queue) != 3 || queue[0].UID != 1 || queue[1].UID != 3 || queue[1].GuardLevel != 1 || queue[1].Username != "新总督" {
		t.Fatalf("guard upgrade did not preserve current and promote waiting user: %#v", queue)
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

func TestManualDebugGiftUsesQueueIDAndSafeUID(t *testing.T) {
	a := newApp(t.TempDir())
	handler := a.routes()

	manualResponse := httptest.NewRecorder()
	manualRequest := httptest.NewRequest(http.MethodPost, "/api/queue/manual", bytes.NewBufferString(`{"username":"手动测试"}`))
	handler.ServeHTTP(manualResponse, manualRequest)
	if manualResponse.Code != http.StatusOK {
		t.Fatalf("manual add status: %d body=%s", manualResponse.Code, manualResponse.Body.String())
	}
	queue := a.state().Queue
	if len(queue) != 1 || !queue[0].Manual {
		t.Fatalf("manual user was not added: %#v", queue)
	}
	const maxSafeInteger = int64(9007199254740991)
	if queue[0].UID < -maxSafeInteger || queue[0].UID > maxSafeInteger {
		t.Fatalf("manual uid is not JavaScript-safe: %d", queue[0].UID)
	}

	exactUID := int64(-1784515861431009600)
	a.mu.Lock()
	a.queue[0].UID = exactUID
	a.config.GiftPriority.Enabled = true
	a.config.GiftPriority.ThresholdBattery = 99
	a.mu.Unlock()

	giftResponse := httptest.NewRecorder()
	giftBody := bytes.NewBufferString(`{"queueUserId":"` + queue[0].ID + `","uid":-1784515861431009500,"giftName":"小礼物","battery":50}`)
	giftRequest := httptest.NewRequest(http.MethodPost, "/api/debug/gift", giftBody)
	handler.ServeHTTP(giftResponse, giftRequest)
	if giftResponse.Code != http.StatusOK {
		t.Fatalf("debug gift status: %d body=%s", giftResponse.Code, giftResponse.Body.String())
	}
	queue = a.state().Queue
	if len(queue) != 1 || queue[0].UID != exactUID || queue[0].Priority || !queue[0].HasGift || queue[0].GiftBattery != 50 {
		t.Fatalf("below-threshold debug gift was not recorded on the exact manual user: %#v", queue)
	}

	defaultResponse := httptest.NewRecorder()
	defaultBody := bytes.NewBufferString(`{"queueUserId":"` + queue[0].ID + `","giftName":"默认门槛礼物"}`)
	defaultRequest := httptest.NewRequest(http.MethodPost, "/api/debug/gift", defaultBody)
	handler.ServeHTTP(defaultResponse, defaultRequest)
	if defaultResponse.Code != http.StatusOK {
		t.Fatalf("default debug gift status: %d body=%s", defaultResponse.Code, defaultResponse.Body.String())
	}
	queue = a.state().Queue
	if len(queue) != 1 || !queue[0].Priority || queue[0].GiftBattery != 149 || queue[0].PriorityGiftBattery != 99 {
		t.Fatalf("blank debug gift did not use the configured threshold: %#v", queue)
	}
}

func TestLegacyQueueGiftFieldsMigrate(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir)
	snapshot := queueSnapshot{
		Date:  time.Now().Format("2006-01-02"),
		Queue: []QueueUser{{ID: "legacy-gift", UID: 1, Username: "旧礼物用户", Priority: true, GiftName: "旧礼物", GiftBattery: 123}},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.queuePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded := newApp(dir)
	queue := reloaded.state().Queue
	if len(queue) != 1 || !queue[0].HasGift || queue[0].PriorityGiftBattery != 123 || queue[0].GiftBattery != 123 {
		t.Fatalf("legacy gift fields were not migrated: %#v", queue)
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

func TestPaidGiftQueueAndPriority(t *testing.T) {
	a := newApp(t.TempDir())
	a.processMessage(ChatMessage{UID: 1, Username: "当前", Text: "排队"})
	a.mu.Lock()
	a.config.GiftPriority.PaidQueueEnabled = true
	a.config.GiftPriority.QueueThresholdBattery = 100
	a.config.GiftPriority.Enabled = true
	a.config.GiftPriority.ThresholdBattery = 300
	a.config.Eligibility = QueueEligibilityConfig{FanMedalEnabled: true, FanMedalLevel: 100, GuardEnabled: true}
	a.mu.Unlock()

	a.processGift(GiftMessage{EventID: "paid-queue-1", UID: 2, Username: "礼物排队", GiftName: "礼物", CoinType: "gold", Battery: 100})
	queue := a.state().Queue
	if len(queue) != 2 || queue[1].UID != 2 || queue[1].Priority || !queue[1].HasGift || queue[1].GiftBattery != 100 {
		t.Fatalf("paid gift queue failed: %#v", queue)
	}

	a.processGift(GiftMessage{EventID: "paid-queue-2", UID: 3, Username: "礼物插队", GiftName: "大礼物", CoinType: "gold", Battery: 300})
	queue = a.state().Queue
	if len(queue) != 3 || queue[1].UID != 3 || !queue[1].Priority || !queue[1].HasGift || queue[1].GiftBattery != 300 || queue[1].PriorityGiftBattery != 300 || queue[2].UID != 2 {
		t.Fatalf("paid gift queue priority failed: %#v", queue)
	}

	a.processGift(GiftMessage{EventID: "paid-queue-3", UID: 4, Username: "未达门槛", GiftName: "小礼物", CoinType: "gold", Battery: 99})
	if len(a.state().Queue) != 3 {
		t.Fatalf("below-threshold gift entered queue: %#v", a.state().Queue)
	}
}

func TestManualUserCanReceiveGiftWithoutDuplicate(t *testing.T) {
	a := newApp(t.TempDir())
	ok, detail := a.addUser(ChatMessage{UID: -123, Username: "手动用户", Manual: true})
	if !ok {
		t.Fatalf("manual user was not added: %s", detail)
	}
	a.mu.Lock()
	a.config.GiftPriority.PaidQueueEnabled = true
	a.config.GiftPriority.QueueThresholdBattery = 100
	a.config.GiftPriority.Enabled = true
	a.config.GiftPriority.ThresholdBattery = 100
	a.mu.Unlock()

	a.processGift(GiftMessage{EventID: "manual-gift", UID: -123, Username: "手动用户", GiftName: "测试礼物", CoinType: "gold", Battery: 100})
	queue := a.state().Queue
	if len(queue) != 1 || queue[0].UID != -123 || !queue[0].Manual || !queue[0].Priority || queue[0].GiftBattery != 100 {
		t.Fatalf("manual gift should update the existing user without duplication: %#v", queue)
	}
}

func TestQueuedGiftDisplayAndSingleGiftPriority(t *testing.T) {
	a := newApp(t.TempDir())
	a.processMessage(ChatMessage{UID: 1, Username: "当前", Text: "排队"})
	a.processMessage(ChatMessage{UID: 2, Username: "送礼用户", Text: "排队"})
	a.mu.Lock()
	a.config.GiftPriority.Enabled = true
	a.config.GiftPriority.ThresholdBattery = 100
	a.mu.Unlock()

	a.processGift(GiftMessage{EventID: "display-small-1", UID: 2, Username: "送礼用户", GiftName: "小礼物", GiftIcon: "small.png", CoinType: "gold", Battery: 60})
	queue := a.state().Queue
	if len(queue) != 2 || queue[1].Priority || !queue[1].HasGift || queue[1].GiftBattery != 60 || queue[1].GiftIcon != "small.png" {
		t.Fatalf("below-threshold paid gift was not displayed: %#v", queue)
	}

	a.processGift(GiftMessage{EventID: "display-free", UID: 2, Username: "送礼用户", GiftName: "免费礼物", GiftIcon: "free.png", CoinType: "silver", Battery: 999})
	queue = a.state().Queue
	if queue[1].Priority || queue[1].GiftBattery != 60 || queue[1].GiftName != "免费礼物" || queue[1].GiftIcon != "free.png" {
		t.Fatalf("free gift should update the latest gift without adding battery: %#v", queue[1])
	}

	a.processGift(GiftMessage{EventID: "display-small-2", UID: 2, Username: "送礼用户", GiftName: "另一个小礼物", GiftIcon: "small-2.png", CoinType: "gold", Battery: 50})
	queue = a.state().Queue
	if queue[1].Priority || queue[1].GiftBattery != 110 {
		t.Fatalf("cumulative small gifts triggered priority: %#v", queue[1])
	}

	a.processGift(GiftMessage{EventID: "display-priority", UID: 2, Username: "送礼用户", GiftName: "达标礼物", GiftIcon: "priority.png", CoinType: "gold", Battery: 100})
	queue = a.state().Queue
	if !queue[1].Priority || queue[1].GiftBattery != 210 || queue[1].PriorityGiftBattery != 100 || queue[1].GiftIcon != "priority.png" {
		t.Fatalf("single qualifying gift did not trigger priority independently: %#v", queue[1])
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
	if queue[1].UID != 3 || queue[1].GiftBattery != 600 || queue[1].PriorityGiftBattery != 500 {
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
	if !cfg.Overlay.ShowGiftBattery || cfg.Overlay.GiftBatterySize != 14 {
		t.Fatalf("gift battery display defaults not added: %#v", cfg.Overlay)
	}
}

func TestV14ConfigMigratesGiftBatteryDisplay(t *testing.T) {
	cfg := defaultConfig()
	cfg.SchemaVersion = 14
	cfg.Overlay.ShowGiftBattery = false
	cfg.Overlay.GiftBatterySize = 0
	applyConfigDefaults(&cfg)
	if !cfg.Overlay.ShowGiftBattery || cfg.Overlay.GiftBatterySize != 14 {
		t.Fatalf("gift battery display not migrated: %#v", cfg.Overlay)
	}

	cfg.Overlay.ShowGiftBattery = false
	applyConfigDefaults(&cfg)
	if cfg.Overlay.ShowGiftBattery {
		t.Fatal("current gift battery visibility setting was overwritten")
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
