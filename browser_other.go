//go:build !windows

package main

import "os/exec"

func windowsOpenBrowserCmd(url string) *exec.Cmd {
	return exec.Command("xdg-open", url)
}
