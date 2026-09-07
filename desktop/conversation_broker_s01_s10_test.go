package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/bot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
)

func brokerPromptController(broker *ConversationBroker, tabID, sessionPath string) *control.Controller {
	var ctrl *control.Controller
	ctrl = control.New(control.Options{
		SessionPath: sessionPath,
		Sink: event.FuncSink(func(e event.Event) {
			broker.Observe(tabID, ctrl, e)
		}),
	})
	return ctrl
}

func waitForBrokerEvent(t *testing.T, events <-chan event.Event, kind event.Kind) event.Event {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-events:
			if e.Kind == kind {
				return e
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for broker event kind %v", kind)
		}
	}
}

func TestConversationBrokerBindsPromptToSourceAndController(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	broker := NewConversationBroker(app)
	sourceA := make(chan event.Event, 4)
	sourceB := make(chan event.Event, 4)
	broker.RegisterSourceSink("bot:a", event.FuncSink(func(e event.Event) { sourceA <- e }))
	broker.RegisterSourceSink("bot:b", event.FuncSink(func(e event.Event) { sourceB <- e }))

	ctrlA := brokerPromptController(broker, "target-a", "session-a")
	ctrlB := brokerPromptController(broker, "target-b", "session-b")
	app.tabs["target-a"] = &WorkspaceTab{ID: "target-a", Ctrl: ctrlA, Ready: true}
	app.tabs["target-b"] = &WorkspaceTab{ID: "target-b", Ctrl: ctrlB, Ready: true}
	taskA := &DispatchTask{ID: "task-a", Status: "running", sourceTabID: "bot:a", targetTabID: "target-a", targetCtrl: ctrlA, targetSessionPath: "session-a", done: make(chan struct{})}
	taskB := &DispatchTask{ID: "task-b", Status: "running", sourceTabID: "bot:b", targetTabID: "target-b", targetCtrl: ctrlB, targetSessionPath: "session-b", done: make(chan struct{})}
	broker.tasks[taskA.ID] = taskA
	broker.tasks[taskB.ID] = taskB

	questions := []event.AskQuestion{{ID: "q", Prompt: "continue?"}}
	answerA := make(chan error, 1)
	go func() {
		_, err := ctrlA.Ask(context.Background(), questions)
		answerA <- err
	}()
	askAEvent := waitForBrokerEvent(t, sourceA, event.AskRequest)

	answerB := make(chan error, 1)
	go func() {
		_, err := ctrlB.Ask(context.Background(), questions)
		answerB <- err
	}()
	askBEvent := waitForBrokerEvent(t, sourceB, event.AskRequest)
	if askAEvent.Ask.ID == askBEvent.Ask.ID {
		t.Fatalf("controllers emitted colliding IDs: %q", askAEvent.Ask.ID)
	}
	if broker.Answer(askAEvent.Ask.ID, nil) != bot.ResponseRejected {
		t.Fatal("unscoped Answer bypassed source authorization")
	}
	if broker.AnswerForSource("bot:b", askAEvent.Ask.ID, nil) != bot.ResponseRejected {
		t.Fatal("wrong source consumed controller A's answer")
	}
	if broker.AnswerForSource("bot:a", askAEvent.Ask.ID, nil) != bot.ResponseConsumed || broker.AnswerForSource("bot:b", askBEvent.Ask.ID, nil) != bot.ResponseConsumed {
		t.Fatal("broker did not route answers to both owning controllers")
	}
	if err := <-answerA; err != nil {
		t.Fatalf("controller A ask failed: %v", err)
	}
	if err := <-answerB; err != nil {
		t.Fatalf("controller B ask failed: %v", err)
	}

	broker.Observe("target-a", ctrlB, event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "wrong-controller"}})
	if broker.Answer("wrong-controller", nil) != bot.ResponseNotOwned {
		t.Fatal("event from the wrong controller was accepted")
	}

	approvalID := "synthetic-approval"
	broker.pendingApprovals[approvalID] = pendingBrokerPrompt{
		taskID: taskA.ID, sourceTabID: taskA.sourceTabID, targetTabID: taskA.targetTabID,
		targetSessionPath: taskA.targetSessionPath, ctrl: ctrlA, id: approvalID,
	}
	if broker.ApproveForSource("bot:b", approvalID, true, false, false) != bot.ResponseRejected {
		t.Fatal("wrong source consumed controller A's approval")
	}
	if broker.ApproveForSource("bot:a", approvalID, true, false, false) != bot.ResponseConsumed {
		t.Fatal("owning source could not consume approval")
	}

	ctx, cancel := context.WithCancel(context.Background())
	deferred := make(chan error, 1)
	go func() {
		_, err := ctrlA.Ask(ctx, questions)
		deferred <- err
	}()
	stale := waitForBrokerEvent(t, sourceA, event.AskRequest)
	broker.finishTask(taskA, "", nil)
	if broker.AnswerForSource("bot:a", stale.Ask.ID, nil) != bot.ResponseRejected {
		t.Fatal("completed task accepted a stale answer")
	}
	cancel()
	if err := <-deferred; err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("stale ask result = %v, want context cancellation", err)
	}

	taskC := &DispatchTask{ID: "task-c", Status: "running", sourceTabID: "bot:a", targetTabID: "target-a", targetCtrl: ctrlA, targetSessionPath: "session-a", done: make(chan struct{})}
	broker.tasks[taskC.ID] = taskC
	changedCtx, changedCancel := context.WithCancel(context.Background())
	changedResult := make(chan error, 1)
	go func() {
		_, err := ctrlA.Ask(changedCtx, questions)
		changedResult <- err
	}()
	changed := waitForBrokerEvent(t, sourceA, event.AskRequest)
	ctrlA.SetSessionPath("session-a-changed")
	if broker.AnswerForSource("bot:a", changed.Ask.ID, nil) != bot.ResponseRejected {
		t.Fatal("changed controller session accepted a stale answer")
	}
	changedCancel()
	if err := <-changedResult; err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("changed-session ask result = %v, want context cancellation", err)
	}
	broker.finishTask(taskC, "", nil)

	completedCtx, completedCancel := context.WithCancel(context.Background())
	completedResult := make(chan error, 1)
	go func() {
		_, err := ctrlB.Ask(completedCtx, questions)
		completedResult <- err
	}()
	completed := waitForBrokerEvent(t, sourceB, event.AskRequest)
	broker.finishTask(taskB, "", nil)
	if broker.AnswerForSource("bot:b", completed.Ask.ID, nil) != bot.ResponseRejected {
		t.Fatal("completed task accepted a stale source-scoped answer")
	}
	completedCancel()
	if err := <-completedResult; err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("completed-task ask result = %v, want context cancellation", err)
	}
}

func TestAppPromptMethodsDoNotFallbackAfterBrokerRejection(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	broker := NewConversationBroker(app)
	app.conversationBroker = broker

	ctrl := brokerPromptController(broker, "target-tab", "session-app")
	app.tabs["target-tab"] = &WorkspaceTab{ID: "target-tab", Ctrl: ctrl, Ready: true}
	task := &DispatchTask{
		ID:                "task-app",
		Status:            "running",
		sourceTabID:       "owner-source",
		targetTabID:       "target-tab",
		targetCtrl:        ctrl,
		targetSessionPath: "session-app",
		done:              make(chan struct{}),
	}
	broker.tasks[task.ID] = task

	ctx, cancel := context.WithCancel(context.Background())
	deferred := make(chan error, 1)
	go func() {
		_, err := ctrl.Ask(ctx, []event.AskQuestion{{ID: "q", Prompt: "continue?"}})
		deferred <- err
	}()
	// The broker sink already recorded the prompt; find it from the broker map.
	deadline := time.Now().Add(2 * time.Second)
	var askID string
	for askID == "" && time.Now().Before(deadline) {
		broker.mu.Lock()
		for id := range broker.pendingAnswers {
			askID = id
		}
		broker.mu.Unlock()
		if askID == "" {
			time.Sleep(time.Millisecond)
		}
	}
	if askID == "" {
		cancel()
		t.Fatal("timed out waiting for broker answer")
	}

	app.AnswerQuestionForTab("target-tab", askID, nil)
	broker.mu.Lock()
	_, pending := broker.pendingAnswers[askID]
	broker.mu.Unlock()
	if !pending {
		t.Fatal("rejected app answer consumed broker pending prompt")
	}
	select {
	case err := <-deferred:
		t.Fatalf("rejected app answer fell back to target controller: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	app.AnswerQuestionForTab("owner-source", askID, nil)
	cancel()
	if err := <-deferred; err != nil {
		t.Fatalf("authorized app answer failed: %v", err)
	}
}

func TestAppApproveTabKeepsBrokerPromptOnSourceRejection(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	broker := NewConversationBroker(app)
	app.conversationBroker = broker
	ctrl := brokerPromptController(broker, "owner-source", "session-approval")
	app.tabs["target-tab"] = &WorkspaceTab{ID: "target-tab", Ctrl: ctrl, Ready: true}
	task := &DispatchTask{ID: "task-approval", Status: "running", sourceTabID: "owner-source", targetTabID: "target-tab", targetCtrl: ctrl, targetSessionPath: "session-approval", done: make(chan struct{})}
	broker.tasks[task.ID] = task
	broker.pendingApprovals["approval-app"] = pendingBrokerPrompt{taskID: task.ID, sourceTabID: task.sourceTabID, targetTabID: task.targetTabID, targetSessionPath: task.targetSessionPath, ctrl: ctrl, id: "approval-app"}

	app.ApproveTab("target-tab", "approval-app", true, false, false)
	if _, ok := broker.pendingApprovals["approval-app"]; !ok {
		t.Fatal("rejected app approval consumed broker pending prompt")
	}
}

func TestWaitForBrokerTabReadySnapshotsStateUnderAppLock(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	tab := &WorkspaceTab{ID: "target"}
	app.tabs[tab.ID] = tab
	result := make(chan error, 1)
	go func() {
		_, err := waitForBrokerTabReady(app, tab)
		result <- err
	}()
	app.mu.Lock()
	tab.Ready = true
	tab.Ctrl = nil
	tab.StartupErr = "synthetic startup failure"
	app.mu.Unlock()
	err := <-result
	if err == nil || !strings.Contains(err.Error(), "synthetic startup failure") {
		t.Fatalf("ready snapshot error = %v", err)
	}
}
