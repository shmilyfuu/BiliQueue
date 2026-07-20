//go:build windows

package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestMiniWindowPreferencesRoundTrip(t *testing.T) {
	a := newApp(t.TempDir())
	want := miniWindowPreferences{Topmost: true}
	if err := saveMiniWindowPreferences(a, want); err != nil {
		t.Fatal(err)
	}
	if got := loadMiniWindowPreferences(a); got != want {
		t.Fatalf("preferences mismatch: got %#v want %#v", got, want)
	}
}

func TestMiniControlWebView2Window(t *testing.T) {
	if os.Getenv("BILIQUEUE_WEBVIEW2_INTEGRATION") != "1" {
		t.Skip("set BILIQUEUE_WEBVIEW2_INTEGRATION=1 to open the native WebView2 test window")
	}
	a := newApp(t.TempDir())
	server := httptest.NewServer(a.routes())
	defer server.Close()
	a.mu.Lock()
	a.config.ListenAddress = strings.TrimPrefix(server.URL, "http://")
	a.mu.Unlock()

	if err := openMiniControlWindow(a); err != nil {
		t.Fatal(err)
	}
	defer closeMiniControlWindow()
	waitForMiniWindowState(t, 15*time.Second, func(state MiniControlWindowState) bool { return state.Active })
	time.Sleep(750 * time.Millisecond)
	nativeMiniWindow.mu.Lock()
	hwnd := nativeMiniWindow.hwnd
	nativeMiniWindow.mu.Unlock()
	var clientRect miniWindowRect
	getClientRect := syscall.NewLazyDLL("user32.dll").NewProc("GetClientRect")
	if result, _, _ := getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&clientRect))); result == 0 {
		t.Fatal("GetClientRect failed")
	}
	if width, height := clientRect.right-clientRect.left, clientRect.bottom-clientRect.top; width != miniWindowWidth || height != miniWindowHeight {
		t.Fatalf("mini client size: got %dx%d want %dx%d", width, height, miniWindowWidth, miniWindowHeight)
	}
	if state := miniControlWindowState(); !state.Visible {
		t.Fatalf("opened mini window should be visible: %#v", state)
	}
	if err := toggleMiniControlWindow(a); err != nil {
		t.Fatal(err)
	}
	waitForMiniWindowState(t, 3*time.Second, func(state MiniControlWindowState) bool { return state.Active && !state.Visible })
	if err := toggleMiniControlWindow(a); err != nil {
		t.Fatal(err)
	}
	waitForMiniWindowState(t, 3*time.Second, func(state MiniControlWindowState) bool { return state.Active && state.Visible })
	if _, err := setMiniControlWindowTopmost(a, true); err != nil {
		t.Fatal(err)
	}
	if _, err := setMiniControlWindowTopmost(a, false); err != nil {
		t.Fatal(err)
	}
	closeMiniControlWindow()
	waitForMiniWindowState(t, 5*time.Second, func(state MiniControlWindowState) bool { return !state.Active && !state.Opening })
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
