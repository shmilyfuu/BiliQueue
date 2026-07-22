//go:build !windows

package main

func openMiniControlWindow(app *App) error { return errMiniControlWindowUnavailable }

func toggleMiniControlWindow(app *App) error { return errMiniControlWindowUnavailable }

func setMiniControlWindowTopmost(app *App, topmost bool) (MiniControlWindowState, error) {
	return miniControlWindowState(), errMiniControlWindowUnavailable
}

func miniControlWindowState() MiniControlWindowState { return MiniControlWindowState{} }

func refreshMiniControlWindow(app *App) {}

func preloadMiniControlWindow(app *App) {}

func closeMiniControlWindow() {}
