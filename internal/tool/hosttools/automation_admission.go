package hosttools

import (
	"context"
	"sync"
	"time"
)

var automationAdmission struct {
	sync.RWMutex
	begin func() (func(), error)
}

// SetAutomationWorkAdmission connects scheduled executions to the desktop's
// installation barrier. Non-desktop hosts can leave this unset.
func SetAutomationWorkAdmission(begin func() (func(), error)) {
	automationAdmission.Lock()
	automationAdmission.begin = begin
	automationAdmission.Unlock()
}

// Acquire outside automationStore.mu: the installation barrier lists jobs while
// holding its write lock. Only denied, never-started ticks may be retried.
func acquireAutomationWork(ctx context.Context) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		automationAdmission.RLock()
		begin := automationAdmission.begin
		automationAdmission.RUnlock()
		if begin == nil {
			return func() {}, nil
		}
		finish, err := begin()
		if err == nil {
			if err := ctx.Err(); err != nil {
				finish()
				return nil, err
			}
			return finish, nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
