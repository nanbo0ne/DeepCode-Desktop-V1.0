# O.R.C.A Desktop Changelog

## V3.0.2 - 2026-09-07

- Fixed Modern header/footer spacing, narrow Composer controls, modal focus, and settings error recovery. Kept the Classic blue-and-white layout and corrected its overlapping search control.
- Preserved drafts and attachments when sending fails. Isolated workspace credentials, attachment roots, and delegated approval responses between conversations.
- Hardened interrupted migrations, named verification checks, provider stream completion, download pause/resume, GPU detection, and local runtime credentials.
- Added cancellation and stale-observation protection to Computer Use, plus independent completion verification and stricter Windows input boundaries.
- Reduced unnecessary tool use for ordinary questions and simplified settings, placeholders, and usage displays.
- Fixed repeated command-palette opening, stale cross-tab context results, inaccessible Send labels, and truncated risk-review requests without changing ordinary chat reasoning.
- Fixed PDF pagination, numeric spreadsheet cells, formula cache handling and bounded slide text. PDF previews render actual pages through optional Poppler; unavailable Office previews are reported honestly. Legacy sidecars remain readable without being overwritten.
- Hardened installer upgrade detection, scoped process closure and uninstall cleanup. Pinned Node and CodeGraph downloads, retained the full portable payload after signing, and added release test/tag checks.
- Added real provider-backed conversation, vision, file-generation and editing scenarios alongside fault-injection and rendered-layout regression tests. Native VM installation, full local-model downloads and multi-monitor desktop acceptance remain separate checks.

## V3.0.1 - 2026-08-25

- Rebuilt the Modern desktop layout around a compact four-menu shell, a responsive single-row Composer, stable send/stop geometry, right-aligned model and reasoning controls, full-width scrolling, and correctly centered empty-session content.
- Restored Classic as an explicit V2.1.3 blue-and-white DOM and CSS branch with native Windows decoration and the original toolbar, sidebar, Composer, status, spacing, and button treatment while retaining all V3 services.
- Added persistent Modern/Classic style selection with restart-safe window framing, removed the retired application-level zoom control, and kept Windows Per-Monitor DPI behavior at a native 100% application scale.
- Hardened the Local AI bridge against `null` collections and failed refreshes, normalized onboarding and settings data, preserved the last valid catalog, and covered multi-adapter systems where the preferred discrete GPU is not `GPU 0`.
- Rebuilt the transparent O.R.C.A. icon family for application, taskbar, tray, notification, shortcut, and installer use, including dedicated small-size ICO entries and retained wordmark artwork.
- Added model-level context and capability metadata, hid unavailable cost and balance fields, isolated provider model refreshes, and updated official DeepSeek weekday peak/off-peak pricing plus Vision Exp routing.
- Added independent model-based risk review for automatic approvals while preserving host deny/ask rules, secret redaction, strict structured output, and warning telemetry on the existing fallback path.
- Refined final readiness so successful work stops on the verified round instead of repeating three times, while explicit tool failures, incomplete tasks, empty final answers, and security boundaries remain hard blockers.
- Rewrote the Chinese, English, desktop, and CLI documentation as a balanced product overview covering conversations, providers, tools, projects, local AI, Computer Use, automation, privacy, migration, platforms, and source builds.

## V3.0.0 - 2026-08-17

- Renamed the product to O.R.C.A for Windows, expanded as Open Reasoning & Computing Agent, with new application identity, repository links, executable/package names, icons, media route, configuration path, and migration-compatible legacy aliases.
- Replaced DeepSeek-specific first launch with provider-neutral onboarding and a backend-owned provider preset catalog covering official, aggregate, domestic, subscription-plan, and custom compatible endpoints with isolated credentials and complete model references.
- Added optional Windows local AI management for a pinned llama.cpp runtime, hardware-aware loading, resumable and checksummed model downloads, Qwen recommendations, model library lifecycle, and local role-model selection.
- Added Windows Computer Use with isolated control-agent context, screenshot/UI Automation observations, guarded pointer and keyboard actions, per-action re-observation, persistent one-time authorization, telemetry without screenshots, visual control overlays, and global Escape cancellation.
- Migrated configuration to V11 and canonical `orca.toml`, `.orca`, `ORCA.md`, `ORCA_*`, and O.R.C.A user-state paths while preserving V2 sessions, attachments, providers, credentials, memory, telemetry, and tracked legacy project files.
- Kept local inference and Computer Use explicitly unavailable on macOS/Linux in V3 while retaining the full conversation, provider, artifact, and engineering experience on those platforms.

## V2.1.3 - 2026-08-15

- Updated official DeepSeek V4 Flash and V4 Pro cost estimates to the new Beijing-time peak and off-peak rates immediately, without waiting for the announced effective date.
- Froze each request's rate when the Provider call starts, so long responses crossing a tariff boundary retain one consistent price across desktop, CLI, service events, and persisted telemetry.
- Preserved static pricing for custom gateways and other providers, retained cache-miss derivation when only cached tokens are reported, and kept historical per-request costs stable after restart.
- Hardened local Windows packaging by waiting for a stable NSIS source hash, copying through a temporary file, comparing source and destination hashes, and running an offline archive integrity test when 7-Zip is available.

## V2.1.2 - 2026-08-11

- Added standalone, persistent mode-switch timeline records with explicit switching, completed, failed, and interrupted states. Blank conversations stay visually clean, while unfinished records recover as interrupted without entering provider history or completed-turn folds.
- Added generation-aware backend runtime-switch progress at preparing, building, restoring, swapping, and completed stages, shown in a compact Composer progress bar without reusing model-thinking feedback.
- Reworked the activity indicator as one regenerating gradient ring: the current direction fades out and is removed before the opposite model/tool direction fades in, including rapid phase coalescing and reduced-motion handling.
- Kept the mode selector icon static during controller rebuilds and aligned Pause, Resume, and Stop on the plain Lucide Pause, Play, and Square icon language.
- Preserved the V10 provider configuration and existing Turn/Item protocol while extending telemetry to version 5 for runtime-switch history.

## V2.1.1 - 2026-08-10

- Separated Mimo API and Mimo Token Plan into distinct provider identities, endpoints, model references, and credential slots. Exact Mimo references can no longer cross-resolve to the other source, and the V10 migration assigns a legacy shared key conservatively from the active model source with a backup before changes.
- Serialized controller replacement per conversation and added generation-based stale-build rejection across model, effort, mode, settings, startup, and Orca model changes. The old controller remains available until a replacement has built and restored history successfully.
- Made Assistant the first public mode and restored Coding's blue treatment with a stable, responsive selector. Mode changes update the interface immediately, keep queued prompts on failure, and send them only after a successful switch.
- Replaced running Pause, Resume, Stop, and queued Send labels with fixed-size icon controls, preserved Escape cancellation, and allowed Stop during approvals, questions, pauses, provider streaming, and foreground tool waits.
- Added acknowledged per-tab cancellation and explicit stopping feedback so cancellation targets the conversation captured at click time and reconciles cleanly when the turn has already ended.

## V2.1.0 - 2026-08-10

- Replaced the legacy Normal/Enhanced split with public Coding and Assistant profiles. Coding retains the reliable V2.0.38 engineering contract and gains the useful recovery and permission guidance from Enhanced; Assistant provides a separate Work-oriented tool and memory surface.
- Added atomic, cache-aware mode switching. Existing visible history is preserved, old system messages are removed, running turns defer a confirmed switch, and failed controller builds leave the original mode untouched.
- Promoted Orca to a fixed top-level control conversation ahead of projects, removed the Automation Workspace presentation and ordinary mode selector, and kept cross-conversation dispatch tools exclusive to Orca.
- Isolated runtime tool registries and memory profiles: Coding owns shell, LSP, CodeGraph, review, and engineering memory; Assistant and Orca share the canonical personal profile and Work tools; only Orca can route or dispatch other conversations.
- Added a bundled, cross-platform artifact runtime and `artifact_create`, `artifact_edit`, `artifact_preview`, and `artifact_validate` tools for structured DOCX, XLSX, PPTX, and PDF output without requiring Python, Office, or LibreOffice.
- Hardened prompt trust boundaries by escaping user-authored host-context markers and generating automation, workflow, and memory reminders as host-owned structured context.
- Migrated configuration to V9, mapping legacy Normal/Enhanced conversations to Coding, preserving ordinary Assistant conversations, reserving Orca for the canonical control topic, and defaulting the first new ordinary conversation after upgrade to Assistant.

## V2.0.38 - 2026-08-10

- Rebuilt Normal and Enhanced conversation rendering around stable Turn, Item, and Message identities. A turn can report success only after an explicit visible final answer has passed readiness and been committed.
- Preserved every non-empty assistant message exactly once: progress updates remain in the process timeline, while the committed final answer stays outside the completed-turn fold across live streaming, history restoration, tab switching, and cold pagination.
- Added explicit failed, cancelled, and interrupted outcomes, prevented backend disconnects and empty responses from masquerading as completed work, and persisted canonical turn boundaries and final-message identity in telemetry.
- Replaced the manually resized Composer with a one-to-ten-line elastic input that grows and contracts with content, wrapping, paste, undo, scale, font, and window changes.
- Restored the stable Codex-style Todo trigger with a single upward anchored detail surface, hover bridging, pinning, keyboard and touch behavior, and automatic completion collapse without shifting the footer.
- Enabled the direction-aware activity spinner by default through the V8 configuration migration while continuing to respect an explicit user opt-out.
- Strengthened Normal-mode completion instructions for concise stage updates, sustained execution, post-write verification, visible blockers, and a separate final response after tool work.

## V2.0.37 - 2026-08-09

- Removed the Automation Workspace history subgroup and migrated its existing conversations into ordinary independent workspaces while preserving transcripts, titles, display text, and source backups.
- Simplified the Orca Composer by hiding its redundant approval selector and moving model controls into the newly available leading space.
- Reworked daily welcome suggestions to learn from sanitized real user messages, imitate the user's phrasing, and reject assistant-style recommendation wording.

## V2.0.36 - 2026-08-09

- Consolidated desktop, Weixin, and QQ automation into one protected Orca main conversation. Thirty-minute continuation decisions and `/new` now create logical segments inside Orca instead of additional sidebar topics, while legacy automation topics remain available as read-only history.
- Added one-time, revocable trusted automation access. Before authorization Orca can chat but protected tools are declined without per-command approval cards; after authorization, allowed tools and plan execution proceed automatically while Ask questions and explicit deny rules remain effective.
- Added a dedicated automation model shared by Orca and remote channels, with full provider/model references, stale Mimo alias migration, bidirectional desktop selection, and idle handover so an active task finishes on its original model.
- Strengthened per-model vision probing with an isolated Orca-icon challenge containing a random code, color, and position marker; automatic and effective capability states are now shown separately and probes run for newly enabled models.
- Added four cached, model-generated home suggestions refreshed every 24 hours from limited local conversation summaries without creating hidden conversations or modifying assistant memory.
- Added the current desktop version to General settings and updated all Chinese and English download documentation for V2.0.36.
- Kept the shared `/start` command inside Orca instead of exposing ordinary engineering sessions, and fixed automation settings so stale channel drafts cannot replace the central automation model, Assistant mode, or canonical workspace.
- Limited `automation_history` to previous logical segments and redacted credentials, attachment wrappers, and local paths before daily suggestion summaries are sent to the model.

## V2.0.35 - 2026-08-08

- Restored completed-turn summaries to the lightweight transparent elapsed-time row, with expanded reasoning and tools rendered directly beneath it rather than inside a rounded outer panel.
- Changed automatic interface scaling to follow Windows PerMonitorV2 DPI at native size on every resolution; manual 80%-125% scaling remains available as a relative override.
- Rebuilt automatic vision probing around an isolated, history-free request containing the Orca icon and a random four-digit verification code, with per-endpoint/model caching and automatic checks for newly configured models.
- Fixed model resolution so enabled full provider/model references remain stable across switching, controller rebuilds, effort changes, restoration, and background refresh, preventing Mimo API from being replaced by the hidden Mimo Token Plan compatibility provider.
- Enabled both `mimo-v2.5` and `mimo-v2.5-pro` by default in the Mimo API template while preserving PRO as the default model and respecting later user selections.

## V2.0.34 - 2026-08-07

- Moved the Todo expansion into the footer's normal layout flow so its height is measured and the transcript always moves clear of the task list instead of being covered by a fixed popover.
- Consolidated Todo, queued prompts, approvals, plan confirmation, questions, and context cleanup into one visual surface per bottom panel with flat internal rows and dividers.
- Aggregated each turn's reasoning, progress text, tools, notices, compaction, and subagent activity into one process panel while keeping the final answer independent.
- Added explicit panel/content surface markers and regression coverage for nested-panel prevention, transparent process rows, stable compact/detailed behavior, and responsive overflow safeguards.

## V2.0.33 - 2026-08-07

- Added automatic interface scaling based on the DPI-adjusted usable display size plus an immediate 80%-125% manual scale control, and rebuilt Composer permission controls so they remain usable at narrow content widths.
- Unified file selection, Explorer clipboard paths, browser paste, native image paste, and drag-and-drop into ordered attachment batches. Added DOCX, XLSX, PPTX, PDF, CSV, TSV, and text extraction without blocking successful files when one item fails.
- Fixed user and project provider configuration merging and protected provider/model saves from stale background refreshes, empty catalog responses, and concurrent settings snapshots.
- Improved vision capability detection with provider metadata, protocol-specific probes, tolerant verification parsing, and per-model automatic/supported/unsupported overrides.
- Separated image thumbnails and file chips from the blue user text bubble, enlarged the completed-turn summary, and replaced nested process cards with a flat chronological activity rail.
- Synchronized Compact and Detailed switching across completed turns and reasoning rows, and fixed the question navigation rail so cold history is paged in the correct direction, mounted before an immediate stable jump, and triggered only once per pointer action.
- Added image support to the custom right-click Paste command and kept native text, file, mixed clipboard, attachment failure, retry, and ordering behavior consistent.
- Stopped the boot subagent test from writing `first review` fixtures into the real Windows profile and added a strict startup cleanup that removes only the exact leaked fixture signature.
- Muted the empty Composer send arrow across themed styles, kept failed-only attachment batches disabled, and made automatic proxy mode bypass loopback addresses so local model endpoints and provider tests are never routed through the Windows system proxy.

## V2.0.31 - 2026-08-02

- Added the Automation Workspace beside project and independent workspaces. Automation conversations use the retained Assistant prompt and canonical assistant profile while ordinary engineering conversations remain Normal or Enhanced.
- Added conversation routing tools for listing, reading, dispatching, waiting, status, cancellation, and ordinary-session creation. Routing stays inside the Assistant model's normal tool loop; only the 30-minute continuity check is a separate low-token request.
- Added QQ and Weixin first-message automation session creation, persisted remote-to-topic restoration, `/new`, `/continue`, `/hi`, and one-time persisted onboarding guidance.
- Added per-session execution leases so desktop and mobile controllers cannot concurrently write the same transcript, and waiting controllers refresh newer history before continuing.
- Preserved legacy Assistant sessions and memory stores while migrating them into a canonical profile without deleting the source data. Fixed automation-root indexing so it is not treated as an ordinary project.
- Updated automation capability tests, routing prompt coverage, broker ownership checks, and desktop/frontend layout regression checks.
- Fixed the running Composer action so typed or attached follow-up content replaces Stop with a same-size blue queued-send button, then restores Stop after the draft is queued.

## V2.0.30 - 2026-08-01

- Reduced process presentation to Compact and Detailed, made Compact the default, and migrated legacy Standard or collapsed-thinking settings to Compact.
- Fixed compact process rows so stable segment IDs preserve explicit open/closed state across streaming updates and closed details leave no residual layout or hit area.
- Added an optional, off-by-default blue spinner below the current turn. It rotates clockwise for model activity and counterclockwise for tool activity, with pause/completion handling and reduced-motion support.
- Removed the full-width footer surface behind Todo, plan, approval, queued prompt, and context cards while retaining normal-flow height and individual card surfaces.
- Split conversation loading into first-paint meta/history and background auxiliary hydration, added immediate switching for already-open topics, cached transcript derivations, and suppressed restored-history entrance replay.
- Changed plain-text paste to remain direct editable text and made the Composer grow automatically for up to ten lines before scrolling.
- Updated the desktop version and download documentation to V2.0.30.

## V2.0.29 - 2026-08-01

- Collapsed every explicitly successful completed turn to the user message, elapsed-time row, and final answer while preserving failed, cancelled, interrupted, and active diagnostics.
- Restored the full chronological process on demand and replaced nested process cards with a flat activity rail for reasoning, tools, notices, and subagents.
- Migrated multimodal vision to Off, Auto, and On modes, with new installations defaulting to per-model automatic capability detection.
- Added isolated visual probes, persisted model/endpoint capability status, bounded retries, manual rechecks, and expected text-only handling for DeepSeek.
- Added current-turn image delegation to confirmed vision-capable subagents without placing image base64 in transcripts, titles, memory, or compaction.
- Ensured Normal mode always includes the complete built-in engineering prompt, appends user instructions, and requires relevant verification after the latest write.
- Updated the desktop version and download documentation to V2.0.29.

## V2.0.28 - 2026-07-31

- Defined the engineering edition boundary: the desktop UI exposes only Normal and Enhanced while retaining Assistant prompt and memory code for the future standalone Orca application.
- Migrated restored Assistant tab and recent/bot preferences to Normal without deleting sessions, attachments, Assistant memories, or pending-memory state.
- Disabled Assistant automatic-memory scheduling and processing in the engineering edition and forced engineering controllers and bot sessions to use shared-agent memory.
- Removed prompt-mode descriptions and the separate help button from the engineering mode menu; compact layouts now show one mode icon with a tooltip.
- Added a capability-driven prompt-mode interface and removed hardcoded Assistant choices from the engineering Composer and bot settings.
- Consolidated the final Composer responsive layout layer across 720, 580, 460, 380, and 320px container widths while preserving model selection and preventing control overlap.
- Rewrote the engineering README documentation and added the Orca assistant application handoff document.

## V2.0.27 - 2026-07-25

- Added fail-closed SignPath Authenticode signing for the Windows application and final NSIS installer, including trusted timestamp verification before release publication.
- Added an off-by-default manual override for the temporary unsigned V2.0.27 Windows release while SignPath Foundation enrollment is pending; signed assets will replace it in place.
- Added a public Windows code-signing policy and verification instructions for official GitHub downloads.
- Centered the compact Todo control and made its width follow the active task text while retaining progress, pinning, hover, keyboard, and narrow-window behavior.
- Reworked automatic topic titles to summarize the complete first user/assistant turn across tool calls instead of stopping at the first assistant fragment.
- Restored original display text before title generation so `Referenced context`, reminders, handoffs, and attachment payloads cannot become conversation titles.
- Added lazy repair for legacy auto-generated wrapper titles without overwriting manually renamed conversations.

## V2.0.26 - 2026-07-24

- Hotfix (2026-07-25): fixed mojibake in the Windows install-options page by compiling the customized NSIS script as BOM-marked UTF-8 Unicode.
- Hotfix (2026-07-25): stopped cumulative session usage from being mistaken for a full 1,000K current context after prompt-mode or controller rebuilds.
- Hotfix (2026-07-25): moved the manual update check beside the automatic update toggle instead of using a separate settings row.
- Replaced the always-open Todo list with a compact progress bar that expands upward on hover or keyboard focus and can be pinned without changing footer height.
- Added systematic narrow-window priorities for the app chrome, topic actions, composer controls, status bar, and bottom panels at the 760px minimum window width.
- Restored safe update detection against official GitHub Releases, filtered to stable `desktop-v*` tags, with a 24-hour cache and notification-only download entry.
- Added General settings for automatic update checks and manual checks; in-app downloading, installation, and forced updates remain disabled.
- Added Windows installer choices for desktop shortcut creation and launch-after-install, including upgrade-state preservation and silent-install behavior.
- Migrated configuration to version 3 so update checks default on once while preserving a user's later explicit opt-out.

## V2.0.25 - 2026-07-24

- Rebuilt the transcript as a chronological event timeline so assistant text, reasoning, tools, notices, image reads, and compaction stay at their real positions.
- Added stable viewport anchoring while streaming, loading images, and growing tool output, with explicit follow-latest behavior.
- Restored Compact process display to single-line collapsed white rows and kept Standard/Detailed process behavior chronological.
- Delayed completed-turn actions until `TurnDone` and moved elapsed/token statistics to a lightweight turn header.
- Unified queued prompts, Todo, approval/plan, questions, and context confirmation into a non-overlapping footer shelf layout.
- Strengthened background bash and subagent guidance so dependent work must be collected with `wait`; background completion no longer implies a new model turn.
- Rewrote current-product README documentation in Chinese and English and updated release links for V2.0.25.

## V2.0.24 - 2026-07-20

- Added an opt-in multimodal vision setting for PNG, JPEG, WebP, and GIF images attached from the composer or referenced from the workspace.
- Added OpenAI-compatible and Anthropic-native image request serialization while keeping image base64 out of session JSONL, memory, compaction, and conversation search.
- Added image count and size limits, workspace image snapshots, independent attachment copies for global forks, and clear handling for missing or unsupported images.
- Added Compact, Standard, and Detailed process display modes. Compact mode keeps live reasoning and tool activity on one low-emphasis expandable line without changing final answer length.
- Preserved prompt mode, approval mode, ask workflow, step thinking, and goal state when settings rebuild the active controller.
- Decoupled balance requests from conversation loading, reduced high-frequency polling, and lowered streaming Markdown and transcript rendering overhead.
- Deferred Assistant memory generation to idle time so background profile updates do not compete with active conversations.
- Fixed the slash skill list subagent label and cleaned overlapping process/composer presentation rules.

## V2.0.23 - 2026-06-21

- Added Assistant mode proactive profile memory.
- Assistant memory generation now uses the same model selected for the source conversation, falling back to the default model only for legacy pending items.
- Assistant memory update failures now use bounded retry state and become ignored after 5 failed attempts unless new conversation messages arrive.
- Added Bot prompt mode selection under the Bot model setting, with Assistant, Normal, and Enhanced modes wired through the desktop and CLI bot gateway.
- Assistant mode now silently marks conversations for memory updates when switching, opening, creating, or closing conversations.
- Assistant memory generation runs as a separate lightweight provider call and does not write into the main transcript, title generation, compression, or token accounting.
- Failed assistant memory generation is recorded as pending/failed state and retried later without blocking the main conversation or app shutdown.
- Assistant mode can inject assistant memories before a turn for relevant recall, with a safe size budget for memory bodies.
- Added settings for Assistant auto memory, proactive assistant memory recall, and clearing only the Assistant memory profile.
- Memory entries now carry optional source, timestamp, confidence, and evidence metadata; the UI can show auto-generated memories.
- Normal and Enhanced modes keep the existing tool-based memory behavior.

## V2.0.22 - 2026-06-21

- Fixed the composer textarea alignment after manual resize so the caret and placeholder stay pinned to the top of the expanded input area.
- Removed FangSong and KaiTi from the appearance font picker.
- Added three easier-reading Windows-friendly font choices: DengXian, SimSun, and Microsoft YaHei UI.
- Kept Heiti as the default display option, implemented with Microsoft YaHei first and SimHei fallback.
- Moved README release notes into this changelog so README files can stay focused on download links and stable product documentation.

## V2.0.21 - Process Statistics And Font Settings

- Folded completed-turn process rows now show only elapsed thinking time by default.
- Token usage is shown in smaller text after expanding the process row.
- Mode menu descriptions were shortened and made more task-specific.
- The font picker was simplified to Chinese display fonts.
- Legacy saved font preferences fall back to the default display font.
- Code, terminal, and tool-output areas keep a monospace primary font to preserve alignment.

## V2.0.20 - Prompt And Memory Profiles

- Added three prompt profiles: Assistant, Normal, and Enhanced.
- Assistant mode runs as `Orca` for general help.
- Normal and Enhanced modes run as `DeepSeek-Orca` for engineering collaboration and stronger agentic coding workflows.
- Prompt profiles were adapted from user-provided Claude, GPT, and Claude Code style references while preserving English model-visible prompt structure.
- Platform-specific tool names were mapped to DeepSeek-Orca's real desktop tools.
- Memory was partitioned by mode: Assistant reads and writes assistant memory only; Normal and Enhanced read all memory but write to the shared agent memory profile.
- The Memory panel added filters for all profiles, Assistant mode, and Normal/Enhanced memory.

## V2.0.19 - Side Chat And Right Dock Fixes

- Fixed a React crash when opening Side Chat with empty or null side-chat history.
- `ListSideChat` now returns an empty list for empty history instead of allowing null bridge results.
- Restored the right dock navigation to a single-row four-tab layout with tighter font, icon, and spacing rules.

## V2.0.18 - Top Bar, Right Dock, Composer, And CodeGraph

- Improved top toolbar layout so normal-width windows have enough room for main action labels before compacting controls.
- Adjusted right dock navigation so Overview, Files, Changes, and Side Chat remain visible.
- Tightened running composer controls so Pause, status text, and Stop no longer push the input row out of alignment.
- Rendered queued prompts as a small floating rounded panel instead of a full-width rectangular row.
- Fixed Tool Library enabled-row colors so blue is reserved for hover emphasis.
- Updated CodeGraph steering to use actual MCP tool names such as `mcp__codegraph__context` and `mcp__codegraph__search`.
- Removed guidance that caused the model to call unknown bare `codegraph_search`.

## V2.0.17 - Tool Library And Long-Term Conversation Search

- Added a Tool Library button next to Automation in the top bar.
- Added switches for thread management, web search, Node/Python REPL, document tools, system/host tools, long-term conversation search, and proactive tool-use steering.
- Disabled tool groups are removed from both the registered tool schema and model-visible routing policy.
- Added read-only `conversation_search` and `conversation_read` tools for finding older local transcript information after context compression.

## V2.0.16 - Search, Automation, Todo, Plan Mode, And Workspace Isolation

- Changed `web_search` to avoid DuckDuckGo and use China-accessible search sources first.
- Added an evidence-first tool policy for Normal and Enhanced modes.
- Added shared automatic Todo tracking policy for complex multi-step work in Normal and Enhanced modes.
- Added Codex-style plan proposal cards.
- Revision requests now include the previous complete plan and ask for a complete replacement plan while staying in plan mode.
- Independent conversations now use separate per-topic workspace roots, session directories, attachment areas, memory/config scopes, and tool cwd.
- Fixed bottom status bar approval indicators so Ask, Auto approve, and Full access remain visible and color-coded.
- Made model switching update visible short model labels immediately while controller rebuild happens in the background.
- Reserved automation for explicit recurring, continuous, or background-monitoring tasks.
- Added the Automation manager next to the Bot button.
- Added Side Chat as a read-only right-dock conversation that can reference the main transcript without writing to the main history.
- Localized slash menu descriptions and browser-demo capability text more consistently in the Chinese UI.
