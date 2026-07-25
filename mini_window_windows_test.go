//go:build windows

package main

import (
	"os"
	"testing"
	"time"
)

func TestMiniWindowPreferencesRoundTrip(t *testing.T) {
	app := newApp(t.TempDir())
	want := miniWindowPreferences{Topmost: true}
	if err := saveMiniWindowPreferences(app, want); err != nil {
		t.Fatal(err)
	}
	if got := loadMiniWindowPreferences(app); got != want {
		t.Fatalf("preferences mismatch: got %#v want %#v", got, want)
	}
}

func TestNativeMiniControlWindow(t *testing.T) {
	if os.Getenv("BILIQUEUE_NATIVE_UI_INTEGRATION") != "1" {
		t.Skip("set BILIQUEUE_NATIVE_UI_INTEGRATION=1 to open the native test window")
	}
	app := newApp(t.TempDir())
	if err := openMiniControlWindow(app); err != nil {
		t.Fatal(err)
	}
	defer closeMiniControlWindow()
	waitForMiniWindowState(t, 5*time.Second, func(state MiniControlWindowState) bool {
		return state.Active && state.Visible
	})
	if err := toggleMiniControlWindow(app); err != nil {
		t.Fatal(err)
	}
	waitForMiniWindowState(t, 3*time.Second, func(state MiniControlWindowState) bool {
		return state.Active && !state.Visible
	})
	if err := toggleMiniControlWindow(app); err != nil {
		t.Fatal(err)
	}
	waitForMiniWindowState(t, 3*time.Second, func(state MiniControlWindowState) bool {
		return state.Active && state.Visible
	})
}

func waitForMiniWindowState(t *testing.T, timeout time.Duration, ready func(MiniControlWindowState) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready(miniControlWindowState()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for mini window state: %#v", miniControlWindowState())
}
