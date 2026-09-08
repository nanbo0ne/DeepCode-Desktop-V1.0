package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
)

// blockingBackend is deliberately input-free. It records whether Service.Stop
// cancels the context given to an in-flight action; no Windows API is touched.
type blockingBackend struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingBackend) Capabilities() computeruse.Capabilities {
	return computeruse.Capabilities{Platform: "fake", Supported: true}
}
func (b *blockingBackend) Observe(_ context.Context, sessionID string, generation uint64) (computeruse.Observation, error) {
	return computeruse.Observation{SessionID: sessionID, Generation: generation}, nil
}
func (b *blockingBackend) Execute(ctx context.Context, _ computeruse.Observation, _ computeruse.Action) error {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return nil
	}
}
func (b *blockingBackend) StartSafetyHooks(func(), func()) error      { return nil }
func (b *blockingBackend) StopSafetyHooks()                           {}
func (b *blockingBackend) ReleaseInjectedInput()                      {}
func (b *blockingBackend) ShowOverlay(computeruse.OverlayState) error { return nil }
func (b *blockingBackend) UpdateOverlay(computeruse.OverlayState)     {}
func (b *blockingBackend) HideOverlay()                               {}

func TestAuditStopDoesNotCancelInFlightAction(t *testing.T) {
	b := &blockingBackend{entered: make(chan struct{}), release: make(chan struct{})}
	s := computeruse.NewService(b, nil)
	if _, err := s.Start(context.Background(), computeruse.StartRequest{Goal: "fake action"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Execute(context.Background(), computeruse.Action{Type: "click", Generation: 1})
		done <- err
	}()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("fake action did not start")
	}
	if err := s.Stop("audit stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("in-flight action returned after Stop: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Current behavior: Stop cancels the session, but Execute received the
		// caller context rather than the session context and is still running.
	}
	close(b.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fake action did not finish after release")
	}
	if s.Current().State != computeruse.StateCancelled {
		t.Fatalf("state after stop = %q", s.Current().State)
	}
}

type orderedObserveBackend struct {
	secondStarted chan struct{}
	thirdStarted  chan struct{}
	releaseSecond chan struct{}
	releaseThird  chan struct{}
}

func (b *orderedObserveBackend) Capabilities() computeruse.Capabilities {
	return computeruse.Capabilities{Platform: "fake", Supported: true}
}
func (b *orderedObserveBackend) Observe(_ context.Context, sessionID string, generation uint64) (computeruse.Observation, error) {
	switch generation {
	case 2:
		close(b.secondStarted)
		<-b.releaseSecond
	case 3:
		close(b.thirdStarted)
		<-b.releaseThird
	}
	return computeruse.Observation{SessionID: sessionID, Generation: generation}, nil
}
func (b *orderedObserveBackend) Execute(context.Context, computeruse.Observation, computeruse.Action) error {
	return nil
}
func (b *orderedObserveBackend) StartSafetyHooks(func(), func()) error      { return nil }
func (b *orderedObserveBackend) StopSafetyHooks()                           {}
func (b *orderedObserveBackend) ReleaseInjectedInput()                      {}
func (b *orderedObserveBackend) ShowOverlay(computeruse.OverlayState) error { return nil }
func (b *orderedObserveBackend) UpdateOverlay(computeruse.OverlayState)     {}
func (b *orderedObserveBackend) HideOverlay()                               {}

func TestAuditOutOfOrderObserveRewindsCurrentGeneration(t *testing.T) {
	b := &orderedObserveBackend{
		secondStarted: make(chan struct{}), thirdStarted: make(chan struct{}),
		releaseSecond: make(chan struct{}), releaseThird: make(chan struct{}),
	}
	s := computeruse.NewService(b, nil)
	if _, err := s.Start(context.Background(), computeruse.StartRequest{Goal: "fake observe"}); err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() { _, err := s.Observe(context.Background()); secondDone <- err }()
	<-b.secondStarted
	thirdDone := make(chan error, 1)
	go func() { _, err := s.Observe(context.Background()); thirdDone <- err }()
	<-b.thirdStarted
	close(b.releaseThird)
	if err := <-thirdDone; err != nil {
		t.Fatal(err)
	}
	close(b.releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	_, err := s.Execute(context.Background(), computeruse.Action{Type: "wait", Generation: 2})
	if errors.Is(err, computeruse.ErrStaleObservation) {
		t.Fatalf("generation 2 was correctly rejected; audit precondition no longer reproduces: %v", err)
	}
	if err != nil {
		t.Fatalf("accepted rewound observation did not complete: %v", err)
	}
}
