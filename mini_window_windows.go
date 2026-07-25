//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"biliqueue/internal/nativeui"
)

type miniWindowPreferences struct {
	Topmost bool `json:"topmost"`
}

func openMiniControlWindow(app *App) error {
	if app == nil {
		return errMiniControlWindowUnavailable
	}
	host, err := nativeui.DefaultHost()
	if err != nil {
		return err
	}
	prefs := loadMiniWindowPreferences(app)
	return host.OpenMini(nativeMiniController(app), prefs.Topmost)
}

func preloadMiniControlWindow(app *App) {
	if app == nil {
		return
	}
	go func() {
		host, err := nativeui.DefaultHost()
		if err != nil {
			return
		}
		prefs := loadMiniWindowPreferences(app)
		_ = host.PreloadMini(nativeMiniController(app), prefs.Topmost)
	}()
}

func toggleMiniControlWindow(app *App) error {
	if app == nil {
		return errMiniControlWindowUnavailable
	}
	host, err := nativeui.DefaultHost()
	if err != nil {
		return err
	}
	prefs := loadMiniWindowPreferences(app)
	return host.ToggleMini(nativeMiniController(app), prefs.Topmost)
}

func miniControlWindowState() MiniControlWindowState {
	host, err := nativeui.DefaultHost()
	if err != nil {
		return MiniControlWindowState{Supported: true}
	}
	state := host.MiniState()
	return MiniControlWindowState{
		Supported: true,
		Active:    state.Active,
		Opening:   false,
		Visible:   state.Visible,
		Topmost:   state.Topmost,
	}
}

func setMiniControlWindowTopmost(app *App, topmost bool) (MiniControlWindowState, error) {
	host, err := nativeui.DefaultHost()
	if err != nil {
		return miniControlWindowState(), err
	}
	state, err := host.SetMiniTopmost(topmost)
	if err != nil {
		return miniControlWindowState(), errMiniControlWindowUnavailable
	}
	if err := saveMiniWindowPreferences(app, miniWindowPreferences{Topmost: topmost}); err != nil {
		return miniControlWindowState(), err
	}
	return MiniControlWindowState{
		Supported: true,
		Active:    state.Active,
		Visible:   state.Visible,
		Topmost:   state.Topmost,
	}, nil
}

func refreshMiniControlWindow(app *App) {
	// The native window subscribes directly to App state, so no navigation or
	// explicit refresh is necessary after a port change.
}

func closeMiniControlWindow() {
	host, err := nativeui.DefaultHost()
	if err == nil {
		host.CloseMini()
	}
}

func nativeMiniController(app *App) nativeui.MiniController {
	return nativeui.MiniController{
		Subscribe: func() (<-chan nativeui.MiniState, func()) {
			source, unsubscribe := app.subscribeState()
			output := make(chan nativeui.MiniState, 1)
			done := make(chan struct{})
			var once sync.Once
			cancel := func() {
				once.Do(func() {
					close(done)
					unsubscribe()
				})
			}
			go func() {
				defer close(output)
				for {
					select {
					case state := <-source:
						converted := toNativeMiniState(state)
						select {
						case output <- converted:
						default:
							select {
							case <-output:
							default:
							}
							select {
							case output <- converted:
							default:
							}
						}
					case <-done:
						return
					}
				}
			}()
			return output, cancel
		},
		Next:      app.advanceQueue,
		SetPaused: app.setQueuePaused,
		Clear:     app.clearQueue,
		Add:       app.addManualUser,
		Remove: func(id string) {
			app.removeQueueUser(id)
		},
		Reorder: app.reorderQueue,
		LoadImage: func(raw string) (image.Image, error) {
			return app.loadNativeImage(raw)
		},
		GuardIcon: loadNativeGuardIcon,
		TopmostChanged: func(topmost bool) {
			_ = saveMiniWindowPreferences(app, miniWindowPreferences{Topmost: topmost})
		},
	}
}

func toNativeMiniState(state PublicState) nativeui.MiniState {
	queue := make([]nativeui.MiniUser, len(state.Queue))
	for index, user := range state.Queue {
		queue[index] = nativeui.MiniUser{
			ID:          user.ID,
			Username:    user.Username,
			Avatar:      user.Avatar,
			GuardLevel:  user.GuardLevel,
			Manual:      user.Manual,
			GiftBattery: user.GiftBattery,
		}
	}
	return nativeui.MiniState{
		Connected: state.ConnectionStatus == "connected",
		Paused:    state.Paused,
		Queue:     queue,
	}
}

func (app *App) loadNativeImage(raw string) (image.Image, error) {
	url, err := normalizeProxyImageURL(raw)
	if err != nil {
		return nil, err
	}
	normalized := url.String()
	path := filepath.Join(app.imageCacheDir(), imageCacheName(normalized))
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data, err = app.fetchAndCacheImage(ctx, normalized, path)
		if err != nil {
			return nil, err
		}
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode native avatar: %w", err)
	}
	return decoded, nil
}

func loadNativeGuardIcon(level int) (image.Image, error) {
	name := map[int]string{
		1: "assets/icon_governor.png",
		2: "assets/icon_supervisor.png",
		3: "assets/icon_captain.png",
	}[level]
	if name == "" {
		return nil, fmt.Errorf("unknown guard level %d", level)
	}
	data, err := webFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	return decoded, err
}

func miniWindowPreferencesPath(app *App) string {
	return filepath.Join(app.dataDir, "mini-window.json")
}

func loadMiniWindowPreferences(app *App) miniWindowPreferences {
	var prefs miniWindowPreferences
	data, err := os.ReadFile(miniWindowPreferencesPath(app))
	if err == nil {
		_ = json.Unmarshal(data, &prefs)
	}
	return prefs
}

func saveMiniWindowPreferences(app *App, prefs miniWindowPreferences) error {
	if app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	return writeJSONAtomic(miniWindowPreferencesPath(app), prefs)
}

func splitNativeListenAddress(value string) (string, string) {
	host, port := splitListenAddress(strings.TrimSpace(value))
	return host, port
}
