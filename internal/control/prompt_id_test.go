package control

import (
	"context"
	"sync"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

type promptTestEvent struct {
	ctrl *Controller
	kind event.Kind
	id   string
}

func newPromptTestController(events chan<- promptTestEvent) *Controller {
	var ctrl *Controller
	ctrl = New(Options{Sink: event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ApprovalRequest:
			events <- promptTestEvent{ctrl: ctrl, kind: e.Kind, id: e.Approval.ID}
		case event.AskRequest:
			events <- promptTestEvent{ctrl: ctrl, kind: e.Kind, id: e.Ask.ID}
		}
	})})
	return ctrl
}

func TestPromptIDsAreUniqueAcrossControllersAndPromptKinds(t *testing.T) {
	events := make(chan promptTestEvent, 8)
	first := newPromptTestController(events)
	second := newPromptTestController(events)

	var approvals sync.WaitGroup
	approvalResults := make(chan error, 2)
	for _, ctrl := range []*Controller{first, second} {
		approvals.Add(1)
		go func(ctrl *Controller) {
			defer approvals.Done()
			_, _, err := gateApprover{ctrl}.Approve(context.Background(), "bash", "go test", nil)
			approvalResults <- err
		}(ctrl)
	}

	ids := map[string]bool{}
	approvalEvents := make([]promptTestEvent, 0, 2)
	for len(approvalEvents) < 2 {
		received := <-events
		if received.kind == event.ApprovalRequest {
			if ids[received.id] {
				t.Fatalf("duplicate approval ID %q", received.id)
			}
			ids[received.id] = true
			approvalEvents = append(approvalEvents, received)
		}
	}
	for _, received := range approvalEvents {
		received.ctrl.Approve(received.id, true, false, false)
	}
	approvals.Wait()
	for range approvalEvents {
		if err := <-approvalResults; err != nil {
			t.Fatalf("approval failed: %v", err)
		}
	}

	taskResults := make(chan error, 2)
	for _, ctrl := range []*Controller{first, second} {
		go func(ctrl *Controller) {
			_, err := ctrl.Ask(context.Background(), []event.AskQuestion{{ID: "q", Prompt: "continue?"}})
			taskResults <- err
		}(ctrl)
	}

	askEvents := make([]promptTestEvent, 0, 2)
	for len(askEvents) < 2 {
		received := <-events
		if received.kind == event.AskRequest {
			if ids[received.id] {
				t.Fatalf("approval and ask IDs collided at %q", received.id)
			}
			ids[received.id] = true
			askEvents = append(askEvents, received)
		}
	}
	for _, received := range askEvents {
		received.ctrl.AnswerQuestion(received.id, nil)
	}
	for range askEvents {
		if err := <-taskResults; err != nil {
			t.Fatalf("ask failed: %v", err)
		}
	}
}
