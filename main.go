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
	"net"
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

//go:embed web/* assets/*
var webFiles embed.FS

type OverlayStyle struct {
	Height                   int     `json:"height"`
	FontSize                 int     `json:"fontSize,omitempty"` // legacy global font size
	CurrentFontSize          int     `json:"currentFontSize"`
	CurrentTextColor         string  `json:"currentTextColor"`
	CurrentFontWeight        int     `json:"currentFontWeight"`
	CurrentTextAlign         string  `json:"currentTextAlign"`
	CurrentTextLineGap       int     `json:"currentTextLineGap"`
	CurrentBadgeText         string  `json:"currentBadgeText"`
	CurrentBadgeTextColor    string  `json:"currentBadgeTextColor"`
	CurrentBadgeBackground   string  `json:"currentBadgeBackground"`
	CurrentBadgeOpacity      float64 `json:"currentBadgeOpacity"`
	CurrentBadgeFontSize     int     `json:"currentBadgeFontSize"`
	CurrentBadgeRadius       int     `json:"currentBadgeRadius"`
	CurrentBadgeOffsetX      int     `json:"currentBadgeOffsetX"`
	CurrentBadgeOffsetY      int     `json:"currentBadgeOffsetY"`
	CurrentFontFile          string  `json:"currentFontFile,omitempty"`
	CurrentTextOpacity       float64 `json:"currentTextOpacity"`
	CurrentTextStrokeWidth   int     `json:"currentTextStrokeWidth"`
	CurrentTextStrokeColor   string  `json:"currentTextStrokeColor"`
	QueueFontSize            int     `json:"queueFontSize"`
	QueueTextColor           string  `json:"queueTextColor"`
	QueueFontWeight          int     `json:"queueFontWeight"`
	QueueTextAlign           string  `json:"queueTextAlign"`
	QueueTextLineGap         int     `json:"queueTextLineGap"`
	QueueFontFile            string  `json:"queueFontFile,omitempty"`
	QueueTextOpacity         float64 `json:"queueTextOpacity"`
	QueueTextStrokeWidth     int     `json:"queueTextStrokeWidth"`
	QueueTextStrokeColor     string  `json:"queueTextStrokeColor"`
	InfoFontSize             int     `json:"infoFontSize"`
	InfoTextColor            string  `json:"infoTextColor"`
	InfoFontWeight           int     `json:"infoFontWeight"`
	InfoTextAlign            string  `json:"infoTextAlign"`
	InfoFontFile             string  `json:"infoFontFile,omitempty"`
	InfoTextOpacity          float64 `json:"infoTextOpacity"`
	InfoTextStrokeWidth      int     `json:"infoTextStrokeWidth"`
	InfoTextStrokeColor      string  `json:"infoTextStrokeColor"`
	Speed                    float64 `json:"speed"`
	EffectInterval           float64 `json:"effectInterval"`
	EffectDuration           float64 `json:"effectDuration"`
	Background               string  `json:"background"`
	Opacity                  float64 `json:"opacity,omitempty"` // legacy global opacity
	GradientTopOpacity       float64 `json:"gradientTopOpacity"`
	GradientBottomOpacity    float64 `json:"gradientBottomOpacity"`
	GradientRange            int     `json:"gradientRange,omitempty"` // legacy gradient length, v0.1.9
	GradientStart            int     `json:"gradientStart"`
	GradientEnd              int     `json:"gradientEnd"`
	AvatarSize               int     `json:"avatarSize"` // legacy shared avatar size
	CurrentAvatarSize        int     `json:"currentAvatarSize"`
	QueueAvatarSize          int     `json:"queueAvatarSize"`
	CurrentAvatarNameGap     int     `json:"currentAvatarNameGap"`
	QueueAvatarNameGap       int     `json:"queueAvatarNameGap"`
	CurrentBackground        string  `json:"currentBackground"`
	CurrentBackgroundOpacity float64 `json:"currentBackgroundOpacity"`
	QueueBackground          string  `json:"queueBackground"`
	QueueBackgroundOpacity   float64 `json:"queueBackgroundOpacity"`
	InfoBackground           string  `json:"infoBackground"`
	InfoBackgroundOpacity    float64 `json:"infoBackgroundOpacity"`
	Radius                   int     `json:"radius"`
	ShowAvatar               bool    `json:"showAvatar"`
	ShowGuardIcon            bool    `json:"showGuardIcon"`
	ShowCount                bool    `json:"showCount"`
	ShowRules                bool    `json:"showRules"`
	CurrentEnabled           bool    `json:"currentEnabled"`
	InfoEnabled              bool    `json:"infoEnabled"`
	ShowGiftIcon             bool    `json:"showGiftIcon"`
	ShowGiftBattery          bool    `json:"showGiftBattery"`
	GiftBatterySize          int     `json:"giftBatterySize"`
	ScrollMode               string  `json:"scrollMode"`
	ShortAlign               string  `json:"shortAlign"`
	CurrentWidth             int     `json:"currentWidth"`
	CurrentSidePadding       int     `json:"currentSidePadding"`
	QueueWidth               int     `json:"queueWidth"`
	InfoWidth                int     `json:"infoWidth"`
	LegacyCountWidth         int     `json:"countWidth,omitempty"`
	QueueLineGap             int     `json:"queueLineGap"`
	QueueItemGap             int     `json:"queueItemGap"`
	QueueSecondPageSize      int     `json:"queueSecondPageSize,omitempty"` // legacy v0.1.12-test10
	QueuePageSize            int     `json:"queuePageSize"`
	InfoLineGap              int     `json:"infoLineGap"`
	DoubleLineEnabled        bool    `json:"doubleLineEnabled"`
	DoubleLineThreshold      int     `json:"doubleLineThreshold,omitempty"` // legacy v0.1.12-test10
	InfoText                 string  `json:"infoText"`
	EmptyText                string  `json:"emptyText"`
	QueueEmptyText           string  `json:"queueEmptyText"`
}

type GiftPriorityConfig struct {
	Enabled               bool    `json:"enabled"`
	ThresholdBattery      float64 `json:"thresholdBattery"`
	SortByValue           bool    `json:"sortByValue"`
	PaidQueueEnabled      bool    `json:"paidQueueEnabled"`
	QueueThresholdBattery float64 `json:"queueThresholdBattery"`
}

type QueueEligibilityConfig struct {
	FanMedalEnabled      bool `json:"fanMedalEnabled"`
	FanMedalLevel        int  `json:"fanMedalLevel"`
	GuardEnabled         bool `json:"guardEnabled"`
	GuardPriorityEnabled bool `json:"guardPriorityEnabled"`
}

type UpdateConfig struct {
	AutoCheck bool `json:"autoCheck"`
}

type HotkeyConfig struct {
	OpenControl     string `json:"openControl"`
	OpenMiniControl string `json:"openMiniControl"`
	NextQueue       string `json:"nextQueue"`
	ClearQueue      string `json:"clearQueue"`
}

type Config struct {
	SchemaVersion int                    `json:"schemaVersion"`
	ListenAddress string                 `json:"listenAddress"`
	RoomID        string                 `json:"roomId"`
	QueueEnabled  bool                   `json:"queueEnabled"`
	JoinCommand   string                 `json:"joinCommand"`
	CancelCommand string                 `json:"cancelCommand"`
	ClearCommand  string                 `json:"clearCommand"`
	NextCommand   string                 `json:"nextCommand"`
	MaxQueue      int                    `json:"maxQueue"`
	GiftPriority  GiftPriorityConfig     `json:"giftPriority"`
	Eligibility   QueueEligibilityConfig `json:"eligibility"`
	Hotkeys       HotkeyConfig           `json:"hotkeys"`
	Updates       UpdateConfig           `json:"updates"`
	Overlay       OverlayStyle           `json:"overlay"`
}

type QueueUser struct {
	ID                  string  `json:"id"`
	UID                 int64   `json:"uid"`
	Username            string  `json:"username"`
	Avatar              string  `json:"avatar,omitempty"`
	MedalLevel          int     `json:"medalLevel,omitempty"`
	GuardLevel          int     `json:"guardLevel,omitempty"`
	JoinedAt            int64   `json:"joinedAt"`
	Manual              bool    `json:"manual,omitempty"`
	Priority            bool    `json:"priority,omitempty"`
	HasGift             bool    `json:"hasGift,omitempty"`
	GiftName            string  `json:"giftName,omitempty"`
	GiftIcon            string  `json:"giftIcon,omitempty"`
	GiftBattery         float64 `json:"giftBattery,omitempty"`
	PriorityGiftBattery float64 `json:"priorityGiftBattery,omitempty"`
	PriorityAt          int64   `json:"priorityAt,omitempty"`
}

type PublicState struct {
	Config           Config            `json:"config"`
	LoginStatus      string            `json:"loginStatus"`
	LoginDetail      string            `json:"loginDetail"`
	LoginUID         int64             `json:"loginUid,omitempty"`
	LoginName        string            `json:"loginName,omitempty"`
	Queue            []QueueUser       `json:"queue"`
	Paused           bool              `json:"paused"`
	ConnectionStatus string            `json:"connectionStatus"`
	ConnectionDetail string            `json:"connectionDetail"`
	ResolvedRoomID   int64             `json:"resolvedRoomId,omitempty"`
	RoomTitle        string            `json:"roomTitle,omitempty"`
	AnchorName       string            `json:"anchorName,omitempty"`
	AnchorUID        int64             `json:"anchorUid,omitempty"`
	ControlURL       string            `json:"controlUrl,omitempty"`
	OverlayURL       string            `json:"overlayUrl,omitempty"`
	MiniControlURL   string            `json:"miniControlUrl,omitempty"`
	HotkeyStatus     map[string]string `json:"hotkeyStatus,omitempty"`
	UpdateStatus     UpdateStatus      `json:"updateStatus"`
	LastMessage      *ChatMessage      `json:"lastMessage,omitempty"`
	LastGift         *GiftMessage      `json:"lastGift,omitempty"`
	Version          string            `json:"version"`
	RuntimeID        string            `json:"runtimeId"`
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

	loginStatus           string
	loginDetail           string
	connectionStatus      string
	connectionDetail      string
	resolvedRoomID        int64
	roomTitle             string
	anchorName            string
	anchorUID             int64
	serverControl         *ServerController
	lastMessage           *ChatMessage
	lastGift              *GiftMessage
	giftEvents            map[string]int64
	hotkeyStatus          map[string]string
	updateStatus          UpdateStatus
	updateNotifiedVersion string
	preparedUpdate        *preparedUpdate
	runtimeID             string

	dataDir  string
	fontsDir string
	clients  map[chan []byte]struct{}

	connectionCancel     context.CancelFunc
	connectionGeneration uint64
	messageSeq           atomic.Uint64
	updateCheckMu        sync.Mutex
	updateInstallMu      sync.Mutex
}

const version = "0.1.18"

// buildProfile is set only for local-purpose builds through -ldflags -X.
var buildProfile string

func defaultConfig() Config {
	return Config{
		SchemaVersion: 16,
		ListenAddress: "127.0.0.1:18303",
		RoomID:        "",
		QueueEnabled:  true,
		JoinCommand:   "排队",
		CancelCommand: "取消排队",
		ClearCommand:  "清空队列",
		NextCommand:   "下一位",
		MaxQueue:      100,
		GiftPriority: GiftPriorityConfig{
			Enabled: true, ThresholdBattery: 100, SortByValue: false,
			PaidQueueEnabled: false, QueueThresholdBattery: 100,
		},
		Eligibility: QueueEligibilityConfig{FanMedalLevel: 1},
		Hotkeys:     HotkeyConfig{},
		Updates:     UpdateConfig{AutoCheck: true},
		Overlay: OverlayStyle{
			Height:                   50,
			FontSize:                 24,
			CurrentFontSize:          24,
			CurrentTextColor:         "#ffffff",
			CurrentFontWeight:        600,
			CurrentTextAlign:         "left",
			CurrentTextLineGap:       0,
			CurrentBadgeText:         "当前",
			CurrentBadgeTextColor:    "#ffffff",
			CurrentBadgeBackground:   "#6577ed",
			CurrentBadgeOpacity:      0.92,
			CurrentBadgeFontSize:     11,
			CurrentBadgeRadius:       8,
			CurrentBadgeOffsetX:      -6,
			CurrentBadgeOffsetY:      -6,
			CurrentTextOpacity:       1,
			CurrentTextStrokeWidth:   0,
			CurrentTextStrokeColor:   "#000000",
			QueueFontSize:            24,
			QueueTextColor:           "#ffffff",
			QueueFontWeight:          500,
			QueueTextAlign:           "left",
			QueueTextLineGap:         0,
			QueueTextOpacity:         1,
			QueueTextStrokeWidth:     0,
			QueueTextStrokeColor:     "#000000",
			InfoFontSize:             18,
			InfoTextColor:            "#ffffff",
			InfoFontWeight:           500,
			InfoTextAlign:            "left",
			InfoTextOpacity:          1,
			InfoTextStrokeWidth:      0,
			InfoTextStrokeColor:      "#000000",
			Speed:                    40,
			EffectInterval:           4,
			EffectDuration:           0.42,
			Background:               "#000000",
			GradientTopOpacity:       0.45,
			GradientBottomOpacity:    0.45,
			GradientRange:            0,
			GradientStart:            0,
			GradientEnd:              100,
			AvatarSize:               32,
			CurrentAvatarSize:        32,
			QueueAvatarSize:          32,
			CurrentAvatarNameGap:     12,
			QueueAvatarNameGap:       10,
			CurrentBackground:        "#ffffff",
			CurrentBackgroundOpacity: 0.07,
			QueueBackground:          "#000000",
			QueueBackgroundOpacity:   0,
			InfoBackground:           "#ffffff",
			InfoBackgroundOpacity:    0.05,
			Radius:                   16,
			ShowAvatar:               true,
			ShowGuardIcon:            true,
			ShowCount:                true,
			ShowRules:                true,
			CurrentEnabled:           true,
			InfoEnabled:              true,
			ShowGiftIcon:             true,
			ShowGiftBattery:          true,
			GiftBatterySize:          14,
			ScrollMode:               "continuous",
			ShortAlign:               "center",
			CurrentWidth:             300,
			CurrentSidePadding:       20,
			QueueWidth:               1220,
			InfoWidth:                400,
			QueueLineGap:             8,
			QueueItemGap:             22,
			QueueSecondPageSize:      5,
			QueuePageSize:            5,
			InfoLineGap:              4,
			DoubleLineEnabled:        true,
			DoubleLineThreshold:      8,
			InfoText:                 "弹幕发送“排队”加入\n达到礼物门槛可进入优先队列",
			EmptyText:                "排队空闲中",
			QueueEmptyText:           "空",
		},
	}
}

func newApp(dataDir string) *App {
	return newAppWithFonts(dataDir, "")
}

func newAppWithFonts(dataDir, fontsOverride string) *App {
	fontsDir := strings.TrimSpace(fontsOverride)
	if fontsDir == "" {
		fontsDir = filepath.Join(dataDir, "fonts")
		if filepath.Base(filepath.Clean(dataDir)) == "data" {
			fontsDir = filepath.Join(filepath.Dir(dataDir), "fonts")
		}
	}
	a := &App{
		config:           defaultConfig(),
		loginStatus:      "logged_out",
		loginDetail:      "尚未登录 B 站",
		connectionStatus: "disconnected",
		connectionDetail: "未连接",
		dataDir:          dataDir,
		fontsDir:         fontsDir,
		clients:          make(map[chan []byte]struct{}),
		giftEvents:       make(map[string]int64),
		hotkeyStatus:     defaultHotkeyStatus("快捷键服务尚未启动"),
		runtimeID:        fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(a.fontsDir, 0o755)
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
		AnchorUID:        a.anchorUID,
		ControlURL:       a.controlURL(),
		OverlayURL:       a.overlayURL(),
		MiniControlURL:   a.miniControlURL(),
		HotkeyStatus:     cloneStringMap(a.hotkeyStatus),
		UpdateStatus:     a.updateStatus,
		LastMessage:      msg,
		LastGift:         gift,
		Version:          version,
		RuntimeID:        a.runtimeID,
	}
}

func (a *App) currentListenAddress() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.ListenAddress
}

func urlForListen(addr, path string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = defaultConfig().ListenAddress
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr + path
}

func (a *App) controlURL() string     { return urlForListen(a.config.ListenAddress, "/control") }
func (a *App) overlayURL() string     { return urlForListen(a.config.ListenAddress, "/overlay") }
func (a *App) miniControlURL() string { return urlForListen(a.config.ListenAddress, "/mini-control") }

func (a *App) state() PublicState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stateLocked()
}

func defaultHotkeyStatus(message string) map[string]string {
	return map[string]string{
		"openControl":     message,
		"openMiniControl": message,
		"nextQueue":       message,
		"clearQueue":      message,
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (a *App) setHotkeyStatus(status map[string]string) {
	a.mu.Lock()
	a.hotkeyStatus = cloneStringMap(status)
	a.mu.Unlock()
}

func (a *App) applyHotkeys(cfg HotkeyConfig) map[string]string {
	status := reloadGlobalHotkeys(cfg)
	a.setHotkeyStatus(status)
	a.broadcast()
	return status
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

func (a *App) clearQueue() {
	a.mu.Lock()
	a.queue = nil
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
}

func (a *App) advanceQueue() {
	a.mu.Lock()
	if len(a.queue) > 0 {
		a.queue = a.queue[1:]
	}
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
}

func (a *App) autoConnectIfReady(reason string) {
	a.mu.RLock()
	auth := a.auth
	roomID := strings.TrimSpace(a.config.RoomID)
	status := a.connectionStatus
	a.mu.RUnlock()
	if auth.UID <= 0 || strings.TrimSpace(auth.Cookie) == "" || roomID == "" {
		return
	}
	if status == "connected" || status == "connecting" || status == "reconnecting" {
		return
	}
	log.Printf("auto connect room %s after %s", roomID, reason)
	go func() {
		if err := a.connect(roomID); err != nil {
			log.Printf("auto connect failed: %v", err)
		}
	}()
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
	legacyV5 := cfg.SchemaVersion < 5
	legacyV6 := cfg.SchemaVersion < 6
	legacyV7 := cfg.SchemaVersion < 7
	legacyV8 := cfg.SchemaVersion < 8
	legacyV9 := cfg.SchemaVersion < 9
	legacyV10 := cfg.SchemaVersion < 10
	legacyV11 := cfg.SchemaVersion < 11
	legacyV12 := cfg.SchemaVersion < 12
	legacyV15 := cfg.SchemaVersion < 15
	legacyV16 := cfg.SchemaVersion < 16
	if strings.TrimSpace(cfg.ListenAddress) == "" {
		cfg.ListenAddress = def.ListenAddress
	}
	cfg.ListenAddress = normalizeListenAddress(cfg.ListenAddress, def.ListenAddress)
	if legacyV8 {
		cfg.QueueEnabled = true
	}
	if strings.TrimSpace(cfg.ClearCommand) == "" {
		cfg.ClearCommand = def.ClearCommand
	}
	if strings.TrimSpace(cfg.NextCommand) == "" {
		cfg.NextCommand = def.NextCommand
	}
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
	if cfg.GiftPriority.QueueThresholdBattery <= 0 {
		cfg.GiftPriority.QueueThresholdBattery = def.GiftPriority.QueueThresholdBattery
	}
	if cfg.Eligibility.FanMedalLevel <= 0 {
		cfg.Eligibility.FanMedalLevel = def.Eligibility.FanMedalLevel
	}
	if legacyV12 && cfg.Overlay.Height <= 0 {
		cfg.Overlay.Height = def.Overlay.Height
	}
	if cfg.Overlay.Height < 0 {
		cfg.Overlay.Height = 0
	}
	if legacyV12 {
		cfg.Overlay.CurrentEnabled = true
		cfg.Overlay.InfoEnabled = true
	}
	if legacyV15 {
		cfg.Overlay.ShowGiftBattery = def.Overlay.ShowGiftBattery
		cfg.Overlay.GiftBatterySize = def.Overlay.GiftBatterySize
	}
	if legacyV16 {
		cfg.Overlay.ShowGuardIcon = def.Overlay.ShowGuardIcon
		cfg.Updates.AutoCheck = def.Updates.AutoCheck
	}
	if cfg.Overlay.GiftBatterySize < 8 || cfg.Overlay.GiftBatterySize > 48 {
		cfg.Overlay.GiftBatterySize = def.Overlay.GiftBatterySize
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
	if legacyV5 {
		cfg.Overlay.CurrentTextOpacity = 1
		cfg.Overlay.QueueTextOpacity = 1
		cfg.Overlay.InfoTextOpacity = 1
		legacyOpacity := cfg.Overlay.Opacity
		if legacyOpacity < 0 || legacyOpacity > 1 {
			legacyOpacity = def.Overlay.GradientTopOpacity
		}
		cfg.Overlay.GradientTopOpacity = legacyOpacity
		cfg.Overlay.GradientBottomOpacity = legacyOpacity
		cfg.Overlay.CurrentBackground = def.Overlay.CurrentBackground
		cfg.Overlay.CurrentBackgroundOpacity = def.Overlay.CurrentBackgroundOpacity
		cfg.Overlay.QueueBackground = def.Overlay.QueueBackground
		cfg.Overlay.QueueBackgroundOpacity = def.Overlay.QueueBackgroundOpacity
		cfg.Overlay.InfoBackground = def.Overlay.InfoBackground
		cfg.Overlay.InfoBackgroundOpacity = def.Overlay.InfoBackgroundOpacity
		cfg.Overlay.GradientRange = def.Overlay.GradientRange
		cfg.Overlay.GradientStart = def.Overlay.GradientStart
		cfg.Overlay.GradientEnd = def.Overlay.GradientEnd
		cfg.Overlay.AvatarSize = def.Overlay.AvatarSize
		cfg.Overlay.CurrentAvatarSize = def.Overlay.CurrentAvatarSize
		cfg.Overlay.QueueAvatarSize = def.Overlay.QueueAvatarSize
		cfg.Overlay.CurrentAvatarNameGap = def.Overlay.CurrentAvatarNameGap
		cfg.Overlay.QueueAvatarNameGap = def.Overlay.QueueAvatarNameGap
	}
	cfg.Overlay.CurrentFontFile = normalizeFontFileName(cfg.Overlay.CurrentFontFile)
	cfg.Overlay.QueueFontFile = normalizeFontFileName(cfg.Overlay.QueueFontFile)
	cfg.Overlay.InfoFontFile = normalizeFontFileName(cfg.Overlay.InfoFontFile)
	if cfg.Overlay.CurrentTextOpacity < 0 || cfg.Overlay.CurrentTextOpacity > 1 {
		cfg.Overlay.CurrentTextOpacity = def.Overlay.CurrentTextOpacity
	}
	if cfg.Overlay.QueueTextOpacity < 0 || cfg.Overlay.QueueTextOpacity > 1 {
		cfg.Overlay.QueueTextOpacity = def.Overlay.QueueTextOpacity
	}
	if cfg.Overlay.InfoTextOpacity < 0 || cfg.Overlay.InfoTextOpacity > 1 {
		cfg.Overlay.InfoTextOpacity = def.Overlay.InfoTextOpacity
	}
	if cfg.Overlay.CurrentTextStrokeWidth < 0 || cfg.Overlay.CurrentTextStrokeWidth > 12 {
		cfg.Overlay.CurrentTextStrokeWidth = def.Overlay.CurrentTextStrokeWidth
	}
	if cfg.Overlay.QueueTextStrokeWidth < 0 || cfg.Overlay.QueueTextStrokeWidth > 12 {
		cfg.Overlay.QueueTextStrokeWidth = def.Overlay.QueueTextStrokeWidth
	}
	if cfg.Overlay.InfoTextStrokeWidth < 0 || cfg.Overlay.InfoTextStrokeWidth > 12 {
		cfg.Overlay.InfoTextStrokeWidth = def.Overlay.InfoTextStrokeWidth
	}
	if cfg.Overlay.CurrentTextStrokeColor == "" {
		cfg.Overlay.CurrentTextStrokeColor = def.Overlay.CurrentTextStrokeColor
	}
	if cfg.Overlay.QueueTextStrokeColor == "" {
		cfg.Overlay.QueueTextStrokeColor = def.Overlay.QueueTextStrokeColor
	}
	if cfg.Overlay.InfoTextStrokeColor == "" {
		cfg.Overlay.InfoTextStrokeColor = def.Overlay.InfoTextStrokeColor
	}
	if cfg.Overlay.CurrentFontWeight < 100 || cfg.Overlay.CurrentFontWeight > 900 {
		cfg.Overlay.CurrentFontWeight = def.Overlay.CurrentFontWeight
	}
	if cfg.Overlay.QueueFontWeight < 100 || cfg.Overlay.QueueFontWeight > 900 {
		cfg.Overlay.QueueFontWeight = def.Overlay.QueueFontWeight
	}
	if cfg.Overlay.QueueTextAlign != "left" && cfg.Overlay.QueueTextAlign != "center" && cfg.Overlay.QueueTextAlign != "right" {
		cfg.Overlay.QueueTextAlign = def.Overlay.QueueTextAlign
	}
	if cfg.Overlay.QueueTextLineGap < 0 || cfg.Overlay.QueueTextLineGap > 30 {
		cfg.Overlay.QueueTextLineGap = def.Overlay.QueueTextLineGap
	}
	if cfg.Overlay.InfoFontWeight < 100 || cfg.Overlay.InfoFontWeight > 900 {
		cfg.Overlay.InfoFontWeight = def.Overlay.InfoFontWeight
	}
	if cfg.Overlay.CurrentTextAlign != "left" && cfg.Overlay.CurrentTextAlign != "center" && cfg.Overlay.CurrentTextAlign != "right" {
		cfg.Overlay.CurrentTextAlign = def.Overlay.CurrentTextAlign
	}
	if cfg.Overlay.CurrentTextLineGap < 0 || cfg.Overlay.CurrentTextLineGap > 30 {
		cfg.Overlay.CurrentTextLineGap = def.Overlay.CurrentTextLineGap
	}
	if legacyV10 {
		cfg.Overlay.CurrentBadgeText = def.Overlay.CurrentBadgeText
		cfg.Overlay.CurrentBadgeTextColor = def.Overlay.CurrentBadgeTextColor
		cfg.Overlay.CurrentBadgeBackground = def.Overlay.CurrentBadgeBackground
		cfg.Overlay.CurrentBadgeOpacity = def.Overlay.CurrentBadgeOpacity
		cfg.Overlay.CurrentBadgeFontSize = def.Overlay.CurrentBadgeFontSize
		cfg.Overlay.CurrentBadgeRadius = def.Overlay.CurrentBadgeRadius
	}
	if legacyV11 {
		cfg.Overlay.CurrentBadgeOffsetX = def.Overlay.CurrentBadgeOffsetX
		cfg.Overlay.CurrentBadgeOffsetY = def.Overlay.CurrentBadgeOffsetY
	}
	if strings.TrimSpace(cfg.Overlay.CurrentBadgeText) == "" {
		cfg.Overlay.CurrentBadgeText = def.Overlay.CurrentBadgeText
	}
	if cfg.Overlay.CurrentBadgeTextColor == "" {
		cfg.Overlay.CurrentBadgeTextColor = def.Overlay.CurrentBadgeTextColor
	}
	if cfg.Overlay.CurrentBadgeBackground == "" {
		cfg.Overlay.CurrentBadgeBackground = def.Overlay.CurrentBadgeBackground
	}
	if cfg.Overlay.CurrentBadgeOpacity < 0 || cfg.Overlay.CurrentBadgeOpacity > 1 {
		cfg.Overlay.CurrentBadgeOpacity = def.Overlay.CurrentBadgeOpacity
	}
	if cfg.Overlay.CurrentBadgeFontSize < 8 || cfg.Overlay.CurrentBadgeFontSize > 28 {
		cfg.Overlay.CurrentBadgeFontSize = def.Overlay.CurrentBadgeFontSize
	}
	if cfg.Overlay.CurrentBadgeRadius < 0 || cfg.Overlay.CurrentBadgeRadius > 28 {
		cfg.Overlay.CurrentBadgeRadius = def.Overlay.CurrentBadgeRadius
	}
	if cfg.Overlay.CurrentBadgeOffsetX < -80 || cfg.Overlay.CurrentBadgeOffsetX > 80 {
		cfg.Overlay.CurrentBadgeOffsetX = def.Overlay.CurrentBadgeOffsetX
	}
	if cfg.Overlay.CurrentBadgeOffsetY < -80 || cfg.Overlay.CurrentBadgeOffsetY > 80 {
		cfg.Overlay.CurrentBadgeOffsetY = def.Overlay.CurrentBadgeOffsetY
	}
	if cfg.Overlay.InfoTextAlign != "left" && cfg.Overlay.InfoTextAlign != "center" && cfg.Overlay.InfoTextAlign != "right" {
		cfg.Overlay.InfoTextAlign = def.Overlay.InfoTextAlign
	}
	if cfg.Overlay.Speed < 0 {
		cfg.Overlay.Speed = def.Overlay.Speed
	}
	if cfg.Overlay.EffectInterval <= 0 {
		cfg.Overlay.EffectInterval = def.Overlay.EffectInterval
	}
	if cfg.Overlay.EffectDuration <= 0 {
		cfg.Overlay.EffectDuration = def.Overlay.EffectDuration
	}
	if cfg.Overlay.EffectDuration > cfg.Overlay.EffectInterval {
		cfg.Overlay.EffectDuration = cfg.Overlay.EffectInterval
	}
	if cfg.Overlay.Background == "" {
		cfg.Overlay.Background = def.Overlay.Background
	}
	if cfg.Overlay.GradientTopOpacity < 0 || cfg.Overlay.GradientTopOpacity > 1 {
		cfg.Overlay.GradientTopOpacity = def.Overlay.GradientTopOpacity
	}
	if cfg.Overlay.GradientBottomOpacity < 0 || cfg.Overlay.GradientBottomOpacity > 1 {
		cfg.Overlay.GradientBottomOpacity = def.Overlay.GradientBottomOpacity
	}
	if legacyV6 || cfg.Overlay.GradientRange <= 0 || cfg.Overlay.GradientRange > 100 {
		cfg.Overlay.GradientRange = 100
	}
	if legacyV7 {
		// v0.1.9 used gradientRange as "length from start to bottom".
		cfg.Overlay.GradientStart = 100 - cfg.Overlay.GradientRange
		cfg.Overlay.GradientEnd = def.Overlay.GradientEnd
	}
	if cfg.Overlay.GradientStart < 0 || cfg.Overlay.GradientStart > 100 {
		cfg.Overlay.GradientStart = def.Overlay.GradientStart
	}
	if cfg.Overlay.GradientEnd < 0 || cfg.Overlay.GradientEnd > 100 {
		cfg.Overlay.GradientEnd = def.Overlay.GradientEnd
	}
	if cfg.Overlay.GradientEnd < cfg.Overlay.GradientStart {
		cfg.Overlay.GradientEnd = cfg.Overlay.GradientStart
	}
	if legacyV6 || cfg.Overlay.AvatarSize < 12 || cfg.Overlay.AvatarSize > 96 {
		cfg.Overlay.AvatarSize = def.Overlay.AvatarSize
		cfg.Overlay.CurrentAvatarSize = def.Overlay.CurrentAvatarSize
		cfg.Overlay.QueueAvatarSize = def.Overlay.QueueAvatarSize
		cfg.Overlay.CurrentAvatarNameGap = def.Overlay.CurrentAvatarNameGap
		cfg.Overlay.QueueAvatarNameGap = def.Overlay.QueueAvatarNameGap
	}
	if cfg.Overlay.CurrentAvatarSize < 12 || cfg.Overlay.CurrentAvatarSize > 96 {
		if cfg.Overlay.AvatarSize >= 12 && cfg.Overlay.AvatarSize <= 96 {
			cfg.Overlay.CurrentAvatarSize = cfg.Overlay.AvatarSize
		} else {
			cfg.Overlay.CurrentAvatarSize = def.Overlay.CurrentAvatarSize
		}
	}
	if cfg.Overlay.QueueAvatarSize < 12 || cfg.Overlay.QueueAvatarSize > 96 {
		if cfg.Overlay.AvatarSize >= 12 && cfg.Overlay.AvatarSize <= 96 {
			cfg.Overlay.QueueAvatarSize = cfg.Overlay.AvatarSize
		} else {
			cfg.Overlay.QueueAvatarSize = def.Overlay.QueueAvatarSize
		}
	}
	if cfg.Overlay.CurrentAvatarNameGap < 0 || cfg.Overlay.CurrentAvatarNameGap > 80 {
		cfg.Overlay.CurrentAvatarNameGap = def.Overlay.CurrentAvatarNameGap
	}
	if cfg.Overlay.QueueAvatarNameGap < 0 || cfg.Overlay.QueueAvatarNameGap > 80 {
		cfg.Overlay.QueueAvatarNameGap = def.Overlay.QueueAvatarNameGap
	}
	if cfg.Overlay.CurrentBackground == "" {
		cfg.Overlay.CurrentBackground = def.Overlay.CurrentBackground
	}
	if cfg.Overlay.QueueBackground == "" {
		cfg.Overlay.QueueBackground = def.Overlay.QueueBackground
	}
	if cfg.Overlay.InfoBackground == "" {
		cfg.Overlay.InfoBackground = def.Overlay.InfoBackground
	}
	if cfg.Overlay.CurrentBackgroundOpacity < 0 || cfg.Overlay.CurrentBackgroundOpacity > 1 {
		cfg.Overlay.CurrentBackgroundOpacity = def.Overlay.CurrentBackgroundOpacity
	}
	if cfg.Overlay.QueueBackgroundOpacity < 0 || cfg.Overlay.QueueBackgroundOpacity > 1 {
		cfg.Overlay.QueueBackgroundOpacity = def.Overlay.QueueBackgroundOpacity
	}
	if cfg.Overlay.InfoBackgroundOpacity < 0 || cfg.Overlay.InfoBackgroundOpacity > 1 {
		cfg.Overlay.InfoBackgroundOpacity = def.Overlay.InfoBackgroundOpacity
	}
	if cfg.Overlay.ScrollMode == "" {
		cfg.Overlay.ScrollMode = def.Overlay.ScrollMode
	}
	if cfg.Overlay.ScrollMode != "continuous" && cfg.Overlay.ScrollMode != "paged" && cfg.Overlay.ScrollMode != "fade" {
		cfg.Overlay.ScrollMode = def.Overlay.ScrollMode
	}
	if cfg.Overlay.ShortAlign == "" {
		cfg.Overlay.ShortAlign = def.Overlay.ShortAlign
	}
	if cfg.Overlay.CurrentWidth <= 0 {
		cfg.Overlay.CurrentWidth = def.Overlay.CurrentWidth
	}
	if cfg.Overlay.CurrentSidePadding < 0 || cfg.Overlay.CurrentSidePadding > 120 {
		cfg.Overlay.CurrentSidePadding = def.Overlay.CurrentSidePadding
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
	if cfg.Overlay.QueueItemGap < 0 || cfg.Overlay.QueueItemGap > 120 {
		cfg.Overlay.QueueItemGap = def.Overlay.QueueItemGap
	}
	if legacyV9 {
		cfg.Overlay.DoubleLineEnabled = cfg.Overlay.DoubleLineThreshold < 50
		if cfg.Overlay.QueuePageSize <= 0 {
			cfg.Overlay.QueuePageSize = cfg.Overlay.QueueSecondPageSize
		}
	}
	if cfg.Overlay.QueuePageSize <= 0 || cfg.Overlay.QueuePageSize > 20 {
		cfg.Overlay.QueuePageSize = def.Overlay.QueuePageSize
	}
	if cfg.Overlay.QueueSecondPageSize <= 0 || cfg.Overlay.QueueSecondPageSize > 20 {
		cfg.Overlay.QueueSecondPageSize = cfg.Overlay.QueuePageSize
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
	cfg.Overlay.Opacity = 0
	cfg.Overlay.GradientRange = 0
	cfg.SchemaVersion = def.SchemaVersion
}

func normalizeListenAddress(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimRight(value, "/")
	if strings.HasPrefix(value, ":") {
		value = "127.0.0.1" + value
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		if p, err2 := strconv.Atoi(value); err2 == nil && p > 0 && p <= 65535 {
			return fmt.Sprintf("127.0.0.1:%d", p)
		}
		return fallback
	}
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p > 65535 {
		return fallback
	}
	return net.JoinHostPort(host, strconv.Itoa(p))
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
	a.autoConnectIfReady("login")
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
	for i := range snap.Queue {
		user := &snap.Queue[i]
		if !user.HasGift && (user.GiftName != "" || user.GiftIcon != "" || user.GiftBattery > 0) {
			user.HasGift = true
		}
		if user.Priority && user.PriorityGiftBattery <= 0 {
			user.PriorityGiftBattery = user.GiftBattery
		}
	}
	a.queue = snap.Queue
}

func (a *App) addUser(msg ChatMessage) (bool, string) {
	msg.GuardLevel = normalizeGuardLevel(msg.GuardLevel)
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
			if msg.GuardLevel > 0 && u.GuardLevel != msg.GuardLevel {
				u.GuardLevel = msg.GuardLevel
				changed = true
			}
			if changed && a.config.Eligibility.GuardPriorityEnabled {
				a.normalizePriorityZoneLocked()
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
		GuardLevel: msg.GuardLevel,
		JoinedAt:   time.Now().UnixMilli(),
		Manual:     msg.Manual,
	})
	if msg.GuardLevel > 0 && a.config.Eligibility.GuardPriorityEnabled {
		a.normalizePriorityZoneLocked()
	}
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
	queueEnabled := a.config.QueueEnabled
	a.mu.Unlock()

	priorityEligible := queueEnabled && gift.CoinType == "gold" && cfg.Enabled && gift.Battery >= cfg.ThresholdBattery
	if a.recordGiftForQueuedUser(gift, priorityEligible) {
		return
	}
	if !queueEnabled || gift.CoinType != "gold" || !cfg.PaidQueueEnabled || gift.Battery < cfg.QueueThresholdBattery {
		a.broadcast()
		return
	}
	if !a.enqueueGiftSender(gift, priorityEligible) {
		a.broadcast()
	}
}

func (a *App) recordGiftForQueuedUser(gift GiftMessage, priority bool) bool {
	a.mu.Lock()
	index := -1
	for i := range a.queue {
		if gift.UID != 0 && a.queue[i].UID == gift.UID {
			index = i
			break
		}
	}
	if index < 0 {
		a.mu.Unlock()
		return false
	}
	now := time.Now().UnixMilli()
	user := &a.queue[index]
	if gift.Username != "" {
		user.Username = gift.Username
	}
	if gift.Avatar != "" {
		user.Avatar = gift.Avatar
	}
	if validGuardLevel(gift.GuardLevel) {
		user.GuardLevel = gift.GuardLevel
	}
	user.HasGift = true
	user.GiftName = gift.GiftName
	user.GiftIcon = gift.GiftIcon
	if gift.CoinType == "gold" && gift.Battery > 0 {
		user.GiftBattery += gift.Battery
	}
	if priority {
		wasPriority := user.Priority
		user.Priority = true
		user.PriorityGiftBattery = gift.Battery
		if !wasPriority || a.config.GiftPriority.SortByValue {
			user.PriorityAt = now
		}
	}
	if priority || (user.GuardLevel > 0 && a.config.Eligibility.GuardPriorityEnabled) {
		a.normalizePriorityZoneLocked()
	}
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
	return true
}

func (a *App) enqueueGiftSender(gift GiftMessage, priority bool) bool {
	gift.GuardLevel = normalizeGuardLevel(gift.GuardLevel)
	a.mu.Lock()
	if a.paused {
		a.mu.Unlock()
		return false
	}
	for _, user := range a.queue {
		if gift.UID != 0 && user.UID == gift.UID {
			a.mu.Unlock()
			return a.recordGiftForQueuedUser(gift, priority)
		}
	}
	if len(a.queue) >= a.config.MaxQueue {
		a.mu.Unlock()
		return false
	}
	now := time.Now().UnixMilli()
	user := QueueUser{
		ID:          fmt.Sprintf("%d-%d", time.Now().UnixNano(), a.messageSeq.Add(1)),
		UID:         gift.UID,
		Username:    gift.Username,
		Avatar:      gift.Avatar,
		GuardLevel:  gift.GuardLevel,
		JoinedAt:    now,
		HasGift:     true,
		GiftName:    gift.GiftName,
		GiftIcon:    gift.GiftIcon,
		GiftBattery: gift.Battery,
	}
	if priority {
		user.Priority = true
		user.PriorityGiftBattery = gift.Battery
		user.PriorityAt = now
	}
	a.queue = append(a.queue, user)
	if priority || (user.GuardLevel > 0 && a.config.Eligibility.GuardPriorityEnabled) {
		a.normalizePriorityZoneLocked()
	}
	a.mu.Unlock()
	a.saveQueue()
	a.broadcast()
	return true
}

func (a *App) normalizePriorityZoneLocked() {
	if len(a.queue) <= 1 {
		return
	}
	current := a.queue[0]
	priority := make([]QueueUser, 0, len(a.queue)-1)
	regular := make([]QueueUser, 0, len(a.queue)-1)
	for _, user := range a.queue[1:] {
		if a.waitingPriorityClassLocked(user) > 0 {
			priority = append(priority, user)
		} else {
			regular = append(regular, user)
		}
	}
	sort.SliceStable(priority, func(i, j int) bool {
		leftClass := a.waitingPriorityClassLocked(priority[i])
		rightClass := a.waitingPriorityClassLocked(priority[j])
		if leftClass != rightClass {
			return leftClass > rightClass
		}
		if leftClass == 2 && priority[i].GuardLevel != priority[j].GuardLevel {
			return priority[i].GuardLevel < priority[j].GuardLevel
		}
		if leftClass == 1 && a.config.GiftPriority.SortByValue {
			if priority[i].PriorityGiftBattery != priority[j].PriorityGiftBattery {
				return priority[i].PriorityGiftBattery > priority[j].PriorityGiftBattery
			}
			if priority[i].PriorityAt != priority[j].PriorityAt {
				return priority[i].PriorityAt < priority[j].PriorityAt
			}
			return priority[i].JoinedAt < priority[j].JoinedAt
		}
		return false
	})
	a.queue = append([]QueueUser{current}, append(priority, regular...)...)
}

func (a *App) waitingPriorityClassLocked(user QueueUser) int {
	if a.config.Eligibility.GuardPriorityEnabled && user.GuardLevel >= 1 && user.GuardLevel <= 3 {
		return 2
	}
	if user.Priority {
		return 1
	}
	return 0
}

func (a *App) processGuard(guard GuardMessage) {
	guard.GuardLevel = normalizeGuardLevel(guard.GuardLevel)
	if guard.GuardLevel == 0 {
		return
	}
	a.mu.Lock()
	changed := false
	for i := range a.queue {
		user := &a.queue[i]
		if user.UID != guard.UID {
			continue
		}
		if guard.Username != "" && user.Username != guard.Username {
			user.Username = guard.Username
			changed = true
		}
		if guard.Avatar != "" && user.Avatar != guard.Avatar {
			user.Avatar = guard.Avatar
			changed = true
		}
		if user.GuardLevel != guard.GuardLevel {
			user.GuardLevel = guard.GuardLevel
			changed = true
		}
		break
	}
	if changed && a.config.Eligibility.GuardPriorityEnabled {
		a.normalizePriorityZoneLocked()
	}
	a.mu.Unlock()
	if !changed {
		return
	}
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
	clearCmd := strings.TrimSpace(a.config.ClearCommand)
	nextCmd := strings.TrimSpace(a.config.NextCommand)
	queueEnabled := a.config.QueueEnabled
	anchorUID := a.anchorUID
	a.mu.Unlock()
	a.broadcast()

	if clearCmd != "" && cmd == clearCmd {
		if anchorUID <= 0 {
			log.Printf("clear queue command ignored: anchor uid is unknown, sender uid=%d", msg.UID)
		} else if msg.UID != anchorUID {
			log.Printf("clear queue command ignored: sender uid=%d does not match anchor uid=%d", msg.UID, anchorUID)
		} else {
			a.clearQueue()
			return
		}
	}
	if nextCmd != "" && cmd == nextCmd {
		if anchorUID <= 0 {
			log.Printf("next queue command ignored: anchor uid is unknown, sender uid=%d", msg.UID)
		} else if msg.UID != anchorUID {
			log.Printf("next queue command ignored: sender uid=%d does not match anchor uid=%d", msg.UID, anchorUID)
		} else {
			a.advanceQueue()
			return
		}
	}
	if !queueEnabled {
		return
	}
	switch cmd {
	case joinCmd:
		if a.canJoinByIdentity(msg) {
			a.addUser(msg)
		} else {
			log.Printf("queue command ignored: sender uid=%d does not meet fan medal or guard eligibility", msg.UID)
		}
	case cancelCmd:
		a.cancelUser(msg.UID)
	}
}

func (a *App) canJoinByIdentity(msg ChatMessage) bool {
	a.mu.RLock()
	cfg := a.config.Eligibility
	roomID := a.resolvedRoomID
	anchorUID := a.anchorUID
	a.mu.RUnlock()
	if !cfg.FanMedalEnabled && !cfg.GuardEnabled {
		return true
	}
	fanEligible := false
	if cfg.FanMedalEnabled && msg.MedalLevel >= cfg.FanMedalLevel {
		currentRoomMedal := msg.MedalCurrentRoom
		if msg.MedalTargetUID > 0 && anchorUID > 0 {
			currentRoomMedal = msg.MedalTargetUID == anchorUID
		} else if msg.MedalRoomID > 0 && roomID > 0 {
			currentRoomMedal = msg.MedalRoomID == roomID
		}
		fanEligible = currentRoomMedal
	}
	guardEligible := cfg.GuardEnabled && validGuardLevel(msg.GuardLevel)
	return fanEligible || guardEligible
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
	a.anchorUID = 0
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
		if status.AnchorUID > 0 {
			a.anchorUID = status.AnchorUID
		}
		a.mu.Unlock()
		a.broadcast()
	}, a.processMessage, a.processGift, a.processGuard)
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
	mux.HandleFunc("/assets/icon_battery.png", serveEmbedded("assets/icon_battery.png", "image/png"))
	mux.HandleFunc("/assets/icon_captain.png", serveEmbedded("assets/icon_captain.png", "image/png"))
	mux.HandleFunc("/assets/icon_supervisor.png", serveEmbedded("assets/icon_supervisor.png", "image/png"))
	mux.HandleFunc("/assets/icon_governor.png", serveEmbedded("assets/icon_governor.png", "image/png"))
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
	mux.HandleFunc("/mini-control", serveEmbedded("web/mini-control.html", "text/html; charset=utf-8"))

	mux.HandleFunc("/api/fonts", a.handleFonts)
	mux.HandleFunc("/fonts/", a.handleFontFile)

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, a.state())
	})
	mux.HandleFunc("/api/update/notes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":  version,
			"notes":    latestEmbeddedReleaseNotes(),
			"releases": embeddedReleaseNotes(),
		})
	})
	mux.HandleFunc("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		info, err := a.checkForUpdates(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		if err := a.installLatestUpdate(ctx); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	})
	mux.HandleFunc("/api/update/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		info, err := a.downloadLatestUpdate(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "version": info.Version})
	})
	mux.HandleFunc("/api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := a.applyPreparedUpdate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	})
	mux.HandleFunc("/api/update/defer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		version, err := a.deferPreparedUpdate()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deferred", "version": version})
	})
	mux.HandleFunc("/api/update/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			AutoCheck bool `json:"autoCheck"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.setAutoCheckUpdates(req.AutoCheck)
		writeJSON(w, http.StatusOK, a.state())
	})
	mux.HandleFunc("/api/window/mini-control", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, miniControlWindowState())
	})
	mux.HandleFunc("/api/window/mini-control/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := openMiniControlWindow(a); err != nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, miniControlWindowState())
	})
	mux.HandleFunc("/api/window/mini-control/topmost", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Topmost bool `json:"topmost"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		state, err := setMiniControlWindowTopmost(a, req.Topmost)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("/events", a.handleEvents)
	mux.HandleFunc("/api/media/image", a.handleMediaImage)
	mux.HandleFunc("/api/config/export", a.handleConfigExport)
	mux.HandleFunc("/api/config/import", a.handleConfigImport)
	mux.HandleFunc("/api/hotkeys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var hotkeys HotkeyConfig
		if err := decodeJSON(r, &hotkeys); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hotkeys.OpenControl = strings.TrimSpace(hotkeys.OpenControl)
		hotkeys.OpenMiniControl = strings.TrimSpace(hotkeys.OpenMiniControl)
		hotkeys.NextQueue = strings.TrimSpace(hotkeys.NextQueue)
		hotkeys.ClearQueue = strings.TrimSpace(hotkeys.ClearQueue)
		a.mu.Lock()
		a.config.Hotkeys = hotkeys
		a.config.SchemaVersion = defaultConfig().SchemaVersion
		a.mu.Unlock()
		a.saveConfig()
		a.applyHotkeys(hotkeys)
		writeJSON(w, http.StatusOK, a.state())
	})
	mux.HandleFunc("/api/server/listen", a.handleServerListen)

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
		oldHotkeys := a.config.Hotkeys
		a.config = cfg
		a.normalizePriorityZoneLocked()
		a.mu.Unlock()
		a.saveConfig()
		if oldHotkeys != cfg.Hotkeys {
			a.applyHotkeys(cfg.Hotkeys)
		} else {
			a.broadcast()
		}
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
		manualSequence := a.messageSeq.Add(1) % 1000
		uid := -(time.Now().UnixMilli()*1000 + int64(manualSequence))
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
		a.advanceQueue()
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
		a.clearQueue()
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
			QueueUserID string  `json:"queueUserId"`
			UID         int64   `json:"uid"`
			Username    string  `json:"username"`
			GiftName    string  `json:"giftName"`
			Battery     float64 `json:"battery"`
			GiftIcon    string  `json:"giftIcon"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.QueueUserID != "" {
			found := false
			a.mu.RLock()
			for _, user := range a.queue {
				if user.ID == req.QueueUserID {
					req.UID = user.UID
					req.Username = user.Username
					found = true
					break
				}
			}
			a.mu.RUnlock()
			if !found {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "目标队列用户不存在"})
				return
			}
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
		a.processGift(GiftMessage{EventID: fmt.Sprintf("debug-%d-%d", time.Now().UnixNano(), a.messageSeq.Add(1)), UID: req.UID, Username: req.Username, GiftName: req.GiftName, GiftIcon: req.GiftIcon, Num: 1, CoinType: "gold", TotalCoin: int64(req.Battery * 100), Battery: req.Battery, ReceivedAt: time.Now().UnixMilli()})
		writeJSON(w, http.StatusOK, a.state())
	})

	mux.HandleFunc("/api/debug/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			UID              int64  `json:"uid"`
			Username         string `json:"username"`
			Text             string `json:"text"`
			MedalLevel       int    `json:"medalLevel"`
			MedalRoomID      int64  `json:"medalRoomId"`
			MedalTargetUID   int64  `json:"medalTargetUid"`
			MedalCurrentRoom bool   `json:"medalCurrentRoom"`
			GuardLevel       int    `json:"guardLevel"`
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
		a.processMessage(ChatMessage{
			UID: req.UID, Username: req.Username, Text: req.Text,
			MedalLevel: req.MedalLevel, MedalRoomID: req.MedalRoomID,
			MedalTargetUID: req.MedalTargetUID, MedalCurrentRoom: req.MedalCurrentRoom,
			GuardLevel: req.GuardLevel,
		})
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

func freshOpenURL(raw string) string {
	separator := "?"
	if strings.Contains(raw, "?") {
		separator = "&"
	}
	return raw + separator + "_open=" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func openBrowser(url string) error {
	log.Printf("open browser: %s", url)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = windowsOpenBrowserCmd(url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func setupLogging(dataDir string) *os.File {
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Printf("create log dir: %v", err)
		return nil
	}
	path := filepath.Join(logDir, "biliqueue.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("open log file: %v", err)
		return nil
	}
	log.SetOutput(io.MultiWriter(file, os.Stderr))
	return file
}

type ServerController struct {
	mu       sync.Mutex
	app      *App
	server   *http.Server
	listener net.Listener
	addr     string
}

func NewServerController(app *App) *ServerController {
	return &ServerController{app: app}
}

func (sc *ServerController) Start(addr string) error {
	addr = normalizeListenAddress(addr, defaultConfig().ListenAddress)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: sc.app.routes(), ReadHeaderTimeout: 10 * time.Second}
	sc.mu.Lock()
	sc.server = srv
	sc.listener = ln
	sc.addr = addr
	sc.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
			showErrorDialog("BiliQueue 服务异常", err.Error())
		}
	}()
	return nil
}

func (sc *ServerController) ChangeListenAddress(addr string) (PublicState, error) {
	addr = normalizeListenAddress(addr, defaultConfig().ListenAddress)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return PublicState{}, err
	}
	newServer := &http.Server{Addr: addr, Handler: sc.app.routes(), ReadHeaderTimeout: 10 * time.Second}
	sc.mu.Lock()
	oldServer := sc.server
	sc.server = newServer
	sc.listener = ln
	sc.addr = addr
	sc.mu.Unlock()

	sc.app.mu.Lock()
	sc.app.config.ListenAddress = addr
	sc.app.mu.Unlock()
	sc.app.saveConfig()
	go func() {
		if err := newServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server changed listener failed: %v", err)
		}
	}()
	refreshMiniControlWindow(sc.app)
	if oldServer != nil {
		go func() {
			time.Sleep(650 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = oldServer.Shutdown(ctx)
		}()
	}
	sc.app.broadcast()
	return sc.app.state(), nil
}

func (sc *ServerController) Shutdown(ctx context.Context) error {
	sc.mu.Lock()
	srv := sc.server
	sc.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (sc *ServerController) Close() error {
	sc.mu.Lock()
	srv := sc.server
	sc.server = nil
	sc.listener = nil
	sc.addr = ""
	sc.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

func (a *App) prepareExit() {
	a.mu.Lock()
	if a.connectionCancel != nil {
		a.connectionCancel()
		a.connectionCancel = nil
	}
	a.connectionGeneration++
	a.connectionStatus = "disconnected"
	a.connectionDetail = "已退出"
	a.mu.Unlock()
	a.saveConfig()
	a.saveQueue()
}

func (a *App) handleServerListen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ListenAddress string `json:"listenAddress"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if a.serverControl == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务控制器未初始化"})
		return
	}
	state, err := a.serverControl.ChangeListenAddress(req.ListenAddress)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "端口切换失败：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func chooseStartupListenAddress(app *App, flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return normalizeListenAddress(flagValue, defaultConfig().ListenAddress)
	}
	return normalizeListenAddress(app.config.ListenAddress, defaultConfig().ListenAddress)
}

func listenPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "18303"
	}
	return port
}

func main() {
	defaultDataDir := "data"
	defaultFontsDir := ""
	defaultListen := ""
	defaultInstanceID := ""
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		defaultDataDir = filepath.Join(executableDir, "data")
		if buildProfile == "isolated-test" {
			defaultDataDir = filepath.Join(executableDir, "test-data")
			defaultFontsDir = filepath.Join(executableDir, "test-fonts")
			defaultListen = "127.0.0.1:18313"
			defaultInstanceID = "v" + version + "-isolated-test"
		}
	}
	listenFlag := flag.String("listen", defaultListen, "HTTP listen address")
	dataDir := flag.String("data", defaultDataDir, "data directory")
	fontsDir := flag.String("fonts", defaultFontsDir, "fonts directory; defaults to sibling fonts directory for normal data dirs")
	openBrowserOnStart := flag.Bool("open-browser", false, "open the control page on startup")
	noBrowser := flag.Bool("no-browser", false, "do not open the control page; kept for compatibility")
	noTray := flag.Bool("no-tray", false, "disable the Windows system tray menu")
	instanceID := flag.String("instance-id", defaultInstanceID, "single-instance namespace; use a unique value for isolated test runs")
	updateHelper := flag.Bool("update-helper", false, "internal update helper mode")
	updateTarget := flag.String("update-target", "", "internal update target")
	updatePackageRoot := flag.String("update-package-root", "", "internal update package root")
	updateRestartFile := flag.String("update-restart-file", "", "internal update restart specification")
	updateParentPID := flag.Int("update-parent-pid", 0, "internal update parent process id")
	updateCleanupRoot := flag.String("update-cleanup-root", "", "internal update cleanup directory")
	updateCleanupBackup := flag.String("update-cleanup-backup", "", "internal update backup path")
	updateCompletedVersion := flag.String("update-completed-version", "", "internal completed update notification version")
	flag.Parse()
	if *updateHelper {
		if err := runUpdateHelper(*updateTarget, *updatePackageRoot, *updateParentPID, *updateRestartFile); err != nil {
			showErrorDialog("BiliQueue 更新失败", err.Error())
		}
		return
	}
	if *updateCleanupRoot != "" || *updateCleanupBackup != "" {
		cleanupUpdateArtifacts(*updateCleanupRoot, *updateCleanupBackup)
	}
	if launched, err := launchDeferredUpdateIfPresent(); err != nil {
		discardDeferredUpdate()
		showErrorDialog("BiliQueue 更新失败", err.Error())
	} else if launched {
		return
	}

	logFile := setupLogging(*dataDir)
	if logFile != nil {
		defer logFile.Close()
	}

	app := newAppWithFonts(*dataDir, *fontsDir)
	preloadAppDialogHost()
	listen := chooseStartupListenAddress(app, *listenFlag)
	if release, already := acquireSingleInstance(*instanceID); already {
		log.Printf("another instance is already running")
		showInfoDialog("啊哦！", "已有 BiliQueue 正在运行！")
		if release != nil {
			release()
		}
		return
	} else if release != nil {
		defer release()
	}

	controller := NewServerController(app)
	app.serverControl = controller

	for {
		if err := controller.Start(listen); err != nil {
			log.Printf("server start failed on %s: %v", listen, err)
			if runtime.GOOS == "windows" {
				input, ok := promptListenAddress("啊哦！", "BiliQueue 所需的端口被占用了！请输入一个新的端口！", "127.0.0.1:"+listenPort(listen))
				if ok {
					listen = normalizeListenAddress(input, defaultConfig().ListenAddress)
					continue
				}
				log.Printf("startup canceled after listen failure")
				return
			}
			showErrorDialog("BiliQueue 启动失败", err.Error())
			os.Exit(1)
		}
		break
	}

	app.mu.Lock()
	app.config.ListenAddress = listen
	app.mu.Unlock()
	app.saveConfig()
	preloadMiniControlWindow(app)

	controlURL := urlForListen(listen, "/control")
	overlayURL := urlForListen(listen, "/overlay")
	log.Printf("BiliQueue %s", version)
	log.Printf("control: %s", controlURL)
	log.Printf("browser source: %s", overlayURL)
	app.autoConnectIfReady("startup")
	app.startAutoUpdateChecks()

	if *openBrowserOnStart && !*noBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := openBrowser(controlURL); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}

	if runtime.GOOS == "windows" {
		if completedVersion := strings.TrimSpace(*updateCompletedVersion); completedVersion != "" {
			go notifyUpdateCompleted(completedVersion)
		}
		if err := runTray(app, controller, *dataDir, !*noTray); err != nil {
			log.Printf("windows message service: %v", err)
			showErrorDialog("BiliQueue Windows 服务启动失败", err.Error())
		}
		_ = controller.Close()
		return
	}

	app.mu.RLock()
	hotkeys := app.config.Hotkeys
	app.mu.RUnlock()
	app.applyHotkeys(hotkeys)
	select {}
}
