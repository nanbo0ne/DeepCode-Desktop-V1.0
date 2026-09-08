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

func TestOccupiedSessionRejectedAndOldSessionCannotStopReplacement(t *testing.T) {
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
	if _, err := s.Start(context.Background(), StartRequest{Goal: "second"}); !errors.Is(err, ErrOccupied) {
		t.Fatalf("occupied session: %v", err)
	}
	if firstCtx.Err() != nil {
		t.Fatal("rejected start cancelled the owner")
	}
	if err := s.StopSession(first.ID, "explicit stop"); err != nil {
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

var errTestReleaseInput = errors.New("test input release failed")

type errorReleaseBackend struct {
	controlledBackend
	failRelease    atomic.Bool
	releases       atomic.Int32
	releasedSignal chan struct{}
}

func (b *errorReleaseBackend) ReleaseInjectedInputError() error {
	b.releases.Add(1)
	if b.releasedSignal != nil {
		select {
		case b.releasedSignal <- struct{}{}:
		default:
		}
	}
	if b.failRelease.Load() {
		return errTestReleaseInput
	}
	return nil
}

func TestNativeObservationCannotBlockCancellationOrFreeOwner(t *testing.T) {
	for _, phase := range []string{"start", "resume", "observe"} {
		t.Run(phase, func(t *testing.T) {
			entered, unblock := make(chan struct{}), make(chan struct{})
			b := &errorReleaseBackend{releasedSignal: make(chan struct{}, 16)}
			var block atomic.Bool
			b.observe = func(_ context.Context, id string, generation uint64) (Observation, error) {
				if block.Load() {
					close(entered)
					<-unblock // Deliberately ignore cancellation, like a blocked COM call.
				}
				return Observation{SessionID: id, Generation: generation, Foreground: Window{Title: "late native result"}}, nil
			}
			s := NewService(b, nil)
			if phase != "start" {
				if _, err := s.Start(context.Background(), StartRequest{TabID: "owner", Goal: "first"}); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "resume" {
				if _, err := s.Pause(); err != nil {
					t.Fatal(err)
				}
			}
			for len(b.releasedSignal) > 0 {
				<-b.releasedSignal
			}
			block.Store(true)
			opDone := make(chan error, 1)
			go func() {
				var err error
				switch phase {
				case "start":
					_, err = s.Start(context.Background(), StartRequest{TabID: "owner", Goal: "first"})
				case "resume":
					_, err = s.Resume(context.Background())
				default:
					_, err = s.Observe(context.Background())
				}
				opDone <- err
			}()
			<-entered
			owner := s.Current().ID
			oldCtx, err := s.Context(owner)
			if err != nil {
				t.Fatal(err)
			}
			stopped := make(chan error, 1)
			go func() { stopped <- s.Stop("explicit cancel") }()
			select {
			case <-b.releasedSignal:
			case <-time.After(time.Second):
				t.Fatal("release waited behind blocked native observation")
			}
			if oldCtx.Err() == nil {
				t.Fatal("controller was not cancelled before release")
			}
			select {
			case err := <-stopped:
				if !errors.Is(err, ErrStopPending) {
					t.Fatalf("blocked stop returned %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stop has no bounded wait")
			}
			if s.Current().State != StateStopping || s.Current().CompletedAt != nil {
				t.Fatal("blocked native operation was marked terminal")
			}
			if _, err := s.Start(context.Background(), StartRequest{Goal: "replacement"}); !errors.Is(err, ErrOccupied) {
				t.Fatalf("new owner admitted: %v", err)
			}
			if _, err := s.Resume(context.Background()); err == nil {
				t.Fatal("pending native operation resumed")
			}
			block.Store(false)
			close(unblock)
			if err := <-opDone; err == nil {
				t.Fatal("late cancelled observation returned success")
			}
			if err := s.Stop("retry after native exit"); err != nil {
				t.Fatal(err)
			}
			second, err := s.Start(context.Background(), StartRequest{TabID: "second", Goal: "replacement"})
			if err != nil {
				t.Fatal(err)
			}
			before := s.Current()
			if _, err := s.finishStartup(oldCtx, owner, errors.New("late startup failure")); err == nil {
				t.Fatal("old startup finalizer returned success")
			}
			if s.Current().ID != second.ID || s.Current().State != StateRunning || s.Current().CurrentApp != before.CurrentApp {
				t.Fatal("old startup finalizer changed replacement owner")
			}
			if _, err := s.Observe(oldCtx); err == nil {
				t.Fatal("old observation context reached replacement owner")
			}
			if err := s.Stop("cleanup"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInputReleaseFailureKeepsStoppingUntilStopRetry(t *testing.T) {
	for _, operation := range []string{"stop", "pause", "complete", "action"} {
		t.Run(operation, func(t *testing.T) {
			b := &errorReleaseBackend{}
			b.execute = func(context.Context, Observation, Action) error { return errors.New("uncertain input") }
			s := NewService(b, nil)
			owner, err := s.Start(context.Background(), StartRequest{Goal: "test"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, _ := s.Context(owner.ID)
			b.failRelease.Store(true)
			switch operation {
			case "stop":
				err = s.Stop("stop")
			case "pause":
				_, err = s.Pause()
			case "complete":
				_, err = s.Complete(1, "verified completion")
			case "action":
				_, err = s.Execute(ctx, Action{Generation: 1, Type: "wait"})
			}
			if !errors.Is(err, errTestReleaseInput) {
				t.Fatalf("release failure swallowed: %v", err)
			}
			if b.released.Load() != 0 {
				t.Fatal("error-capable backend fell back to void release")
			}
			if s.Current().State != StateStopping || s.Current().CompletedAt != nil || ctx.Err() == nil {
				t.Fatal("failed release did not cancel and retain ownership")
			}
			if _, err := s.Start(context.Background(), StartRequest{Goal: "other"}); !errors.Is(err, ErrOccupied) {
				t.Fatalf("replacement admitted: %v", err)
			}
			if err := s.Stop("still failing"); !errors.Is(err, errTestReleaseInput) {
				t.Fatal(err)
			}
			attempts := b.releases.Load()
			b.failRelease.Store(false)
			if err := s.Stop("retry release"); err != nil {
				t.Fatal(err)
			}
			if b.releases.Load() <= attempts || s.Current().State != StateCancelled {
				t.Fatal("retry did not release and finish")
			}
			if _, err := s.Start(context.Background(), StartRequest{Goal: "new owner"}); err != nil {
				t.Fatal(err)
			}
			if err := s.Stop("cleanup"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type blockedSetupBackend struct {
	errorReleaseBackend
	entered chan struct{}
	unblock chan struct{}
}

func (b *blockedSetupBackend) ShowOverlay(OverlayState) error {
	close(b.entered)
	<-b.unblock
	return nil
}

func TestNativeSetupDoesNotHoldLifecycleAgainstStop(t *testing.T) {
	b := &blockedSetupBackend{entered: make(chan struct{}), unblock: make(chan struct{})}
	b.releasedSignal = make(chan struct{}, 4)
	s := NewService(b, nil)
	started := make(chan error, 1)
	go func() { _, err := s.Start(context.Background(), StartRequest{Goal: "test"}); started <- err }()
	<-b.entered
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop("stop during setup") }()
	select {
	case <-b.releasedSignal:
	case <-time.After(time.Second):
		t.Fatal("blocked overlay setup prevented input release")
	}
	close(b.unblock)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := <-started; err == nil {
		t.Fatal("cancelled setup reported success")
	}
	if s.Current().State != StateCancelled {
		t.Fatal("late setup overwrote cancellation")
	}
}

func TestBlockedNativeActionRejectsQueuedInputAndReleasesOnParentCancel(t *testing.T) {
	entered, unblock := make(chan struct{}), make(chan struct{})
	b := &errorReleaseBackend{releasedSignal: make(chan struct{}, 8)}
	var actions atomic.Int32
	b.execute = func(context.Context, Observation, Action) error {
		actions.Add(1)
		close(entered)
		<-unblock
		return nil
	}
	s := NewService(b, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.Start(ctx, StartRequest{Goal: "test"}); err != nil {
		t.Fatal(err)
	}
	actionDone := make(chan error, 1)
	go func() { _, err := s.Execute(ctx, Action{Type: "wait", Generation: 1}); actionDone <- err }()
	<-entered
	queued := make(chan error, 1)
	go func() { _, err := s.Execute(ctx, Action{Type: "wait", Generation: 1}); queued <- err }()
	select {
	case err := <-queued:
		if !errors.Is(err, ErrStopPending) {
			t.Fatalf("queued input=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued input blocked behind native call")
	}
	cancel()
	select {
	case <-b.releasedSignal:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not release input")
	}
	if s.Current().State != StateStopping {
		t.Fatal("active native action lost occupancy")
	}
	close(unblock)
	if err := <-actionDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Stop("finish cleanup"); err != nil {
		t.Fatal(err)
	}
	if actions.Load() != 1 {
		t.Fatal("uncertain native input was replayed")
	}
}
