//go:build windows

package main

import "testing"

func TestSingleInstanceMutexName(t *testing.T) {
	const releaseName = "Local\\BiliQueueSingleInstance"
	if got := singleInstanceMutexName(""); got != releaseName {
		t.Fatalf("default mutex changed: %q", got)
	}
	if got := singleInstanceMutexName("   "); got != releaseName {
		t.Fatalf("blank instance id changed the default mutex: %q", got)
	}
	one := singleInstanceMutexName("v0.1.15-local-test")
	two := singleInstanceMutexName("another-test")
	if one == releaseName || one == two {
		t.Fatalf("isolated mutex names are not distinct: %q %q", one, two)
	}
}
