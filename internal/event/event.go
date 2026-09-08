// Package event defines the typed event stream the agent emits as it runs a
// turn, and the Sink it emits to. It decouples "what happened" (the model
// produced reasoning, a tool was dispatched, a turn used N tokens) from "how to
// show it" (ANSI scrollback in a terminal, a card in a webview).
//
// The agent depends only on Sink; each frontend implements one. The chat TUI
// renders events to its scrollback; a headless run renders them to plain ANSI
// on stdout; a future GUI/serve transport forwards them to a webview or
// websocket. This replaces the old io.Writer contract, where the agent wrote
// pre-formatted ANSI and the consumer had to re-derive structure by matching
// line prefixes — fragile, and lossy for any frontend richer than a terminal.
package event

import (
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/evidence"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/nilutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Kind tags an Event. Read the field(s) documented for that kind.
type Kind int

const (
	// TurnStarted marks the start of one top-level Run (one user turn). Sinks
	// reset any per-turn rendering state on it. Carries no payload.
	TurnStarted Kind = iota
	// Reasoning is a thinking-mode reasoning delta (Text). Streamed before the
	// visible answer; sinks typically render it muted under a "thinking" header.
	Reasoning
	// Text is an answer-text delta (Text).
	Text
	// Message marks the assistant turn's text as complete: Text holds the full
	// answer and Reasoning the full chain-of-thought (both already streamed via
	// the deltas above). A sink may use it to re-render the streamed raw text as
	// styled markdown; a plain sink can ignore it.
	Message
	// ToolDispatch announces a tool call is about to run (Tool: ID/Name/Args/ReadOnly).
	ToolDispatch
	// ToolResult reports a finished tool call (Tool: Output/Err/Truncated set).
	ToolResult
	// Usage carries per-turn token telemetry (Usage; Pricing optional, for cost).
	Usage
	// Notice is an out-of-band message — a warning, truncation, block, or
	// compaction notice (Level + Text).
	Notice
	// Phase marks a coordinator boundary, e.g. planner→executor handoff (Text =
	// label such as "deepseek · planning").
	Phase
	// ApprovalRequest asks the frontend to approve a pending tool call
	// (Approval: ID/Tool/Subject). The run blocks until the controller's
	// Approve(ID, …) resolves it; a frontend shows a prompt and answers.
	ApprovalRequest
	// AskRequest asks the frontend to put one or more structured multiple-choice
	// questions to the user (Ask: ID + Questions). The run blocks until the
	// controller's AnswerQuestion(ID, …) resolves it. Powers the `ask` tool.
	AskRequest
	// TurnDone marks the end of one top-level Run (Err non-nil on failure;
	// nil also for a user cancellation, which is not an error). Always the
	// last event of a turn.
	TurnDone
	// CompactionStarted marks the start of a context-compaction pass (Compaction
	// payload: Trigger). A frontend shows a "compacting…" placeholder while the
	// summarizer runs; CompactionDone replaces it. Mirrors ToolDispatch/ToolResult.
	CompactionStarted
	// CompactionDone reports a finished compaction pass (Compaction payload:
	// Trigger/Messages/Summary/Archive). An aborted pass emits this with an empty
	// Summary so the placeholder still resolves. Replaces the older plain Notice
	// so a sink can render a distinct, expandable card.
	CompactionDone
	// ToolProgress streams a chunk of a still-running tool's combined output
	// (Tool: ID + Output = the new chunk). Emitted between ToolDispatch and
	// ToolResult for long tools like bash so a frontend can show live progress.
	// Appended last to keep the Kind values before it wire-stable.
	ToolProgress
	// MCPSurfaceReady fires once per server when its background-loaded surface
	// (prompts or resources) finishes after startup. Lets UIs refresh /mcp
	// status without polling. Text carries "<server>: <surface> ready (<count>
	// items)". Appended last to keep the Kind values before it wire-stable.
	MCPSurfaceReady
	// Retrying fires before each backoff sleep while the provider re-attempts the
	// connection+header phase after a transient failure (RetryAttempt of RetryMax).
	// A frontend shows a transient "retrying (n/m)" indicator that the next stream
	// event — or TurnDone — clears. Appended last to keep the Kind values before
	// it wire-stable.
	Retrying
	// Steer fires when a mid-turn steer message is consumed from the queue and
	// injected as a user message. Text carries the raw steer content (without the
	// wrapper prefix), so a frontend can display it to the user as confirmation.
	// Frontends use Steer to know a queued message has been delivered.
	Steer
	// AnswerCommitted marks the exact assistant message that passed the host's
	// final-answer readiness checks. TurnDone may only report success after this
	// event has been emitted.
	AnswerCommitted
	// ItemStarted, ItemDelta, and ItemCompleted expose the normalized Turn -> Item lifecycle
	// used by rich frontends. Existing typed events remain the item-specific
	// payload stream for backwards compatibility.
	ItemStarted
	ItemDelta
	ItemCompleted
	// ChildStarted and ChildDone delimit a background child attached to the
	// parent turn. They are internal telemetry receipts; desktop sinks consume
	// them without forwarding them to the user-facing event stream.
	ChildStarted
	ChildDone
)

type TurnState string

const (
	TurnStateInProgress TurnState = "in_progress"
	TurnStateFinished   TurnState = "finished"
)

type TurnOutcome string

const (
	TurnOutcomeSuccess     TurnOutcome = "success"
	TurnOutcomeFailed      TurnOutcome = "failed"
	TurnOutcomeCancelled   TurnOutcome = "cancelled"
	TurnOutcomeInterrupted TurnOutcome = "interrupted"
)

type ItemType string

const (
	ItemUserMessage  ItemType = "user_message"
	ItemAgentMessage ItemType = "agent_message"
	ItemReasoning    ItemType = "reasoning"
	ItemTool         ItemType = "tool"
	ItemPlan         ItemType = "plan"
	ItemNotice       ItemType = "notice"
	ItemCompaction   ItemType = "compaction"
)

type ItemStatus string

const (
	ItemStatusStarted   ItemStatus = "started"
	ItemStatusStreaming ItemStatus = "streaming"
	ItemStatusCompleted ItemStatus = "completed"
	ItemStatusFailed    ItemStatus = "failed"
)

// TurnItem is the persisted identity and terminal status of one canonical item.
// MessageOrdinal maps visible assistant messages back to provider history without
// making tool-only assistant records consume a display identity.
type TurnItem struct {
	ItemID         string     `json:"itemId"`
	MessageID      string     `json:"messageId,omitempty"`
	Type           ItemType   `json:"type"`
	Status         ItemStatus `json:"status"`
	MessageOrdinal int        `json:"messageOrdinal,omitempty"`
}

// TurnRecord is the durable projection of a top-level user turn. The provider
// transcript remains unchanged; this record preserves exact UI classification.
type TurnRecord struct {
	TurnID         string      `json:"turnId"`
	CheckpointTurn int         `json:"checkpointTurn,omitempty"`
	State          TurnState   `json:"state,omitempty"`
	Outcome        TurnOutcome `json:"outcome,omitempty"`
	StartedAt      int64       `json:"startedAt"`
	CompletedAt    int64       `json:"completedAt,omitempty"`
	ElapsedMs      int64       `json:"elapsedMs,omitempty"`
	Tokens         int         `json:"tokens,omitempty"`
	RequestCount   int         `json:"requestCount,omitempty"`
	Cost           float64     `json:"cost,omitempty"`
	Currency       string      `json:"currency,omitempty"`
	CostAvailable  bool        `json:"costAvailable"`
	FinalItemID    string      `json:"finalItemId,omitempty"`
	FinalMessageID string      `json:"finalMessageId,omitempty"`
	Items          []TurnItem  `json:"items,omitempty"`
}

// Level classifies a Notice so sinks can style or filter it.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
)

// Profile carries the subagent model/effort resolved for this call.
type Profile struct {
	Model  string
	Effort string
}

// Tool describes a tool call for ToolDispatch / ToolResult events. On dispatch
// only ID/Name/Args/ReadOnly are set; on result Output/Err/Truncated are filled
// in. Args is the raw JSON arguments — a sink compacts it for display.
type Tool struct {
	ID         string
	Name       string
	Args       string
	Output     string // ToolResult: the result text fed to the model
	Err        string // ToolResult: non-empty when the call failed or was blocked
	ReadOnly   bool
	Truncated  bool  // ToolResult: Output was head+tailed before display/model
	DurationMs int64 // ToolResult: wall-clock execution time in milliseconds
	// Partial marks an early ToolDispatch emitted when a call begins (ID/Name set,
	// Args still streaming) so a frontend can show the card immediately; a second,
	// full ToolDispatch (Partial false, Args set) follows when the call completes.
	Partial bool
	// ParentID, when set, is the ID of the tool call that spawned this one — a
	// sub-agent's calls carry the parent `task` call's ID so a frontend can nest
	// them under it. Empty for top-level calls.
	ParentID string
	FileDiff
	Profile *Profile // ToolDispatch: subagent model/effort (set for task/skill calls)
}

// FileDiff is a previewed change carried on a writer tool's full ToolDispatch
// and on its ApprovalRequest, so a frontend can render +/- lines before the
// call runs. Diff is the unified diff (empty for read-only tools, binary files,
// or no-op changes); Added/Removed are its line tallies.
type FileDiff struct {
	Diff    string
	Added   int
	Removed int
}

// Approval identifies a pending tool-call approval for an ApprovalRequest
// event. ID correlates the request with the controller's Approve(ID, …) reply.
type Approval struct {
	ID      string
	Tool    string
	Subject string
}

// AskOption is one choice the user can pick for an AskQuestion.
type AskOption struct {
	Label       string
	Description string // optional one-line explanation shown under the label
}

// AskQuestion is one structured question the `ask` tool puts to the user.
type AskQuestion struct {
	ID      string // stable per-question id, so answers correlate back
	Header  string // short label (the tab title)
	Prompt  string // the question text
	Options []AskOption
	Multi   bool // allow selecting more than one option
}

// Ask carries an AskRequest: a batch of questions and the ID that correlates the
// controller's AnswerQuestion(ID, …) reply.
type Ask struct {
	ID        string
	Questions []AskQuestion
}

// Compaction carries a context-compaction pass for the CompactionStarted /
// CompactionDone events. On CompactionStarted only Trigger is set. On
// CompactionDone, Messages/Summary/Archive are filled in (an aborted pass leaves
// Summary empty). Trigger is "auto" (the prompt reached the window threshold) or
// "manual" (the user ran /compact).
type Compaction struct {
	Trigger  string // "auto" | "manual"
	Messages int    // Done: how many messages were folded into the summary
	Summary  string // Done: the briefing the agent keeps relying on
	Archive  string // Done: path the dropped originals were archived to ("" if none)
}

// AskAnswer is the user's reply to one AskQuestion: the chosen option label(s)
// (a free-typed answer is carried as a single Selected entry).
type AskAnswer struct {
	QuestionID string
	Selected   []string
}

// CacheDiagnostics describes whether and why the cacheable prefix changed since
// the last turn. It rides on the Usage event so every frontend can show
// cache-churn attribution.
type CacheDiagnostics struct {
	PrefixHash          string
	PrefixChanged       bool
	PrefixChangeReasons []string // "system", "tools", "log_rewrite"
	SystemHash          string
	ToolsHash           string
	LogRewriteVersion   int
	ToolSchemaTokens    int
	CacheMissTokens     int
	CacheHitTokens      int
}

// Event is one increment in a turn's event stream. Read the field(s) documented
// for Kind; the others are zero.
type Event struct {
	Kind   Kind
	TurnID string
	// ParentTurnID attributes usage emitted by a child or auxiliary reviewer to
	// the user-facing host turn. TurnID remains the ordinary lifecycle identity.
	ParentTurnID string
	// RequestID identifies the provider request represented by a Usage event.
	// Desktop telemetry uses it to deduplicate repeated usage receipts.
	RequestID string
	// ChildID identifies one background child for ChildStarted/ChildDone and its
	// forwarded usage receipts. ChildUsageReported is set on ChildDone only.
	ChildID            string
	ChildUsageReported bool
	// ProviderEndpoint is the actual configured request endpoint, retained for
	// provenance checks. A model name alone is not sufficient to establish
	// official pricing.
	ProviderEndpoint string
	ItemID           string
	MessageID        string
	FinalItemID      string
	FinalMessageID   string
	ItemType         ItemType
	ItemStatus       ItemStatus
	Outcome          TurnOutcome
	Text             string            // Reasoning / Text / Message / Notice / Phase
	Reasoning        string            // Message: the full reasoning chain
	Tool             Tool              // ToolDispatch / ToolResult
	Usage            *provider.Usage   // Usage
	Pricing          *provider.Pricing // Usage: for cost display (nil = omit cost)
	CacheDiagnostics *CacheDiagnostics // Usage: cache-churn attribution (nil = N/A)
	// SessionHit/SessionMiss carry cumulative cache tokens across the whole
	// session (Usage events only), so a frontend can show the aggregate hit-rate
	// — which doesn't crater on a short turn or after compaction — alongside
	// Usage's single-turn numbers.
	SessionHit   int        // Usage: cumulative cache-hit prompt tokens this session
	SessionMiss  int        // Usage: cumulative cache-miss prompt tokens this session
	Level        Level      // Notice
	Approval     Approval   // ApprovalRequest
	Ask          Ask        // AskRequest
	Err          error      // TurnDone: non-nil on failure
	Compaction   Compaction // Compaction
	RetryAttempt int        // Retrying: 1-based attempt about to be made
	RetryMax     int        // Retrying: total attempts before giving up
	// Turn* is populated by the desktop sink on TurnDone from its turn ledger.
	TurnTokens        int
	TurnCost          float64
	TurnCurrency      string
	TurnCostAvailable bool
}

// ReadinessAuditSink is an optional sink capability. Sinks that do not care
// about readiness audit receipts can implement only Sink and will ignore them.
type ReadinessAuditSink interface {
	RecordReadinessAudit(evidence.ReadinessAudit)
}

type RiskReviewAudit struct {
	Turn       int    `json:"turn,omitempty"`
	At         int64  `json:"at"`
	Level      string `json:"level,omitempty"`
	Model      string `json:"model,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Result     string `json:"result"`
	ErrorType  string `json:"errorType,omitempty"`
}

type RiskReviewAuditSink interface {
	RecordRiskReviewAudit(RiskReviewAudit)
}

// RecordReadinessAudit forwards a readiness audit receipt to sinks that opt in.
func RecordReadinessAudit(s Sink, a evidence.ReadinessAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if rs, ok := s.(ReadinessAuditSink); ok {
		rs.RecordReadinessAudit(a)
	}
}

func RecordRiskReviewAudit(s Sink, audit RiskReviewAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if recorder, ok := s.(RiskReviewAuditSink); ok {
		recorder.RecordRiskReviewAudit(audit)
	}
}

// NewRequestID returns a request receipt identity generated at request start.
// Durable sinks use it to deduplicate replayed usage receipts.
func NewRequestID() string { return lifecycleID("req") }

// Sink consumes a turn's events. The agent calls Emit serially from its run
// loop (tool execution may fan out across goroutines, but emission does not),
// so an implementation need not be safe for concurrent Emit. Emit must not
// block indefinitely — a channel-backed sink should be buffered or drained by
// a live reader.
type Sink interface {
	Emit(Event)
}

// FuncSink adapts a plain function to a Sink.
type FuncSink func(Event)

// Emit calls the wrapped function.
func (f FuncSink) Emit(e Event) {
	if f != nil {
		f(e)
	}
}

// Discard is a Sink that drops every event. Useful in tests and for runs that
// only care about the final session state.
var Discard Sink = FuncSink(func(Event) {})
