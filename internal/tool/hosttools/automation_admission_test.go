package hosttools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAutomationWorkAdmissionRefusalIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	SetAutomationWorkAdmission(func() (func(), error) {
		calls++
		cancel()
		return nil, errors.New("installation in progress")
	})
	defer SetAutomationWorkAdmission(nil)
	if _, err := acquireAutomationWork(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cancelled work retried %d times", calls)
	}
}

func TestAutomationWorkAdmissionTickRefusalDoesNotExecute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item := &automationItem{Status: "scheduled", Action: "test_invalid_action"}
	SetAutomationWorkAdmission(func() (func(), error) {
		cancel()
		return nil, errors.New("installation in progress")
	})
	defer SetAutomationWorkAdmission(nil)
	runAutomation(ctx, item)
	if item.Status != "scheduled" || !item.LastRunAt.IsZero() || item.Error != "" {
		t.Fatal("denied tick mutated/executed scheduled work")
	}
}

func TestAutomationWorkAdmissionTickReleasesAfterResultPersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	previous := automations
	automations = newAutomationStore()
	automations.loaded = true
	defer func() { automations = previous }()
	item := &automationItem{ID: "test", Status: "scheduled", Action: "test_invalid_action"}
	automations.items[item.ID] = item
	released := false
	SetAutomationWorkAdmission(func() (func(), error) {
		return func() {
			released = true
			if item.Status != "failed" || item.Error == "" {
				t.Error("released before recording execution failure")
			}
			store := newAutomationStore()
			store.ensureLoadedLocked()
			if saved := store.items[item.ID]; saved == nil || saved.Status != "failed" {
				t.Error("released before persisting execution failure")
			}
		}, nil
	})
	defer SetAutomationWorkAdmission(nil)
	// An invalid action takes the executor's error branch without native effects.
	runAutomation(context.Background(), item)
	if !released {
		t.Fatal("tick leaked its admission reservation")
	}
}

func TestAutomationWorkAdmissionReturnsReservation(t *testing.T) {
	released := false
	SetAutomationWorkAdmission(func() (func(), error) {
		return func() { released = true }, nil
	})
	defer SetAutomationWorkAdmission(nil)
	finish, err := acquireAutomationWork(context.Background())
	if err != nil || released {
		t.Fatal("reservation did not cover the work", err)
	}
	finish()
	if !released {
		t.Fatal("reservation not released")
	}
}

func TestAutomationWorkAdmissionCancellationDuringAcquireReleases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	released := false
	SetAutomationWorkAdmission(func() (func(), error) {
		cancel()
		return func() { released = true }, nil
	})
	defer SetAutomationWorkAdmission(nil)
	if _, err := acquireAutomationWork(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("cancelled acquisition leaked reservation")
	}
}
