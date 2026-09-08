package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func TestWorkAdmissionBarrierRejectsActiveAndAllowsRetry(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	done, err := a.beginAppWork()
	if err != nil {
		t.Fatal(err)
	}
	if finish, err := a.acquireUpdateWorkBarrier(); err == nil {
		finish(false)
		t.Fatal("installation admitted active work")
	}
	if a.updateApplying.Load() || a.admittedWork.Load() != 1 {
		t.Fatal("busy refusal changed active work or left installation enabled")
	}
	done()
	done()
	finish, err := a.acquireUpdateWorkBarrier()
	if err != nil {
		t.Fatal(err)
	}
	if !a.updateApplying.Load() {
		t.Fatal("barrier did not publish installation state")
	}
	finish(false)
	done, err = a.beginAppWork()
	if err != nil {
		t.Fatal("failed installation did not reopen admission:", err)
	}
	done()
	finish, err = a.acquireUpdateWorkBarrier()
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if _, err := a.beginAppWork(); !errors.Is(err, errUpdateWorkAdmission) {
		t.Fatal("successful handoff reopened admission:", err)
	}
}

func TestWorkAdmissionConcurrentInstallerAndSubmit(t *testing.T) {
	isolateDesktopUserDirs(t)
	for i := 0; i < 100; i++ {
		a := NewApp()
		start := make(chan struct{})
		admitted := make(chan bool, 1)
		installed := make(chan bool, 1)
		release := make(chan struct{})
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			<-start
			done, err := a.beginAppWork()
			admitted <- err == nil
			if err == nil {
				<-release
				done()
			}
		}()
		go func() {
			<-start
			finish, err := a.acquireUpdateWorkBarrier()
			if err == nil {
				finish(true)
			}
			installed <- err == nil
		}()
		close(start)
		workWon, installWon := <-admitted, <-installed
		close(release)
		<-ended
		if workWon == installWon {
			t.Fatalf("iteration %d: work=%v install=%v; exactly one must win", i, workWon, installWon)
		}
	}
}

func TestWorkAdmissionNestedCallDoesNotRetainReadLock(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	outer, err := a.beginAppWork()
	if err != nil {
		t.Fatal(err)
	}
	defer outer()
	writerStarted := make(chan struct{})
	writerEnded := make(chan error, 1)
	go func() {
		close(writerStarted)
		finish, err := a.acquireUpdateWorkBarrier()
		if err == nil {
			finish(false)
		}
		writerEnded <- err
	}()
	<-writerStarted
	nestedEnded := make(chan error, 1)
	go func() {
		done, err := a.beginAppWork()
		if err == nil {
			done()
		}
		nestedEnded <- err
	}()
	select {
	case err := <-writerEnded:
		if err == nil {
			t.Fatal("writer ignored outer work")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("outer reservation retained a read lock")
	}
	select {
	case err := <-nestedEnded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nested admission deadlocked with writer")
	}
}

func TestWorkAdmissionEntrypointsRejectBeforeSideEffects(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	finish, err := a.acquireUpdateWorkBarrier()
	if err != nil {
		t.Fatal(err)
	}
	defer finish(false)
	checks := map[string]func() error{
		"submit":          func() error { return a.SubmitToTab("missing", "hello") },
		"display":         func() error { return a.SubmitDisplayToTab("missing", "hello", "hello") },
		"sidechat":        func() error { return a.SendSideChat("missing", "hello") },
		"automation":      func() error { return a.ResumeAutomation("missing") },
		"compact":         a.Compact,
		"clear":           a.ClearSession,
		"new session":     a.NewSession,
		"model":           func() error { return a.SetModelForTab("missing", "model") },
		"effort":          func() error { return a.SetEffortForTab("missing", "auto") },
		"mode":            func() error { return a.SetConversationModeForTab("missing", "coding") },
		"rewind":          func() error { return a.Rewind(0, "conversation") },
		"fork":            func() error { _, err := a.Fork(0); return err },
		"restore":         func() error { _, err := a.ResumeSessionForTab("missing", ""); return err },
		"start computer":  func() error { _, err := a.StartComputerUseSession(computeruse.StartRequest{}); return err },
		"resume computer": func() error { _, err := a.ResumeComputerUse(); return err },
		"computer tool": func() error {
			_, err := a.runComputerTask(context.Background(), computeruse.StartRequest{})
			return err
		},
		"computer loop": func() error {
			_, err := a.runComputerTaskWithProvider(context.Background(), computeruse.StartRequest{}, nil)
			return err
		},
		"session lease": func() error { _, err := a.sessionGate.Acquire(context.Background(), ""); return err },
		"dispatch":      func() error { _, err := a.conversationBroker.Dispatch("", "", "", ""); return err },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !errors.Is(err, errUpdateWorkAdmission) {
				t.Fatalf("got %v, want admission refusal", err)
			}
		})
	}
	// Nil targets would panic if startup/drain got past the gate.
	a.startTabControllerBuild(nil)
	a.buildTabController(nil)
	a.drainRuntimeSubmits(nil, []pendingRuntimeSubmit{{input: "never execute"}})
	tab := &WorkspaceTab{ID: "blocked"}
	if a.beginTabRuntimeReconfigure(tab) != 0 || tab.runtimeReconfiguring {
		t.Fatal("runtime switch bypassed installation barrier")
	}
	if !a.processPendingAssistantMemories() {
		t.Fatal("blocked memory work should remain eligible for later retry")
	}
	a.RunShellForTab("missing", "must not execute")
	a.SteerForTab("missing", "must not submit")
	if a.admittedWork.Load() != 0 {
		t.Fatal("refused entry leaked a reservation")
	}
}

func TestWorkAdmissionQueuedDrainTransfersBeforeRemovingQueue(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	r := &admissionBlockingRunner{started: make(chan context.Context, 1), release: make(chan struct{})}
	ended := make(chan struct{}, 1)
	ctrl := control.New(control.Options{Runner: r, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			ended <- struct{}{}
		}
	})})
	a.setTestCtrl(ctrl, "")
	defer ctrl.Close()
	tab := a.tabs["test"]
	generation := a.beginTabRuntimeReconfigure(tab)
	tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "queued test"}}
	a.finishTabRuntimeReconfigure(tab, generation, true)
	if finish, err := a.acquireUpdateWorkBarrier(); err == nil {
		finish(false)
		t.Fatal("queue removal exposed an idle gap before turn start")
	}
	<-r.started
	close(r.release)
	<-ended
}

func TestWorkAdmissionSessionLeaseTracksWaiterAndCancellation(t *testing.T) {
	a := NewApp()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	first, err := a.sessionGate.Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	admitted := make(chan struct{}, 1)
	a.sessionGate.admit = func() (func(), error) {
		done, err := a.beginAppWork()
		admitted <- struct{}{}
		return done, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() {
		finish, err := a.sessionGate.Acquire(ctx, path)
		if err == nil {
			finish()
		}
		ended <- err
	}()
	<-admitted
	if a.admittedWork.Load() != 2 {
		t.Fatal("waiting turn was not reserved")
	}
	cancel()
	if err := <-ended; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if a.admittedWork.Load() != 1 {
		t.Fatal("cancelled waiter leaked admission")
	}
	first()
	first()
	if a.admittedWork.Load() != 0 {
		t.Fatal("session release leaked admission")
	}
	if _, err := a.sessionGate.Acquire(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled empty-path lease admitted work")
	}
}

type admissionBlockingRunner struct {
	started chan context.Context
	release chan struct{}
}

func (r *admissionBlockingRunner) Run(ctx context.Context, _ string) error {
	r.started <- ctx
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestWorkAdmissionControllerAndQueuedRuntimeRemainBusy(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	r := &admissionBlockingRunner{started: make(chan context.Context, 1), release: make(chan struct{})}
	ended := make(chan struct{}, 1)
	ctrl := control.New(control.Options{Runner: r, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			ended <- struct{}{}
		}
	})})
	a.setTestCtrl(ctrl, "")
	defer ctrl.Close()
	if err := a.SubmitToTab("test", "test only"); err != nil {
		t.Fatal(err)
	}
	ctx := <-r.started
	if finish, err := a.acquireUpdateWorkBarrier(); err == nil {
		finish(false)
		t.Fatal("running controller did not prevent installation")
	}
	if ctx.Err() != nil {
		t.Fatal("update attempt cancelled existing work")
	}
	close(r.release)
	<-ended
	tab := a.tabs["test"]
	tab.pendingRuntimeSubmits = []pendingRuntimeSubmit{{input: "queued"}}
	if finish, err := a.acquireUpdateWorkBarrier(); err == nil {
		finish(false)
		t.Fatal("pending runtime submit did not prevent installation")
	}
	if len(tab.pendingRuntimeSubmits) != 1 {
		t.Fatal("installation consumed queued work")
	}
	tab.pendingRuntimeSubmits = nil
	tab.runtimeReconfiguring = true
	if finish, err := a.acquireUpdateWorkBarrier(); err == nil {
		finish(false)
		t.Fatal("runtime build did not prevent installation")
	}
}
