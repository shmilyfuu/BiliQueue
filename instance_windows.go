//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const errorAlreadyExists = 183

var procCreateMutexW = kernel32.NewProc("CreateMutexW")
var procReleaseMutex = kernel32.NewProc("ReleaseMutex")
var procCloseHandle = kernel32.NewProc("CloseHandle")

func acquireSingleInstance() (func(), bool) {
	name, _ := syscall.UTF16PtrFromString("Local\\BiliQueueSingleInstance")
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
