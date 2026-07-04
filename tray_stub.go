//go:build !windows

package main

import "net/http"

func runTray(app *App, controlURL, overlayURL, dataDir string, server *http.Server) error {
	return nil
}

func showErrorDialog(title, message string) {}
