package computeruse

import (
	"context"
	"errors"
	"testing"
)

func TestServiceParentTurnReservesBeforeInputAndNeverResets(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget", true: "uncertain"}[uncertain], func(t *testing.T) {
			calls := 0
			b := &controlledBackend{execute: func(context.Context, Observation, Action) error {
				calls++
				if uncertain {
					return errors.New("uncertain input")
				}
				return nil
			}}
			s := NewService(b, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer s.Stop("cleanup")
			req := StartRequest{TabID: "tab", ParentSessionID: "parent-session", ParentTurnID: "turn", Goal: "test"}
			if _, err := s.Start(ctx, req); !errors.Is(err, ErrParentTurnStopped) {
				t.Fatalf("unadmitted start: %v", err)
			}
			end, err := s.BeginParentTask(ctx, ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			defer end(true)
			current, err := s.Start(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if current.ParentTurnID != req.ParentTurnID || current.ParentSessionID != req.ParentSessionID {
				t.Fatal("session lost parent binding")
			}
			runCtx, err := s.Context(current.ID)
			if err != nil {
				t.Fatal(err)
			}
			limit := ParentTurnMaxActions
			if uncertain {
				limit = 1
			}
			for i := 0; i < limit; i++ {
				o, err := s.Observe(runCtx)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Execute(runCtx, Action{Type: "wait", SessionID: current.ID, Generation: o.Generation})
				if !uncertain && err != nil {
					t.Fatal(err)
				}
				if uncertain && err == nil {
					t.Fatal("uncertain input accepted")
				}
			}
			o, err := s.Observe(runCtx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Execute(runCtx, Action{Type: "wait", SessionID: current.ID, Generation: o.Generation}); !errors.Is(err, ErrParentTurnStopped) {
				t.Fatalf("input replay: %v", err)
			}
			if calls != limit {
				t.Fatalf("backend calls=%d want %d", calls, limit)
			}
			end(false) // Even a faulty caller cannot clear the service failure latch.
			req.Goal = "renamed"
			if _, err = s.BeginParentTask(ctx, ctx, req); !errors.Is(err, ErrParentTurnStopped) {
				t.Fatalf("new task reset budget: %v", err)
			}
		})
	}
}

func TestServiceParentTurnExpiredLifetimeCannotReenter(t *testing.T) {
	s := NewService(&controlledBackend{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	req := StartRequest{TabID: "tab", ParentSessionID: "session", ParentTurnID: "turn", Goal: "test"}
	end, err := s.BeginParentTask(ctx, ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	end(true)
	cancel()
	if _, err := s.BeginParentTask(context.Background(), ctx, req); err == nil {
		t.Fatal("expired parent lifetime reentered")
	}
	newCtx, endTurn := context.WithCancel(context.Background())
	defer endTurn()
	req.ParentTurnID = "explicit-new-user-turn"
	end, err = s.BeginParentTask(newCtx, newCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	end(false)
}
