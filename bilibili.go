package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const biliUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36"

type ChatMessage struct {
	UID        int64  `json:"uid"`
	Username   string `json:"username"`
	Avatar     string `json:"avatar,omitempty"`
	Text       string `json:"text"`
	MedalLevel int    `json:"medalLevel,omitempty"`
	Manual     bool   `json:"manual,omitempty"`
	ReceivedAt int64  `json:"receivedAt"`
}

type GiftMessage struct {
	EventID    string  `json:"eventId,omitempty"`
	UID        int64   `json:"uid"`
	Username   string  `json:"username"`
	Avatar     string  `json:"avatar,omitempty"`
	GiftID     int64   `json:"giftId,omitempty"`
	GiftName   string  `json:"giftName"`
	GiftIcon   string  `json:"giftIcon,omitempty"`
	Num        int64   `json:"num"`
	CoinType   string  `json:"coinType"`
	TotalCoin  int64   `json:"totalCoin"`
	Battery    float64 `json:"battery"`
	ReceivedAt int64   `json:"receivedAt"`
}

type ConnectionUpdate struct {
	Status     string
	Detail     string
	RoomID     int64
	RoomTitle  string
	AnchorName string
}

type BiliClient struct {
	httpClient *http.Client
	auth       BiliAuth
}

func NewBiliClient(auth ...BiliAuth) *BiliClient {
	client := &BiliClient{httpClient: &http.Client{Timeout: 12 * time.Second}}
	if len(auth) > 0 {
		client.auth = auth[0]
	}
	return client
}

type biliSession struct {
	RoomID     int64
	UID        int64
	Token      string
	Cookie     string
	Buvid3     string
	Buvid4     string
	WSURL      string
	RoomTitle  string
	AnchorName string
}

func (b *BiliClient) Run(ctx context.Context, roomInput string, onStatus func(ConnectionUpdate), onMessage func(ChatMessage), onGift func(GiftMessage)) {
	inputID, err := strconv.ParseInt(roomInput, 10, 64)
	if err != nil || inputID <= 0 {
		onStatus(ConnectionUpdate{Status: "error", Detail: "直播间号格式不正确"})
		return
	}

	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		onStatus(ConnectionUpdate{Status: "connecting", Detail: "正在获取直播间信息"})
		session, err := b.prepareSession(ctx, inputID)
		if err != nil {
			attempt++
			delay := reconnectDelay(attempt)
			onStatus(ConnectionUpdate{Status: "reconnecting", Detail: fmt.Sprintf("获取直播间信息失败：%v；%s 后重试", err, delay)})
			if !sleepContext(ctx, delay) {
				return
			}
			continue
		}

		onStatus(ConnectionUpdate{
			Status:     "connecting",
			Detail:     "正在连接弹幕服务器",
			RoomID:     session.RoomID,
			RoomTitle:  session.RoomTitle,
			AnchorName: session.AnchorName,
		})
		err = b.connectOnce(ctx, session, func(detail string) {
			onStatus(ConnectionUpdate{
				Status:     "connected",
				Detail:     detail,
				RoomID:     session.RoomID,
				RoomTitle:  session.RoomTitle,
				AnchorName: session.AnchorName,
			})
		}, onMessage, onGift)
		if ctx.Err() != nil {
			return
		}
		attempt++
		delay := reconnectDelay(attempt)
		onStatus(ConnectionUpdate{
			Status:     "reconnecting",
			Detail:     fmt.Sprintf("连接中断：%v；%s 后重试", err, delay),
			RoomID:     session.RoomID,
			RoomTitle:  session.RoomTitle,
			AnchorName: session.AnchorName,
		})
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	seconds := math.Min(30, 2*math.Pow(1.6, float64(attempt-1)))
	return time.Duration(seconds * float64(time.Second))
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (b *BiliClient) prepareSession(ctx context.Context, inputID int64) (biliSession, error) {
	if b.auth.UID <= 0 || strings.TrimSpace(b.auth.Cookie) == "" {
		return biliSession{}, errors.New("请先在控制台扫码登录 B 站")
	}
	cookies := cookieMap(b.auth.Cookie)
	buvid3 := cookies["buvid3"]
	buvid4 := cookies["buvid4"]
	if buvid3 == "" || buvid4 == "" {
		fetched3, fetched4, _ := b.fetchBuvid(ctx)
		if buvid3 == "" {
			buvid3 = fetched3
		}
		if buvid4 == "" {
			buvid4 = fetched4
		}
	}
	cookie := mergeCookie(b.auth.Cookie, map[string]string{"buvid3": buvid3, "buvid4": buvid4})

	var roomResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RoomID int64 `json:"room_id"`
		} `json:"data"`
	}
	roomURL := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/room_init?id=%d", inputID)
	if err := b.getJSON(ctx, roomURL, cookie, &roomResp); err != nil {
		return biliSession{}, err
	}
	if roomResp.Code != 0 || roomResp.Data.RoomID <= 0 {
		return biliSession{}, fmt.Errorf("直播间解析失败：%s", roomResp.Message)
	}
	realRoomID := roomResp.Data.RoomID

	var danmuResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Token    string `json:"token"`
			HostList []struct {
				Host    string `json:"host"`
				Port    int    `json:"port"`
				WSPort  int    `json:"ws_port"`
				WSSPort int    `json:"wss_port"`
			} `json:"host_list"`
		} `json:"data"`
	}
	danmuParams := url.Values{
		"id":           {strconv.FormatInt(realRoomID, 10)},
		"type":         {"0"},
		"web_location": {"444.8"},
	}
	danmuURL, err := b.makeWBIURL(ctx, "https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo", danmuParams, cookie)
	if err != nil {
		return biliSession{}, fmt.Errorf("WBI 签名失败：%w", err)
	}
	if err := b.getJSON(ctx, danmuURL, cookie, &danmuResp); err != nil {
		return biliSession{}, err
	}
	if danmuResp.Code != 0 {
		return biliSession{}, fmt.Errorf("弹幕连接信息获取失败：%s", danmuResp.Message)
	}

	wsURL := "wss://broadcastlv.chat.bilibili.com/sub"
	if len(danmuResp.Data.HostList) > 0 {
		h := danmuResp.Data.HostList[0]
		if h.Host != "" {
			port := h.WSSPort
			if port <= 0 {
				port = 443
			}
			wsURL = fmt.Sprintf("wss://%s:%d/sub", h.Host, port)
		}
	}

	title, anchor := b.fetchRoomMetadata(ctx, realRoomID, cookie)
	return biliSession{
		RoomID:     realRoomID,
		UID:        b.auth.UID,
		Token:      danmuResp.Data.Token,
		Cookie:     cookie,
		Buvid3:     buvid3,
		Buvid4:     buvid4,
		WSURL:      wsURL,
		RoomTitle:  title,
		AnchorName: anchor,
	}, nil
}

func buildBiliCookie(b3, b4 string) string {
	parts := make([]string, 0, 2)
	if b3 != "" {
		parts = append(parts, "buvid3="+b3)
	}
	if b4 != "" {
		parts = append(parts, "buvid4="+b4)
	}
	return strings.Join(parts, "; ")
}

func (b *BiliClient) fetchBuvid(ctx context.Context) (string, string, error) {
	var resp struct {
		Code int `json:"code"`
		Data struct {
			B3 string `json:"b_3"`
			B4 string `json:"b_4"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, "https://api.bilibili.com/x/frontend/finger/spi", "", &resp); err != nil {
		return "", "", err
	}
	if resp.Code != 0 {
		return "", "", fmt.Errorf("buvid 获取失败")
	}
	return resp.Data.B3, resp.Data.B4, nil
}

var wbiMixinKeyEncTab = [...]int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

func (b *BiliClient) makeWBIURL(ctx context.Context, endpoint string, params url.Values, cookie string) (string, error) {
	mixinKey, err := b.fetchWBIMixinKey(ctx, cookie)
	if err != nil {
		return "", err
	}

	signed := make(url.Values, len(params)+2)
	for key, values := range params {
		for _, value := range values {
			signed.Add(key, sanitizeWBIValue(value))
		}
	}
	signed.Set("wts", strconv.FormatInt(time.Now().Unix(), 10))
	query := signed.Encode()
	digest := md5.Sum([]byte(query + mixinKey))
	signed.Set("w_rid", fmt.Sprintf("%x", digest))
	return endpoint + "?" + signed.Encode(), nil
}

func (b *BiliClient) fetchWBIMixinKey(ctx context.Context, cookie string) (string, error) {
	var resp struct {
		Code int `json:"code"`
		Data struct {
			WBIImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	if err := b.getJSON(ctx, "https://api.bilibili.com/x/web-interface/nav", cookie, &resp); err != nil {
		return "", err
	}
	imgKey := wbiFilenameKey(resp.Data.WBIImg.ImgURL)
	subKey := wbiFilenameKey(resp.Data.WBIImg.SubURL)
	if imgKey == "" || subKey == "" {
		return "", fmt.Errorf("未取得 WBI 密钥")
	}
	origin := imgKey + subKey
	var builder strings.Builder
	for _, index := range wbiMixinKeyEncTab {
		if index < len(origin) {
			builder.WriteByte(origin[index])
		}
	}
	key := builder.String()
	if len(key) > 32 {
		key = key[:32]
	}
	if len(key) != 32 {
		return "", fmt.Errorf("WBI 密钥长度异常")
	}
	return key, nil
}

func wbiFilenameKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := parsed.Path
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		name = name[:dot]
	}
	return name
}

func sanitizeWBIValue(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		default:
			return r
		}
	}, value)
}

func (b *BiliClient) fetchRoomMetadata(ctx context.Context, roomID int64, cookie string) (string, string) {
	var resp struct {
		Code int `json:"code"`
		Data struct {
			RoomInfo struct {
				Title string `json:"title"`
			} `json:"room_info"`
			AnchorInfo struct {
				BaseInfo struct {
					Uname string `json:"uname"`
				} `json:"base_info"`
			} `json:"anchor_info"`
		} `json:"data"`
	}
	u := fmt.Sprintf("https://api.live.bilibili.com/xlive/web-room/v1/index/getInfoByRoom?room_id=%d", roomID)
	if err := b.getJSON(ctx, u, cookie, &resp); err != nil || resp.Code != 0 {
		return "", ""
	}
	return resp.Data.RoomInfo.Title, resp.Data.AnchorInfo.BaseInfo.Uname
}

func (b *BiliClient) getJSON(ctx context.Context, endpoint, cookie string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	setBiliHeaders(req, cookie)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func setBiliHeaders(req *http.Request, cookie string) {
	req.Header.Set("User-Agent", biliUserAgent)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

func (b *BiliClient) connectOnce(ctx context.Context, session biliSession, onConnected func(string), onMessage func(ChatMessage), onGift func(GiftMessage)) error {
	ws, err := dialWebSocket(ctx, session.WSURL)
	if err != nil {
		return err
	}
	defer ws.Close()

	auth := map[string]any{
		"uid":      session.UID,
		"roomid":   session.RoomID,
		"protover": 2,
		"platform": "web",
		"type":     2,
	}
	if session.Token != "" {
		auth["key"] = session.Token
	}
	if session.Buvid3 != "" {
		auth["buvid"] = session.Buvid3
	}
	body, _ := json.Marshal(auth)
	if err := ws.WriteBinary(encodeBiliPacket(7, 1, body)); err != nil {
		return err
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = ws.Close()
				return
			case <-done:
				return
			case <-ticker.C:
				_ = ws.WriteBinary(encodeBiliPacket(2, 1, nil))
			}
		}
	}()

	connectedAnnounced := false
	for {
		payload, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		err = decodeBiliPackets(payload, func(operation, protocol int, body []byte) error {
			switch operation {
			case 8:
				var authReply struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				if len(bytes.TrimSpace(body)) > 0 {
					if err := json.Unmarshal(body, &authReply); err != nil {
						return fmt.Errorf("弹幕鉴权回复解析失败：%w", err)
					}
					if authReply.Code != 0 {
						return fmt.Errorf("弹幕鉴权失败：code=%d %s", authReply.Code, authReply.Message)
					}
				}
				_ = ws.WriteBinary(encodeBiliPacket(2, 1, nil))
				if !connectedAnnounced {
					connectedAnnounced = true
					onConnected(fmt.Sprintf("已登录并连接直播间 %d", session.RoomID))
				}
			case 5:
				for _, obj := range decodeJSONObjects(body) {
					if msg, ok := parseDanmuMessage(obj); ok {
						msg.ReceivedAt = time.Now().UnixMilli()
						onMessage(msg)
					}
					if gift, ok := parseGiftMessage(obj); ok {
						gift.ReceivedAt = time.Now().UnixMilli()
						onGift(gift)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
}

func encodeBiliPacket(operation, protocol int, body []byte) []byte {
	packet := make([]byte, 16+len(body))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(packet)))
	binary.BigEndian.PutUint16(packet[4:6], 16)
	binary.BigEndian.PutUint16(packet[6:8], uint16(protocol))
	binary.BigEndian.PutUint32(packet[8:12], uint32(operation))
	binary.BigEndian.PutUint32(packet[12:16], 1)
	copy(packet[16:], body)
	return packet
}

func decodeBiliPackets(data []byte, handler func(operation, protocol int, body []byte) error) error {
	for len(data) >= 16 {
		packetLen := int(binary.BigEndian.Uint32(data[0:4]))
		headerLen := int(binary.BigEndian.Uint16(data[4:6]))
		protocol := int(binary.BigEndian.Uint16(data[6:8]))
		operation := int(binary.BigEndian.Uint32(data[8:12]))
		if packetLen < headerLen || headerLen < 16 || packetLen > len(data) {
			return fmt.Errorf("无效弹幕数据包")
		}
		body := data[headerLen:packetLen]
		if protocol == 2 {
			zr, err := zlib.NewReader(bytes.NewReader(body))
			if err != nil {
				return err
			}
			inflated, err := io.ReadAll(io.LimitReader(zr, 64<<20))
			_ = zr.Close()
			if err != nil {
				return err
			}
			if err := decodeBiliPackets(inflated, handler); err != nil {
				return err
			}
		} else {
			if err := handler(operation, protocol, body); err != nil {
				return err
			}
		}
		data = data[packetLen:]
	}
	return nil
}

func decodeJSONObjects(body []byte) []map[string]any {
	body = bytes.Trim(body, "\x00\r\n \t")
	if len(body) == 0 {
		return nil
	}
	var one map[string]any
	if json.Unmarshal(body, &one) == nil {
		return []map[string]any{one}
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	var out []map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			break
		}
		out = append(out, obj)
	}
	return out
}

func parseDanmuMessage(obj map[string]any) (ChatMessage, bool) {
	cmd, _ := obj["cmd"].(string)
	if !strings.HasPrefix(cmd, "DANMU_MSG") {
		return ChatMessage{}, false
	}

	info, _ := obj["info"].([]any)
	if len(info) >= 3 {
		text, _ := info[1].(string)
		user, _ := info[2].([]any)
		if len(user) >= 2 {
			uid := numberToInt64(user[0])
			username, _ := user[1].(string)
			medal := 0
			if len(info) > 3 {
				if medalData, ok := info[3].([]any); ok && len(medalData) > 0 {
					medal = int(numberToInt64(medalData[0]))
				}
			}
			avatar := extractDanmuAvatar(info)
			if uid != 0 && username != "" {
				return ChatMessage{UID: uid, Username: username, Avatar: avatar, Text: text, MedalLevel: medal}, true
			}
		}
	}

	// Some newer events expose normalized fields under data.
	if data, ok := obj["data"].(map[string]any); ok {
		text := firstString(data, "msg", "message", "text")
		username := firstString(data, "uname", "username", "name")
		uid := firstInt64(data, "uid", "mid")
		avatar := firstString(data, "face", "avatar")
		if avatar == "" {
			avatar = nestedString(data, "sender_uinfo", "base", "face")
		}
		if uid != 0 && username != "" {
			return ChatMessage{UID: uid, Username: username, Avatar: normalizeBiliImageURL(avatar), Text: text}, true
		}
	}
	return ChatMessage{}, false
}

func extractDanmuAvatar(info []any) string {
	// Web DANMU_MSG usually stores sender details in info[0][15].user.base.face.
	if len(info) > 0 {
		if meta, ok := info[0].([]any); ok && len(meta) > 15 {
			if ext, ok := meta[15].(map[string]any); ok {
				if face := nestedString(ext, "user", "base", "face"); face != "" {
					return normalizeBiliImageURL(face)
				}
				if face := firstString(ext, "face", "avatar"); face != "" {
					return normalizeBiliImageURL(face)
				}
			}
		}
	}
	// Some variants expose the extension object at info[15].
	if len(info) > 15 {
		if ext, ok := info[15].(map[string]any); ok {
			if face := nestedString(ext, "user", "base", "face"); face != "" {
				return normalizeBiliImageURL(face)
			}
		}
	}
	return ""
}

func normalizeBiliImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

func parseGiftMessage(obj map[string]any) (GiftMessage, bool) {
	cmd, _ := obj["cmd"].(string)
	if cmd != "SEND_GIFT" {
		return GiftMessage{}, false
	}
	data, ok := obj["data"].(map[string]any)
	if !ok {
		return GiftMessage{}, false
	}
	uid := firstInt64(data, "uid")
	username := firstString(data, "uname", "username")
	giftName := firstString(data, "giftName", "gift_name")
	giftID := firstInt64(data, "giftId", "gift_id")
	num := firstInt64(data, "num")
	if num <= 0 {
		num = 1
	}
	totalCoin := firstInt64(data, "total_coin")
	if totalCoin <= 0 {
		price := firstInt64(data, "price", "discount_price")
		totalCoin = price * num
	}
	coinType := strings.ToLower(firstString(data, "coin_type"))
	avatar := firstString(data, "face")
	if avatar == "" {
		avatar = nestedString(data, "sender_uinfo", "base", "face")
	}
	avatar = normalizeBiliImageURL(avatar)
	giftIcon := nestedString(data, "gift_info", "img_basic")
	if giftIcon == "" {
		giftIcon = nestedString(data, "gift_info", "webp")
	}
	if giftIcon == "" {
		giftIcon = firstString(data, "gift_icon", "img_basic", "webp")
	}
	giftIcon = normalizeBiliImageURL(giftIcon)
	eventID := firstString(data, "tid", "batch_combo_id", "combo_id", "rnd")
	if eventID == "" {
		if numericID := firstInt64(data, "tid", "rnd"); numericID != 0 {
			eventID = strconv.FormatInt(numericID, 10)
		}
	}
	if eventID == "" {
		eventID = fmt.Sprintf("%d:%d:%d:%d:%d", uid, giftID, num, totalCoin, firstInt64(data, "timestamp"))
	}
	if uid <= 0 || username == "" || giftName == "" {
		return GiftMessage{}, false
	}
	return GiftMessage{
		EventID: eventID, UID: uid, Username: username, Avatar: avatar,
		GiftID: giftID, GiftName: giftName, GiftIcon: giftIcon, Num: num,
		CoinType: coinType, TotalCoin: totalCoin, Battery: float64(totalCoin) / 100,
	}, true
}

func nestedString(root map[string]any, keys ...string) string {
	var current any = root
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	s, _ := current.(string)
	return s
}

func numberToInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok {
			return s
		}
	}
	return ""
}

func firstInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if n := numberToInt64(m[key]); n != 0 {
			return n
		}
	}
	return 0
}

// Minimal RFC 6455 client. It keeps the executable free from external modules.
type wsClient struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	closed  bool
}

func dialWebSocket(ctx context.Context, rawURL string) (*wsClient, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket scheme: %s", u.Scheme)
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "wss" {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: u.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		_ = conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	req := strings.Builder{}
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&req, "Host: %s\r\n", u.Host)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	req.WriteString("Origin: https://live.bilibili.com\r\n")
	fmt.Fprintf(&req, "User-Agent: %s\r\n\r\n", biliUserAgent)
	if _, err := io.WriteString(conn, req.String()); err != nil {
		_ = conn.Close()
		return nil, err
	}

	reader := bufio.NewReaderSize(conn, 64<<10)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake HTTP %d", resp.StatusCode)
	}
	accept := resp.Header.Get("Sec-WebSocket-Accept")
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expected := base64.StdEncoding.EncodeToString(h[:])
	if accept != expected {
		_ = conn.Close()
		return nil, errors.New("websocket handshake validation failed")
	}

	ws := &wsClient{conn: conn, reader: reader}
	go func() {
		<-ctx.Done()
		_ = ws.Close()
	}()
	return ws, nil
}

func (w *wsClient) WriteBinary(payload []byte) error {
	return w.writeFrame(0x2, payload)
}

func (w *wsClient) writeFrame(opcode byte, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.closed {
		return net.ErrClosed
	}

	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode)
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, 0x80|byte(length))
	case length <= 65535:
		header = append(header, 0x80|126, byte(length>>8), byte(length))
	default:
		header = append(header, 0x80|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(length))
		header = append(header, b[:]...)
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *wsClient) ReadMessage() ([]byte, error) {
	var message []byte
	var activeOpcode byte
	for {
		first, err := w.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		second, err := w.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		fin := first&0x80 != 0
		opcode := first & 0x0f
		masked := second&0x80 != 0
		length := uint64(second & 0x7f)
		switch length {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.reader, b[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.reader, b[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(b[:])
		}
		if length > 64<<20 {
			return nil, fmt.Errorf("websocket frame too large: %d", length)
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.reader, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, int(length))
		if _, err := io.ReadFull(w.reader, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			_ = w.writeFrame(0xA, payload)
			continue
		case 0xA:
			continue
		case 0x1, 0x2:
			activeOpcode = opcode
			message = append(message[:0], payload...)
		case 0x0:
			if activeOpcode == 0 {
				return nil, errors.New("unexpected websocket continuation frame")
			}
			message = append(message, payload...)
		default:
			continue
		}
		if fin && (activeOpcode == 0x1 || activeOpcode == 0x2) {
			return message, nil
		}
	}
}

func (w *wsClient) Close() error {
	w.writeMu.Lock()
	if w.closed {
		w.writeMu.Unlock()
		return nil
	}
	w.closed = true
	w.writeMu.Unlock()
	return w.conn.Close()
}
