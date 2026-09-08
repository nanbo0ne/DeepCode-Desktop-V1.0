# Local lifecycle closeout

Date: 2026-09-07
Branch: `codex/v3.0.0`
Worktree: `D:\AI-Reasonix\.tmp\v2.1.3-worktree`

## Desktop runtime token

Read the worktree implementation in `internal/config/config.go`: `ProviderEntry.WithAPIKey(token)` stores a private in-memory override, and `ProviderEntry.APIKey()` uses that override before scoped environment lookup.

`desktop/local_ai_app.go` now builds the runtime provider with an empty `APIKeyEnv` and binds `status.APIKey` through `entry.WithAPIKey(status.APIKey)`. The local runtime token therefore stays in the per-controller provider entry; `ORCA_LOCAL_API_KEY` is no longer written with `os.Setenv`.

`desktop/local_ai_app_lifecycle_test.go` preloads `ORCA_LOCAL_API_KEY` with a sentinel, verifies the runtime provider resolves its session token from memory, requires an empty `APIKeyEnv`, and confirms the process environment remains unchanged.

## Single instance

`ORCA_DEV` disables the single-instance lock only for explicit true values accepted by `strconv.ParseBool`, including `true`, `TRUE`, and `1`. Empty, `false`, `0`, and arbitrary text retain the lock. Coverage is in `desktop/single_instance_lifecycle_test.go`.

## Download worker handoff

`internal/localai/manager.go` keeps a per-task worker completion channel. Resume waits for the previous generation outside `m.mu`; cancellation of a waiting new generation prevents it from entering `run`. Worker completion checks both generation and channel identity, so stale workers cannot publish the current task snapshot. `start` copies its queued snapshot before launching the goroutine.

The HTTP regression `TestPauseResumeWaitsForPreviousHTTPWorker` exercises a partial first response followed by rapid Pause/Resume, checks one active HTTP worker, validates the final `.part` checksum, and rejects stale queued-state publication. `TestCancelledResumeHandoffDoesNotStartWorker` covers cancellation while handoff is waiting.

## OpenAI stream cancellation

`internal/provider/openai/openai.go` now routes every stream output kind
(error, usage, reasoning/text, tool-call start, tool-call, and done) through a
context-aware send. `streamWithReconnect` checks cancellation before emitting
errors and before reconnecting, so a closed/cancelled response cannot start a
new request. Partial output and the existing replay rule remain unchanged.

`TestStreamCancelWithUnreadConsumerReturnsRepeatedly` drives the real
`streamWithReconnect` path 100 times with an unbuffered output channel that is
never read. It cancels while the first partial chunk is blocked and waits for
the stream goroutine to return; it also asserts that reconnect is never called.

## Verification

- `go test ./internal/config -run '^TestProviderEntryWithAPIKeyIsInMemoryOnly$' -count=1` passed.
- `go test ./internal/localai -count=20 -timeout=4m` passed.
- `go test . -run 'Test(LocalRuntimeProvider|DevMode|SingleInstanceLock)' -count=1` passed in `desktop`.
- `go test . -count=1 -timeout=3m` passed in `desktop`.
- `go vet ./internal/localai` passed.
- `go test ./internal/provider/openai -count=1 -timeout=2m` passed.
- `go test ./internal/provider/openai -run '^TestStreamCancelWithUnreadConsumerReturnsRepeatedly$' -count=10 -timeout=2m` passed.
- `go vet ./internal/provider/openai` passed.
- `git diff --check` passed for the touched source and test files.

No runtime installation/start, real API, commit, push, or publish was performed.
