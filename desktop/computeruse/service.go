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
	ErrNotSupported      = errors.New("computer use is not supported on this platform")
	ErrNotRunning        = errors.New("computer use session is not running")
	ErrStaleObservation  = errors.New("the observation is stale; observe again before acting")
	ErrProtectedSurface  = errors.New("the target is protected and cannot be automated")
	ErrUserInputPaused   = errors.New("computer use paused because the user took control")
	ErrStopPending       = errors.New("computer input is still stopping; wait before starting another task")
	ErrOccupied          = errors.New("the desktop is owned by another running or paused session")
	ErrWrongOwner        = errors.New("computer session ownership does not match")
	ErrParentTurnStopped = errors.New("computer control is stopped for this parent turn; do not retry computer_task or reset its session; stop and request an explicit user decision in a new turn")
)

const ParentTurnMaxActions = 40

type parentTurnBudget struct {
	tabID, sessionID string
	actions          int
	active, blocked  bool
	pausedID         string
}

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

type inputErrorReleaser interface {
	ReleaseInjectedInputError() error
}

func releaseInjectedInputFor(backend Backend) error {
	if releaser, ok := backend.(inputErrorReleaser); ok {
		return releaser.ReleaseInjectedInputError()
	}
	backend.ReleaseInjectedInput()
	return nil
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
	nativeCalls  int
	nativeDone   chan struct{}
	parentTurns  map[string]*parentTurnBudget
	stopParent   func() bool
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

// Admission precedes provider qualification. A failed call cannot get a fresh
// allowance by selecting a new goal or creating a new desktop session.
func (s *Service) BeginParentTask(ctx, lifetime context.Context, request StartRequest) (func(bool), error) {
	if err := s.Availability(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lifetime == nil || lifetime.Err() != nil || request.ParentTurnID == "" || request.ParentSessionID == "" || request.TabID == "" {
		return nil, ErrWrongOwner
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parentTurns == nil {
		s.parentTurns = make(map[string]*parentTurnBudget)
	}
	budget := s.parentTurns[request.ParentTurnID]
	if budget != nil {
		if budget.tabID != request.TabID || budget.sessionID != request.ParentSessionID {
			return nil, ErrWrongOwner
		}
		if budget.blocked || budget.actions >= ParentTurnMaxActions {
			return nil, ErrParentTurnStopped
		}
		if budget.active {
			return nil, ErrOccupied
		}
		if budget.pausedID != "" && budget.pausedID != request.ResumeSessionID {
			return nil, ErrParentTurnStopped
		}
	}
	if request.ResumeSessionID != "" {
		if s.session.ID != request.ResumeSessionID || s.session.TabID != request.TabID || s.session.ParentSessionID != request.ParentSessionID {
			return nil, ErrWrongOwner
		}
		if s.session.State != StatePaused {
			return nil, ErrParentTurnStopped
		}
	}
	if budget == nil {
		budget = &parentTurnBudget{tabID: request.TabID, sessionID: request.ParentSessionID}
		if request.ResumeSessionID != "" {
			budget.actions = s.session.ActionCount
		}
		s.parentTurns[request.ParentTurnID] = budget
		owned := budget
		context.AfterFunc(lifetime, func() {
			s.mu.Lock()
			if s.parentTurns[request.ParentTurnID] == owned {
				delete(s.parentTurns, request.ParentTurnID)
			}
			s.mu.Unlock()
		})
	}
	if budget.actions >= ParentTurnMaxActions {
		budget.blocked = true
		return nil, ErrParentTurnStopped
	}
	budget.active = true
	var once sync.Once
	return func(failed bool) {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			budget.active = false
			budget.blocked = budget.blocked || failed
			if s.session.ParentTurnID == request.ParentTurnID && s.session.State == StatePaused && !failed {
				budget.pausedID = s.session.ID
			} else {
				budget.pausedID = ""
			}
		})
	}, nil
}

func (s *Service) ParentTurnActions(turnID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	budget := s.parentTurns[turnID]
	if budget == nil || budget.blocked || !budget.active {
		return 0, ErrParentTurnStopped
	}
	return budget.actions, nil
}

func (s *Service) Start(ctx context.Context, request StartRequest) (Session, error) {
	if err := s.Availability(); err != nil {
		return Session{}, err
	}
	s.lifecycle.Lock()
	unlock := sync.OnceFunc(s.lifecycle.Unlock)
	defer unlock()
	if s.backend == nil || !s.backend.Capabilities().Supported {
		return Session{}, ErrNotSupported
	}
	if request.Goal == "" {
		return Session{}, fmt.Errorf("computer task goal is required")
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	s.mu.Lock()
	occupied := s.session.State == StateRunning || s.session.State == StatePaused || s.session.State == StateStopping || s.actionDone != nil || s.nativeCalls != 0
	if request.ParentTurnID != "" || request.ParentSessionID != "" {
		budget := s.parentTurns[request.ParentTurnID]
		if budget == nil || !budget.active || budget.blocked || budget.tabID != request.TabID || budget.sessionID != request.ParentSessionID {
			s.mu.Unlock()
			return Session{}, ErrParentTurnStopped
		}
	}
	s.mu.Unlock()
	if occupied {
		return s.Current(), ErrOccupied
	}
	id, err := sessionID()
	if err != nil {
		return Session{}, err
	}
	runCtx, cancel := context.WithCancel(context.WithValue(ctx, ownerContextKey{}, id))
	now := time.Now().UTC()
	s.mu.Lock()
	s.cancel = cancel
	s.runCtx = runCtx
	s.stopParent = context.AfterFunc(ctx, func() { _ = s.stopContext(id, runCtx) })
	s.observation = Observation{}
	s.session = Session{ID: id, TabID: request.TabID, Goal: request.Goal, SuccessCriteria: request.SuccessCriteria, Restrictions: request.Restrictions, ModelRef: request.ModelRef, State: StateRunning, StartedAt: now, UpdatedAt: now, Logs: []ActionLog{}}
	s.session.ParentTurnID, s.session.ParentSessionID = request.ParentTurnID, request.ParentSessionID
	session := cloneSession(s.session)
	finishSetup := s.beginNativeLocked()
	s.mu.Unlock()
	unlock()
	defer finishSetup()
	err = func() error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := s.backend.StartSafetyHooks(func() { _ = s.StopSession(id, "escape") }, func() { _, _ = s.PauseSession(id) }); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := s.backend.ShowOverlay(OverlayState{State: StateRunning, Action: "正在观察屏幕"}); err != nil {
			return err
		}
		if err := runCtx.Err(); err != nil {
			return err
		}
		s.publish(Event{Kind: "started", Session: session})
		_, err := s.Observe(runCtx)
		return err
	}()
	finishSetup()
	return s.finishStartup(runCtx, id, err)
}

// Native calls may ignore cancellation. Keep the desktop occupied until they
// really return, without holding lifecycle across COM, capture or overlay calls.
// Caller holds mu; the returned release is idempotent.
func (s *Service) beginNativeLocked() func() {
	if s.nativeCalls == 0 {
		s.nativeDone = make(chan struct{})
	}
	s.nativeCalls++
	return sync.OnceFunc(func() {
		s.mu.Lock()
		s.nativeCalls--
		if s.nativeCalls == 0 {
			close(s.nativeDone)
			s.nativeDone = nil
		}
		s.mu.Unlock()
	})
}

func (s *Service) finishStartup(ctx context.Context, id string, cause error) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	current := s.session.ID == id && s.runCtx == ctx && s.session.State == StateRunning
	session := cloneSession(s.session)
	s.mu.Unlock()
	if !current {
		return session, errors.Join(cause, ErrNotRunning)
	}
	if cause == nil {
		cause = ctx.Err()
	}
	if cause != nil {
		session, cleanupErr := s.finish(StateFailed, cause.Error())
		return session, errors.Join(cause, cleanupErr)
	}
	return session, nil
}

func (s *Service) Observe(ctx context.Context) (Observation, error) {
	return s.observe(ctx, false)
}

func (s *Service) observe(ctx context.Context, afterAction bool) (Observation, error) {
	if err := s.Availability(); err != nil {
		return Observation{}, err
	}
	s.mu.Lock()
	if err := s.checkOwnerContext(ctx); err != nil {
		s.mu.Unlock()
		return Observation{}, err
	}
	if s.actionDone != nil && !afterAction {
		s.mu.Unlock()
		return Observation{}, ErrStopPending
	}
	if s.session.State != StateRunning && s.session.State != StatePaused {
		s.mu.Unlock()
		return Observation{}, ErrNotRunning
	}
	s.generation++
	generation, sessionID := s.generation, s.session.ID
	runCtx := s.runCtx
	finishNative := s.beginNativeLocked()
	s.mu.Unlock()
	defer finishNative()
	ctx, cancel := linkedContext(ctx, runCtx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
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
	if s.session.ID != sessionID || s.runCtx != runCtx || (s.session.State != StateRunning && s.session.State != StatePaused) {
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

func (s *Service) Execute(ctx context.Context, action Action) (result ActionResult, retErr error) {
	if err := s.Availability(); err != nil {
		return result, err
	}
	if !s.actionMu.TryLock() {
		return ActionResult{}, ErrStopPending
	}
	defer s.actionMu.Unlock()
	s.mu.Lock()
	if err := s.checkOwnerContext(ctx); err != nil {
		s.mu.Unlock()
		return ActionResult{}, err
	}
	if action.SessionID != "" && action.SessionID != s.session.ID {
		s.mu.Unlock()
		return ActionResult{}, ErrWrongOwner
	}
	if s.session.State == StatePaused {
		s.mu.Unlock()
		return ActionResult{}, ErrUserInputPaused
	}
	if s.session.State != StateRunning {
		s.mu.Unlock()
		return ActionResult{}, ErrNotRunning
	}
	if s.nativeCalls != 0 {
		s.mu.Unlock()
		return ActionResult{}, ErrStopPending
	}
	if action.Generation == 0 || action.Generation != s.observation.Generation || action.Generation != s.generation {
		s.mu.Unlock()
		return ActionResult{}, ErrStaleObservation
	}
	if s.observation.SecureDesktop || s.observation.Foreground.HigherTrust {
		s.mu.Unlock()
		return ActionResult{}, ErrProtectedSurface
	}
	if err := validateActionTarget(s.observation, action); err != nil {
		s.mu.Unlock()
		return ActionResult{}, err
	}
	ctx, cancel := linkedContext(ctx, s.runCtx)
	if err := ctx.Err(); err != nil {
		cancel()
		s.mu.Unlock()
		return ActionResult{}, err
	}
	if s.session.ParentTurnID != "" {
		budget := s.parentTurns[s.session.ParentTurnID]
		if budget == nil || budget.blocked || !budget.active || budget.actions >= ParentTurnMaxActions {
			cancel()
			s.mu.Unlock()
			return ActionResult{}, ErrParentTurnStopped
		}
		budget.actions++ // Reserve before input, including uncertain input.
	}
	done := make(chan struct{})
	s.actionCancel, s.actionDone = cancel, done
	finishNative := s.beginNativeLocked()
	runCtx := s.runCtx
	observation := s.observation
	// Consume before input begins, including uncertain or cancelled input.
	s.generation++
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
			if err := releaseInjectedInputFor(s.backend); err != nil {
				s.cleanupFailed(sessionID, runCtx, err)
				result.Success, result.Error = false, err.Error()
				retErr = errors.Join(retErr, err)
			}
		}
		cancel()
		s.mu.Lock()
		if retErr != nil && s.session.ID == sessionID && s.runCtx == runCtx {
			if budget := s.parentTurns[s.session.ParentTurnID]; budget != nil {
				budget.blocked = true
			}
		}
		if s.actionDone == done {
			s.actionCancel, s.actionDone = nil, nil
		}
		close(done)
		current := cloneSession(s.session)
		s.mu.Unlock()
		if result.ActionID != "" {
			s.publish(Event{Kind: "action", Session: current, Action: &result})
		}
		finishNative()
	}()
	s.backend.UpdateOverlay(OverlayState{State: StateRunning, App: observation.Foreground.Title, Action: session.CurrentAction})

	started := time.Now()
	err := s.backend.Execute(ctx, observation, action)
	if err == nil {
		err = ctx.Err()
	}
	result = ActionResult{ActionID: fmt.Sprintf("action-%d", started.UnixNano()), Action: action, Success: err == nil, DurationMS: time.Since(started).Milliseconds()}
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
		return result, err
	}
	// Every successful action invalidates the previous element IDs and coordinates.
	next, observeErr := s.observe(ctx, true)
	result.Observation = next
	if observeErr != nil {
		result.Success, result.Error = false, observeErr.Error()
		err = observeErr
	}
	shouldRelease = err != nil
	return result, err
}

func (s *Service) Pause() (Session, error) {
	s.cancelController("")
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	return s.pause("", "", false)
}

func (s *Service) PauseSession(id string) (Session, error) {
	s.cancelController(id)
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	current := id != "" && s.session.ID == id
	s.mu.Unlock()
	if !current {
		return Session{}, ErrWrongOwner
	}
	return s.pause("", "", false)
}

func (s *Service) Escalate(id, tabID string) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	return s.pause(id, tabID, true)
}

func (s *Service) pause(id, tabID string, escalation bool) (Session, error) {
	s.mu.Lock()
	if escalation && (id == "" || tabID == "" || s.session.ID != id || s.session.TabID != tabID) {
		s.mu.Unlock()
		return Session{}, ErrWrongOwner
	}
	if s.session.State != StateRunning {
		s.mu.Unlock()
		return Session{}, ErrNotRunning
	}
	if escalation && s.runCtx.Err() != nil {
		err := s.runCtx.Err()
		s.mu.Unlock()
		return Session{}, err
	}
	s.session.State, s.session.UpdatedAt = StatePaused, time.Now().UTC()
	s.session.Escalated = escalation
	if s.cancel != nil {
		s.cancel()
	}
	if escalation && s.stopParent != nil {
		s.stopParent()
		s.stopParent = nil
	}
	s.generation++
	cancel, done := s.actionCancel, s.nativeDone
	runCtx := s.runCtx
	session := cloneSession(s.session)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if err := releaseInjectedInputFor(s.backend); err != nil {
		s.cleanupFailed(session.ID, runCtx, err)
		return s.Current(), err
	}
	s.backend.UpdateOverlay(OverlayState{State: StatePaused, App: session.CurrentApp, Action: "已暂停"})
	s.publish(Event{Kind: "paused", Session: session})
	if err := waitForAction(done); err != nil {
		return s.Current(), err
	}
	if err := releaseInjectedInputFor(s.backend); err != nil {
		s.cleanupFailed(session.ID, runCtx, err)
		return s.Current(), err
	}
	current := s.Current()
	if current.State == StateStopping {
		return current, ErrStopPending
	}
	return current, nil
}

func (s *Service) Resume(ctx context.Context) (Session, error) {
	return s.resume(ctx, "", "", false, "", "")
}

// ResumeTask requires an explicit session and tab, and never changes its goal,
// model or restrictions. Only the isolated loop may resume an escalation.
func (s *Service) ResumeTask(ctx context.Context, id, tabID string, parent ...string) (Session, error) {
	parentSession, parentTurn := "", ""
	if len(parent) == 2 {
		parentSession, parentTurn = parent[0], parent[1]
	}
	return s.resume(ctx, id, tabID, true, parentSession, parentTurn)
}

func (s *Service) resume(ctx context.Context, id, tabID string, owned bool, parentSession, parentTurn string) (Session, error) {
	if err := s.Availability(); err != nil {
		return Session{}, err
	}
	s.lifecycle.Lock()
	unlock := sync.OnceFunc(s.lifecycle.Unlock)
	defer unlock()
	s.mu.Lock()
	if owned && (id == "" || tabID == "" || s.session.ID != id || s.session.TabID != tabID) {
		s.mu.Unlock()
		return Session{}, ErrWrongOwner
	}
	if s.session.ParentTurnID != "" {
		budget := s.parentTurns[parentTurn]
		if !owned || s.session.ParentSessionID != parentSession || budget == nil || !budget.active || budget.blocked || budget.tabID != tabID || budget.sessionID != parentSession {
			s.mu.Unlock()
			return Session{}, ErrWrongOwner
		}
	}
	if s.session.Escalated && !owned {
		s.mu.Unlock()
		return Session{}, fmt.Errorf("resume the escalated task through computer_task with resume_session_id")
	}
	if s.session.State != StatePaused {
		s.mu.Unlock()
		return Session{}, ErrNotRunning
	}
	if s.actionDone != nil || s.nativeCalls != 0 {
		s.mu.Unlock()
		return Session{}, ErrStopPending
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return Session{}, err
	}
	{
		id = s.session.ID
		if s.stopParent != nil {
			s.stopParent()
		}
		if s.cancel != nil {
			s.cancel()
		}
		runCtx, cancel := context.WithCancel(context.WithValue(ctx, ownerContextKey{}, id))
		s.runCtx, s.cancel = runCtx, cancel
		s.stopParent = context.AfterFunc(ctx, func() { _ = s.stopContext(id, runCtx) })
	}
	s.session.Escalated = false
	if parentTurn != "" {
		s.session.ParentTurnID = parentTurn
	}
	s.session.State, s.session.UpdatedAt = StateRunning, time.Now().UTC()
	session := cloneSession(s.session)
	runCtx := s.runCtx
	finishSetup := s.beginNativeLocked()
	s.mu.Unlock()
	unlock()
	defer finishSetup()
	s.backend.UpdateOverlay(OverlayState{State: StateRunning, App: session.CurrentApp, Action: "正在重新观察"})
	_, err := s.Observe(runCtx)
	finishSetup()
	current, err := s.finishStartup(runCtx, session.ID, err)
	if err == nil {
		s.publish(Event{Kind: "resumed", Session: current})
	}
	return current, err
}

func (s *Service) Complete(generation uint64, message string) (Session, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.session.State != StateRunning || s.actionDone != nil || s.nativeCalls != 0 || s.session.LastError != "" || s.runCtx.Err() != nil {
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
	s.mu.Unlock()
	return s.finish(StateSucceeded, message)
}

func (s *Service) Stop(reason string) error {
	s.cancelController("")
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	return s.stop(reason)
}

func (s *Service) StopSession(id, reason string) error {
	s.cancelController(id)
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

// Cancellation must not wait for lifecycle operations that are observing the
// screen. Their backend calls are linked to this context and must unwind first.
func (s *Service) cancelController(id string) {
	s.mu.Lock()
	if (id == "" || s.session.ID == id) && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// StopController cannot let a late error from a previous controller epoch stop
// an explicitly resumed controller using the same session ID.
func (s *Service) StopController(ctx context.Context, id, reason string) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	current := s.session.ID == id && s.runCtx == ctx
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
	s.mu.Unlock()
	_, err := s.finish(StateCancelled, reason)
	return err
}

func linkedContext(ctx, session context.Context) (context.Context, context.CancelFunc) {
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(session, cancel)
	if session.Err() != nil {
		cancel()
	}
	return linked, func() { stop(); cancel() }
}

type ownerContextKey struct{}

// Caller must hold mu. Provider loops carry an immutable owner in their context;
// native UI calls also have generation/session checks and may use boot context.
func (s *Service) checkOwnerContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id, ok := ctx.Value(ownerContextKey{}).(string); ok && id != s.session.ID {
		return ErrWrongOwner
	}
	return nil
}

func (s *Service) stopContext(id string, runCtx context.Context) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	current := s.session.ID == id && s.runCtx == runCtx && !s.session.Escalated
	s.mu.Unlock()
	if !current {
		return nil
	}
	return s.stop("owning request cancelled")
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

func (s *Service) cleanupFailed(id string, ctx context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.ID != id || s.runCtx != ctx {
		return
	}
	s.session.State, s.session.LastError = StateStopping, err.Error()
	if budget := s.parentTurns[s.session.ParentTurnID]; budget != nil {
		budget.blocked = true
	}
	s.session.CurrentAction, s.session.UpdatedAt = "正在停止", time.Now().UTC()
	s.session.CompletedAt = nil
	s.generation++
	if s.cancel != nil {
		s.cancel()
	}
}

// Caller holds lifecycle. Terminal state is published only after native calls
// have drained and input/hook cleanup has succeeded; Stop can retry failures.
func (s *Service) finish(state SessionState, message string) (Session, error) {
	s.mu.Lock()
	if state != StateSucceeded {
		if budget := s.parentTurns[s.session.ParentTurnID]; budget != nil {
			budget.blocked = true
		}
	}
	s.session.State, s.session.CurrentAction, s.session.UpdatedAt = StateStopping, "正在停止", time.Now().UTC()
	s.session.CompletedAt = nil
	s.generation++
	if s.stopParent != nil {
		s.stopParent()
		s.stopParent = nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	done, runCtx := s.nativeDone, s.runCtx
	session := cloneSession(s.session)
	s.mu.Unlock()
	s.publish(Event{Kind: "stopping", Session: session})
	fail := func(err error) (Session, error) {
		s.cleanupFailed(session.ID, runCtx, err)
		return s.Current(), err
	}
	if s.backend != nil {
		if err := releaseInjectedInputFor(s.backend); err != nil {
			return fail(err)
		}
	}
	if err := waitForAction(done); err != nil {
		return fail(err)
	}
	if s.backend != nil {
		// An in-flight input may have unwound after the first release.
		if err := releaseInjectedInputFor(s.backend); err != nil {
			return fail(err)
		}
		if err := stopSafetyHooksFor(s.backend); err != nil {
			return fail(err)
		}
		s.backend.HideOverlay()
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.cancel = nil
	s.session.State, s.session.UpdatedAt, s.session.CompletedAt = state, now, &now
	if state == StateFailed {
		s.session.LastError = message
	}
	session = cloneSession(s.session)
	s.mu.Unlock()
	s.publish(Event{Kind: string(state), Session: session})
	return session, nil
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
