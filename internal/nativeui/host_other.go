//go:build !windows

package nativeui

type Host struct{}

func DefaultHost() (*Host, error) { return nil, ErrUnavailable }
func Preload() error              { return ErrUnavailable }
func (h *Host) CloseDialogs()     {}
