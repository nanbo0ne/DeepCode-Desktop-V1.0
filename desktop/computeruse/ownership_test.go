package computeruse

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentStartsHaveOneOwner(t *testing.T) {
	s := NewService(&controlledBackend{}, nil)
	defer s.Stop("cleanup")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Start(context.Background(), StartRequest{TabID: "owner", Goal: "test"})
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrOccupied) {
				t.Errorf("start: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("owners=%d", accepted.Load())
	}
}

func TestEscalationRetainsOwnershipAndCancelsOldController(t *testing.T) {
	s := NewService(&controlledBackend{}, nil)
	defer s.Stop("cleanup")
	parent, cancel := context.WithCancel(context.Background())
	first, err := s.Start(parent, StartRequest{TabID: "a", Goal: "original", Restrictions: "no send", ModelRef: "original-model"})
	if err != nil {
		t.Fatal(err)
	}
	oldCtx, _ := s.Context(first.ID)
	if _, err = s.Escalate(first.ID, "b"); !errors.Is(err, ErrWrongOwner) {
		t.Fatal(err)
	}
	if _, err = s.Escalate(first.ID, "a"); err != nil {
		t.Fatal(err)
	}
	cancel()
	if oldCtx.Err() == nil {
		t.Fatal("old controller still live")
	}
	if _, err = s.Start(context.Background(), StartRequest{TabID: "b", Goal: "other"}); !errors.Is(err, ErrOccupied) {
		t.Fatal(err)
	}
	if _, err = s.Resume(context.Background()); err == nil {
		t.Fatal("unowned escalation resume accepted")
	}
	if _, err = s.ResumeTask(context.Background(), first.ID, "b"); !errors.Is(err, ErrWrongOwner) {
		t.Fatal(err)
	}
	resumed, err := s.ResumeTask(context.Background(), first.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != first.ID || resumed.Goal != first.Goal || resumed.Restrictions != first.Restrictions || resumed.ModelRef != first.ModelRef {
		t.Fatal("resume changed ownership or scope")
	}
	if err := s.StopController(oldCtx, first.ID, "late old error"); err != nil {
		t.Fatal(err)
	}
	if s.Current().State != StateRunning {
		t.Fatal("old cleanup stopped resumed controller")
	}
	if _, err = s.Observe(oldCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("old controller observed after resume: %v", err)
	}
	if _, err = s.Execute(context.Background(), Action{Type: "wait", Generation: s.generation, SessionID: "other"}); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("wrong action owner: %v", err)
	}
}

func TestOwnerCancellationReleasesIdleInput(t *testing.T) {
	b := &controlledBackend{}
	stopped := make(chan struct{}, 1)
	s := NewService(b, func(e Event) {
		if e.Kind == "cancelled" {
			stopped <- struct{}{}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := s.Start(ctx, StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("owner cancellation did not stop idle session")
	}
	if b.released.Load() == 0 || s.Current().State != StateCancelled {
		t.Fatal("input was not released")
	}
}

func TestUncertainInputConsumesObservationAndIsNotReplayed(t *testing.T) {
	var actions atomic.Int32
	b := &controlledBackend{execute: func(context.Context, Observation, Action) error { actions.Add(1); return errors.New("partial input") }}
	s := NewService(b, nil)
	if _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop("cleanup")
	if _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: 1}); err == nil {
		t.Fatal("partial input accepted")
	}
	if _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: 1}); !errors.Is(err, ErrStaleObservation) {
		t.Fatal(err)
	}
	if actions.Load() != 1 {
		t.Fatal("uncertain input replayed")
	}
	o, err := s.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(o.Generation, "done"); !errors.Is(err, ErrNotRunning) {
		t.Fatal("failed input completed")
	}
}

func TestObservationCannotRunDuringInput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	b := &controlledBackend{execute: func(context.Context, Observation, Action) error { close(entered); <-release; return nil }}
	s := NewService(b, nil)
	if _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop("cleanup")
	done := make(chan error, 1)
	go func() { _, err := s.Execute(context.Background(), Action{Type: "wait", Generation: 1}); done <- err }()
	<-entered
	_, err := s.Observe(context.Background())
	close(release)
	if !errors.Is(err, ErrStopPending) {
		t.Fatalf("concurrent observation: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestActionTargetsStayWithinCurrentObservation(t *testing.T) {
	o := Observation{SessionID: "one", Generation: 2, DisplayID: "crop", Elements: []Element{{ID: "current", Enabled: true}}, Windows: []Window{{ID: "protected", HigherTrust: true}}}
	for _, a := range []Action{
		{Generation: 2, SessionID: "two"}, {Generation: 1}, {Generation: 2, ElementID: "old"}, {Generation: 2, DisplayID: "other"}, {Generation: 2, X: 1.1}, {Generation: 2, WindowID: "missing"}, {Generation: 2, WindowID: "protected"},
	} {
		if err := validateActionTarget(o, a); err == nil {
			t.Fatalf("invalid target accepted: %+v", a)
		}
	}
	if err := validateActionTarget(o, Action{Generation: 2, SessionID: "one", ElementID: "current", DisplayID: "crop"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationInterruptsInitialObservation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "parent", true: "stop"}[stop], func(t *testing.T) {
			entered := make(chan struct{})
			b := &controlledBackend{observe: func(ctx context.Context, _ string, _ uint64) (Observation, error) {
				close(entered)
				<-ctx.Done()
				return Observation{}, ctx.Err()
			}}
			s := NewService(b, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.Start(ctx, StartRequest{Goal: "test"}); done <- err }()
			<-entered
			if stop {
				if err := s.Stop("explicit stop"); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation blocked behind lifecycle lock")
			}
		})
	}
}
