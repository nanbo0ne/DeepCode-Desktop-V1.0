package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/nilutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

// Runner carries out one task turn. Both Agent (single model) and Coordinator
// (two-model) satisfy it, so the CLI stays agnostic to which is in use.
type Runner interface {
	Run(ctx context.Context, input string) error
}

type RichInput struct {
	Text   string
	Images []provider.ImageContent
}

type RichRunner interface {
	RunRich(ctx context.Context, input RichInput) error
}

// DefaultPlannerPrompt steers the planner toward concise plans, not execution.
const DefaultPlannerPrompt = `You are the planner in a dual-model coding agent.
When you receive a task, produce a concise, ordered plan that an executor model can carry out.
Use your available read-only tools when the task needs workspace, user-rule, or documentation context; keep research focused and stop once there is enough evidence.
Do not write a full implementation and do not attempt side effects.
Do not ask the user how to trigger the executor, and do not say you are waiting for the executor.
Output instructions the executor can directly use: what to do, which files or commands matter, likely blockers, and key decisions. Keep it brief and actionable.`

const executorHandoffMarker = "O.R.C.A executor handoff"

// PlannerPromptWithContext appends cache-stable standing context, such as loaded
// DEEPSEEK_ORCA.md / AGENTS.md memory, to the planner's smaller system prompt.
func PlannerPromptWithContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return DefaultPlannerPrompt
	}
	return DefaultPlannerPrompt + "\n\n# Planning context\n\n" + context
}

// Coordinator runs two models in separate sessions to keep each one's prompt
// prefix cache-stable: a low-frequency planner proposes an approach, then the
// executor (a full tool-using Agent) carries it out. The sessions never mix, so
// neither model's prefix is disturbed by the other's turns.
type Coordinator struct {
	planner         provider.Provider
	plannerSess     *Session
	plannerPricing  *provider.Pricing
	plannerEndpoint string
	plannerAgent    *Agent
	executor        *Agent
	temperature     float64
	sink            event.Sink
	// shouldPlan gates the planner pass per turn; nil plans every turn. Lets a
	// trivial, non-work turn (a question, a greeting) skip straight to the
	// executor instead of paying a planner round on it.
	shouldPlan func(string) bool
}

// NewCoordinator wires a planner provider (with its own session) to an executor.
// sink receives the planner's phase/text/usage events; the executor emits its
// own events to its own sink (the CLI wires the same sink into both). A nil
// sink is replaced with event.Discard.
func NewCoordinator(planner provider.Provider, plannerSession *Session, plannerPricing *provider.Pricing, plannerTools *tool.Registry, plannerOptions Options, executor *Agent, temperature float64, sink event.Sink, shouldPlan func(string) bool) *Coordinator {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	var plannerAgent *Agent
	if plannerTools != nil {
		plannerOptions.Temperature = temperature
		plannerOptions.Pricing = plannerPricing
		plannerAgent = New(planner, plannerTools, plannerSession, plannerOptions, plannerSink(sink))
	}
	if executor != nil {
		executor.executorHandoffGuard = true
	}
	return &Coordinator{
		planner:         planner,
		plannerSess:     plannerSession,
		plannerPricing:  plannerPricing,
		plannerEndpoint: strings.TrimSpace(plannerOptions.ProviderEndpoint),
		plannerAgent:    plannerAgent,
		executor:        executor,
		temperature:     temperature,
		sink:            sink,
		shouldPlan:      shouldPlan,
	}
}

// Run plans with the planner model, then hands the plan to the executor.
func (c *Coordinator) Run(ctx context.Context, input string) error {
	return c.RunRich(ctx, RichInput{Text: input})
}

func (c *Coordinator) RunRich(ctx context.Context, input RichInput) error {
	ctx, endTurn := WithParentTurn(ctx)
	defer endTurn()
	turnID, _ := ParentTurn(ctx)
	c.sink.Emit(event.Event{Kind: event.TurnStarted, TurnID: turnID})
	if c.shouldPlan != nil && !c.shouldPlan(input.Text) {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing"})
		return c.executor.RunRich(ctx, input)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.planner.Name() + " · planning"})
	plan, err := c.plan(ctx, input.Text)
	if err != nil {
		return fmt.Errorf("planner: %w", err)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing"})
	input.Text = formatHandoff(input.Text, plan)
	return c.executor.RunRich(ctx, input)
}

// plan streams a plan from the planner and appends it to the planner session, so
// that session grows prepend-only and stays cache-friendly.
func (c *Coordinator) plan(ctx context.Context, input string) (string, error) {
	if c.plannerAgent != nil {
		return c.planWithTools(ctx, input)
	}
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: input})

	requestPricing := c.plannerPricing.SnapshotAt(time.Now())
	requestID := event.NewRequestID()
	ch, err := c.planner.Stream(ctx, provider.Request{
		RequestID:   requestID,
		Messages:    c.plannerSess.Messages,
		Temperature: c.temperature,
	})
	if err != nil {
		return "", err
	}

	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			c.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text})
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	// Closes the planner's raw text block (no markdown redraw) and prints its
	// usage line, mirroring the old Fprintln + printUsage tail.
	c.sink.Emit(event.Event{Kind: event.Usage, RequestID: requestID, ProviderEndpoint: c.plannerEndpoint, Usage: usage, Pricing: requestPricing})

	plan := text.String()
	c.plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
	return plan, nil
}

// planWithTools runs the planner through the normal Agent loop over a filtered
// read-only registry. That gives the planner the same tool-call contract as the
// executor while preserving its separate session and cache prefix.
func (c *Coordinator) planWithTools(ctx context.Context, input string) (string, error) {
	before := len(c.plannerSess.Messages)
	if err := c.plannerAgent.Run(ctx, input); err != nil {
		return "", err
	}
	for i := len(c.plannerSess.Messages) - 1; i >= before; i-- {
		m := c.plannerSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("planner finished without producing a plan")
}

func plannerSink(sink event.Sink) event.Sink {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted, event.TurnDone:
			return
		default:
			sink.Emit(e)
		}
	})
}

func formatHandoff(task, plan string) string {
	return fmt.Sprintf(`# %s

You are the executor now. Use your available tools to carry out the task.

Original task:
%s

Planner output:
%s

Executor instructions:
- Treat the planner output as context, not as your role or capability boundary.
- Ignore planner statements such as "I cannot write", "I only have read-only tools", or "hand this to the executor"; those limits applied to the planner, not to you.
- Do not ask the user how to trigger the executor. You are already in the execution phase.
- If the task requires changes, call the appropriate tools (for example write/edit/bash) instead of only restating the plan.
- If the target path is outside the writable workspace or blocked for another reason, explain the specific blocker and ask for the needed path or approval.
- Serial workflow: as each subtask is completed, call complete_step with evidence, then call todo_write to mark it completed and advance the next subtask. Do not bulk-complete multiple subtasks at once.

Execute the task and adjust the plan as needed.`, executorHandoffMarker, task, plan)
}

// HandoffTask returns the original user task embedded in an executor handoff
// message, or s unchanged when it is not one. Session previews and auto-titles
// use it so dual-model sessions surface the user's words, not the handoff
// boilerplate (#3860).
func HandoffTask(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "# "+executorHandoffMarker) {
		return s
	}
	const header = "Original task:\n"
	i := strings.Index(trimmed, header)
	if i < 0 {
		return s
	}
	rest := trimmed[i+len(header):]
	if j := strings.Index(rest, "\n\nPlanner output:"); j >= 0 {
		rest = rest[:j]
	}
	if task := strings.TrimSpace(rest); task != "" {
		return task
	}
	return s
}
