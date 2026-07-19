//go:build !windows

package main

func acquireSingleInstance(instanceID string) (func(), bool) { return nil, false }
