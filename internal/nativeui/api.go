package nativeui

import (
	"errors"
	"image"
)

var ErrUnavailable = errors.New("native Windows UI is unavailable")

type DialogKind string

const (
	DialogInfo     DialogKind = "info"
	DialogError    DialogKind = "error"
	DialogConfirm  DialogKind = "confirm"
	DialogDanger   DialogKind = "confirmDanger"
	DialogPort     DialogKind = "port"
	DialogProgress DialogKind = "progress"
)

type DialogRequest struct {
	Target      string
	Kind        DialogKind
	Title       string
	Message     string
	ConfirmText string
	CancelText  string
	Host        string
	Port        string
	Progress    int
	Stage       string
}

type DialogResult struct {
	Accepted bool
	Host     string
	Port     string
}

type ProgressHandle interface {
	Update(stage string, percent int)
	Close()
}

type MiniUser struct {
	ID          string
	Username    string
	Avatar      string
	GuardLevel  int
	Manual      bool
	GiftBattery float64
}

type MiniState struct {
	Connected bool
	Paused    bool
	Queue     []MiniUser
}

type MiniController struct {
	Subscribe      func() (<-chan MiniState, func())
	Next           func()
	SetPaused      func(bool)
	Clear          func()
	Add            func(string) (bool, string)
	Remove         func(string)
	Reorder        func([]string)
	LoadImage      func(string) (image.Image, error)
	GuardIcon      func(int) (image.Image, error)
	TopmostChanged func(bool)
}

type MiniWindowState struct {
	Active  bool
	Visible bool
	Topmost bool
}

type MenuItem struct {
	ID        int
	Label     string
	Checked   bool
	Separator bool
	Danger    bool
}
