# Computer Use Read-Only Review

- Date: 2026-09-07
- Worktree: `D:\AI-Reasonix\.tmp\v2.1.3-worktree`, branch `codex/v3.0.0`
- Scope: `desktop/computeruse/service.go`, `desktop/computeruse/backend_windows.go`, `desktop/computer_use_tool.go`, related new tests, and the Anthropic stream cancellation boundary.
- Method: source and line-number review plus focused tests. No native input, real window, API, or real key was used during the initial review; implementation status and post-fix checks are recorded below.

## Findings

### [P1] Integrity-level check parses the Windows token buffer incorrectly

- Location: `desktop/computeruse/backend_windows.go:797-811`
- Status: confirmed by Windows ABI inspection; not executed on a native Windows desktop here.
- `GetTokenInformation(TokenIntegrityLevel, ...)` returns a `Tokenmandatorylabel` whose first field is a pointer to the SID (`SIDAndAttributes.Sid`), followed by attributes; the SID bytes are not represented by `buf[1]` and do not start at offset 8. The code treats `buf[1]` as the SID sub-authority count and reads the level from an invented offset.
- Impact: `isHigherIntegrity` can return an error/fail-closed result for ordinary windows, making all observed windows appear protected and blocking normal CU. If the accidental bytes pass the length check, the comparison is still not a real integrity comparison, so the target-trust boundary is not reliable.
- Fix direction: parse `windows.Tokenmandatorylabel` from the returned buffer, follow `Label.Sid`, validate the SID, and use `SubAuthorityCount()`/`SubAuthority(last)` (or a tested Windows helper). Add native-build unit coverage for same-process, lower-integrity, and higher-integrity tokens without opening a target window.

### [P1] Held input is forgotten when the release event fails

- Location: `desktop/computeruse/backend_windows.go:938-958`
- Status: reproducible on the `SendInput` error path; no native call was made here.
- `ReleaseInjectedInput` clears `pressedKeys` and `pressedButtons` at lines 947-948 before sending key-up/button-up events, then discards every send error at lines 951, 954, and 957.
- Impact: a desktop switch, locked session, transient `SendInput` failure, or other native rejection can leave a key or mouse button physically held while the backend has lost the only record needed to retry. Stop/Pause/finish then report cleanup attempts but cannot guarantee release.
- Fix direction: retain failed releases as pending state and retry on the next stop/finish, or only clear each entry after a successful key-up/button-up. Return/record cleanup failure and add a test with injectable release calls that fail once, then succeed.

### [P1] Coordinate protection check fails open on UI Automation errors

- Location: `desktop/computeruse/backend_windows.go:824-843`
- Status: reproducible on the UIA error path; native UIA was not invoked here.
- `protectedElementAt` ignores the error from `withUIAutomation` and from `ElementFromPoint` (lines 826-830), then returns `protected == false`. `WindowsBackend.Execute` proceeds with coordinate input when this check cannot inspect the target.
- Impact: a transient COM/UIA initialization or point-query failure can turn an unknown target into an allowed click/type location, bypassing the password/CAPTCHA protection that this check is intended to enforce.
- Fix direction: return `(false, error)` or a protected/indeterminate result and deny coordinate injection when inspection fails. Add a test that makes UIA lookup fail and asserts `ErrProtectedSurface` (or a dedicated inspection-denied error).

### [P1] Session replacement can orphan the new safety-hook thread

- Location: `desktop/computeruse/backend_windows.go:846-855`, `857-863`, and `921-925`
- Status: theoretical timing race, not reproduced because native hooks were not started.
- `StartSafetyHooks` calls `StopSafetyHooks` but does not wait for the previous `hookLoop` to exit. During rapid stop/start, the old loop can finish after the new loop has installed its hooks; the old cleanup unconditionally writes `hookThread`, `keyboardHook`, and `mouseHook` to zero at lines 923-925. That can erase the new thread/handle state.
- Impact: the replacement session may keep an installed hook that `StopSafetyHooks` can no longer address (`hookThread == 0`), leaving callbacks/resources live across session replacement and weakening the Escape/user-input safety boundary.
- Fix direction: give each hook loop an instance generation and only clear fields if they still belong to that instance, and wait for the old loop before installing a replacement. Add a fake hook lifecycle test that completes old cleanup after new startup and verifies the new hook remains stoppable.

### [P2] Anthropic cancellation can strand the stream goroutine on an output send

- Location: `internal/provider/anthropic/anthropic.go:159-160` and output sends such as `351-354`, `371-379`, `393-396`, `424-443`
- Status: code-confirmed cancellation race; no real API was used.
- `Stream` starts `readStream` on an unbuffered channel. `collectComputerControlResponse` and `verifyComputerCompletion` return immediately when their context is canceled, but `readStream` sends chunks with plain `out <- ...` and never selects on the same context. If cancellation occurs while `readStream` is trying to deliver a chunk, the goroutine remains blocked forever, so its deferred body close/channel close and watchdog cleanup do not run.
- Impact: repeated Stop/Pause/session replacement during active Anthropic output can accumulate blocked provider goroutines and response bodies. The new missing-`message_stop` test does not cover this cancellation boundary.
- Fix direction: pass the request context into `readStream` and use a context-aware send helper for every chunk, including error/final chunks; add a synthetic stream test that cancels while the consumer is not receiving and asserts the channel closes promptly.

## Verification

- `go test ./computeruse -run 'Test(StopAndPauseCancelInFlightAction|ObserveDiscardsOutOfOrderCompletion|StoppedSessionCannotBeCompleted|SupersededSessionCannotActOrStopNewSession|InputDelayHonorsCancellation|ActionRequiresCurrentObservationAndRefreshesAfterward|UserInputPausesAndEscapeStops|ProtectedObservationCannotStartActions)$' -count=1 -v -timeout=2m`: PASS.
- `go test ./internal/provider/anthropic -run 'Test(ReadStreamRequiresMessageStop|ReadStream)' -count=1 -v -timeout=2m`: PASS.
- `GOOS=windows GOARCH=amd64 go test -c ./desktop/computeruse`: PASS.
- `GOOS=windows GOARCH=amd64 go test -c ./internal/provider/anthropic`: PASS.
- `go test ./... -run '^$' -count=1` from `desktop`: BLOCKED by unrelated existing `internal/bot/gateway.go:654,655,678,679` references to undefined `state`.
- `-race` was not obtained in this environment because the default Go toolchain has CGO disabled; no native/API test was substituted.

## Implementation Follow-up

The five findings above were implemented in the authorized files:

- `desktop/computeruse/backend_windows.go`: integrity parsing now follows `Tokenmandatorylabel.Label.Sid`; release state is retained until each release succeeds; UIA inspection errors deny the action; safety hooks have serialized replacement, instance generation checks, a completion channel, and stale-cleanup protection.
- `internal/provider/anthropic/anthropic.go`: `readStream` receives the request context, closes the body on cancellation, and uses context-aware sends for text, tool, error, usage, and done chunks.
- Regression coverage: `desktop/computeruse/backend_windows_regression_test.go` covers the SID layout, retry retention, fail-closed decision, and stale hook cleanup. `internal/provider/anthropic/anthropic_test.go` covers cancellation while a chunk send is blocked. Existing service/session and `message_stop` tests were retained.

Post-fix verification:

- `go test ./computeruse -count=20 -timeout=2m`: PASS.
- `go test ./internal/provider/anthropic -count=20 -timeout=2m`: PASS.
- `GOOS=windows GOARCH=amd64 go test -c ./computeruse`: PASS.
- `GOOS=windows GOARCH=amd64 go test -c ./internal/provider/anthropic`: PASS.
- `go vet ./computeruse`: only the existing `unsafe.Pointer` warnings at the Windows hook callback conversions.
- `go vet ./internal/provider/anthropic`: PASS.
- No real `SendInput`, window automation, safety hook startup, provider API, or credential was used.

This follow-up changed only the authorized Computer Use/Anthropic source and test files; no config, prompt profile, desktop product file, audit snapshot, commit, push, or release was performed.

## Hook/UIA Follow-up

The parent review identified two additional issues, and the authorized implementation now covers them:

- `desktop/computeruse/backend_windows.go:946-1040`: `hookLoop` calls `PeekMessageW` before publishing `ready`, creating the thread message queue before any stop request can target it.
- `desktop/computeruse/backend_windows.go:1043-1087`: `StopSafetyHooksError` and `stopSafetyHooks` check `PostThreadMessageW` through `postHookStop`; a failed post returns immediately and does not wait on `done`. The hook state is retained so a later stop can retry. `desktop/computeruse/service.go:35-46,376-384` uses this error-returning path for the real service stop flow while preserving the existing `Backend` interface for other platforms.
- `desktop/computeruse/backend_windows.go:518-533,850-933`: both `uiaAction` and coordinate protection reject a nil `ElementFromPoint` result and call the direct COM `CurrentIsPassword` and `CurrentName` getters. Their HRESULTs are checked; property-read failure is wrapped as `ErrProtectedSurface` by `protectedElementDecision`, so a valid COM element cannot fail open.
- `desktop/computeruse/backend_windows_regression_test.go:89-130` covers password/name HRESULT failures using synthetic COM vtable callbacks. `:132-151` covers a failed post without waiting for an open completion channel.

Additional verification:

- `go test ./computeruse -count=20 -timeout=2m`: PASS.
- `go test ./internal/provider/anthropic -count=20 -timeout=2m`: PASS.
- `go test -c -o computeruse.review.test.exe ./computeruse`: PASS; artifact removed immediately.
- `go test -c -o anthropic.review.test.exe ./internal/provider/anthropic`: PASS; artifact removed immediately.
- `go vet ./computeruse`: only the existing Windows callback `unsafe.Pointer` warnings, plus the equivalent synthetic-vtable test warning; no new semantic vet error.
- `go vet ./internal/provider/anthropic`: PASS.
- `go test -race ./computeruse`: not available because this Go environment has `CGO_ENABLED=0`.

Native Windows desktop acceptance remains an explicit gap: no real hook installation, `SendInput`, window automation, provider API, or credential was used. No further requirements were added after this freeze point.

## Final Vtable Simplification

Per parent module verification, the HRESULT path now casts the COM object header to the library-exported `uia.IUIAutomationElementVtbl` and reads `Get_CurrentIsPassword` / `Get_CurrentName` directly. The self-defined `[36]uintptr` prefix and magic indexes were removed; the synthetic fixture uses the same exported vtable fields.

Final focused verification:

- `go test ./computeruse -count=30 -timeout=2m`: PASS.
- `go test ./internal/provider/anthropic -count=30 -timeout=2m`: PASS.
- `git diff --check -- desktop/computeruse/backend_windows.go desktop/computeruse/backend_windows_regression_test.go`: PASS.

This is the final freeze point. No additional scope was added.
