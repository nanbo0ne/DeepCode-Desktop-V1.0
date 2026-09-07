package computeruse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotSupported     = errors.New("computer use is not supported on this platform")
	ErrNotRunning       = errors.New("computer use session is not running")
	ErrStaleObservation = errors.New("the observation is stale; observe again before acting")
	ErrProtectedSurface = errors.New("the target is protected and cannot be automated")
	ErrUserInputPaused  = errors.New("computer use paused because the user took control")
	ErrStopPending      = errors.New("computer input is still stopping; wait before starting another task")
)

type Backend interface {
	Capabilities() Capabilities
	Observe(ctx context.Context, sessionID string, generation uint64) (Observation, error)
	Execute(ctx context.Context, observation Observation, action Action) error
	StartSafetyHooks(onEmergencyStop func(), onUserInput func()) error
	StopSafetyHooks()
	ReleaseInjectedInput()
	ShowOverlay(OverlayState) error
	UpdateOverlay(OverlayState)
	HideOverlay()
}

type safetyHookErrorStopper interface {
	StopSafetyHooksError() error
}

func stopSafetyHooksFor(backend Backend) error {
	if stopper, ok := backend.(safetyHookErrorStopper); ok {
		return stopper.StopSafetyHooksError()
	}
	backend.StopSafetyHooks()
	return nil
}

type Event struct {
	Kind        string        `json:"kind"`
	Session     Session       `json:"session"`
	Observation *Observation  `json:"observation,omitempty"`
	Action      *ActionResult `json:"action,omitempty"`
}

type Service struct {
	mu           sync.Mutex
	lifecycle    sync.Mutex
	actionMu     sync.Mutex
	backend      Backend
	session      Session
	observation  Observation
	generation   uint64
	cancel       context.CancelFunc
	runCtx       context.Context
	actionCancel context.CancelFunc
	actionDone   chan struct{}
	emit         func(Event)
}

func NewService(backend Backend, emit func(Event)) *Service {
	return &Service{backend: backend, emit: emit, session: Session{State: StateIdle, Logs: []ActionLog{}}}
}

func (s *Service) Capabilities() Capabilities {
	if s.backend == nil {
		return Capabilities{Supported: false, UnavailableReason: ErrNotSupported.Error()}
	}
	return s.backend.Capabilities()
}

func (s *Service) Current() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSession(s.session)
}

// Context binds provider requests to the same lifetime as desktop actions.
func (s *Service) Context(sessionID string) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.ID != sessionID || s.session.State != StateRunning || s.runCtx == nil {
		return nil, ErrNotRunning
	}
	return s.runCtx, nil
}

func (s *Service) Start(ctx context.Context, request StartRequest) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.backend == nil || !s.backend.Capabilities().Supported {
		return Session{}, ErrNotSupported
	}
	if request.Goal == "" {
		return Session{}, fmt.Errorf("computer task goal is required")
	}
	if err := s.stop("superseded"); err != nil {
		return s.Current(), err
	}
	id, err := sessionID()
	if err != nil {
		return Session{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()
	s.mu.Lock()
	s.cancel = cancel
	s.runCtx = runCtx
	s.observation = Observation{}
	s.session = Session{ID: id, TabID: request.TabID, Goal: request.Goal, SuccessCriteria: request.SuccessCriteria, Restrictions: request.Restrictions, ModelRef: request.ModelRef, State: StateRunning, StartedAt: now, UpdatedAt: now, Logs: []ActionLog{}}
	session := cloneSession(s.session)
	s.mu.Unlock()
	if err := s.backend.StartSafetyHooks(func() { _ = s.Stop("escape") }, s.pauseForUserInput); err != nil {
		cancel()
		return s.finish(StateFailed, err.Error()), err
	}
	if err := s.backend.ShowOverlay(OverlayState{State: StateRunning, Action: "正在观察屏幕"}); err != nil {
		_ = stopSafetyHooksFor(s.backend)
		cancel()
		return s.finish(StateFailed, err.Error()), err
	}
	s.publish(Event{Kind: "started", Session: session})
	if _, err := s.Observe(runCtx); err != nil {
		_ = s.stop("observation failed")
		return s.Current(), err
	}
	return s.Current(), nil
}

func (s *Service) Observe(ctx context.Context) (Observation, error) {
	s.mu.Lock()
	if s.session.State != StateRunning && s.session.State != StatePaused {
		s.mu.Unlock()
		return Observation{}, ErrNotRunning
	}
	s.generation++
	generation, sessionID := s.generation, s.session.ID
	runCtx := s.runCtx
	s.mu.Unlock()
	ctx, cancel := linkedContext(ctx, runCtx)
	defer cancel()
	observation, err := s.backend.Observe(ctx, sessionID, generation)
	if err != nil {
		return Observation{}, err
	}
	if observation.SecureDesktop {
		return Observation{}, ErrProtectedSurface
	}
	observation.SessionID = sessionID
	observation.Generation = generation
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	s.mu.Lock()
	if s.session.ID != sessionID || (s.session.State != StateRunning && s.session.State != StatePaused) {
		s.mu.Unlock()
		return Observation{}, ErrNotRunning
	}
	if generation != s.generation || ctx.Err() != nil {
		s.mu.Unlock()
		return Observation{}, ErrStaleObservation
	}
	s.observation = observation
	s.session.CurrentApp = observation.Foreground.Title
	s.session.UpdatedAt = time.Now().UTC()
	session := cloneSession(s.session)
	s.mu.Unlock()
	public := observation
	public.Screenshot = ""
	s.publish(Event{Kind: "observation", Session: session, Observation: &public})
	return observation, nil
}

func (s *Service) Execute(ctx context.Context, action Action) (ActionResult, error) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	s.mu.Lock()
	if s.session.State == StatePaused {
		s.mu.Unlock()
		return ActionResult{}, ErrUserInputPaused
	}
	if s.session.State != StateRunning {
		s.mu.Unlock()
		return ActionResult{}, ErrNotRunning
	}
	if action.Generation == 0 || action.Generation != s.observation.Generation || action.Generation != s.generation {
		s.mu.Unlock()
		return ActionResult{}, ErrStaleObservation
	}
	if s.observation.SecureDesktop || s.observation.Foreground.HigherTrust {
		s.mu.Unlock()
		return ActionResult{}, ErrProtectedSurface
	}
	ctx, cancel := linkedContext(ctx, s.runCtx)
	done := make(chan struct{})
	s.actionCancel, s.actionDone = cancel, done
	observation := s.observation
	sessionID := s.session.ID
	s.session.CurrentAction = action.Description
	if s.session.CurrentAction == "" {
		s.session.CurrentAction = action.Type
	}
	s.session.UpdatedAt = time.Now().UTC()
	session := cloneSession(s.session)
	s.mu.Unlock()
	shouldRelease := true
	defer func() {
		if shouldRelease || ctx.Err() != nil {
			s.backend.ReleaseInjectedInput()
		}
		cancel()
		s.mu.Lock()
		if s.actionDone == done {
			s.actionCancel, s.actionDone = nil, nil
		}
		close(done)
		s.mu.Unlock()
	}()
	s.backend.UpdateOverlay(OverlayState{State: StateRunning, App: observation.Foreground.Title, Action: session.CurrentAction})

	started := time.Now()
	err := s.backend.Execute(ctx, observation, action)
	if err == nil {
		err = ctx.Err()
	}
	result := ActionResult{ActionID: fmt.Sprintf("action-%d", started.UnixNano()), Action: action, Success: err == nil, DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		result.Error = err.Error()
	}
	log := ActionLog{ID: result.ActionID, Type: action.Type, Window: observation.Foreground.Title, StartedAt: started.UTC(), DurationMS: result.DurationMS, Success: result.Success, Error: result.Error}
	s.mu.Lock()
	if s.session.ID != sessionID {
		s.mu.Unlock()
		return result, ErrNotRunning
	}
	if s.session.State == StateRunning {
		s.session.ActionCount++
		s.session.Logs = append(s.session.Logs, log)
		s.session.LastError = result.Error
		s.session.UpdatedAt = time.Now().UTC()
		if err != nil {
			// A failed action may already have moved the screen.
			s.generation++
		}
	}
	session = cloneSession(s.session)
	s.mu.Unlock()
	if session.State != StateRunning && err == nil {
		err = ErrNotRunning
		result.Success, result.Error = false, err.Error()
	}
	if err != nil {
		s.publish(Event{Kind: "action", Session: session, Action: &result})
		return result, err
	}
	// Every successful action invalidates the previous element IDs and coordinates.
	next, observeErr := s.Observe(ctx)
	result.Observation = next
	if observeErr != nil {
		result.Success, result.Error = false, observeErr.Error()
		err = observeErr
	}
	s.publish(Event{Kind: "action", Session: s.Current(), Action: &result})
	shouldRelease = err != nil
	return result, err
}

func (s *Service) Pause() (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.session.State != StateRunning {
		s.mu.Unlock()
		return Session{}, ErrNotRunning
	}
	s.session.State, s.session.UpdatedAt = StatePaused, time.Now().UTC()
	s.generation++
	cancel, done := s.actionCancel, s.actionDone
	session := cloneSession(s.session)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.backend.ReleaseInjectedInput()
	s.backend.UpdateOverlay(OverlayState{State: StatePaused, App: session.CurrentApp, Action: "已暂停"})
	s.publish(Event{Kind: "paused", Session: session})
	return session, waitForAction(done)
}

func (s *Service) Resume(ctx context.Context) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.session.State != StatePaused {
		s.mu.Unlock()
		return Session{}, ErrNotRunning
	}
	if s.actionDone != nil {
		s.mu.Unlock()
		return Session{}, ErrStopPending
	}
	s.session.State, s.session.UpdatedAt = StateRunning, time.Now().UTC()
	session := cloneSession(s.session)
	s.mu.Unlock()
	s.backend.UpdateOverlay(OverlayState{State: StateRunning, App: session.CurrentApp, Action: "正在重新观察"})
	if _, err := s.Observe(ctx); err != nil {
		return s.finish(StateFailed, err.Error()), err
	}
	s.publish(Event{Kind: "resumed", Session: s.Current()})
	return s.Current(), nil
}

func (s *Service) Complete(generation uint64, message string) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.session.State != StateRunning || s.actionDone != nil || s.session.LastError != "" || s.runCtx.Err() != nil {
		s.mu.Unlock()
		return s.Current(), ErrNotRunning
	}
	if generation == 0 || generation != s.generation || generation != s.observation.Generation {
		s.mu.Unlock()
		return s.Current(), ErrStaleObservation
	}
	if strings.TrimSpace(message) == "" {
		s.mu.Unlock()
		return s.Current(), fmt.Errorf("computer completion summary is empty")
	}
	s.session.State = StateSucceeded
	s.mu.Unlock()
	return s.finish(StateSucceeded, message), nil
}

func (s *Service) Stop(reason string) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	return s.stop(reason)
}

func (s *Service) StopSession(id, reason string) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	current := s.session.ID == id
	s.mu.Unlock()
	if !current {
		return nil
	}
	return s.stop(reason)
}

func (s *Service) stop(reason string) error {
	s.mu.Lock()
	if s.session.State == StateIdle || s.session.State == StateCancelled || s.session.State == StateSucceeded || s.session.State == StateFailed {
		s.mu.Unlock()
		return nil
	}
	s.session.State, s.session.CurrentAction, s.session.UpdatedAt = StateStopping, "正在停止", time.Now().UTC()
	cancel := s.cancel
	done := s.actionDone
	session := cloneSession(s.session)
	s.mu.Unlock()
	s.publish(Event{Kind: "stopping", Session: session})
	if cancel != nil {
		cancel()
	}
	s.backend.ReleaseInjectedInput()
	hookErr := stopSafetyHooksFor(s.backend)
	s.backend.HideOverlay()
	if hookErr != nil {
		return hookErr
	}
	if err := waitForAction(done); err != nil {
		return err
	}
	s.finish(StateCancelled, reason)
	return nil
}

func linkedContext(ctx, session context.Context) (context.Context, context.CancelFunc) {
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(session, cancel)
	if session.Err() != nil {
		cancel()
	}
	return linked, func() { stop(); cancel() }
}

func waitForAction(done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return ErrStopPending
	}
}

func waitInputDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) finish(state SessionState, message string) Session {
	now := time.Now().UTC()
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.session.State, s.session.UpdatedAt, s.session.CompletedAt = state, now, &now
	if state == StateFailed {
		s.session.LastError = message
	}
	session := cloneSession(s.session)
	s.mu.Unlock()
	if s.backend != nil {
		s.backend.ReleaseInjectedInput()
		_ = stopSafetyHooksFor(s.backend)
		s.backend.HideOverlay()
	}
	s.publish(Event{Kind: string(state), Session: session})
	return session
}

func (s *Service) pauseForUserInput() {
	_, _ = s.Pause()
}

func (s *Service) publish(event Event) {
	if s.emit != nil {
		s.emit(event)
	}
}

func cloneSession(in Session) Session {
	in.Logs = append([]ActionLog(nil), in.Logs...)
	return in
}

func sessionID() (string, error) {
	body := make([]byte, 12)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return "computer-" + hex.EncodeToString(body), nil
}
