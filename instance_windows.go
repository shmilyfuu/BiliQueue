//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"syscall"
	"unsafe"
)

const errorAlreadyExists = 183

var procCreateMutexW = kernel32.NewProc("CreateMutexW")
var procReleaseMutex = kernel32.NewProc("ReleaseMutex")
var procCloseHandle = kernel32.NewProc("CloseHandle")

func acquireSingleInstance(instanceID string) (func(), bool) {
	name, _ := syscall.UTF16PtrFromString(singleInstanceMutexName(instanceID))
	handle, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, false
	}
	release := func() {
		procReleaseMutex.Call(handle)
		procCloseHandle.Call(handle)
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		return release, true
	}
	return release, false
}

func singleInstanceMutexName(instanceID string) string {
	mutexName := "Local\\BiliQueueSingleInstance"
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		digest := sha256.Sum256([]byte(instanceID))
		mutexName += "-" + hex.EncodeToString(digest[:8])
	}
	return mutexName
}
