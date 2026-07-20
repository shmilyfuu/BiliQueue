//go:build !windows

package main

func reloadGlobalHotkeys(cfg HotkeyConfig) map[string]string {
	return defaultHotkeyStatus("全局快捷键仅支持 Windows")
}
