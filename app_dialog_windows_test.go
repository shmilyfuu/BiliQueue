//go:build windows

package main

import (
	"os"
	"testing"
	"time"
)

func TestPersistentAppDialogReuse(t *testing.T) {
	if os.Getenv("BILIQUEUE_WEBVIEW2_DIALOG_INTEGRATION") != "1" {
		t.Skip("set BILIQUEUE_WEBVIEW2_DIALOG_INTEGRATION=1 to test the persistent WebView2 dialog host")
	}
	if !webView2AvailableQuietly() {
		t.Skip("WebView2 Runtime is not installed")
	}
	preloadAppDialogHost()

	show := func(accepted bool) (uintptr, bool) {
		done := make(chan bool, 1)
		go func() {
			result, opened := showWebViewChoiceDialog("BiliQueue test", "Verify persistent dialog reuse.", "Confirm", "Cancel")
			done <- opened && result == accepted
		}()
		hwnd := waitForAppDialog(t)
		appDialogHost.resolve(appDialogResult{Accepted: accepted})
		select {
		case ok := <-done:
			return hwnd, ok
		case <-time.After(5 * time.Second):
			t.Fatal("dialog result timed out")
			return 0, false
		}
	}

	firstHWND, firstOK := show(true)
	secondHWND, secondOK := show(false)
	if !firstOK || !secondOK {
		t.Fatalf("unexpected dialog results: first=%v second=%v", firstOK, secondOK)
	}
	if firstHWND == 0 || firstHWND != secondHWND {
		t.Fatalf("dialog HWND was not reused: first=%#x second=%#x", firstHWND, secondHWND)
	}
}

func waitForAppDialog(t *testing.T) uintptr {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		appDialogHost.mu.Lock()
		hwnd := appDialogHost.hwnd
		current := appDialogHost.current
		appDialogHost.mu.Unlock()
		if hwnd != 0 && current != nil {
			return hwnd
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("persistent WebView2 dialog did not become ready")
	return 0
}
