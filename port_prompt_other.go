//go:build !windows

package main

func promptListenAddress(title, message, defaultValue string) (string, bool) { return "", false }
