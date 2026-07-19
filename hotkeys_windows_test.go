//go:build windows

package main

import "testing"

func TestParseWindowsHotkey(t *testing.T) {
	tests := []struct {
		input     string
		modifiers uint32
		key       uint32
		canonical string
	}{
		{input: "Ctrl+Alt+N", modifiers: modControl | modAlt, key: 'N', canonical: "Ctrl+Alt+N"},
		{input: "F8", key: 0x77, canonical: "F8"},
		{input: "Win+Shift+ArrowUp", modifiers: modWin | modShift, key: 0x26, canonical: "Win+Shift+ArrowUp"},
	}
	for _, test := range tests {
		modifiers, key, canonical, err := parseWindowsHotkey(test.input)
		if err != nil {
			t.Fatalf("parse %q: %v", test.input, err)
		}
		if modifiers != test.modifiers || key != test.key || canonical != test.canonical {
			t.Fatalf("parse %q: got modifiers=%#x key=%#x canonical=%q", test.input, modifiers, key, canonical)
		}
	}
	if _, _, _, err := parseWindowsHotkey("Ctrl+Escape"); err == nil {
		t.Fatal("Escape should not be accepted as a hotkey")
	}
}
