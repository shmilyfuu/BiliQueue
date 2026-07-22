//go:build !windows

package main

func notifyUpdateCompleted(string) {}

func preloadAppDialogHost() {}

func runTray(app *App, controller *ServerController, dataDir string, showIcon bool) error { return nil }

func showErrorDialog(title, message string) {}
func showInfoDialog(title, message string)  {}
