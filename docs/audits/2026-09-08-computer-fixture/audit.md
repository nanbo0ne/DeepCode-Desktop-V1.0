# O.R.C.A 3.0.3 Computer Use Fixture Harness

This directory records the isolated real-use fixture prepared for O.R.C.A 3.0.3. It is test infrastructure only. No production file, release metadata, README, or existing audit was changed.

## Files

- `desktop/computer_e2e_fixture_test.go` contains the localhost fixture, validator, HWND/PID target guard, request/timeout limits, unit checks, and the opt-in native test.

The fixture is a fresh `httptest` server for every test. It shows random per-run Chinese form values, a select, a checkbox, a long scrollable/filterable list, and a drag canvas. The validator URL is held only by the Go test. It is not rendered into the page and is not included in the natural-language task. The page posts interaction facts to a separate local event route so the validator can independently confirm the rendered workflow.

## Real test path

The opt-in test starts a browser with a temporary profile and opens only the fixture URL. It then builds the production desktop Controller through the owner-aware `App.buildController(..., "test")` path, binds the real `test` tab to the fixture workspace, submits one natural-language request, and expects the main model to call `computer_task`. `runComputerTask` performs the image/function qualification first, selects the official vision-capable model, and drives the existing Computer Use service, so the test is not a direct `computer_action` shortcut or a fake provider loop. There is no empty-tab alias in the harness.

The injected test-only backend wrapper checks the exact fixture HWND and PID before every observation and action and again after each action. A changed foreground window fails before production capture or input is reached. It also rejects window-management actions and system/navigation/devtools keys, including `Ctrl+L`, so a browser PID match cannot silently leave the fixture. The harness filters the live production Controller registry before the first turn and asserts that only `computer_task` and `computer_stop` remain; this is not a prompt-only prohibition. The existing backend still performs its own secure-desktop, trust, stale-generation, UI Automation, and protected-element checks.

The native test runs three repetitions in one Go process and shares a process-wide transport budget: `40` Computer Use steps per case, `5m` per case, and `200` DeepSeek connection/request attempts across all repetitions. Repetitions one and two retain UI Automation elements; repetition three strips elements after native capture for a visual-only path, rebuilds `Summary` from only foreground title/bounds/crop, and reduces `Windows` to the target window. A `httptrace.ClientTrace.GetConn` is attached at the case-context root and counts only `api.deepseek.com:443`; overflow cancels the inherited context before the transport dial/write path, including reused connections. Qualification, main-controller, computer-loop, and completion requests inherit that trace. The harness no longer counts `Usage` events or observations as model requests. The unit probes verify both the visual-only summary/window redaction and the request overflow path reaching neither `DialContext` nor the network.

## Exact command

Run from `D:\AI-Reasonix\.tmp\v2.1.3-worktree\desktop` in an interactive Windows session. Keep the API key in the environment; do not put it in the command line, config, fixture page, or logs.

```powershell
if (-not $env:DEEPSEEK_API_KEY) { throw "DEEPSEEK_API_KEY is not set" }
$env:ORCA_COMPUTER_E2E = "1"
go test . -run '^TestComputerE2ENativeController$' -count=1 -timeout=17m
Remove-Item Env:ORCA_COMPUTER_E2E
```

The default command remains safe and skips the native case:

```powershell
go test . -count=1
```

The opt-in command needs Microsoft Edge or Chrome, an ordinary interactive desktop, and a DeepSeek account/model route that supports the configured official vision model. The harness checks `PATH`, then standard absolute Edge/Chrome installation paths, and owns only its temporary browser profile, temporary workspace, and localhost server; it does not close or automate an existing browser process.

## Unit checks run here

The fixture page/validator contract, target guard, request-trace overflow probe, and step/task-text contract were run without a browser, model, screen capture, or input injection:

```powershell
go test . -run '^(TestComputerFixture|TestComputerE2ERequestTraceStopsBeforeDial|TestComputerE2EContract)$' -count=1
```

Observed result: passed. The real model/native test was not run in this preparation turn.

## Uncovered native cases

- No real provider-backed desktop action was executed here, so model behavior, visual grounding quality, and provider quota behavior remain unverified.
- Native UI Automation exposure of every browser version/control is not guaranteed; the first two repetitions require at least one UIA element and a nonblank captured screenshot, while the third intentionally strips UIA elements and requires a nonblank captured screenshot.
- Multi-monitor coordinates, negative display origins, mixed monitor scaling, browser zoom, Windows text scaling, remote desktop scaling, and physical DPI changes are not exercised.
- UAC/elevated windows, secure desktop, lock screen, user takeover/Escape timing, browser crash/restart, and stale HWND reuse are not exercised beyond the production guard paths.
- The `200` guard is exact only for standard Go HTTP transport attempts that call `GetConn` with `api.deepseek.com:443`; alternate transports, HTTP/3, another hostname, or a provider path that does not inherit the case context remain outside this fixture's count and must be reported from provider receipts/quota after a live run.
- The current checkout now contains `ResumeSessionID`/`Guidance`, `ResumeTask`, and `Escalate`; this real fixture case does not branch into an escalation/resume flow. Ownership and scope behavior remain covered by the separate `desktop/computeruse/ownership_test.go`; native resume after a real visual escalation is not exercised here.
