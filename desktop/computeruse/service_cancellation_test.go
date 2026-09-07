package computeruse

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type controlledBackend struct {
	observe  func(context.Context, string, uint64) (Observation, error)
	execute  func(context.Context, Observation, Action) error
	released atomic.Int32
}

func (*controlledBackend) Capabilities() Capabilities { return Capabilities{Supported: true} }
func (b *controlledBackend) Observe(ctx context.Context, id string, gen uint64) (Observation, error) {
	if b.observe != nil {
		return b.observe(ctx, id, gen)
	}
	return Observation{SessionID: id, Generation: gen}, nil
}
func (b *controlledBackend) Execute(ctx context.Context, o Observation, a Action) error {
	if b.execute != nil {
		return b.execute(ctx, o, a)
	}
	return ctx.Err()
}
func (*controlledBackend) StartSafetyHooks(func(), func()) error { return nil }
func (*controlledBackend) StopSafetyHooks()                      {}
func (b *controlledBackend) ReleaseInjectedInput()               { b.released.Add(1) }
func (*controlledBackend) ShowOverlay(OverlayState) error        { return nil }
func (*controlledBackend) UpdateOverlay(OverlayState)            {}
func (*controlledBackend) HideOverlay()                          {}

func TestStopAndPauseCancelInFlightAction(t *testing.T) {
	for _, pause := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "pause"}[pause], func(t *testing.T) {
			entered := make(chan struct{})
			b := &controlledBackend{execute: func(ctx context.Context, _ Observation, _ Action) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}}
			s := NewService(b, nil)
			if _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: 1}); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("action not started")
			}
			if pause {
				if _, err := s.Pause(); err != nil {
					t.Fatal(err)
				}
			} else if err := s.Stop("test"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("action error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("action did not stop")
			}
			if b.released.Load() == 0 {
				t.Fatal("input not released")
			}
			if pause {
				if _, err := s.Resume(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Execute(context.Background(), Action{Type: "click", Generation: 1}); !errors.Is(err, ErrStaleObservation) {
					t.Fatalf("old observation accepted: %v", err)
				}
				_ = s.Stop("cleanup")
			} else if s.Current().State != StateCancelled {
				t.Fatalf("state=%s", s.Current().State)
			}
		})
	}
}

func TestObserveDiscardsOutOfOrderCompletion(t *testing.T) {
	second, release := make(chan struct{}), make(chan struct{})
	b := &controlledBackend{observe: func(ctx context.Context, id string, g uint64) (Observation, error) {
		if g == 2 {
			close(second)
			select {
			case <-release:
			case <-ctx.Done():
				return Observation{}, ctx.Err()
			}
		}
		return Observation{SessionID: id, Generation: g}, nil
	}}
	s := NewService(b, nil)
	if _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop("cleanup")
	done := make(chan error, 1)
	go func() { _, err := s.Observe(context.Background()); done <- err }()
	<-second
	if o, err := s.Observe(context.Background()); err != nil || o.Generation != 3 {
		t.Fatalf("latest=%+v err=%v", o, err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("out of order error=%v", err)
	}
	if _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: 2}); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("old action=%v", err)
	}
	if _, err := s.Complete(2, "done"); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("stale completion=%v", err)
	}
	if _, err := s.Complete(3, "done"); err != nil {
		t.Fatal(err)
	}
}

func TestStoppedSessionCannotBeCompleted(t *testing.T) {
	s := NewService(&controlledBackend{}, nil)
	if _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Stop("cancel")
	if _, err := s.Complete(1, "done"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("completion after stop=%v", err)
	}
	if s.Current().State != StateCancelled {
		t.Fatal("completion changed cancelled state")
	}
}

func TestSupersededSessionCannotActOrStopNewSession(t *testing.T) {
	s := NewService(&fakeBackend{}, nil)
	first, err := s.Start(context.Background(), StartRequest{Goal: "first"})
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, err := s.Context(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(context.Background(), StartRequest{Goal: "second"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop("test cleanup")
	if firstCtx.Err() == nil {
		t.Fatal("old provider context is still live")
	}
	if err := s.StopSession(first.ID, "late cleanup"); err != nil {
		t.Fatal(err)
	}
	if s.Current().ID != second.ID || s.Current().State != StateRunning {
		t.Fatal("old cleanup stopped new task")
	}
	if _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: old.Generation}); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("old action: %v", err)
	}
	if _, err := s.Complete(old.Generation, "old completion"); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("old completion: %v", err)
	}
}

func TestInputDelayHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitInputDelay(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
