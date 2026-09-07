package computeruse

import (
	"context"
	"errors"
	"testing"
)

type fakeBackend struct {
	generation uint64
	actions    []Action
	released   int
	hidden     int
	emergency  func()
	userInput  func()
}

func (f *fakeBackend) Capabilities() Capabilities {
	return Capabilities{Platform: "fake", Supported: true}
}
func (f *fakeBackend) Observe(_ context.Context, sessionID string, generation uint64) (Observation, error) {
	f.generation = generation
	return Observation{SessionID: sessionID, Generation: generation, Foreground: Window{Title: "Test"}, Elements: []Element{{ID: elementID(generation)}}}, nil
}
func (f *fakeBackend) Execute(_ context.Context, _ Observation, action Action) error {
	f.actions = append(f.actions, action)
	return nil
}
func (f *fakeBackend) StartSafetyHooks(emergency, userInput func()) error {
	f.emergency, f.userInput = emergency, userInput
	return nil
}
func (f *fakeBackend) StopSafetyHooks()               {}
func (f *fakeBackend) ReleaseInjectedInput()          { f.released++ }
func (f *fakeBackend) ShowOverlay(OverlayState) error { return nil }
func (f *fakeBackend) UpdateOverlay(OverlayState)     {}
func (f *fakeBackend) HideOverlay()                   { f.hidden++ }

func elementID(generation uint64) string { return "g" + string(rune('0'+generation)) + "-element" }

func TestActionRequiresCurrentObservationAndRefreshesAfterward(t *testing.T) {
	backend := &fakeBackend{}
	service := NewService(backend, nil)
	if _, err := service.Start(context.Background(), StartRequest{Goal: "click"}); err != nil {
		t.Fatal(err)
	}
	first := backend.generation
	result, err := service.Execute(context.Background(), Action{Type: "click", Generation: first, X: .5, Y: .5})
	if err != nil {
		t.Fatal(err)
	}
	if result.Observation.Generation <= first {
		t.Fatalf("observation generation did not advance: %d -> %d", first, result.Observation.Generation)
	}
	if _, err := service.Execute(context.Background(), Action{Type: "click", Generation: first, X: .5, Y: .5}); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("stale action error = %v", err)
	}
}

func TestUserInputPausesAndEscapeStops(t *testing.T) {
	backend := &fakeBackend{}
	service := NewService(backend, nil)
	if _, err := service.Start(context.Background(), StartRequest{Goal: "type"}); err != nil {
		t.Fatal(err)
	}
	backend.userInput()
	if got := service.Current().State; got != StatePaused {
		t.Fatalf("state after user input = %s", got)
	}
	if _, err := service.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend.emergency()
	if got := service.Current().State; got != StateCancelled {
		t.Fatalf("state after emergency stop = %s", got)
	}
	if backend.released == 0 || backend.hidden == 0 {
		t.Fatalf("stop did not release input/hide overlay: %+v", backend)
	}
}

func TestProtectedObservationCannotStartActions(t *testing.T) {
	backend := &fakeBackend{}
	service := NewService(backend, nil)
	if _, err := service.Start(context.Background(), StartRequest{Goal: "observe"}); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.observation.SecureDesktop = true
	service.mu.Unlock()
	if _, err := service.Execute(context.Background(), Action{Type: "click", Generation: backend.generation}); !errors.Is(err, ErrProtectedSurface) {
		t.Fatalf("protected observation accepted: %v", err)
	}
}
