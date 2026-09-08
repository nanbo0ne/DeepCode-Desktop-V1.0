package agent

import (
	"context"
	"crypto/rand"
)

type parentTurnContextKey struct{}
type parentTurnContext struct {
	id       string
	lifetime context.Context
}

// WithParentTurn creates one identity for a host turn, not for each model/tool
// iteration. Nested agents and automatic continuations inherit it unchanged.
func WithParentTurn(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Value(parentTurnContextKey{}).(parentTurnContext); ok {
		return ctx, func() {}
	}
	lifetime, cancel := context.WithCancel(ctx)
	return context.WithValue(lifetime, parentTurnContextKey{}, parentTurnContext{id: rand.Text(), lifetime: lifetime}), cancel
}

func ParentTurn(ctx context.Context) (id string, lifetime context.Context) {
	turn, _ := ctx.Value(parentTurnContextKey{}).(parentTurnContext)
	return turn.id, turn.lifetime
}
