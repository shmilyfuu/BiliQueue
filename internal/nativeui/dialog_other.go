//go:build !windows

package nativeui

func (h *Host) ShowDialog(DialogRequest) (DialogResult, error) {
	return DialogResult{}, ErrUnavailable
}

func (h *Host) OpenProgress(DialogRequest) (ProgressHandle, error) {
	return nil, ErrUnavailable
}
