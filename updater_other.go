//go:build !windows

package main

import "errors"

func validateSelfUpdateTarget() error {
	return errors.New("自动安装更新目前仅支持 Windows 发布版")
}

func launchUpdateHelper(app *App, prepared preparedUpdate) error {
	return errors.New("自动安装更新目前仅支持 Windows 发布版")
}

func notifyUpdateAvailable(app *App, info UpdateInfo) {}

func launchDeferredUpdateIfPresent() (bool, error) {
	return false, nil
}

func runUpdateHelper(target, packageRoot string, parentPID int, restartFile string) error {
	return errors.New("自动安装更新目前仅支持 Windows 发布版")
}

func cleanupUpdateArtifacts(root, backup string) {}
