package main

import (
	"errors"
	"strings"
	"sync"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
)

var errUpdateWorkAdmission = errors.New("正在准备安装更新，请等待应用重新启动")

// The read lock covers admission, not the lifetime of a provider call. The
// reservation stays visible until work finishes or publishes Controller.Running.
// No caller carries an RLock into another App method (RWMutex is not recursive).
func (a *App) beginAppWork() (func(), error) {
	if a.updateApplying.Load() {
		return nil, errUpdateWorkAdmission
	}
	a.workAdmission.RLock()
	if a.updateApplying.Load() {
		a.workAdmission.RUnlock()
		return nil, errUpdateWorkAdmission
	}
	a.admittedWork.Add(1)
	a.workAdmission.RUnlock()
	var once sync.Once
	return func() { once.Do(func() { a.admittedWork.Add(-1) }) }, nil
}

// Acquire before verification/snapshots and hold through installer handoff and
// quit. Reject active work rather than cancelling it or waiting for it to end.
func (a *App) acquireUpdateWorkBarrier() (func(bool), error) {
	a.workAdmission.Lock()
	if a.updateApplying.Load() {
		a.workAdmission.Unlock()
		return nil, errUpdateWorkAdmission
	}
	if err := a.updateIdleCheck(); err != nil {
		a.workAdmission.Unlock()
		return nil, err
	}
	a.updateApplying.Store(true)
	return func(success bool) {
		if !success {
			a.updateApplying.Store(false)
		}
		a.workAdmission.Unlock()
	}, nil
}

// The controller starts these management commands in untracked goroutines.
// Transfer a reservation before spawning them so installation cannot overtake
// an accepted compaction or session rewrite. Ordinary turns publish Running.
func (a *App) submitAdmittedController(ctrl *control.Controller, display, input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "/new" && trimmed != "/clear" && trimmed != "/compact" && !strings.HasPrefix(trimmed, "/compact ") {
		ctrl.SubmitDisplay(display, input)
		return
	}
	// Caller already owns a reservation, so a writer cannot install in between.
	a.admittedWork.Add(1)
	go func() {
		defer a.admittedWork.Add(-1)
		var err error
		switch {
		case trimmed == "/new":
			err = ctrl.NewSession()
		case trimmed == "/clear":
			err = ctrl.ClearSession()
		default:
			err = ctrl.Compact(a.bootContext(), strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact")))
			if err == nil {
				err = ctrl.Snapshot()
			}
		}
		if err != nil {
			a.notice(err.Error())
		}
	}()
}
