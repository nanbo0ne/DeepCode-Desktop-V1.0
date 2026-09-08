package computeruse

import (
	"context"
	"errors"
	"testing"
)

func TestReleaseDisablesNativeBackendWithoutDeletingImplementation(t *testing.T) {
	t.Setenv("ORCA_COMPUTER_USE_ENABLED", "true")
	t.Setenv("ORCA_COMPUTER_E2E", "1")
	backend := NewPlatformBackend()
	if _, ok := backend.(disabledBackend); !ok {
		t.Fatal("release instantiated a native backend")
	}
	c := backend.Capabilities()
	if !c.TemporarilyDisabled || c.Supported || c.ScreenCapture || c.UIAutomation || c.InputInjection || c.Overlay || c.EmergencyStop {
		t.Fatalf("disabled release advertises native capabilities: %+v", c)
	}
	ctx := context.Background()
	_, observeErr := backend.Observe(ctx, "old-session", 1)
	for _, err := range []error{observeErr, backend.Execute(ctx, Observation{}, Action{Type: "click"}), backend.StartSafetyHooks(nil, nil), backend.ShowOverlay(OverlayState{})} {
		if !errors.Is(err, ErrTemporarilyDisabled) {
			t.Fatalf("native entry did not reject disabled release: %v", err)
		}
	}
}

func TestDisabledServiceRejectsOldSessionAndNewTask(t *testing.T) {
	s := NewService(NewPlatformBackend(), func(Event) { t.Fatal("disabled service emitted an action or observation") })
	ctx := context.Background()
	_, startErr := s.Start(ctx, StartRequest{Goal: "test"})
	_, admissionErr := s.BeginParentTask(ctx, ctx, StartRequest{})
	// Old persisted/session-shaped state cannot enable the release.
	s.session = Session{ID: "old", TabID: "owner", State: StatePaused}
	_, resumeErr := s.Resume(ctx)
	_, resumeTaskErr := s.ResumeTask(ctx, "old", "owner")
	_, observeErr := s.Observe(ctx)
	_, actionErr := s.Execute(ctx, Action{Type: "click"})
	for _, err := range []error{startErr, admissionErr, resumeErr, resumeTaskErr, observeErr, actionErr} {
		if !errors.Is(err, ErrTemporarilyDisabled) {
			t.Fatalf("service bypassed release gate: %v", err)
		}
	}
	if got := s.Current(); got.ActionCount != 0 || got.State != StatePaused {
		t.Fatalf("disabled service changed existing state: %+v", got)
	}
}
