package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/jobs"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

// DefaultTaskSystemPrompt steers a sub-agent toward focused, terse delivery —
// it doesn't see the parent's conversation so it must self-contain.
const DefaultTaskSystemPrompt = `你是由父级编程 Agent 调用的 subagent，只负责完成一个聚焦任务。
术语规则：subagent 是专有名词，任何语言中都必须保留为 subagent，不要翻译成中文称呼。
使用提供的工具进行调查或执行操作。最终只返回一段简洁且自包含的答案；
父级 Agent 只能看到这段答案，看不到你的工具调用或推理过程。
如果必须澄清问题，请以一个精确问题失败返回，不要猜测。`

var subagentMetaTools = []string{
	"task",
	"run_skill",
	"read_skill",
	"install_skill",
	"install_source",
	"explore",
	"research",
	"review",
	"security_review",
}

// SubagentMetaTools returns the tool names that spawned agents should not inherit
// from the parent registry unless a future call site deliberately opts into a
// different boundary. They can spawn or author more agent work, so excluding them
// preserves one layer of delegation without adding a spawn-count cap.
func SubagentMetaTools() []string {
	out := make([]string, len(subagentMetaTools))
	copy(out, subagentMetaTools)
	return out
}

// TaskTool spawns a sub-agent in its own session for a focused sub-task. The
// sub-agent runs with a filtered tool whitelist and the same step budget shape
// as the parent (see Execute); its tool calls are forwarded to the parent's
// event stream nested under this call, while only its final assistant message is
// returned to the parent model. Use cases: keep noisy tool sequences (multi-file
// exploration, repeated grep / read_file) out of the parent's context budget, or
// parallel research across independent areas (the parallel-dispatch path picks
// these up only when readOnly, which task is not).
type TaskTool struct {
	prov              provider.Provider
	pricing           *provider.Pricing
	parentReg         *tool.Registry
	maxSteps          int
	contextWindow     int
	softCompactRatio  float64
	compactRatio      float64
	compactForceRatio float64
	temperature       float64
	archiveDir        string
	sysPrompt         string
	gate              Gate
	subagentModel     string
	subagentEffort    string
	resolveProvider   func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	providerEndpoint  string
	resolveEndpoint   func(modelRef, effort string) string
	transcripts       *SubagentStore
	workspaceRoot     string
	baseModel         string
	baseEffort        string
	identityProfile   func(modelRef, effort string) (string, string)
	visionModel       string
	visionFallback    string
	visionMode        string
	visionCapability  func(modelRef string) string
	imageLoader       func(context.Context, provider.ImageContent) (provider.ImageContent, error)
}

// NewTaskTool wires a task tool to the parent agent's environment so its
// sub-agents can use the same provider and tools. sysPrompt is the system
// prompt every sub-agent starts with; pass "" for DefaultTaskSystemPrompt. gate
// is the permission gate sub-agents inherit — pass the headless variant so
// deny rules still bite while autonomous sub-agents are never blocked on an
// interactive prompt (there is no UI to answer one).
func NewTaskTool(prov provider.Provider, pricing *provider.Pricing, parentReg *tool.Registry,
	maxSteps, contextWindow int, softCompactRatio, compactRatio, compactForceRatio, temperature float64, archiveDir, sysPrompt string, gate Gate,
	subagentModel, subagentEffort string, resolveProvider func(string, string) (provider.Provider, *provider.Pricing, int, error)) *TaskTool {
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	return &TaskTool{
		prov:              prov,
		pricing:           pricing,
		parentReg:         parentReg,
		maxSteps:          maxSteps,
		contextWindow:     contextWindow,
		softCompactRatio:  softCompactRatio,
		compactRatio:      compactRatio,
		compactForceRatio: compactForceRatio,
		temperature:       temperature,
		archiveDir:        archiveDir,
		sysPrompt:         sysPrompt,
		gate:              gate,
		subagentModel:     subagentModel,
		subagentEffort:    subagentEffort,
		resolveProvider:   resolveProvider,
	}
}

// WithTranscripts enables persisted sub-agent transcript continuation for this
// task tool. The base model/effort are the parent provider identity used when no
// subagent override is configured.
func (t *TaskTool) WithTranscripts(store *SubagentStore, workspaceRoot, baseModel, baseEffort string) *TaskTool {
	t.transcripts = store
	t.workspaceRoot = strings.TrimSpace(workspaceRoot)
	t.baseModel = strings.TrimSpace(baseModel)
	t.baseEffort = strings.TrimSpace(baseEffort)
	return t
}

func (t *TaskTool) WithTranscriptIdentityResolver(resolve func(modelRef, effort string) (string, string)) *TaskTool {
	t.identityProfile = resolve
	return t
}

// WithProviderEndpoint records the concrete endpoint used by the default
// subagent provider for provenance-aware cost attribution.
func (t *TaskTool) WithProviderEndpoint(endpoint string) *TaskTool {
	t.providerEndpoint = strings.TrimSpace(endpoint)
	return t
}

// WithProviderEndpointResolver supplies the concrete endpoint for an explicit
// subagent model override without changing the provider resolver contract.
func (t *TaskTool) WithProviderEndpointResolver(resolve func(modelRef, effort string) string) *TaskTool {
	t.resolveEndpoint = resolve
	return t
}

// WithVisionDefault configures the model reference used for image-bearing task
// calls that do not provide an explicit model. It never changes the ordinary
// subagent default and is consulted only when the task includes images.
func (t *TaskTool) WithVisionDefault(modelRef string) *TaskTool {
	t.visionModel = strings.TrimSpace(modelRef)
	return t
}

// WithVisionFallback accepts only a host-verified official fallback, distinct
// from the explicit vision-role override passed to WithVisionDefault.
func (t *TaskTool) WithVisionFallback(modelRef string) *TaskTool {
	t.visionFallback = strings.TrimSpace(modelRef)
	return t
}

func (t *TaskTool) WithVision(mode string, capability func(string) string, loader func(context.Context, provider.ImageContent) (provider.ImageContent, error)) *TaskTool {
	t.visionMode, t.visionCapability, t.imageLoader = strings.ToLower(strings.TrimSpace(mode)), capability, loader
	return t
}

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Spawn a sub-agent for a focused sub-task. The sub-agent runs in its own session with the same provider and a filtered tool list (defaults to every parent tool except subagent/skill meta-tools, so delegation stays one layer deep). Only its final answer is returned. Use this to (a) keep long exploration sequences out of the parent's context budget, or (b) delegate self-contained work like 'find every place that calls X and summarise the patterns'. If the current user-facing answer depends on the sub-agent's research, run it in the foreground or call wait before giving the final answer; do not start a background task and then treat the turn as complete."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the sub-agent should accomplish. Be specific about the deliverable — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label for the sub-task (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist. Subagent/skill meta-tools are still excluded so delegation stays one layer deep."},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds. Defaults to half the parent's cap (min 5).","minimum":1},
  "run_in_background":{"type":"boolean","description":"Run the sub-agent asynchronously: returns a job id immediately and keeps working across turns. Use only when the user explicitly wants background work or when the sub-task is independent of the current answer. If the current answer needs this research, do not final-answer after starting it; call wait first to collect the result. You'll be notified when it finishes."},
  "model":{"type":"string","description":"Optional model override for the sub-agent (a configured provider/model name)."},
  "effort":{"type":"string","description":"Optional reasoning effort for the sub-agent (e.g. high, max)."},
  "images":{"type":"array","maxItems":8,"items":{"type":"string"},"description":"Optional current-user image names/snapshot paths or workspace image paths (including generated frames). Workspace images require host read and selected-model image-send approval. Only request these when visual inspection is necessary and the selected sub-agent model supports vision."},
  "continue_from":{"type":"string","description":"Optional subagent transcript reference to continue in place. The current kind, prompt persona, tools, model, effort, and workspace must match the saved transcript."},
  "fork_from":{"type":"string","description":"Optional subagent transcript reference to copy into a new transcript before running this task. Mutually exclusive with continue_from."}
},
"required":["prompt"]
}`)
}

// ReadOnly is false: a sub-agent can invoke any whitelisted tool, including
// writers. Conservative classification keeps the parallel-dispatch path from
// running two sub-agents at once and letting their writes race.
func (t *TaskTool) ReadOnly() bool { return false }

// ResolveProfile extracts model/effort from task args and applies config defaults.
func (t *TaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	var p struct {
		Model  string   `json:"model"`
		Effort string   `json:"effort"`
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	if len(p.Images) > 0 && strings.TrimSpace(p.Model) == "" &&
		strings.ToLower(strings.TrimSpace(t.visionMode)) != "off" &&
		t.defaultImageModel() == "" {
		return nil
	}
	model, effort := t.effectiveProfileForImages(p.Model, p.Effort, len(p.Images) > 0)
	if model == "" && effort == "" {
		return nil
	}
	return &event.Profile{Model: model, Effort: effort}
}

func (t *TaskTool) effectiveProfile(model, effort string) (string, string) {
	return t.effectiveProfileForImages(model, effort, false)
}

func (t *TaskTool) effectiveProfileForImages(model, effort string, hasImages bool) (string, string) {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	if model == "" && hasImages && t.visionMode != "off" {
		model = t.defaultImageModel()
	}
	if model == "" {
		model = strings.TrimSpace(t.subagentModel)
	}
	if effort == "" {
		effort = strings.TrimSpace(t.subagentEffort)
	}
	return model, effort
}

func (t *TaskTool) defaultImageModel() string {
	if t.visionModel != "" {
		return t.visionModel
	}
	if t.baseModel != "" && t.visionCapability != nil && t.visionCapability(t.baseModel) == "supported" {
		return t.baseModel
	}
	return t.visionFallback
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt          string   `json:"prompt"`
		Description     string   `json:"description"`
		Tools           []string `json:"tools"`
		MaxSteps        int      `json:"max_steps"`
		RunInBackground bool     `json:"run_in_background"`
		Model           string   `json:"model"`
		Effort          string   `json:"effort"`
		Images          []string `json:"images"`
		ContinueFrom    string   `json:"continue_from"`
		ForkFrom        string   `json:"fork_from"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if len(p.Images) > 0 && strings.TrimSpace(p.Model) == "" &&
		strings.ToLower(strings.TrimSpace(t.visionMode)) != "off" &&
		t.defaultImageModel() == "" {
		return "", fmt.Errorf("image task requires a configured vision model; set agent.subagent_models[%q] or provide an explicit model", "vision")
	}

	maxSteps := p.MaxSteps
	if maxSteps <= 0 {
		// No explicit cap from the caller: mirror the parent. A finite parent caps
		// the sub-agent at half its budget (min 5) so a delegated sub-task stays
		// shorter than the whole turn; an unbounded parent yields an unbounded
		// sub-agent. The sub-agent shares the parent's ctx, so cancelling the turn
		// stops it, and it compacts its own context — the same bounds the parent has.
		if t.maxSteps > 0 {
			maxSteps = t.maxSteps / 2
			if maxSteps < 5 {
				maxSteps = 5
			}
		}
	}

	subReg := t.buildSubReg(p.Tools)
	modelRef, effortRef := t.effectiveProfileForImages(p.Model, p.Effort, len(p.Images) > 0)
	selectedImages, err := t.selectImages(ctx, p.Images, modelRef)
	if err != nil {
		return "", err
	}
	parentID, parent, _, _ := CallContext(ctx)
	parentTurnID, _ := ParentTurn(ctx)
	run, err := t.prepareTranscriptRun(subReg, modelRef, effortRef, ParentSession(ctx), parentID, p.ContinueFrom, p.ForkFrom)
	if err != nil {
		return "", err
	}
	prov, pricing, ctxWin, err := t.resolveSubSessionRuntime(modelRef, effortRef)
	if err != nil {
		run.Release()
		return "", fmt.Errorf("sub-agent profile: %w", err)
	}

	// Background: register a job that runs the sub-agent under the manager's
	// session context (so it survives this turn) and return immediately. The
	// sub-agent's tool activity still streams, nested under this call, because the
	// nested sink captures the parent ID + stream now (not from the job ctx).
	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			if run != nil {
				run.Release()
			}
			return "", fmt.Errorf("background execution is not available in this context")
		}
		nested := subSinkFor(parentID, parent, parentTurnID)
		childID := event.NewRequestID()
		var childUsageReported atomic.Bool
		if parent != nil && strings.TrimSpace(parentTurnID) != "" {
			parent.Emit(event.Event{Kind: event.ChildStarted, TurnID: parentTurnID, ParentTurnID: parentTurnID, ChildID: childID})
		}
		childSink := event.FuncSink(func(e event.Event) {
			if e.Kind == event.Usage {
				childUsageReported.Store(true)
				e.ChildID = childID
			}
			nested.Emit(e)
		})
		label := p.Description
		if label == "" {
			label = "task"
		}
		if t.transcripts != nil && run != nil && run.Ref != "" {
			if err := t.transcripts.MarkRunning(run); err != nil {
				run.Release()
				return "", err
			}
		}
		job := jm.Start("task", label, func(jobCtx context.Context, _ io.Writer) (string, error) {
			defer func() {
				if parent != nil && strings.TrimSpace(parentTurnID) != "" {
					parent.Emit(event.Event{Kind: event.ChildDone, TurnID: parentTurnID, ParentTurnID: parentTurnID, ChildID: childID, ChildUsageReported: childUsageReported.Load()})
				}
			}()
			defer run.Release()
			answer, err := t.runSubSession(jobCtx, p.Prompt, selectedImages, subReg, childSink, maxSteps, prov, pricing, t.subagentEndpoint(modelRef, effortRef), ctxWin, run.Session)
			if err != nil {
				return FormatSubagentResult("", run.Ref, true), errors.Join(err, t.transcripts.SaveFailed(run))
			}
			if err := t.transcripts.SaveCompleted(run); err != nil {
				return FormatSubagentResult("", run.Ref, true), errors.Join(err, t.transcripts.SaveFailed(run))
			}
			return FormatSubagentResult(answer, run.Ref, false), nil
		})
		if run != nil && run.Ref != "" {
			return fmt.Sprintf("Started background task %q (%s).\nSubagent reference: %s\nIt runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes. If this task is needed for the current user request, call wait before giving the final answer.", job.ID, label, run.Ref), nil
		}
		return fmt.Sprintf("Started background task %q (%s). It runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes. If this task is needed for the current user request, call wait before giving the final answer.", job.ID, label), nil
	}

	// Foreground: run synchronously, nesting events under this call.
	defer run.Release()
	answer, err := t.runSubSession(ctx, p.Prompt, selectedImages, subReg, subSink(ctx), maxSteps, prov, pricing, t.subagentEndpoint(modelRef, effortRef), ctxWin, run.Session)
	if err != nil {
		return "", errors.Join(err, t.transcripts.SaveFailed(run))
	}
	if t.transcripts != nil && run.Ref != "" {
		if err := t.transcripts.SaveCompleted(run); err != nil {
			return "", errors.Join(err, t.transcripts.SaveFailed(run))
		}
		return FormatSubagentResult(answer, run.Ref, false), nil
	}
	return answer, nil
}

func (t *TaskTool) subagentEndpoint(modelRef, effort string) string {
	if t.resolveEndpoint != nil {
		return strings.TrimSpace(t.resolveEndpoint(modelRef, effort))
	}
	return strings.TrimSpace(t.providerEndpoint)
}

func (t *TaskTool) prepareTranscriptRun(subReg *tool.Registry, modelRef, effortRef, parentSession, parentID, continueFrom, forkFrom string) (*SubagentRun, error) {
	continueFrom = strings.TrimSpace(continueFrom)
	forkFrom = strings.TrimSpace(forkFrom)
	parentSession = strings.TrimSpace(parentSession)
	if continueFrom != "" && forkFrom != "" {
		return nil, fmt.Errorf("continue_from and fork_from are mutually exclusive")
	}
	if t.transcripts == nil {
		return nil, fmt.Errorf("subagent transcript store is required")
	}
	// Headless runs (e.g. `orca run`) never mint a session path, so there is
	// no parent session to own a transcript. Run the sub-agent ephemerally —
	// exactly as before persisted transcripts existed — instead of failing the
	// call. Continuation/fork need a persisted owner, so they error here.
	if parentSession == "" {
		if continueFrom != "" || forkFrom != "" {
			return nil, fmt.Errorf("continue_from/fork_from require a persisted session; none is active in this run")
		}
		return EphemeralSubagentRun(t.sysPrompt), nil
	}
	identityModel, identityEffort := t.effectiveIdentity(modelRef, effortRef)
	spec := SubagentSpec{
		Kind:             "task",
		Name:             "task",
		WorkspaceRoot:    t.workspaceRoot,
		ParentSession:    parentSession,
		ParentToolCallID: parentID,
		SystemPrompt:     t.sysPrompt,
		Registry:         subReg,
		Model:            identityModel,
		Effort:           identityEffort,
	}
	if continueFrom != "" || forkFrom != "" {
		if continueFrom != "" {
			return t.transcripts.PrepareContinue(continueFrom, spec)
		}
		return t.transcripts.PrepareFork(forkFrom, spec)
	}
	return t.transcripts.PrepareFresh(spec)
}

func (t *TaskTool) effectiveIdentity(modelRef, effort string) (string, string) {
	if t.identityProfile != nil {
		model, eff := t.identityProfile(modelRef, effort)
		return strings.TrimSpace(model), strings.TrimSpace(eff)
	}
	return t.effectiveModelIdentity(modelRef), t.effectiveEffortIdentity(effort)
}

func (t *TaskTool) effectiveModelIdentity(modelRef string) string {
	if strings.TrimSpace(modelRef) != "" {
		return strings.TrimSpace(modelRef)
	}
	return strings.TrimSpace(t.baseModel)
}

func (t *TaskTool) effectiveEffortIdentity(effort string) string {
	if strings.TrimSpace(effort) != "" {
		return strings.TrimSpace(effort)
	}
	return strings.TrimSpace(t.baseEffort)
}

// buildSubReg returns the sub-agent's tool set: the named whitelist (minus
// subagent/skill meta-tools, to bar recursive nesting), or every parent tool
// except those meta-tools.
func (t *TaskTool) buildSubReg(names []string) *tool.Registry {
	return FilterRegistry(t.parentReg, names, SubagentMetaTools()...)
}

// FilterRegistry builds a sub-registry from parent: the named whitelist (empty =
// every parent tool), minus any excluded names. Used to scope what a spawned
// sub-agent — a `task` sub-agent or a subagent skill — may call, e.g. excluding
// `task` to bar recursive nesting, or restricting to a skill's allowed-tools.
func FilterRegistry(parent *tool.Registry, names []string, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	src := names
	if len(src) == 0 {
		src = parent.Names()
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		if tl, ok := parent.Get(name); ok {
			sub.Add(tl)
		}
	}
	return sub
}

var plannerNonResearchTools = []string{
	"ask",
	"bash_output",
	"complete_step",
	"slash_command",
	"todo_write",
	"wait",
}

// PlannerToolRegistry returns the tool set exposed to the two-model planner:
// read-only research tools only. It deliberately excludes workflow/meta tools
// that are technically read-only but can prompt the user, update visible task
// state, wait on jobs, or expand commands instead of inspecting context.
func PlannerToolRegistry(parent *tool.Registry) *tool.Registry {
	exclude := append(SubagentMetaTools(), plannerNonResearchTools...)
	return FilterReadOnlyRegistry(parent, exclude...)
}

// FilterReadOnlyRegistry builds a sub-registry containing only tools whose
// ReadOnly contract is true, minus explicit exclusions.
func FilterReadOnlyRegistry(parent *tool.Registry, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	for _, name := range parent.Names() {
		if ex[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok || !tl.ReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	return sub
}

func (t *TaskTool) resolveSubSessionRuntime(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
	prov, pricing, ctxWin := t.prov, t.pricing, t.contextWindow
	if t.resolveProvider != nil && (modelRef != "" || effort != "") {
		p, pr, cw, err := t.resolveProvider(modelRef, effort)
		if err != nil {
			return nil, nil, 0, err
		}
		prov, pricing, ctxWin = p, pr, cw
	}
	return prov, pricing, ctxWin, nil
}

func (t *TaskTool) runSubSession(ctx context.Context, prompt string, images []provider.ImageContent, subReg *tool.Registry, sink event.Sink, maxSteps int, prov provider.Provider, pricing *provider.Pricing, endpoint string, ctxWin int, sess *Session) (string, error) {
	return RunSubAgentWithRichSession(ctx, prov, subReg, sess, RichInput{Text: prompt, Images: images}, Options{
		MaxSteps:          maxSteps,
		Temperature:       t.temperature,
		Pricing:           pricing,
		ProviderEndpoint:  endpoint,
		Gate:              t.gate,
		ContextWindow:     ctxWin,
		SoftCompactRatio:  t.softCompactRatio,
		CompactRatio:      t.compactRatio,
		CompactForceRatio: t.compactForceRatio,
		ArchiveDir:        t.archiveDir,
		ImageLoader:       frozenTaskImageLoader(images),
	}, sink)
}

func FormatSubagentResult(answer, ref string, failed bool) string {
	if ref == "" {
		return answer
	}
	if failed {
		if answer == "" {
			return "Subagent reference (failed): " + ref
		}
		return "Subagent reference (failed): " + ref + "\n\nFinal answer:\n" + answer
	}
	return "Subagent reference: " + ref + "\n\nFinal answer:\n" + answer
}

// RunSubAgentWithSession continues an existing sub-agent session with prompt and
// returns the latest final assistant answer. Fresh sub-agents pass a newly-created
// session; continued sub-agents pass a loaded transcript session.
func RunSubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *Session, prompt string, opts Options, sink event.Sink) (string, error) {
	return RunSubAgentWithRichSession(ctx, prov, reg, sess, RichInput{Text: prompt}, opts, sink)
}

func RunSubAgentWithRichSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *Session, input RichInput, opts Options, sink event.Sink) (string, error) {
	if sess == nil {
		return "", fmt.Errorf("sub-agent session is nil")
	}
	sub := New(prov, reg, sess, opts, sink)
	if err := sub.RunRich(ctx, input); err != nil {
		return "", fmt.Errorf("sub-agent: %w", err)
	}
	// Walk the session backwards for the last assistant message with content —
	// that's the sub-agent's final answer. Intermediate assistant messages with
	// tool_calls but no text don't count.
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

func (t *TaskTool) selectImages(ctx context.Context, names []string, modelRef string) ([]provider.ImageContent, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if t.visionMode == "off" {
		return nil, fmt.Errorf("vision is disabled in Settings")
	}
	identity := strings.TrimSpace(modelRef)
	if identity == "" {
		identity = strings.TrimSpace(t.baseModel)
	}
	if t.visionMode == "auto" && (t.visionCapability == nil || t.visionCapability(identity) != "supported") {
		return nil, fmt.Errorf("model %q is not confirmed to support vision; choose a supported model, re-run its vision check, or use vision mode on", identity)
	}
	if len(names) > 8 {
		return nil, fmt.Errorf("a task can include at most 8 images")
	}
	if resolve, ok := ctx.Value(taskImageResolverKey{}).(TaskImageResolver); ok && resolve != nil {
		identity, _ = t.effectiveIdentity(modelRef, "")
		return resolve(ctx, names, identity)
	}
	available := TurnImages(ctx)
	selected := make([]provider.ImageContent, 0, len(names))
	var totalBytes int64
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("image name must not be empty")
		}
		var found *provider.ImageContent
		for i := range available {
			if name == available[i].Path || name == available[i].Name {
				found = &available[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("image %q is not one of the current user's validated attachments", name)
		}
		if seen[found.Path] {
			continue
		}
		seen[found.Path] = true
		image := *found
		if err := t.checkImagePermission(ctx, "read_file", image.Path); err != nil {
			return nil, err
		}
		if t.imageLoader == nil {
			return nil, fmt.Errorf("image loading is unavailable for subagents")
		}
		hydrated, err := t.imageLoader(ctx, image)
		if err != nil {
			return nil, fmt.Errorf("image %q: %w", name, err)
		}
		// The legacy loader is host-supplied, but still cannot exceed the task's
		// byte budget even when metadata understates the encoded payload size.
		encodedSize := int64(len(hydrated.Data))*3/4 - int64(len(hydrated.Data)-len(strings.TrimRight(hydrated.Data, "=")))
		bytes := max(hydrated.Size, encodedSize)
		totalBytes += bytes
		if bytes > 10*1024*1024 || totalBytes > 20*1024*1024 {
			return nil, fmt.Errorf("task images exceed the 10 MB image or 20 MB total budget")
		}
		selected = append(selected, hydrated)
	}
	identity, _ = t.effectiveIdentity(modelRef, "")
	if err := t.checkImagePermission(ctx, "image_send", identity); err != nil {
		return nil, err
	}
	return selected, nil
}

// NestedSink returns a sink that forwards a sub-agent's tool activity to the
// parent stream, nested under the tool call carried by ctx, so a frontend shows
// it beneath that call (the same nesting `task` uses). Falls back to the given
// sink when ctx carries no call context. Used by subagent skills.
func NestedSink(ctx context.Context, fallback event.Sink) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return fallback
	}
	parentTurnID, _ := ParentTurn(ctx)
	return subSinkFor(parentID, parent, parentTurnID)
}

// subSink forwards a sub-agent's tool dispatch/result events to the parent's
// event stream, tagged with the parent task call's ID so a frontend nests them
// under it. The sub-agent's own turn/text/reasoning events are dropped;
// usage receipts are forwarded for parent-turn accounting, while tool activity
// (the part worth seeing live) and the final answer (returned by Execute) reach
// the parent. The forwarded call IDs are namespaced
// with the parent ID so a sub-agent call can never collide with a parent call in
// the frontend's dispatch→result matching. Falls back to Discard when there's no
// parent stream (the headless run loop, or a direct Execute in tests).
func subSink(ctx context.Context) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return event.Discard
	}
	parentTurnID, _ := ParentTurn(ctx)
	return subSinkFor(parentID, parent, parentTurnID)
}

// subSinkFor builds the nesting sink from an already-captured parent ID + stream,
// for the background path where the job runs under a context that no longer
// carries the call context. Falls back to Discard when there's no parent stream.
func subSinkFor(parentID string, parent event.Sink, parentTurnID ...string) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult:
			e.Tool.ParentID = parentID
			e.Tool.ID = parentID + "/" + e.Tool.ID
			parent.Emit(e)
		case event.Usage:
			if len(parentTurnID) > 0 && strings.TrimSpace(parentTurnID[0]) != "" {
				e.ParentTurnID = strings.TrimSpace(parentTurnID[0])
			}
			parent.Emit(e)
		}
	})
}
