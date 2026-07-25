package nativeui

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// The production application embeds the designer-authored files. They are
// intentionally not written beside the executable or exposed for live editing.
//
//go:embed design/theme.json
var embeddedTheme []byte

//go:embed design/layout.json
var embeddedLayout []byte

type Theme struct {
	SchemaVersion   int                       `json:"schemaVersion,omitempty"`
	Material        WindowMaterial            `json:"material"`
	TargetMaterials map[string]WindowMaterial `json:"targetMaterials,omitempty"`
	Colors          ThemeColors               `json:"colors"`
	Radius          ThemeRadius               `json:"radius"`
	Typography      ThemeTypography           `json:"typography"`
	Styles          map[string]ElementVisual  `json:"styles"`
}

type WindowMaterial struct {
	Backdrop                 string `json:"backdrop"`
	WindowOpacity            int    `json:"windowOpacity"`
	BackgroundOpacity        int    `json:"backgroundOpacity"`
	TintColor                string `json:"tintColor"`
	TintOpacity              int    `json:"tintOpacity"`
	BlurStrength             int    `json:"blurStrength"`
	NoiseOpacity             int    `json:"noiseOpacity"`
	TitleBarFollowBackground bool   `json:"titleBarFollowBackground"`
	TitleBarColor            string `json:"titleBarColor"`
	TitleBarTextColor        string `json:"titleBarTextColor"`
	TitleBarBorderColor      string `json:"titleBarBorderColor"`
}

type GradientStop struct {
	Position int    `json:"position"`
	Color    string `json:"color"`
	Opacity  int    `json:"opacity"`
}

type BrushSpec struct {
	Type    string         `json:"type"`
	Stops   []GradientStop `json:"stops,omitempty"`
	Color1  string         `json:"color1,omitempty"`
	Color2  string         `json:"color2,omitempty"`
	Opacity int            `json:"opacity"`
	Angle   int            `json:"angle"`
	Stop    int            `json:"stop,omitempty"`
	CenterX int            `json:"centerX"`
	CenterY int            `json:"centerY"`
	Radius  int            `json:"radius,omitempty"`
	Shape   string         `json:"shape,omitempty"`
}

type AcrylicSpec struct {
	Enabled           bool   `json:"enabled"`
	Blur              int    `json:"blur"`
	TintColor         string `json:"tintColor"`
	TintOpacity       int    `json:"tintOpacity"`
	LuminosityOpacity int    `json:"luminosityOpacity"`
	NoiseOpacity      int    `json:"noiseOpacity"`
}

type ElementVisual struct {
	Material       string      `json:"material,omitempty"`
	Acrylic        AcrylicSpec `json:"acrylic,omitempty"`
	Hidden         bool        `json:"hidden,omitempty"`
	FillDisabled   bool        `json:"fillDisabled,omitempty"`
	BorderDisabled bool        `json:"borderDisabled,omitempty"`
	BorderPosition string      `json:"borderPosition,omitempty"`
	Fill           BrushSpec   `json:"fill"`
	Border         BrushSpec   `json:"border"`
	BorderColor    string      `json:"borderColor,omitempty"`
	BorderWidth    int         `json:"borderWidth"`
	Text           string      `json:"text,omitempty"`
	TextColor      string      `json:"textColor"`
	Radius         int         `json:"radius"`
	FontSize       int         `json:"fontSize"`
	FontWeight     int         `json:"fontWeight,omitempty"`
}

type ThemeColors struct {
	WindowBackground    string `json:"windowBackground"`
	WindowBackgroundTop string `json:"windowBackgroundTop"`
	CardBackground      string `json:"cardBackground"`
	CardBorder          string `json:"cardBorder"`
	Surface             string `json:"surface"`
	SurfaceHover        string `json:"surfaceHover"`
	InputBackground     string `json:"inputBackground"`
	InputBorder         string `json:"inputBorder"`
	Primary             string `json:"primary"`
	PrimaryHover        string `json:"primaryHover"`
	Danger              string `json:"danger"`
	DangerHover         string `json:"dangerHover"`
	Button              string `json:"button"`
	ButtonHover         string `json:"buttonHover"`
	TextPrimary         string `json:"textPrimary"`
	TextSecondary       string `json:"textSecondary"`
	TextMuted           string `json:"textMuted"`
	StatusSurface       string `json:"statusSurface"`
	StatusBorder        string `json:"statusBorder"`
	StatusConnected     string `json:"statusConnected"`
	StatusDisconnected  string `json:"statusDisconnected"`
	GiftSurface         string `json:"giftSurface"`
	GiftBorder          string `json:"giftBorder"`
	GiftText            string `json:"giftText"`
	Selection           string `json:"selection"`
	Inspector           string `json:"inspector"`
	OverlayText         string `json:"overlayText"`
}

type ThemeRadius struct {
	Card   int `json:"card"`
	Button int `json:"button"`
	Input  int `json:"input"`
	Row    int `json:"row"`
	Pill   int `json:"pill"`
	Avatar int `json:"avatar"`
}

type ThemeTypography struct {
	Family       string `json:"family"`
	TitleSize    int    `json:"titleSize"`
	SubtitleSize int    `json:"subtitleSize"`
	HeaderSize   int    `json:"headerSize"`
	BodySize     int    `json:"bodySize"`
	SmallSize    int    `json:"smallSize"`
}

type PointDelta struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type SizeDelta struct {
	W int `json:"w"`
	H int `json:"h"`
}

type TargetLayout struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	Padding int `json:"padding"`
}

type Layout struct {
	Window struct {
		Width   int `json:"width"`
		Height  int `json:"height"`
		Padding int `json:"padding"`
	} `json:"window"`
	TopBar struct {
		Height       int `json:"height"`
		MarginBottom int `json:"marginBottom"`
		StatusWidth  int `json:"statusWidth"`
		ActionGap    int `json:"actionGap"`
	} `json:"topBar"`
	Card struct {
		HeaderHeight int `json:"headerHeight"`
		BodyPadding  int `json:"bodyPadding"`
	} `json:"card"`
	Toolbar struct {
		Height       int `json:"height"`
		Gap          int `json:"gap"`
		MarginBottom int `json:"marginBottom"`
		ButtonWidth  int `json:"buttonWidth"`
	} `json:"toolbar"`
	Manual struct {
		Height        int `json:"height"`
		Gap           int `json:"gap"`
		MarginBottom  int `json:"marginBottom"`
		PaddingBottom int `json:"paddingBottom"`
		ButtonWidth   int `json:"buttonWidth"`
	} `json:"manual"`
	Queue struct {
		RowHeight             int  `json:"rowHeight"`
		RowGap                int  `json:"rowGap"`
		RowPadding            int  `json:"rowPadding"`
		DragWidth             int  `json:"dragWidth"`
		PositionWidth         int  `json:"positionWidth"`
		AvatarSize            int  `json:"avatarSize"`
		RemoveWidth           int  `json:"removeWidth"`
		GiftMaxWidth          int  `json:"giftMaxWidth"`
		CellGap               int  `json:"cellGap"`
		ScrollbarWidth        int  `json:"scrollbarWidth,omitempty"`
		ScrollbarInset        int  `json:"scrollbarInset,omitempty"`
		ScrollbarMinThumb     int  `json:"scrollbarMinThumb,omitempty"`
		ScrollbarAutoHide     bool `json:"scrollbarAutoHide,omitempty"`
		ScrollbarShowDelayMS  int  `json:"scrollbarShowDelayMs,omitempty"`
		ScrollbarFadeDuration int  `json:"scrollbarFadeDurationMs,omitempty"`
	} `json:"queue"`
	Targets    map[string]TargetLayout `json:"targets,omitempty"`
	Offsets    map[string]PointDelta   `json:"offsets"`
	SizeDeltas map[string]SizeDelta    `json:"sizeDeltas"`
}

type Design struct {
	Theme  Theme
	Layout Layout
}

func LoadDesign() (Design, error) {
	var result Design
	if err := json.Unmarshal(embeddedTheme, &result.Theme); err != nil {
		return Design{}, fmt.Errorf("decode embedded native theme: %w", err)
	}
	if err := json.Unmarshal(embeddedLayout, &result.Layout); err != nil {
		return Design{}, fmt.Errorf("decode embedded native layout: %w", err)
	}
	if result.Theme.SchemaVersion != 16 {
		return Design{}, fmt.Errorf("unsupported native theme schema %d", result.Theme.SchemaVersion)
	}
	// The production mini-control content size is intentionally fixed, even
	// though the designer JSON was exported with a shorter preview height.
	result.Layout.Window.Width = 420
	result.Layout.Window.Height = 560
	if result.Layout.Window.Padding <= 0 {
		result.Layout.Window.Padding = 16
	}
	if result.Theme.Styles == nil || result.Layout.Targets == nil {
		return Design{}, fmt.Errorf("embedded native design is incomplete")
	}
	return result, nil
}

func (d Design) Style(path string) ElementVisual {
	return d.Theme.Styles[path]
}

func (d Design) MaterialFor(target string) WindowMaterial {
	if value, ok := d.Theme.TargetMaterials[target]; ok {
		return value
	}
	return d.Theme.Material
}

func (d Design) TargetLayout(target string, fallback TargetLayout) TargetLayout {
	if value, ok := d.Layout.Targets[target]; ok {
		if value.Width > 0 {
			fallback.Width = value.Width
		}
		if value.Height > 0 {
			fallback.Height = value.Height
		}
		if value.Padding > 0 {
			fallback.Padding = value.Padding
		}
	}
	return fallback
}
