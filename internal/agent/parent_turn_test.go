package agent

import (
	"context"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func TestParentTurnSurvivesToolAndNestedAgentContexts(t *testing.T) {
	if id, lifetime := ParentTurn(context.Background()); id != "" || lifetime != nil {
		t.Fatal("unbound context has identity")
	}
	ctx, end := WithParentTurn(context.Background())
	defer end()
	id, lifetime := ParentTurn(ctx)
	if id == "" || lifetime == nil || lifetime.Err() != nil {
		t.Fatal("invalid parent identity")
	}
	call, cancelCall := context.WithCancel(withCallContext(WithParentSession(ctx, "session"), "tool-call", event.Discard, nil))
	nested, endNested := WithParentTurn(call)
	nestedID, nestedLifetime := ParentTurn(nested)
	if nestedID != id || nestedLifetime != lifetime || ParentSession(nested) != "session" {
		t.Fatal("nested context reset parent identity")
	}
	endNested()
	if call.Err() != nil {
		t.Fatal("nested agent ended parent turn")
	}
	cancelCall()
	if lifetime.Err() != nil {
		t.Fatal("tool cancellation ended parent budget lifetime")
	}
	end()
	if lifetime.Err() == nil {
		t.Fatal("parent lifetime not cancelled")
	}
	other, endOther := WithParentTurn(context.Background())
	defer endOther()
	if otherID, _ := ParentTurn(other); otherID == id {
		t.Fatal("independent user turns share identity")
	}
}
