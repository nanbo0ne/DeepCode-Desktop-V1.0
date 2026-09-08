package localai

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RuntimeState string

const (
	RuntimeUnavailable RuntimeState = "unavailable"
	RuntimeStopped     RuntimeState = "stopped"
	RuntimeLoading     RuntimeState = "loading"
	RuntimeRunning     RuntimeState = "running"
	RuntimeFailed      RuntimeState = "failed"
)

type LoadProfile struct {
	ContextSize int    `json:"contextSize"`
	BatchSize   int    `json:"batchSize"`
	UBatchSize  int    `json:"ubatchSize"`
	GPULayers   string `json:"gpuLayers"`
	Threads     int    `json:"threads"`
	VRAMReserve int64  `json:"vramReserveMiB"`
	Attempt     int    `json:"attempt"`
}

type RuntimeStatus struct {
	State       RuntimeState `json:"state"`
	Supported   bool         `json:"supported"`
	Installed   bool         `json:"installed"`
	ModelID     string       `json:"modelId,omitempty"`
	BaseURL     string       `json:"baseUrl,omitempty"`
	APIKey      string       `json:"-"`
	PID         int          `json:"pid,omitempty"`
	Profile     LoadProfile  `json:"profile"`
	Message     string       `json:"message,omitempty"`
	LastError   string       `json:"lastError,omitempty"`
	LogPath     string       `json:"logPath,omitempty"`
	UpdatedAt   int64        `json:"updatedAt"`
	IdleSeconds int          `json:"idleSeconds"`
}

type RuntimeServer struct {
	mu       sync.Mutex
	startMu  sync.Mutex
	status   RuntimeStatus
	cmd      *exec.Cmd
	stopCh   chan struct{}
	idle     *time.Timer
	idleFor  time.Duration
	emit     func(RuntimeStatus)
	client   *http.Client
	logFile  *os.File
	logLines []string
}

func NewRuntimeServer(emit func(RuntimeStatus)) *RuntimeServer {
	return &RuntimeServer{
		status: RuntimeStatus{State: RuntimeStopped, Supported: runtime.GOOS == "windows" && LocalAIEnabled, UpdatedAt: time.Now().UnixMilli()},
		emit:   emit,
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

func (s *RuntimeServer) Status() RuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *RuntimeServer) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != RuntimeRunning || s.idleFor <= 0 {
		return
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(s.idleFor, func() { _ = s.Stop() })
}

func (s *RuntimeServer) Start(ctx context.Context, installation RuntimeInstallation, model ModelInstallation, spec ModelSpec, hardware HardwareProfile, reserveMiB int64, idle time.Duration) (RuntimeStatus, error) {
	if err := RequireEnabled(); err != nil {
		return s.Status(), err
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if runtime.GOOS != "windows" {
		return s.setFailure(false, "本地推理首版仅支持 Windows", errors.New("local runtime is unavailable on this platform"))
	}
	if installation.ServerPath == "" || model.ModelPath == "" {
		return s.setFailure(false, "请先安装 llama.cpp 运行器和本地模型", errors.New("runtime or model is not installed"))
	}
	s.mu.Lock()
	if s.status.State == RuntimeRunning && s.status.ModelID == model.ID {
		status := s.status
		s.mu.Unlock()
		s.Touch()
		return status, nil
	}
	s.mu.Unlock()
	_ = s.stopProcess()

	if reserveMiB <= 0 {
		reserveMiB = dynamicVRAMReserve(hardware)
	}
	threads := recommendedThreads(hardware.CPULogicalCores)
	plans := loadProfiles(spec, threads, reserveMiB)
	var lastErr error
	for _, profile := range plans {
		if err := ctx.Err(); err != nil {
			return s.setFailure(true, "本地模型加载已取消", err)
		}
		status, err := s.startAttempt(ctx, installation, model, profile, idle)
		if err == nil {
			return status, nil
		}
		lastErr = err
		_ = s.stopProcess()
	}
	return s.setFailure(true, "所有显存安全加载档位均失败", lastErr)
}

func (s *RuntimeServer) startAttempt(ctx context.Context, installation RuntimeInstallation, model ModelInstallation, profile LoadProfile, idle time.Duration) (RuntimeStatus, error) {
	port, err := availablePort()
	if err != nil {
		return RuntimeStatus{}, err
	}
	token, err := randomToken()
	if err != nil {
		return RuntimeStatus{}, err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	logPath := filepath.Join(installation.Path, "orca-runtime.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return RuntimeStatus{}, err
	}
	args := serverArgs(model, profile, port, token)
	cmd := exec.Command(installation.ServerPath, args...)
	cmd.Dir = installation.Path
	configureHiddenProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = logFile.Close()
		return RuntimeStatus{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = logFile.Close()
		return RuntimeStatus{}, err
	}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return RuntimeStatus{}, err
	}
	stopCh := make(chan struct{})
	s.mu.Lock()
	s.cmd, s.stopCh, s.logFile, s.idleFor = cmd, stopCh, logFile, idle
	s.status = RuntimeStatus{State: RuntimeLoading, Supported: true, Installed: true, ModelID: model.ID, BaseURL: baseURL, APIKey: token, PID: cmd.Process.Pid, Profile: profile, Message: fmt.Sprintf("正在加载，本次使用 %d token 上下文", profile.ContextSize), LogPath: logPath, UpdatedAt: time.Now().UnixMilli(), IdleSeconds: int(idle.Seconds())}
	status := s.status
	s.mu.Unlock()
	s.publish(status)
	go s.captureOutput(stdout, logFile)
	go s.captureOutput(stderr, logFile)
	go s.waitProcess(cmd, stopCh)

	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	healthURL := strings.TrimSuffix(baseURL, "/v1") + "/health"
	for {
		select {
		case <-ctx.Done():
			return RuntimeStatus{}, ctx.Err()
		case <-stopCh:
			return RuntimeStatus{}, fmt.Errorf("llama.cpp exited while loading: %s", s.lastLogText())
		case <-deadline.C:
			return RuntimeStatus{}, fmt.Errorf("llama.cpp model load timed out: %s", s.lastLogText())
		case <-tick.C:
			resp, reqErr := s.client.Get(healthURL)
			if reqErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					s.mu.Lock()
					if s.cmd != cmd {
						s.mu.Unlock()
						return RuntimeStatus{}, errors.New("local runtime was superseded")
					}
					s.status.State, s.status.Message, s.status.UpdatedAt = RuntimeRunning, "本地模型已就绪", time.Now().UnixMilli()
					status = s.status
					s.mu.Unlock()
					s.publish(status)
					s.Touch()
					return status, nil
				}
			}
		}
	}
}

func (s *RuntimeServer) Stop() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.stopProcess()
}

func (s *RuntimeServer) stopProcess() error {
	s.mu.Lock()
	cmd := s.cmd
	stopCh := s.stopCh
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	s.cmd = nil
	s.stopCh = nil
	s.status = RuntimeStatus{State: RuntimeStopped, Supported: runtime.GOOS == "windows" && LocalAIEnabled, Installed: s.status.Installed, UpdatedAt: time.Now().UnixMilli()}
	status := s.status
	s.mu.Unlock()
	var err error
	if cmd != nil && cmd.Process != nil {
		err = terminateProcess(cmd, stopCh)
	}
	s.closeLog()
	s.publish(status)
	return err
}

func (s *RuntimeServer) waitProcess(cmd *exec.Cmd, stopCh chan struct{}) {
	err := cmd.Wait()
	close(stopCh)
	s.mu.Lock()
	if s.cmd != cmd {
		s.mu.Unlock()
		return
	}
	s.cmd = nil
	if s.status.State == RuntimeRunning || s.status.State == RuntimeLoading {
		s.status.State = RuntimeFailed
		s.status.LastError = processError(err, s.logLines)
		s.status.Message = "本地推理进程已退出"
		s.status.UpdatedAt = time.Now().UnixMilli()
	}
	status := s.status
	s.mu.Unlock()
	s.closeLog()
	s.publish(status)
}

func (s *RuntimeServer) captureOutput(reader io.Reader, logFile *os.File) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(logFile, line)
		s.mu.Lock()
		s.logLines = append(s.logLines, line)
		if len(s.logLines) > 80 {
			s.logLines = append([]string(nil), s.logLines[len(s.logLines)-80:]...)
		}
		s.mu.Unlock()
	}
}

func (s *RuntimeServer) closeLog() {
	s.mu.Lock()
	file := s.logFile
	s.logFile = nil
	s.mu.Unlock()
	if file != nil {
		_ = file.Close()
	}
}

func (s *RuntimeServer) lastLogText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return processError(nil, s.logLines)
}

func (s *RuntimeServer) setFailure(installed bool, message string, err error) (RuntimeStatus, error) {
	s.mu.Lock()
	s.status = RuntimeStatus{State: RuntimeFailed, Supported: runtime.GOOS == "windows" && LocalAIEnabled, Installed: installed, Message: message, UpdatedAt: time.Now().UnixMilli()}
	if err != nil {
		s.status.LastError = err.Error()
	}
	status := s.status
	s.mu.Unlock()
	s.publish(status)
	return status, err
}

func (s *RuntimeServer) publish(status RuntimeStatus) {
	if s.emit != nil {
		s.emit(status)
	}
}

func loadProfiles(spec ModelSpec, threads int, reserveMiB int64) []LoadProfile {
	contexts := append([]int{spec.ContextSize}, spec.ContextFallback...)
	seen := map[int]bool{}
	profiles := make([]LoadProfile, 0, 4)
	for _, ctx := range contexts {
		if ctx <= 0 || seen[ctx] {
			continue
		}
		seen[ctx] = true
		ubatch := 512
		if ctx <= 8192 {
			ubatch = 256
		}
		profiles = append(profiles, LoadProfile{ContextSize: ctx, BatchSize: 2048, UBatchSize: ubatch, GPULayers: "auto", Threads: threads, VRAMReserve: reserveMiB, Attempt: len(profiles) + 1})
	}
	lastCtx := 8192
	if len(profiles) > 0 && profiles[len(profiles)-1].ContextSize < lastCtx {
		lastCtx = profiles[len(profiles)-1].ContextSize
	}
	profiles = append(profiles, LoadProfile{ContextSize: lastCtx, BatchSize: 1024, UBatchSize: 256, GPULayers: "20", Threads: threads, VRAMReserve: reserveMiB, Attempt: len(profiles) + 1})
	return profiles
}

func serverArgs(model ModelInstallation, profile LoadProfile, port int, token string) []string {
	args := []string{
		"--model", model.ModelPath,
		"--alias", ProviderID + "/" + model.ID,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--api-key", token,
		"--cors-origins", "localhost",
		"--no-webui",
		"--jinja",
		"--ctx-size", strconv.Itoa(profile.ContextSize),
		"--parallel", "1",
		"--threads", strconv.Itoa(profile.Threads),
		"--threads-batch", strconv.Itoa(profile.Threads),
		"--batch-size", strconv.Itoa(profile.BatchSize),
		"--ubatch-size", strconv.Itoa(profile.UBatchSize),
		"--cache-type-k", "q8_0",
		"--cache-type-v", "q8_0",
		"--kv-unified",
		"--n-gpu-layers", profile.GPULayers,
		"--fit", "on",
		"--fit-target", strconv.FormatInt(profile.VRAMReserve, 10),
		"--fit-ctx", "4096",
		"--reasoning-format", "deepseek",
	}
	if model.MMProjPath != "" {
		args = append(args, "--mmproj", model.MMProjPath)
	}
	return args
}

func recommendedThreads(logical int) int {
	if logical <= 0 {
		logical = runtime.NumCPU()
	}
	threads := logical * 3 / 4
	if threads < 4 {
		threads = 4
	}
	if threads > 12 {
		threads = 12
	}
	return threads
}

func dynamicVRAMReserve(hardware HardwareProfile) int64 {
	reserve := int64(2048)
	var available int64
	for _, gpu := range hardware.GPUs {
		if gpu.AvailableMiB > available {
			available = gpu.AvailableMiB
		}
	}
	if share := available * 15 / 100; share > reserve {
		reserve = share
	}
	return reserve
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func randomToken() (string, error) {
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return hex.EncodeToString(body), nil
}

func processError(err error, lines []string) string {
	start := len(lines) - 8
	if start < 0 {
		start = 0
	}
	text := strings.TrimSpace(strings.Join(lines[start:], "\n"))
	if text != "" {
		return text
	}
	if err != nil {
		return err.Error()
	}
	return "llama.cpp exited without diagnostics"
}
