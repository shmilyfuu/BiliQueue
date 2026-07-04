//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func windowsOpenBrowserCmd(url string) *exec.Cmd {
	cmd := exec.Command("cmd", "/c", "start", "", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
