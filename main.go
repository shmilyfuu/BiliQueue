package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type OverlayStyle struct {
	Height              int     `json:"height"`
	FontSize            int     `json:"fontSize,omitempty"` // legacy global font size
	CurrentFontSize     int     `json:"currentFontSize"`
	CurrentTextColor    string  `json:"currentTextColor"`
	CurrentFontWeight   int     `json:"currentFontWeight"`
	CurrentTextAlign    string  `json:"currentTextAlign"`
	QueueFontSize       int     `json:"queueFontSize"`
	QueueTextColor      string  `json:"queueTextColor"`
	QueueFontWeight     int     `json:"queueFontWeight"`
	InfoFontSize        int     `json:"infoFontSize"`
	InfoTextColor       string  `json:"infoTextColor"`
	InfoFontWeight      int     `json:"infoFontWeight"`
	InfoTextAlign       string  `json:"infoTextAlign"`
	Speed               float64 `json:"speed"`
	Background          string  `json:"background"`
	Opacity             float64 `json:"opacity"`
	Radius              int     `json:"radius"`
	ShowAvatar          bool    `json:"showAvatar"`
	ShowCount           bool    `json:"showCount"`
	ShowRules           bool    `json:"showRules"`
	ShowGiftIcon        bool    `json:"showGiftIcon"`
	ScrollMode          string  `json:"scrollMode"`
	ShortAlign          string  `json:"shortAlign"`
	CurrentWidth        int     `json:"currentWidth"`
	QueueWidth          int     `json:"queueWidth"`
	InfoWidth           int     `json:"infoWidth"`
	LegacyCountWidth    int     `json:"countWidth,omitempty"`
	QueueLineGap        int     `json:"queueLineGap"`
	InfoLineGap         int     `json:"infoLineGap"`
	DoubleLineThreshold int     `json:"doubleLineThreshold"`
	InfoText            string  `json:"infoText"`
	EmptyText           string  `json:"emptyText"`
	QueueEmptyText      string  `json:"queueEmptyText"`
}

type GiftPriorityConfig struct {
	Enabled          bool    `json:"enabled"`
	ThresholdBattery float64 `json:"thresholdBattery"`
	SortByValue      bool    `json:"sortByValue"`
}

type Config struct {
	SchemaVersion int                `json:"schemaVersion"`
	RoomID        string             `json:"roomId"`
	JoinCommand   string             `json:"joinCommand"`
	CancelCommand string             `json:"cancelCommand"`
	MaxQueue      int                `json:"maxQueue"`
	GiftPriority  GiftPriorityConfig `json:"giftPriority"`
	Overlay       OverlayStyle       `json:"overlay"`
}

type QueueUser struct {
	ID          string  `json:"id"`
	UID         int64   `json:"uid"`
	Username    string  `json:"username"`
	Avatar      string  `json:"avatar,omitempty"`
	MedalLevel  int     `json:"medalLevel,omitempty"`
	JoinedAt    int64   `json:"joinedAt"`
	Manual      bool    `json:"manual,omitempty"`
	Priority    bool    `json:"priority,omitempty"`
	GiftName    string  `json:"giftName,omitempty"`
	GiftIcon    string  `json:"giftIcon,omitempty"`
	GiftBattery float64 `json:"giftBattery,omitempty"`
	PriorityAt  int64   `json:"priorityAt,omitempty"`
}

type PublicState struct {
	Config           Config       `json:"config"`
	LoginStatus      string       `json:"loginStatus"`
	LoginDetail      string       `json:"loginDetail"`
	LoginUID         int64        `json:"loginUid,omitempty"`
	LoginName        string       `json:"loginName,omitempty"`
	Queue            []QueueUser  `json:"queue"`
	Paused           bool         `json:"paused"`
	ConnectionStatus string       `json:"connectionStatus"`
	ConnectionDetail string       `json:"connectionDetail"`
	ResolvedRoomID   int64        `json:"resolvedRoomId,omitempty"`
	RoomTitle        string       `json:"roomTitle,omitempty"`
	AnchorName       string       `json:"anchorName,omitempty"`
	LastMessage      *ChatMessage `json:"lastMessage,omitempty"`
	LastGift         *GiftMessage `json:"lastGift,omitempty"`
	Version          string       `json:"version"`
}

type queueSnapshot struct {
	Date  string      `json:"date"`
	Queue []QueueUser `json:"queue"`
}

type App struct {
	mu sync.RWMutex

	config Config
	auth   BiliAuth
	queue  []QueueUser
	paused bool

	loginStatus      string
	loginDetail      string
	connectionStatus string
	connectionDetail string
	resolvedRoomID   int64
	roomTitle        string
	anchorName       string
	lastMessage      *ChatMessage
	lastGift         *GiftMessage
	giftEvents       map[string]int64

	dataDir string
	clients map[chan []byte]struct{}

	connectionCancel     context.CancelFunc
	connectionGeneration uint64
	messageSeq           atomic.Uint64
}

const version = "0.1.6"

func defaultConfig() Config {
	return Config{
		SchemaVersion: 4,
		RoomID:        "",
		JoinCommand:   "排队",
		CancelCommand: "取消排队",
		MaxQueue:      100,
		GiftPriority:  GiftPriorityConfig{Enabled: true, ThresholdBattery: 100, SortByValue: false},
		Overlay: OverlayStyle{
			Height:              120,
			FontSize:            24,
			CurrentFontSize:     24,
			CurrentTextColor:    "#ffffff",
			CurrentFontWeight:   600,
			CurrentTextAlign:    "left",
			QueueFontSize:       24,
			QueueTextColor:      "#ffffff",
			QueueFontWeight:     500,
			InfoFontSize:        18,
			InfoTextColor:       "#ffffff",
			InfoFontWeight:      500,
			InfoTextAlign:       "left",
			Speed:               40,
			Background:          "#000000",
			Opacity:             0.45,
			Radius:              16,
			ShowAvatar:          true,
			ShowCount:           true,
			ShowRules:           true,
			ShowGiftIcon:        true,
			ScrollMode:          "continuous",
			ShortAlign:          "center",
			CurrentWidth:        300,
			QueueWidth:          1220,
			InfoWidth:           400,
			QueueLineGap:        8,
			InfoLineGap:         4,
			DoubleLineThreshold: 8,
			InfoText:            "弹幕发送“排队”加入\n达到礼物门槛可进入优先队列",
			EmptyText:           "排队空闲中",
			QueueEmptyText:      "空",
		},
	}
}

func newApp(dataDir string) *App {
	a := &App{
		config:           defaultConfig(),
		loginStatus:      "logged_out",
		loginDetail:      "尚未登录 B 站",
		connectionStatus: "disconnected",
		connectionDetail: "未连接",
		dataDir:          dataDir,
		clients:          make(map[chan []byte]struct{}),
		giftEvents:       make(map[string]int64),
	}
	_ = os.MkdirAll(dataDir, 0o755)
	a.loadConfig()
	a.loadAuth()
	a.loadTodayQueue()
	return a
}

func (a *App) stateLocked() PublicState {
	queue := append([]QueueUser{}, a.queue...)
	cfg := a.config
	var msg *ChatMessage
	if a.lastMessage != nil {
		cp := *a.lastMessage
		msg = &cp
	}
	var gift *GiftMessage
	if a.lastGift != nil {
		cp := *a.lastGift
		gift = &cp
	}
	return PublicState{
		Config:           cfg,
		LoginStatus:      a.loginStatus,
		LoginDetail:      a.loginDetail,
		LoginUID:         a.auth.UID,
		LoginName:        a.auth.Username,
		Queue:            queue,
		Paused:           a.paused,
		ConnectionStatus: a.connectionStatus,
		ConnectionDetail: a.connectionDetail,
		ResolvedRoomID:   a.resolvedRoomID,
		RoomTitle:        a.roomTitle,
		AnchorName:       a.anchorName,
		LastMessage:      msg,
		LastGift:         gift,
		Version:          version,
	}
}

func (a *App) state() PublicState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stateLocked()
}

func (a *App) broadcast() {
	a.mu.RLock()
	payload, err := json.Marshal(a.stateLocked())
	if err != nil {
		a.mu.RUnlock()
		return
	}
	clients := make([]chan []byte, 0, len(a.clients))
	for ch := range a.clients {
		clients = append(clients, ch)
	}
	a.mu.RUnlock()

	for _, ch := range clients {
		select {
		case ch <- payload:
		default:
		}
	}
}

func (a *App) configPath() string { return filepath.Join(a.dataDir, "config.json") }
func (a *App) authPath() string   { return filepath.Join(a.dataDir, "auth.json") }
func (a *App) queuePath() string  { return filepath.Join(a.dataDir, "queue-today.json") }

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(tmp, path)
}

func (a *App) saveConfig() {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()
	if err := writeJSONAtomic(a.configPath(), cfg); err != nil {
		log.Printf("save config: %v", err)
	}
}

func (a *App) saveQueue() {
	a.mu.RLock()
	snap := queueSnapshot{Date: time.Now().Format("2006-01-02"), Queue: append([]QueueUser(nil), a.queue...)}
	a.mu.RUnlock()
	if err := writeJSONAtomic(a.queuePath(), snap); err != nil {
		log.Printf("save queue: %v", err)
	}
}

func (a *App) loadConfig() {
	data, err := os.ReadFile(a.configPath())
	if err != nil {
		return
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	applyConfigDefaults(&cfg)
	a.config = cfg
}

func applyConfigDefaults(cfg *Config) {
	def := defaultConfig()
	legacyV2 := cfg.SchemaVersion < 2
	legacyV3 := cfg.SchemaVersion < 3
	if strings.TrimSpace(cfg.JoinCommand) == "" {
		cfg.JoinCommand = def.JoinCommand
	}
	if strings.TrimSpace(cfg.CancelCommand) == "" {
		cfg.CancelCommand = def.CancelCommand
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = def.MaxQueue
	}
	if cfg.GiftPriority.ThresholdBattery <= 0 {
		cfg.GiftPriority.ThresholdBattery = def.GiftPriority.ThresholdBattery
	}
	if cfg.Overlay.Height < 48 {
		cfg.Overlay.Height = def.Overlay.Height
	}
	if cfg.Overlay.FontSize < 12 {
		cfg.Overlay.FontSize = def.Overlay.FontSize
	}
	if legacyV3 {
		legacyFontSize := cfg.Overlay.FontSize
		if legacyFontSize < 12 {
			legacyFontSize = def.Overlay.FontSize
		}
		if cfg.Overlay.CurrentFontSize < 12 {
			cfg.Overlay.CurrentFontSize = legacyFontSize
		}
		if cfg.Overlay.QueueFontSize < 12 {
			cfg.Overlay.QueueFontSize = legacyFontSize
		}
		if cfg.Overlay.InfoFontSize < 10 {
			cfg.Overlay.InfoFontSize = def.Overlay.InfoFontSize
		}
	}
	if cfg.Overlay.CurrentFontSize < 12 {
		cfg.Overlay.CurrentFontSize = def.Overlay.CurrentFontSize
	}
	if cfg.Overlay.QueueFontSize < 12 {
		cfg.Overlay.QueueFontSize = def.Overlay.QueueFontSize
	}
	if cfg.Overlay.InfoFontSize < 10 {
		cfg.Overlay.InfoFontSize = def.Overlay.InfoFontSize
	}
	if cfg.Overlay.CurrentTextColor == "" {
		cfg.Overlay.CurrentTextColor = def.Overlay.CurrentTextColor
	}
	if cfg.Overlay.QueueTextColor == "" {
		cfg.Overlay.QueueTextColor = def.Overlay.QueueTextColor
	}
	if cfg.Overlay.InfoTextColor == "" {
		cfg.Overlay.InfoTextColor = def.Overlay.InfoTextColor
	}
	if cfg.Overlay.CurrentFontWeight < 100 || cfg.Overlay.CurrentFontWeight > 900 {
		cfg.Overlay.CurrentFontWeight = def.Overlay.CurrentFontWeight
	}
	if cfg.Overlay.QueueFontWeight < 100 || cfg.Overlay.QueueFontWeight > 900 {
		cfg.Overlay.QueueFontWeight = def.Overlay.QueueFontWeight
	}
	if cfg.Overlay.InfoFontWeight < 100 || cfg.Overlay.InfoFontWeight > 900 {
		cfg.Overlay.InfoFontWeight = def.Overlay.InfoFontWeight
	}
	if cfg.Overlay.CurrentTextAlign != "left" && cfg.Overlay.CurrentTextAlign != "center" && cfg.Overlay.CurrentTextAlign != "right" {
		cfg.Overlay.CurrentTextAlign = def.Overlay.CurrentTextAlign
	}
	if cfg.Overlay.InfoTextAlign != "left" && cfg.Overlay.InfoTextAlign != "center" && cfg.Overlay.InfoTextAlign != "right" {
		cfg.Overlay.InfoTextAlign = def.Overlay.InfoTextAlign
	}
	if cfg.Overlay.Speed < 0 {
		cfg.Overlay.Speed = def.Overlay.Speed
	}
	if cfg.Overlay.Background == "" {
		cfg.Overlay.Background = def.Overlay.Background
	}
	if cfg.Overlay.Opacity < 0 || cfg.Overlay.Opacity > 1 {
		cfg.Overlay.Opacity = def.Overlay.Opacity
	}
	if cfg.Overlay.ScrollMode == "" {
		cfg.Overlay.ScrollMode = def.Overlay.ScrollMode
	}
	if cfg.Overlay.ShortAlign == "" {
		cfg.Overlay.ShortAlign = def.Overlay.ShortAlign
	}
	if cfg.Overlay.CurrentWidth <= 0 {
		cfg.Overlay.CurrentWidth = def.Overlay.CurrentWidth
	}
	if cfg.Overlay.InfoWidth <= 0 {
		if cfg.Overlay.LegacyCountWidth > 0 {
			cfg.Overlay.InfoWidth = cfg.Overlay.LegacyCountWidth
		} else {
			cfg.Overlay.InfoWidth = def.Overlay.InfoWidth
		}
	}
	if cfg.Overlay.QueueWidth <= 0 {
		cfg.Overlay.QueueWidth = def.Overlay.QueueWidth
	}
	if legacyV2 {
		if cfg.Overlay.QueueLineGap == 0 {
			cfg.Overlay.QueueLineGap = def.Overlay.QueueLineGap
		}
		if cfg.Overlay.InfoLineGap == 0 {
			cfg.Overlay.InfoLineGap = def.Overlay.InfoLineGap
		}
		if cfg.Overlay.DoubleLineThreshold == 0 {
			cfg.Overlay.DoubleLineThreshold = def.Overlay.DoubleLineThreshold
		}
		if cfg.Overlay.InfoText == "" {
			cfg.Overlay.InfoText = def.Overlay.InfoText
		}
		if cfg.Overlay.EmptyText == "" {
			cfg.Overlay.EmptyText = def.Overlay.EmptyText
		}
	}
	if legacyV3 && cfg.Overlay.QueueEmptyText == "" {
		cfg.Overlay.QueueEmptyText = def.Overlay.QueueEmptyText
	}
	if cfg.Overlay.QueueLineGap < 0 {
		cfg.Overlay.QueueLineGap = 0
	}
	if cfg.Overlay.InfoLineGap < 0 {
		cfg.Overlay.InfoLineGap = 0
	}
	if cfg.Overlay.DoubleLineThreshold <= 0 {
		cfg.Overlay.DoubleLineThreshold = def.Overlay.DoubleLineThreshold
	}
	if strings.TrimSpace(cfg.Overlay.EmptyText) == "" {
		cfg.Overlay.EmptyText = def.Overlay.EmptyText
	}
	if strings.TrimSpace(cfg.Overlay.QueueEmptyText) == "" {
		cfg.Overlay.QueueEmptyText = def.Overlay.QueueEmptyText
	}
	cfg.Overlay.LegacyCountWidth = 0
	cfg.SchemaVersion = def.SchemaVersion
}

func (a *App) loadAuth() {
	data, err := os.ReadFile(a.authPath())
	if err != nil {
		return
	}
	var auth BiliAuth
	if json.Unmarshal(data, &auth) != nil || auth.UID <= 0 || strings.TrimSpace(auth.Cookie) == "" {
		return
	}
	a.auth = auth
	a.loginStatus = "logged_in"
	if auth.Username != "" {
		a.loginDetail = "已登录：" + auth.Username
	} else {
		a.loginDetail = fmt.Sprintf("已登录 UID %d", auth.UID)
	}
}

func (a *App) saveAuth() error {
	a.mu.RLock()
	auth := a.auth
	a.mu.RUnlock()
	return writeJSONAtomic(a.authPath(), auth)
}

func (a *App) setAuth(auth BiliAuth) error {
	if auth.UID <= 0 || strings.TrimSpace(auth.Cookie) == "" {
		return errors.New("登录数据不完整")
	}
	a.mu.Lock()
	a.auth = auth
	a.loginStatus = "logged_in"
	if auth.Username != "" {
		a.loginDetail = "已登录：" + auth.Username
	} else {
		a.loginDetail = fmt.Sprintf("已登录 UID %d", auth.UID)
	}
	a.mu.Unlock()
	if err := a.saveAuth(); err != nil {
		return err
	}
	a.broadcast()
	return nil
}

func (a *App) logout() {
	a.disconnect()
	a.mu.Lock()
	a.auth = BiliAuth{}
	a.loginStatus = "logged_out"
	a.loginDetail = "尚未登录 B 站"
	a.mu.Unlock()
	_ = os.Remove(a.authPath())
	a.broadcast()
}

func (a *App) loadTodayQueue() {
	data, err := os.ReadFile(a.queuePath())
	if err != nil {
		return
	}
	var snap queueSnapshot
	if json.Unmarshal(data, &snap) != nil {
		return
	}
	if snap.Date != time.Now().Format("2006-01-02") {
		_ = os.Remove(a.queuePath())
		return
	}
	a.queue = snap.Queue
}

func (a *App) addUser(msg ChatMessage) (bool, string) {
	a.mu.Lock()
	if a.paused {
		a.mu.Unlock()
		return false, "排队已暂停"
	}
	for i := range a.queue {
		u := &a.queue[i]
		if msg.UID > 0 && u.UID == msg.UID {
			changed := false
			if msg.Username != "" && u.Username != msg.Username {
				u.Username = msg.Username
				changed = true
			}
			if msg.Avatar != "" && u.Avatar != msg.Avatar {
				u.Avatar = msg.Avatar
				changed = true
			}
			if msg.MedalLevel > 0 && u.MedalLevel != msg.MedalLevel {
				u.MedalLevel = msg.MedalLevel
				changed = true
			}
			a.mu.Unlock()
			if changed {
				a.saveQueue()
				a.broadcast()
			}
			return false, "已经在队列中"
		}
	}
	if len(a.queue) >= a.config.MaxQueue {
		a.mu.Unlock()
		return false, "队列人数已达上限"
	}
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), a.messageSeq.Add(1))
	a.queue = append(a.queue, QueueUser{
		ID:         id,
		UID:        msg.UID,
		Username:   msg.Username,
		Avatar:     msg.Avatar,
		MedalLevel: msg.MedalLevel,
		JoinedAt:   time.Now().UnixMilli(),
		Manual:     msg.Manual,
	})
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
	return true, "已加入队列"
}

func (a *App) cancelUser(uid int64) bool {
	a.mu.Lock()
	idx := -1
	for i, u := range a.queue {
		if uid > 0 && u.UID == uid {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mu.Unlock()
		return false
	}
	a.queue = append(a.queue[:idx], a.queue[idx+1:]...)
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
	return true
}

func (a *App) processGift(gift GiftMessage) {
	if gift.ReceivedAt == 0 {
		gift.ReceivedAt = time.Now().UnixMilli()
	}
	a.mu.Lock()
	if gift.EventID != "" {
		if seenAt, ok := a.giftEvents[gift.EventID]; ok && gift.ReceivedAt-seenAt < int64(10*time.Minute/time.Millisecond) {
			a.mu.Unlock()
			return
		}
		a.giftEvents[gift.EventID] = gift.ReceivedAt
		for id, seenAt := range a.giftEvents {
			if gift.ReceivedAt-seenAt > int64(30*time.Minute/time.Millisecond) {
				delete(a.giftEvents, id)
			}
		}
	}
	cp := gift
	a.lastGift = &cp
	cfg := a.config.GiftPriority
	a.mu.Unlock()
	a.broadcast()

	if !cfg.Enabled || gift.CoinType != "gold" || gift.Battery < cfg.ThresholdBattery {
		return
	}
	a.prioritizeGiftSender(gift)
}

func (a *App) normalizePriorityZoneLocked() {
	if len(a.queue) <= 1 {
		return
	}
	current := a.queue[0]
	priority := make([]QueueUser, 0, len(a.queue)-1)
	regular := make([]QueueUser, 0, len(a.queue)-1)
	for _, user := range a.queue[1:] {
		if user.Priority {
			priority = append(priority, user)
		} else {
			regular = append(regular, user)
		}
	}
	if a.config.GiftPriority.SortByValue {
		sort.SliceStable(priority, func(i, j int) bool {
			if priority[i].GiftBattery != priority[j].GiftBattery {
				return priority[i].GiftBattery > priority[j].GiftBattery
			}
			if priority[i].PriorityAt != priority[j].PriorityAt {
				return priority[i].PriorityAt < priority[j].PriorityAt
			}
			return priority[i].JoinedAt < priority[j].JoinedAt
		})
	}
	a.queue = append([]QueueUser{current}, append(priority, regular...)...)
}

func (a *App) prioritizeGiftSender(gift GiftMessage) {
	a.mu.Lock()
	now := time.Now().UnixMilli()
	index := -1
	for i, user := range a.queue {
		if user.UID == gift.UID {
			index = i
			break
		}
	}
	if index == 0 {
		user := &a.queue[0]
		user.Priority = true
		user.GiftName = gift.GiftName
		user.GiftIcon = gift.GiftIcon
		user.GiftBattery = gift.Battery
		user.PriorityAt = now
		if gift.Avatar != "" {
			user.Avatar = gift.Avatar
		}
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		return
	}

	if index >= 0 {
		user := &a.queue[index]
		wasPriority := user.Priority
		if gift.Username != "" {
			user.Username = gift.Username
		}
		if gift.Avatar != "" {
			user.Avatar = gift.Avatar
		}
		user.Priority = true
		user.GiftName = gift.GiftName
		user.GiftIcon = gift.GiftIcon
		user.GiftBattery = gift.Battery
		if !wasPriority || a.config.GiftPriority.SortByValue {
			user.PriorityAt = now
		}
	} else {
		a.queue = append(a.queue, QueueUser{
			ID:          fmt.Sprintf("%d-%d", time.Now().UnixNano(), a.messageSeq.Add(1)),
			UID:         gift.UID,
			Username:    gift.Username,
			Avatar:      gift.Avatar,
			JoinedAt:    now,
			Priority:    true,
			GiftName:    gift.GiftName,
			GiftIcon:    gift.GiftIcon,
			GiftBattery: gift.Battery,
			PriorityAt:  now,
		})
	}
	a.normalizePriorityZoneLocked()
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
}

func (a *App) processMessage(msg ChatMessage) {
	if msg.ReceivedAt == 0 {
		msg.ReceivedAt = time.Now().UnixMilli()
	}
	cmd := strings.TrimSpace(msg.Text)
	a.mu.Lock()
	cp := msg
	a.lastMessage = &cp
	joinCmd := strings.TrimSpace(a.config.JoinCommand)
	cancelCmd := strings.TrimSpace(a.config.CancelCommand)
	a.mu.Unlock()
	a.broadcast()

	switch cmd {
	case joinCmd:
		a.addUser(msg)
	case cancelCmd:
		a.cancelUser(msg.UID)
	}
}

func (a *App) connect(roomID string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return errors.New("直播间号不能为空")
	}
	if _, err := strconv.ParseInt(roomID, 10, 64); err != nil {
		return errors.New("直播间号格式不正确")
	}

	a.mu.RLock()
	auth := a.auth
	a.mu.RUnlock()
	if auth.UID <= 0 || strings.TrimSpace(auth.Cookie) == "" {
		return errors.New("请先点击“扫码登录”并用 B 站客户端确认")
	}

	a.mu.Lock()
	if a.connectionCancel != nil {
		a.connectionCancel()
		a.connectionCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.connectionCancel = cancel
	a.connectionGeneration++
	generation := a.connectionGeneration
	a.config.RoomID = roomID
	a.connectionStatus = "connecting"
	a.connectionDetail = "正在连接"
	a.resolvedRoomID = 0
	a.roomTitle = ""
	a.anchorName = ""
	a.mu.Unlock()
	a.saveConfig()
	a.broadcast()

	client := NewBiliClient(auth)
	go client.Run(ctx, roomID, func(status ConnectionUpdate) {
		a.mu.Lock()
		if a.connectionGeneration != generation {
			a.mu.Unlock()
			return
		}
		a.connectionStatus = status.Status
		a.connectionDetail = status.Detail
		if status.RoomID > 0 {
			a.resolvedRoomID = status.RoomID
		}
		if status.RoomTitle != "" {
			a.roomTitle = status.RoomTitle
		}
		if status.AnchorName != "" {
			a.anchorName = status.AnchorName
		}
		a.mu.Unlock()
		a.broadcast()
	}, a.processMessage, a.processGift)
	return nil
}

func (a *App) disconnect() {
	a.mu.Lock()
	if a.connectionCancel != nil {
		a.connectionCancel()
		a.connectionCancel = nil
	}
	a.connectionGeneration++
	a.connectionStatus = "disconnected"
	a.connectionDetail = "已断开"
	a.mu.Unlock()
	a.broadcast()
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	assets := http.FileServer(http.FS(sub))
	mux.Handle("/assets/", http.StripPrefix("/assets/", assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/control", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/control", serveEmbedded("web/control.html", "text/html; charset=utf-8"))
	mux.HandleFunc("/overlay", serveEmbedded("web/overlay.html", "text/html; charset=utf-8"))

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/events", a.handleEvents)
	mux.HandleFunc("/api/media/image", a.handleMediaImage)
	mux.HandleFunc("/api/config/export", a.handleConfigExport)
	mux.HandleFunc("/api/config/import", a.handleConfigImport)

	mux.HandleFunc("/api/auth/qrcode/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		loginURL, key, err := NewBiliClient().StartQRLogin(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		a.loginStatus = "waiting"
		a.loginDetail = "等待扫码"
		a.mu.Unlock()
		a.broadcast()
		writeJSON(w, http.StatusOK, map[string]string{"url": loginURL, "key": key})
	})

	mux.HandleFunc("/api/auth/qrcode/poll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Key string `json:"key"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		result, err := NewBiliClient().PollQRLogin(ctx, req.Key)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if result.Status == "success" {
			if err := a.setAuth(result.Auth); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			a.mu.Lock()
			a.loginStatus = result.Status
			a.loginDetail = result.Message
			a.mu.Unlock()
			a.broadcast()
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": result.Status, "message": result.Message})
	})

	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.logout()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var cfg Config
		if err := decodeJSON(r, &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		applyConfigDefaults(&cfg)
		a.mu.Lock()
		oldRoom := a.config.RoomID
		a.config = cfg
		a.normalizePriorityZoneLocked()
		a.mu.Unlock()
		a.saveConfig()
		a.broadcast()
		if cfg.RoomID != "" && oldRoom != cfg.RoomID {
			_ = a.connect(cfg.RoomID)
		}
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			RoomID string `json:"roomId"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := a.connect(req.RoomID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.disconnect()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("/api/queue/manual", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
		}
		if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Username) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "用户名不能为空"})
			return
		}
		uid := -time.Now().UnixNano()
		a.mu.RLock()
		joinCommand := a.config.JoinCommand
		a.mu.RUnlock()
		ok, detail := a.addUser(ChatMessage{UID: uid, Username: strings.TrimSpace(req.Username), Text: joinCommand, Manual: true})
		writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "detail": detail})
	})

	mux.HandleFunc("/api/queue/next", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.mu.Lock()
		if len(a.queue) > 0 {
			a.queue = a.queue[1:]
		}
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/queue/skip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.mu.Lock()
		if len(a.queue) > 1 {
			first := a.queue[0]
			a.queue = append(a.queue[1:], first)
			a.normalizePriorityZoneLocked()
		}
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/queue/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		for i, u := range a.queue {
			if u.ID == req.ID {
				a.queue = append(a.queue[:i], a.queue[i+1:]...)
				break
			}
		}
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/queue/reorder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		byID := make(map[string]QueueUser, len(a.queue))
		for _, u := range a.queue {
			byID[u.ID] = u
		}
		ordered := make([]QueueUser, 0, len(a.queue))
		for _, id := range req.IDs {
			if u, ok := byID[id]; ok {
				ordered = append(ordered, u)
				delete(byID, id)
			}
		}
		for _, u := range a.queue {
			if _, ok := byID[u.ID]; ok {
				ordered = append(ordered, u)
			}
		}
		a.queue = ordered
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/queue/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Paused bool `json:"paused"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		a.paused = req.Paused
		a.mu.Unlock()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/queue/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.mu.Lock()
		a.queue = nil
		a.mu.Unlock()
		a.saveQueue()
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/session/end", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.disconnect()
		a.mu.Lock()
		a.queue = nil
		a.paused = false
		a.lastMessage = nil
		a.lastGift = nil
		a.mu.Unlock()
		_ = os.Remove(a.queuePath())
		a.broadcast()
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/debug/gift", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			UID      int64   `json:"uid"`
			Username string  `json:"username"`
			GiftName string  `json:"giftName"`
			Battery  float64 `json:"battery"`
			GiftIcon string  `json:"giftIcon"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.UID == 0 {
			req.UID = time.Now().UnixNano()
		}
		if strings.TrimSpace(req.Username) == "" {
			req.Username = "礼物测试用户"
		}
		if strings.TrimSpace(req.GiftName) == "" {
			req.GiftName = "测试礼物"
		}
		if req.Battery <= 0 {
			req.Battery = a.state().Config.GiftPriority.ThresholdBattery
		}
		a.processGift(GiftMessage{EventID: fmt.Sprintf("debug-%d", time.Now().UnixNano()), UID: req.UID, Username: req.Username, GiftName: req.GiftName, GiftIcon: req.GiftIcon, Num: 1, CoinType: "gold", TotalCoin: int64(req.Battery * 100), Battery: req.Battery, ReceivedAt: time.Now().UnixMilli()})
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/debug/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			UID      int64  `json:"uid"`
			Username string `json:"username"`
			Text     string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.UID == 0 {
			req.UID = time.Now().UnixNano()
		}
		if strings.TrimSpace(req.Username) == "" {
			req.Username = fmt.Sprintf("测试用户%d", a.messageSeq.Add(1))
		}
		a.processMessage(ChatMessage{UID: req.UID, Username: req.Username, Text: req.Text})
		writeJSON(w, http.StatusOK, a.state())
	})

	return withSecurityHeaders(mux)
}

func serveEmbedded(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := webFiles.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 4)
	a.mu.Lock()
	a.clients[ch] = struct{}{}
	initial, _ := json.Marshal(a.stateLocked())
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.clients, ch)
		a.mu.Unlock()
	}()

	fmt.Fprintf(w, "data: %s\n\n", initial)
	flusher.Flush()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func main() {
	defaultDataDir := "data"
	if executable, err := os.Executable(); err == nil {
		defaultDataDir = filepath.Join(filepath.Dir(executable), "data")
	}
	listen := flag.String("listen", "127.0.0.1:18303", "HTTP listen address")
	dataDir := flag.String("data", defaultDataDir, "data directory")
	noBrowser := flag.Bool("no-browser", false, "do not open the control page")
	flag.Parse()

	app := newApp(*dataDir)
	server := &http.Server{
		Addr:              *listen,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	url := "http://" + *listen + "/control"
	log.Printf("BiliQueue %s", version)
	log.Printf("control: %s", url)
	log.Printf("overlay: http://%s/overlay", *listen)
	if !*noBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
