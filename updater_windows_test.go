//go:build windows

package main

import (
	"reflect"
	"testing"
)

func TestFilterUpdaterArgsRemovesInternalUpdateFlags(t *testing.T) {
	args := []string{
		"-listen", "127.0.0.1:18319",
		"-update-helper",
		"-update-target", "old.exe",
		"-update-completed-version", "0.1.18",
		"-data", "test-data",
	}
	want := []string{"-listen", "127.0.0.1:18319", "-data", "test-data"}
	if got := filterUpdaterArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("filterUpdaterArgs()=%q want=%q", got, want)
	}
}
