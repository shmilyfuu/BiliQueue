//go:build !windows

package nativeui

func (h *Host) ShowMenu([]MenuItem, int, int) (int, error) {
	return 0, ErrUnavailable
}
