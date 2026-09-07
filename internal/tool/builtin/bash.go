package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	fileenc "github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil/encoding"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/jobs"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/sandbox"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

const (
	bashWaitDelay = 5 * time.Second
)

var errBashTimeout = errors.New("bash foreground timeout")

func init() { tool.RegisterBuiltin(bash{}) }

var bashShellPATH = cachedBashShellPATH

// cachedBashShellPATH memoizes the login-shell PATH probe per login shell so a
// shell isn't spawned on every bash tool call (the probe runs up to three
// interactive-login shells with a 2s timeout each). Empty results are cached too,
// so a host without a usable login shell doesn't re-probe each command.
var (
	bashPathMu    sync.Mutex
	bashPathCache = map[string]string{}
)

func cachedBashShellPATH(ctx context.Context) string {
	key := loginShell()
	bashPathMu.Lock()
	if v, ok := bashPathCache[key]; ok {
		bashPathMu.Unlock()
		return v
	}
	bashPathMu.Unlock()

	v := defaultBashShellPATH(ctx)

	bashPathMu.Lock()
	bashPathCache[key] = v
	bashPathMu.Unlock()
	return v
}

// bash runs a shell command. sb, when it enforces, wraps the command in an OS
// sandbox; the zero value registered at init runs unconfined and is overridden
// per run by ConfineBash. shell is the resolved interpreter (real bash, or
// PowerShell on a Windows host without bash); the zero value resolves lazily.
// workDir, when non-empty, is the directory the command runs in (cmd.Dir);
// empty uses the process cwd. timeout optionally caps foreground commands;
// zero or negative means no tool-local cap, while parent context cancellation
// still kills the process tree.
type bash struct {
	sb          sandbox.Spec
	shell       sandbox.Shell
	workDir     string
	timeout     time.Duration
	hostSteer   string
	environment []string
}

// BindBashEnvironment snapshots one workspace's environment during assembly.
// The registered builtin and other workspaces remain unchanged.
func BindBashEnvironment(reg *tool.Registry, env []string) {
	t, ok := reg.Get("bash")
	if !ok {
		return
	}
	b, ok := t.(bash)
	if !ok {
		return
	}
	b.environment = append([]string{}, env...)
	reg.Add(b)
}

func (bash) Name() string { return "bash" }

func (b bash) Description() string {
	if b.resolved().Kind == sandbox.ShellPowerShell {
		return "Execute a command in the shell and return combined stdout/stderr. " +
			"NOTE: bash is not available on this host — commands run under Windows PowerShell, so write PowerShell, not bash:\n" +
			"  - chaining: ';' runs both regardless; 'if ($?) { ... }' is conditional. '&&' and '||' are NOT parsed.\n" +
			"  - redirect/vars: $null not /dev/null; $env:VAR not $VAR; '2>$null' drops stderr.\n" +
			"  - file ops: Get-ChildItem (ls), Get-Content (cat), Remove-Item -Recurse -Force (rm -rf), Copy-Item (cp), Select-String (grep).\n" +
			"  - no head/tail/which/touch: use Select-Object -First/-Last N, (Get-Command x).Source, New-Item.\n" +
			"  - multi-line text to a native exe (e.g. git commit -m): use a single-quoted here-string @'...'@ (closing '@ at column 0)." +
			bashToolSteer + b.hostToolSteer()
	}
	return "Execute a command in the shell and return combined stdout/stderr." + bashToolSteer + b.hostToolSteer()
}

// bashToolSteer points the model at the cross-platform built-in tools instead of
// shell utilities, so it doesn't reach for grep/cat/ls/find (absent or different
// on native Windows) when a native tool already does the job everywhere.
const bashToolSteer = " Use for builds, tests, git, package managers, etc. To search/read/list/edit files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file) over shell grep/cat/ls/find/sed — they behave identically on every OS. For symbol search, call graphs, or architecture questions, use codegraph tools instead of grep."

const hostToolSteer = " For host/system actions, prefer host tools before bash: host_command for native OS commands, host_list_processes/host_kill_process for processes, host_open_app for launching apps, host_clipboard for clipboard, notify_user for direct notifications, automation_create only for clearly recurring/continuous/background-monitoring tasks, web_search for unknown URLs, node_repl_exec/python_repl_exec for reusable runtime work, and document_inspect/document_extract for Word/PPT/Excel/PDF/PDF-like files."

func (b bash) hostToolSteer() string {
	if strings.TrimSpace(b.hostSteer) != "" {
		return b.hostSteer
	}
	return hostToolSteer
}

// resolved returns the bound shell, resolving lazily for the zero-value instance
// (e.g. a registry that never went through ConfineBash).
func (b bash) resolved() sandbox.Shell {
	if b.shell.Path != "" {
		return b.shell
	}
	return sandbox.ResolveShell()
}

func (bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no foreground timeout). Use for servers, watchers, or work independent of the current answer. Read new output with bash_output, wait for it with wait, stop it with kill_shell. If the current answer depends on this command, do not finalize after starting it; call wait first."}},"required":["command"]}`)
}

// ReadOnly is false: bash's effect cannot be inferred from args (rm, curl,
// git commit, etc. are all reachable). Conservative even when a particular
// command happens to be read-only — the agent batch decision can't tell.
func (bash) ReadOnly() bool { return false }

func (b bash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	sh := b.resolved()
	if !sh.SupportsChaining() && (hasUnquotedSeq(p.Command, "&&") || hasUnquotedSeq(p.Command, "||")) {
		return "", fmt.Errorf("this shell is Windows PowerShell, which does not parse '&&' or '||'. " +
			"Sequence with ';' (both run regardless of the first's result), use 'if ($?) { ... }' for " +
			"conditional chaining, or issue the commands as separate calls")
	}

	command := rewriteWindowsShutdownCommand(sh, p.Command)
	// Wrap in the OS sandbox when configured; otherwise argv is just the shell.
	argv, _ := sandbox.Command(b.sb, sh, command)
	cmdEnv := bashEnvironment(ctx, b.environment)

	if p.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			return "", fmt.Errorf("background execution is not available in this context")
		}
		workDir := b.workDir
		// The job runs under the manager's session context (no foreground timeout), so it
		// survives this turn; its combined output streams to the job buffer.
		job := jm.Start("bash", commandPreview(command), func(jobCtx context.Context, out io.Writer) (string, error) {
			cmd := exec.CommandContext(jobCtx, argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Env = cmdEnv
			setKillTree(cmd)
			cmd.WaitDelay = bashWaitDelay
			cmd.Stdout = out
			cmd.Stderr = out
			runErr := cmd.Run()
			reapTree(cmd) // reap process-group stragglers the job left running (#3702)
			return "", runErr
		})
		return fmt.Sprintf("Started background job %q. It keeps running across turns; read new output with bash_output(job_id=%q), wait for it with wait, or stop it with kill_shell(job_id=%q). If the current answer depends on this command, do not give the final answer yet; call wait first.", job.ID, job.ID, job.ID), nil
	}

	runCtx := ctx
	timeout := b.foregroundTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeoutCause(ctx, timeout, errBashTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = b.workDir // "" lets exec use the process working directory
	cmd.Env = cmdEnv
	setKillTree(cmd)
	cmd.WaitDelay = bashWaitDelay
	var buf bytes.Buffer
	w := io.Writer(&buf)
	var progress *progressWriter
	if emit, ok := tool.ProgressFrom(ctx); ok {
		progress = newProgressWriter(emit)
		w = io.MultiWriter(&buf, progress)
	}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	if progress != nil {
		progress.Flush()
	}
	// A foreground command that spawned a lingering child (e.g. `bazel run`'s
	// server) leaves it in the process group; Wait only reaped the shell leader.
	// Kill the group so those don't accumulate into an OOM (#3702). On cancel/
	// timeout setKillTree's Cancel already did this; this covers normal exit.
	reapTree(cmd)
	out := fileenc.DecodeText(buf.Bytes())

	if errors.Is(context.Cause(runCtx), errBashTimeout) {
		return out, fmt.Errorf("command timed out (> %s)", timeout)
	}
	if err != nil {
		// Non-zero exit: feed output and error back so the model can self-correct.
		return out, shellExitError(sh, p.Command, command, out, err)
	}
	return out, nil
}

func rewriteWindowsShutdownCommand(sh sandbox.Shell, command string) string {
	if runtime.GOOS != "windows" || sh.Kind != sandbox.ShellBash {
		return command
	}
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "shutdown ") && lower != "shutdown" {
		return command
	}
	if !(strings.Contains(lower, " /s") || strings.Contains(lower, " /r") || strings.Contains(lower, " /g") || strings.Contains(lower, " /l") || strings.Contains(lower, " /a")) {
		return command
	}
	escaped := strings.ReplaceAll(trimmed, `"`, `\"`)
	return `cmd.exe /c "` + escaped + `"`
}

func shellExitError(sh sandbox.Shell, originalCommand, executedCommand, output string, err error) error {
	code := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	reason := shellExitReason(sh, originalCommand, output, err)
	var b strings.Builder
	if code >= 0 {
		fmt.Fprintf(&b, "command exited with code %d", code)
	} else {
		fmt.Fprintf(&b, "command exited: %v", err)
	}
	fmt.Fprintf(&b, " (shell: %s)", sh.Kind.String())
	if reason != "" {
		b.WriteString(": ")
		b.WriteString(reason)
	}
	if strings.TrimSpace(executedCommand) != strings.TrimSpace(originalCommand) {
		b.WriteString(" [executed via: ")
		b.WriteString(executedCommand)
		b.WriteString("]")
	}
	return errors.New(b.String())
}

func shellExitReason(sh sandbox.Shell, command, output string, err error) string {
	lowerCmd := strings.ToLower(strings.TrimSpace(command))
	lowerOut := strings.ToLower(output)
	if strings.HasPrefix(lowerCmd, "shutdown") {
		if strings.Contains(lowerOut, "usage:") || strings.Contains(lowerOut, "用法:") || strings.Contains(lowerOut, "没有参数") || strings.Contains(lowerOut, "must be used") {
			return "Windows shutdown arguments were rejected or the current shell did not pass them correctly; use host_command with shell=cmd/powershell for Windows-native commands"
		}
		return "shutdown failed; likely causes are insufficient permission, local policy restrictions, or arguments rejected by the operating system"
	}
	if sh.Kind == sandbox.ShellPowerShell && (hasUnquotedSeq(command, "&&") || hasUnquotedSeq(command, "||")) {
		return "Windows PowerShell 5.1 does not support bash-style &&/|| chaining"
	}
	if code := exitCodeOf(err); code >= 0 {
		return "process returned non-zero exit code " + strconv.Itoa(code) + "; inspect the command output above for the concrete cause"
	}
	return ""
}

func exitCodeOf(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func (b bash) foregroundTimeout() time.Duration {
	if b.timeout <= 0 {
		return 0
	}
	return b.timeout
}

// progressWriter forwards command output to a tool.ProgressFunc while keeping
// UTF-8 rune boundaries intact. Pipes may split a multibyte CJK filename across
// writes; emitting string(p) directly would briefly show mojibake in the tool
// card even though the final tool result decodes correctly.
type progressWriter struct {
	emit    tool.ProgressFunc
	pending []byte
}

func newProgressWriter(emit tool.ProgressFunc) *progressWriter { return &progressWriter{emit: emit} }

func (w *progressWriter) Write(p []byte) (int, error) {
	data := append(w.pending, p...)
	w.pending = w.pending[:0]
	if len(data) == 0 {
		return len(p), nil
	}
	if utf8.Valid(data) {
		w.emit(string(data))
		return len(p), nil
	}
	cut := len(data)
	for cut > 0 && !utf8.Valid(data[:cut]) {
		cut--
	}
	if cut > 0 {
		w.emit(string(data[:cut]))
		w.pending = append(w.pending, data[cut:]...)
		return len(p), nil
	}
	if len(data) <= utf8.UTFMax {
		w.pending = append(w.pending, data...)
		return len(p), nil
	}
	w.emit(fileenc.DecodeText(data))
	return len(p), nil
}

func (w *progressWriter) Flush() {
	if len(w.pending) == 0 {
		return
	}
	w.emit(fileenc.DecodeText(w.pending))
	w.pending = w.pending[:0]
}

// hasUnquotedSeq reports whether seq appears in s outside any single- or
// double-quoted span, so a literal "a && b" string argument doesn't trip the
// PowerShell chaining guard.
func hasUnquotedSeq(s, seq string) bool {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if strings.HasPrefix(s[i:], seq) {
			return true
		}
	}
	return false
}

// commandPreview is a short single-line label for a background bash job, surfaced
// in the status bar and completion notices.
func commandPreview(cmd string) string {
	cmd = strings.TrimSpace(strings.ReplaceAll(cmd, "\n", " "))
	const max = 48
	r := []rune(cmd)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return cmd
}

func bashCommandEnv(ctx context.Context) []string {
	return bashEnvironment(ctx, nil)
}

func bashEnvironment(ctx context.Context, base []string) []string {
	env := append([]string(nil), base...)
	if base == nil {
		env = os.Environ()
	}
	if runtime.GOOS == "windows" {
		return env
	}
	currentPath, _ := envValue(env, "PATH")
	if shellPath := strings.TrimSpace(bashShellPATH(ctx)); shellPath != "" {
		if merged := mergePathLists(shellPath, currentPath); merged != currentPath {
			env = setEnvValue(env, "PATH", merged)
		}
	}
	return env
}

func defaultBashShellPATH(ctx context.Context) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := loginShell()
	if shell == "" {
		return ""
	}
	const marker = "__DEEPSEEK_ORCA_BASH_PATH__="
	script := "printf '\\n" + marker + "%s\\n' \"$PATH\""
	for _, args := range [][]string{
		{"-l", "-i", "-c", script},
		{"-l", "-c", script},
		{"-c", script},
	} {
		out := runShellPATHCommand(ctx, shell, args)
		if path := parseShellPATH(out, marker); path != "" {
			return path
		}
	}
	return ""
}

func loginShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		if hasPathSeparator(shell) {
			if isExecutableFile(shell) {
				return shell
			}
		} else if p, err := exec.LookPath(shell); err == nil {
			return p
		}
	}
	for _, shell := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if isExecutableFile(shell) {
			return shell
		}
	}
	return ""
}

func runShellPATHCommand(parent context.Context, shell string, args []string) []byte {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Stdin = strings.NewReader("")
	out, _ := cmd.CombinedOutput()
	return out
}

func parseShellPATH(out []byte, marker string) string {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], marker) {
			return strings.TrimSpace(strings.TrimPrefix(lines[i], marker))
		}
	}
	return ""
}

func hasPathSeparator(s string) bool {
	return strings.ContainsAny(s, `/\`)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func setEnvValue(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && envKeyEqual(k, key) {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}

func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func mergePathLists(primary, secondary string) string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		for _, part := range filepath.SplitList(path) {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	add(primary)
	add(secondary)
	return strings.Join(out, string(os.PathListSeparator))
}
