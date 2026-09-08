package main

import (
	"context"
	"sync"
	"testing"
)

func TestSaveWindowStateSyncSkipsInvalidLifecycleContext(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	if got, ok := a.saveWindowStateSync(context.Background(), true); ok || got != (DesktopWindowState{}) {
		t.Fatalf("invalid lifecycle context was captured: got=%+v ok=%v", got, ok)
	}
	if a.windowStateClosing.Load() {
		t.Fatal("invalid lifecycle context closed future window-state saves")
	}
}

func TestSaveWindowStateIgnoresLateAsyncSaveAfterClose(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	want := DesktopWindowState{Width: 1200, Height: 800, X: 20, Y: 30}
	if err := a.SaveWindowState(want); err != nil {
		t.Fatal(err)
	}

	a.closeWindowStateSaves()
	late := DesktopWindowState{Width: 640, Height: 480, X: 0, Y: 0}
	if err := a.SaveWindowState(late); err != nil {
		t.Fatal(err)
	}

	got, ok := loadWindowState()
	if !ok || got != want {
		t.Fatalf("late save changed final state: got=%+v ok=%v want=%+v", got, ok, want)
	}
}

func TestSaveWindowStateConcurrentLateSavesCannotOverwriteFinalState(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	want := DesktopWindowState{Width: 1400, Height: 900, X: 40, Y: 50}
	if err := a.SaveWindowState(want); err != nil {
		t.Fatal(err)
	}
	a.closeWindowStateSaves()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = a.SaveWindowState(DesktopWindowState{
				Width:  600 + i,
				Height: 400 + i,
				X:      i,
				Y:      i,
			})
		}(i)
	}
	wg.Wait()

	got, ok := loadWindowState()
	if !ok || got != want {
		t.Fatalf("concurrent late save changed final state: got=%+v ok=%v want=%+v", got, ok, want)
	}
}
