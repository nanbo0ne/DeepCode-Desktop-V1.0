//go:build windows

package computeruse

import (
	"context"
	"errors"
	"testing"
)

func TestWindowActionsRejectProtectedOrMissingObservationTarget(t *testing.T) {
	b := &WindowsBackend{}
	for _, kind := range []string{"activate_window", "close_window", "move_window", "resize_window"} {
		obs := Observation{Windows: []Window{{ID: "protected", HigherTrust: true}}}
		if err := b.windowAction(context.Background(), obs, Action{Type: kind, WindowID: "protected"}); !errors.Is(err, ErrProtectedSurface) {
			t.Fatalf("%s protected=%v", kind, err)
		}
		if err := b.windowAction(context.Background(), obs, Action{Type: kind, WindowID: "missing"}); !errors.Is(err, ErrStaleObservation) {
			t.Fatalf("%s missing=%v", kind, err)
		}
	}
}
