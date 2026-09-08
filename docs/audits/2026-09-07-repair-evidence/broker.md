# S01/S10 broker repair

Date: 2026-09-07
Worktree: `D:\AI-Reasonix\.tmp\v2.1.3-worktree`
Branch: `codex/v3.0.0`

## Scope

Implemented S01/S10. Existing modifications, including the pre-existing `desktop/app_test.go` change, were preserved. The response authorization follow-up also touched the narrow caller wiring in `desktop/app.go`, `desktop/bot_runtime_app.go`, and `internal/bot/gateway.go`. No API credentials, live bots, commit, push, or publish were used.

## Changes

- `desktop/conversation_broker.go`
  - Bound dispatched prompt routing to the running task's source tab, target tab, controller pointer, and session path.
  - Removed approval/ask registrations when a task completes or is cancelled; stale responses are rejected.
  - Added source-scoped `ApproveForSource`/`AnswerForSource`; unscoped broker response methods reject, and responses require the live task/controller/session.
  - Used one locked Ready/Ctrl/StartupErr snapshot in `waitForBrokerTabReady`.
- `internal/control/controller.go`
  - Replaced per-controller prompt counters with one process-wide atomic sequence shared by approval and ask IDs.
- `desktop/app.go`, `desktop/bot_runtime_app.go`, `internal/bot/gateway.go`
  - Frontend and bot `/approve`, `/deny`, `/answer` paths now pass the source ID to source-aware broker routers; source-aware configuration does not fall back to the unscoped callback.
- Response routing now uses `bot.ResponseRouteResult`: `ResponseNotOwned` is the only fallback case, `ResponseRejected` blocks fallback, and `ResponseConsumed` confirms consumption. Completed-task IDs retain a rejection tombstone.
- Added App response-path tests and a bot test proving a rejected external answer leaves `pendingAsks` intact.
- New tests:
  - `internal/control/prompt_id_test.go`
  - `desktop/conversation_broker_s01_s10_test.go`

## Commands and results

- `gofmt -w desktop/conversation_broker.go internal/control/controller.go desktop/conversation_broker_s01_s10_test.go internal/control/prompt_id_test.go` -> passed.
- `git diff --check` -> passed; only existing LF/CRLF conversion warnings were reported.
- `go test ./internal/control -count=1 -timeout=5m` -> PASS.
- `go test . -count=1 -timeout=5m` from `desktop` -> PASS.
- `go test ./... -count=1 -p=1 -timeout=6m` from the worktree root -> PASS.
- `go test ./internal/bot -count=1 -timeout=2m` -> PASS.
- Focused S01/S10 tests in both packages -> PASS.
- Initial root full test hit a transient `internal/config` testmain mismatch while another agent edited config tests; the retry completed with all root packages PASS.
- `go test -race ...` -> not run: Go reported `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; this environment has no gcc and `CGO_ENABLED` is empty.

## Follow-up verification

- `go test ./internal/bot -run TestGatewayRejectedExternalAnswerKeepsPendingAsk -count=1 -timeout=2m` -> PASS (`0.744s`).
- `go test . -run TestConversationBrokerBindsPromptToSourceAndController|TestAppPromptMethodsDoNotFallbackAfterBrokerRejection|TestAppApproveTabKeepsBrokerPromptOnSourceRejection|TestWaitForBrokerTabReadySnapshotsStateUnderAppLock -count=1 -timeout=2m` from `desktop` -> PASS (`0.290s`).
- No root/full desktop rerun was performed; parent-owned full runs already passed.

## Timing

- Self desktop package full test: `32.573s` wall time.
- Parent-owned final full root (`fullroot63815`) and full desktop (`97929`): PASS and exited; elapsed time remains in the parent session record and was not re-run here.
