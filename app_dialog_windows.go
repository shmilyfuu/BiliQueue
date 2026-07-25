//go:build windows

package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"biliqueue/internal/nativeui"
)

func preloadAppDialogHost() {
	_ = nativeui.Preload()
}

func promptListenAddress(title, message, defaultValue string) (string, bool) {
	hostValue, portValue := splitListenAddress(defaultValue)
	target := "portChange"
	if strings.Contains(message, "占用") {
		target = "portConflict"
	}
	result, err := showNativeDialog(nativeui.DialogRequest{
		Target: target,
		Kind:   nativeui.DialogPort,
		Host:   hostValue,
		Port:   portValue,
	})
	if err != nil {
		log.Printf("native listen dialog unavailable: %v", err)
		return "", false
	}
	if !result.Accepted {
		return "", false
	}
	hostValue = strings.TrimSpace(result.Host)
	if hostValue == "" {
		hostValue = "127.0.0.1"
	}
	return net.JoinHostPort(hostValue, strings.TrimSpace(result.Port)), true
}

func splitListenAddress(value string) (string, string) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, ":") {
		value = "127.0.0.1" + value
	}
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		if strings.TrimSpace(host) == "" {
			host = "127.0.0.1"
		}
		return host, port
	}
	if value != "" && !strings.Contains(value, ":") {
		return "127.0.0.1", value
	}
	return "127.0.0.1", ""
}

func showStyledConfirmDialog(title, message string) bool {
	return showStyledChoiceDialogWithTarget("clearQueue", nativeui.DialogConfirm, title, message, "确定", "取消")
}

func showStyledChoiceDialog(title, message, confirmText, cancelText string) bool {
	target := "genericConfirm"
	switch title {
	case "发现新版本":
		target = "updateAvailable"
	case "更新已下载":
		target = "updateReady"
	}
	return showStyledChoiceDialogWithTarget(target, nativeui.DialogConfirm, title, message, confirmText, cancelText)
}

func showStyledChoiceDialogWithTarget(target string, kind nativeui.DialogKind, title, message, confirmText, cancelText string) bool {
	result, err := showNativeDialog(nativeui.DialogRequest{
		Target:      target,
		Kind:        kind,
		Title:       title,
		Message:     message,
		ConfirmText: confirmText,
		CancelText:  cancelText,
	})
	if err != nil {
		return messageBox(title, message, 0x00000001|mbSetForeground) == 1
	}
	return result.Accepted
}

func showStyledInfoDialog(title, message string) {
	target := "genericInfo"
	if strings.Contains(message, "已经在运行") || strings.Contains(message, "正在运行") {
		target = "duplicateInstance"
	} else if strings.Contains(title, "更新完成") {
		target = "updateComplete"
	}
	if _, err := showNativeDialog(nativeui.DialogRequest{
		Target:      target,
		Kind:        nativeui.DialogInfo,
		Title:       title,
		Message:     message,
		ConfirmText: "确认",
	}); err != nil {
		messageBox(title, message, mbOK|mbIconInfo|mbSetForeground)
	}
}

func showStyledErrorDialog(title, message string) {
	target := "genericError"
	if strings.Contains(title, "启动失败") || strings.Contains(title, "服务异常") {
		target = "startupFailed"
	} else if strings.Contains(title, "更新失败") {
		target = "updateFailed"
	} else if strings.Contains(title, "复制失败") {
		target = "copyURLFailed"
	}
	if _, err := showNativeDialog(nativeui.DialogRequest{
		Target:      target,
		Kind:        nativeui.DialogError,
		Title:       title,
		Message:     message,
		ConfirmText: "确认",
	}); err != nil {
		messageBox(title, message, mbOK|mbIconError|mbSetForeground)
	}
}

func showNativeDialog(request nativeui.DialogRequest) (nativeui.DialogResult, error) {
	host, err := nativeui.DefaultHost()
	if err != nil {
		return nativeui.DialogResult{}, err
	}
	result, err := host.ShowDialog(request)
	if err != nil {
		return nativeui.DialogResult{}, fmt.Errorf("show native dialog: %w", err)
	}
	return result, nil
}

func closeActiveWebDialog() {
	closeActivePrompt()
}

func closeActivePrompt() {
	if host, err := nativeui.DefaultHost(); err == nil {
		host.CloseDialogs()
	}
}

func runUpdateHelperProgressWindow(targetVersion string, task func(report func(string, int)) error) error {
	host, err := nativeui.DefaultHost()
	if err != nil {
		return task(func(string, int) {})
	}
	handle, err := host.OpenProgress(nativeui.DialogRequest{
		Target:      "updateProgress",
		Kind:        nativeui.DialogProgress,
		Title:       "正在更新 BiliQueue v" + targetVersion,
		Message:     "正在启动更新助手",
		ConfirmText: "后台运行",
		Progress:    8,
		Stage:       "正在启动更新助手",
	})
	if err != nil {
		return task(func(string, int) {})
	}
	defer handle.Close()
	taskErr := task(func(stage string, percent int) {
		handle.Update(stage, percent)
	})
	if taskErr == nil {
		handle.Update("新版本已启动", 100)
		time.Sleep(900 * time.Millisecond)
	}
	return taskErr
}
