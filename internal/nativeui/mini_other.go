//go:build !windows

package nativeui

func (h *Host) PreloadMini(MiniController, bool) error { return ErrUnavailable }
func (h *Host) OpenMini(MiniController, bool) error    { return ErrUnavailable }
func (h *Host) ToggleMini(MiniController, bool) error  { return ErrUnavailable }
func (h *Host) SetMiniTopmost(bool) (MiniWindowState, error) {
	return MiniWindowState{}, ErrUnavailable
}
func (h *Host) MiniState() MiniWindowState { return MiniWindowState{} }
func (h *Host) CloseMini()                 {}
