package computeruse

import (
	"context"
	"errors"
	"runtime"
)

// Re-enable only after native acceptance. This is deliberately not a user or
// environment setting; saved consent and model preferences remain untouched.
const releaseEnabled = false

var ErrTemporarilyDisabled = errors.New("computer control is temporarily disabled in this release")

func NewPlatformBackend() Backend {
	if !releaseEnabled {
		return disabledBackend{}
	}
	return newNativeBackend()
}

type disabledBackend struct{}

func (disabledBackend) Capabilities() Capabilities {
	return Capabilities{Platform: runtime.GOOS, TemporarilyDisabled: true, UnavailableReason: ErrTemporarilyDisabled.Error()}
}
func (disabledBackend) Observe(context.Context, string, uint64) (Observation, error) {
	return Observation{}, ErrTemporarilyDisabled
}
func (disabledBackend) Execute(context.Context, Observation, Action) error {
	return ErrTemporarilyDisabled
}
func (disabledBackend) StartSafetyHooks(func(), func()) error { return ErrTemporarilyDisabled }
func (disabledBackend) ShowOverlay(OverlayState) error        { return ErrTemporarilyDisabled }
func (disabledBackend) StopSafetyHooks()                      {}
func (disabledBackend) ReleaseInjectedInput()                 {}
func (disabledBackend) UpdateOverlay(OverlayState)            {}
func (disabledBackend) HideOverlay()                          {}

func (s *Service) Availability() error {
	capabilities := s.Capabilities()
	if capabilities.TemporarilyDisabled {
		return ErrTemporarilyDisabled
	}
	if !capabilities.Supported {
		return ErrNotSupported
	}
	return nil
}
