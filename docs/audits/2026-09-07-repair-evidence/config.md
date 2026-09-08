# Config Repair Notes

Scope: `codex/v3.0.0` in `D:\AI-Reasonix\.tmp\v2.1.3-worktree`. This records the config and installer changes only; release, commit, and push are intentionally out of scope.

## Public config APIs

```go
func (c *Config) Env(key string) (string, bool)
func (c *Config) Environment() map[string]string
func (c *Config) EnvironmentSlice() []string
func (c *Config) ExpandVars(s string) string
func (c *Config) ExpandPlugin(e PluginEntry) PluginEntry
func (e *ProviderEntry) WithAPIKey(token string) *ProviderEntry
```

`Env` and `Environment` give host variables precedence over the workspace's `.env`, without writing project values to the process environment. `Environment` returns an independent merged map for bash/tool registries; `EnvironmentSlice` is stable `KEY=value` form for `exec.Cmd.Env`. `ExpandPlugin` applies the same scoped lookup to MCP command, args, URL, headers, and per-plugin env values. `WithAPIKey` is an in-memory provider-entry override and is excluded from TOML rendering.

## Fixed findings

- F04: a qualified `provider/model` reference is an identity assertion; it no longer falls back to a bare model on another provider.
- F06: dotenv values are isolated per loaded root, with host environment priority and no global project-value mutation. Provider `APIKey` resolution is bound to the loaded scope while preserving its existing API.
- F10: V2 state migration uses a persistent OS exclusive lock (`LockFileEx` on Windows, `flock` on Unix) that releases with the handle/process; PID/time metadata is diagnostic only. It reports active locks as typed `ErrMigrationBusy`, copies through synced same-directory temporary files, and reports divergent destinations without writing a success marker.
- F11: legacy JSON migration records config and credentials stages separately, so a credentials failure is retryable and a newer credentials assignment is preserved. Migration no longer calls `os.Setenv`.
- S07: explicit installer data deletion includes `%AppData%\orca` as well as the legacy deepseek-orca directory.
- S08: new/upserted/imported plugin validation rejects legacy SSE with Streamable HTTP migration guidance. Existing SSE entries remain loadable for startup compatibility and no SSE transport was added.

## Verification

- `go test ./internal/config -count=1` passes.
- The boot migration test now asserts that migration does not mutate process-global `DEEPSEEK_API_KEY`; scoped config resolution and `cfg.EnvironmentSlice()` are used instead.
- Race testing was attempted but is unavailable in this host because CGO is disabled and `gcc` is not installed.
- Audit snapshot files and their bug-demonstration tests were not modified.
