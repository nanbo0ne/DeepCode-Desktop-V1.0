# Native Findings

- COM release handling, the `POINT` ABI, and overlay call errors have been confirmed and repaired.
- The DPI risk was repaired with physical-coordinate scoping: thread pinning, Per-Monitor V2 context, restoration, and the corresponding native paths. This does not prove that DPI caused the earlier 93-action case.
- The current foreground is a protected `GameInputSvc.exe` window (PID 109684) and cannot be taken over safely, so further native testing is blocked for now.
- The shared budget is at `123/200`; it is not exhausted, but no test may exceed the `200` request ceiling.
- A normal repeat, deliberate manual takeover, multi-monitor behavior, and system-DPI variants remain unverified.
