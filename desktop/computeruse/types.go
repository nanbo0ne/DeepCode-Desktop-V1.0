package computeruse

import "time"

type SessionState string

const (
	StateIdle      SessionState = "idle"
	StateRunning   SessionState = "running"
	StatePaused    SessionState = "paused"
	StateStopping  SessionState = "stopping"
	StateSucceeded SessionState = "succeeded"
	StateFailed    SessionState = "failed"
	StateCancelled SessionState = "cancelled"
)

type Capabilities struct {
	TemporarilyDisabled bool   `json:"temporarilyDisabled"`
	Platform            string `json:"platform"`
	Supported           bool   `json:"supported"`
	ScreenCapture       bool   `json:"screenCapture"`
	UIAutomation        bool   `json:"uiAutomation"`
	InputInjection      bool   `json:"inputInjection"`
	Overlay             bool   `json:"overlay"`
	EmergencyStop       bool   `json:"emergencyStop"`
	UnavailableReason   string `json:"unavailableReason,omitempty"`
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Display struct {
	ID      string `json:"id"`
	Bounds  Rect   `json:"bounds"`
	Primary bool   `json:"primary"`
	Scale   int    `json:"scale"`
}

type Window struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Process     string `json:"process"`
	ProcessID   uint32 `json:"processId"`
	Bounds      Rect   `json:"bounds"`
	Foreground  bool   `json:"foreground"`
	Minimized   bool   `json:"minimized"`
	HigherTrust bool   `json:"higherTrust"`
}

type Element struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Role         string   `json:"role,omitempty"`
	AutomationID string   `json:"automationId,omitempty"`
	Bounds       Rect     `json:"bounds"`
	Enabled      bool     `json:"enabled"`
	Focused      bool     `json:"focused"`
	Password     bool     `json:"password"`
	Patterns     []string `json:"patterns,omitempty"`
}

type Observation struct {
	SessionID        string    `json:"sessionId"`
	Generation       uint64    `json:"generation"`
	ObservedAt       time.Time `json:"observedAt"`
	DisplayID        string    `json:"displayId"`
	Crop             Rect      `json:"crop"`
	Foreground       Window    `json:"foreground"`
	Windows          []Window  `json:"windows"`
	Elements         []Element `json:"elements"`
	Screenshot       string    `json:"-"`
	ScreenshotMIME   string    `json:"-"`
	ScreenshotWidth  int       `json:"screenshotWidth"`
	ScreenshotHeight int       `json:"screenshotHeight"`
	Summary          string    `json:"summary"`
	SecureDesktop    bool      `json:"secureDesktop"`
}

type Action struct {
	SessionID   string   `json:"sessionId,omitempty"`
	Type        string   `json:"type"`
	Generation  uint64   `json:"generation"`
	ElementID   string   `json:"elementId,omitempty"`
	DisplayID   string   `json:"displayId,omitempty"`
	X           float64  `json:"x,omitempty"`
	Y           float64  `json:"y,omitempty"`
	EndX        float64  `json:"endX,omitempty"`
	EndY        float64  `json:"endY,omitempty"`
	DeltaX      int      `json:"deltaX,omitempty"`
	DeltaY      int      `json:"deltaY,omitempty"`
	Text        string   `json:"text,omitempty"`
	Key         string   `json:"key,omitempty"`
	Keys        []string `json:"keys,omitempty"`
	WindowID    string   `json:"windowId,omitempty"`
	TimeoutMS   int      `json:"timeoutMs,omitempty"`
	Description string   `json:"description,omitempty"`
}

type ActionResult struct {
	ActionID    string      `json:"actionId"`
	Action      Action      `json:"action"`
	Success     bool        `json:"success"`
	Error       string      `json:"error,omitempty"`
	DurationMS  int64       `json:"durationMs"`
	Observation Observation `json:"observation"`
}

type ActionLog struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Window     string    `json:"window,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMS int64     `json:"durationMs"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
}

type Session struct {
	ParentTurnID    string       `json:"parentTurnId,omitempty"`
	ParentSessionID string       `json:"parentSessionId,omitempty"`
	Escalated       bool         `json:"escalated,omitempty"`
	ID              string       `json:"id"`
	TabID           string       `json:"tabId,omitempty"`
	Goal            string       `json:"goal"`
	SuccessCriteria string       `json:"successCriteria,omitempty"`
	Restrictions    string       `json:"restrictions,omitempty"`
	ModelRef        string       `json:"modelRef,omitempty"`
	State           SessionState `json:"state"`
	CurrentApp      string       `json:"currentApp,omitempty"`
	CurrentAction   string       `json:"currentAction,omitempty"`
	ActionCount     int          `json:"actionCount"`
	StartedAt       time.Time    `json:"startedAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
	CompletedAt     *time.Time   `json:"completedAt,omitempty"`
	LastError       string       `json:"lastError,omitempty"`
	Logs            []ActionLog  `json:"logs"`
}

type StartRequest struct {
	ParentTurnID    string `json:"parentTurnId,omitempty"`
	ParentSessionID string `json:"parentSessionId,omitempty"`
	ResumeSessionID string `json:"resumeSessionId,omitempty"`
	Guidance        string `json:"guidance,omitempty"`
	TabID           string `json:"tabId,omitempty"`
	Goal            string `json:"goal"`
	SuccessCriteria string `json:"successCriteria,omitempty"`
	Restrictions    string `json:"restrictions,omitempty"`
	ModelRef        string `json:"modelRef,omitempty"`
}

type OverlayState struct {
	State         SessionState `json:"state"`
	App           string       `json:"app,omitempty"`
	Action        string       `json:"action,omitempty"`
	PointerX      int          `json:"pointerX,omitempty"`
	PointerY      int          `json:"pointerY,omitempty"`
	ClickKind     string       `json:"clickKind,omitempty"`
	ReducedMotion bool         `json:"reducedMotion"`
}
