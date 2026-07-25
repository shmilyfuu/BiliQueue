//go:build windows

package main

import (
	"testing"

	"biliqueue/internal/nativeui"
)

func TestNativeDialogDesignLoads(t *testing.T) {
	if _, err := nativeui.DefaultHost(); err != nil {
		t.Fatalf("start native UI host: %v", err)
	}
}
