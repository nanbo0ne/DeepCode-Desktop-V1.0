package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

type sessionExecutionGate struct {
	mu    sync.Mutex
	locks map[string]*sessionExecutionLock
	admit func() (func(), error)
}

type sessionExecutionLock struct {
	token chan struct{}
	refs  int
}

func newSessionExecutionGate() *sessionExecutionGate {
	return &sessionExecutionGate{locks: map[string]*sessionExecutionLock{}}
}

func (g *sessionExecutionGate) Acquire(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil {
		return func() {}, nil
	}
	finish := func() {}
	if g.admit != nil {
		var err error
		finish, err = g.admit()
		if err != nil {
			return nil, err
		}
	}
	key := canonicalExecutionPath(path)
	if key == "" {
		return finish, nil
	}
	g.mu.Lock()
	lock := g.locks[key]
	if lock == nil {
		lock = &sessionExecutionLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		g.locks[key] = lock
	}
	lock.refs++
	g.mu.Unlock()

	select {
	case <-ctx.Done():
		g.releaseRef(key, lock, false)
		finish()
		return nil, ctx.Err()
	case <-lock.token:
		var once sync.Once
		return func() { once.Do(func() { g.releaseRef(key, lock, true); finish() }) }, nil
	}
}

func (g *sessionExecutionGate) releaseRef(key string, lock *sessionExecutionLock, held bool) {
	if held {
		lock.token <- struct{}{}
	}
	g.mu.Lock()
	lock.refs--
	if lock.refs == 0 && g.locks[key] == lock {
		delete(g.locks, key)
	}
	g.mu.Unlock()
}

func canonicalExecutionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return strings.ToLower(filepath.Clean(path))
}
