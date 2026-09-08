package control

import (
	"context"
	"errors"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func TestTurnLeaseAdmissionIncludesUnsavedSession(t *testing.T) {
	refused := errors.New("installation in progress")
	called := false
	c := New(Options{TurnLease: func(_ context.Context, path string) (func(), error) {
		called = true
		if path != "" {
			t.Fatalf("unexpected session path %q", path)
		}
		return nil, refused
	}})
	if err := c.RunTurn(context.Background(), "must not start"); !errors.Is(err, refused) {
		t.Fatalf("unsaved session bypassed admission: %v", err)
	}
	if !called || c.Running() {
		t.Fatal("lease refusal did not unwind turn")
	}
}

func TestSteerRunningDoesNotStartIdleFallback(t *testing.T) {
	session := agent.NewSession("")
	executor := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	c := New(Options{Executor: executor})
	if c.SteerRunning("new task") {
		t.Fatal("idle controller claimed guidance delivery")
	}
	if c.Running() {
		t.Fatal("idle guidance started an unadmitted turn")
	}
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	if !c.SteerRunning("guidance") {
		t.Fatal("running controller rejected guidance")
	}
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}
