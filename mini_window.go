package main

import "errors"

var errMiniControlWindowUnavailable = errors.New("原生简易控制窗口当前不可用")

type MiniControlWindowState struct {
	Supported bool `json:"supported"`
	Active    bool `json:"active"`
	Opening   bool `json:"opening"`
	Topmost   bool `json:"topmost"`
}
