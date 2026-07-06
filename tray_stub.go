//go:build !windows

package main

func runTray(app *App, controller *ServerController, dataDir string) error { return nil }

func showErrorDialog(title, message string) {}
func showInfoDialog(title, message string)  {}
