package localai

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/fileutil"
)

type TaskKind string
type TaskState string

const (
	TaskModel   TaskKind = "model"
	TaskRuntime TaskKind = "runtime"

	TaskQueued      TaskState = "queued"
	TaskDownloading TaskState = "downloading"
	TaskPaused      TaskState = "paused"
	TaskVerifying   TaskState = "verifying"
	TaskInstalling  TaskState = "installing"
	TaskCompleted   TaskState = "completed"
	TaskFailed      TaskState = "failed"
	TaskCancelled   TaskState = "cancelled"
)

type DownloadTask struct {
	ID              string    `json:"id"`
	Kind            TaskKind  `json:"kind"`
	TargetID        string    `json:"targetId"`
	Label           string    `json:"label"`
	State           TaskState `json:"state"`
	Artifact        string    `json:"artifact,omitempty"`
	DownloadedBytes int64     `json:"downloadedBytes"`
	TotalBytes      int64     `json:"totalBytes"`
	BytesPerSecond  int64     `json:"bytesPerSecond"`
	ETASeconds      int64     `json:"etaSeconds"`
	Source          string    `json:"source,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       int64     `json:"createdAt"`
	UpdatedAt       int64     `json:"updatedAt"`
}

type ModelInstallation struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	ModelPath   string    `json:"modelPath"`
	MMProjPath  string    `json:"mmprojPath,omitempty"`
	Size        int64     `json:"size"`
	InstalledAt time.Time `json:"installedAt"`
	Vision      bool      `json:"vision"`
	ToolUse     bool      `json:"toolUse"`
}

type RuntimeInstallation struct {
	ID          string    `json:"id"`
	Backend     string    `json:"backend"`
	Version     string    `json:"version"`
	Path        string    `json:"path"`
	ServerPath  string    `json:"serverPath"`
	InstalledAt time.Time `json:"installedAt"`
}

type persistedState struct {
	Tasks []DownloadTask `json:"tasks"`
}

type Manager struct {
	mu                sync.Mutex
	root              string
	models            string
	runtimes          string
	downloads         string
	tasks             map[string]*DownloadTask
	generations       map[string]uint64
	cancels           map[string]context.CancelFunc
	cancelGenerations map[string]uint64
	workerDone        map[string]chan struct{}
	emit              func(DownloadTask)
	client            *http.Client
	diskFree          func(string) int64
	resolveArtifacts  func(TaskKind, string) ([]Artifact, bool)
}

func DefaultRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "O.R.C.A", "local-ai")
}

func DefaultModelsDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "O.R.C.A", "models")
}

func NewManager(root string, emit func(DownloadTask)) *Manager {
	return NewManagerWithModels(root, "", emit)
}

func NewManagerWithModels(root, models string, emit func(DownloadTask)) *Manager {
	if strings.TrimSpace(root) == "" {
		root = DefaultRoot()
	}
	if strings.TrimSpace(models) == "" {
		models = DefaultModelsDir()
	}
	m := &Manager{
		root: root, models: models, runtimes: filepath.Join(root, "runtimes"), downloads: filepath.Join(root, "downloads"),
		tasks: map[string]*DownloadTask{}, generations: map[string]uint64{}, cancels: map[string]context.CancelFunc{}, cancelGenerations: map[string]uint64{}, workerDone: map[string]chan struct{}{}, emit: emit,
		client:   &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 8, IdleConnTimeout: 60 * time.Second}},
		diskFree: diskFreeBytes,
	}
	m.resolveArtifacts = func(kind TaskKind, id string) ([]Artifact, bool) {
		if kind == TaskModel {
			spec, ok := ModelByID(id)
			return spec.Artifacts, ok
		}
		spec, ok := RuntimeByID(id)
		return spec.Artifacts, ok
	}
	_ = os.MkdirAll(m.downloads, 0o755)
	m.loadState()
	return m
}

func (m *Manager) Root() string { return m.root }

func (m *Manager) Tasks() []DownloadTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DownloadTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		out = append(out, *task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (m *Manager) StartModelDownload(id string) (DownloadTask, error) {
	spec, ok := ModelByID(id)
	if !ok {
		return DownloadTask{}, fmt.Errorf("unknown local model %q", id)
	}
	return m.start(TaskModel, id, spec.Name, spec.Artifacts)
}

func (m *Manager) StartRuntimeInstall(id string) (DownloadTask, error) {
	spec, ok := RuntimeByID(id)
	if !ok {
		return DownloadTask{}, fmt.Errorf("unknown local runtime %q", id)
	}
	return m.start(TaskRuntime, id, "llama.cpp "+spec.Version+" · "+spec.Backend, spec.Artifacts)
}

func (m *Manager) start(kind TaskKind, targetID, label string, artifacts []Artifact) (DownloadTask, error) {
	if len(artifacts) == 0 {
		return DownloadTask{}, fmt.Errorf("%s has no downloadable artifacts", targetID)
	}
	var total int64
	for _, artifact := range artifacts {
		if artifact.Size <= 0 || len(artifact.SHA256) != 64 || len(artifact.Sources) == 0 {
			return DownloadTask{}, fmt.Errorf("%s has an incomplete signed manifest", artifact.Name)
		}
		total += artifact.Size
	}
	if err := m.ensureDiskSpace(total + 2<<30); err != nil {
		return DownloadTask{}, err
	}
	now := time.Now().UnixMilli()
	task := &DownloadTask{ID: fmt.Sprintf("%s-%s-%d", kind, targetID, now), Kind: kind, TargetID: targetID, Label: label, State: TaskQueued, TotalBytes: total, CreatedAt: now, UpdatedAt: now}
	m.mu.Lock()
	for _, existing := range m.tasks {
		if existing.Kind == kind && existing.TargetID == targetID && (existing.State == TaskQueued || existing.State == TaskDownloading || existing.State == TaskPaused || existing.State == TaskVerifying || existing.State == TaskInstalling) {
			copy := *existing
			m.mu.Unlock()
			return copy, nil
		}
	}
	m.tasks[task.ID] = task
	m.generations[task.ID] = 1
	m.saveStateLocked()
	snapshot := *task
	m.mu.Unlock()
	m.launch(task.ID, 1)
	return snapshot, nil
}

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		return fmt.Errorf("download task %q not found", id)
	}
	if task.State != TaskDownloading && task.State != TaskQueued {
		m.mu.Unlock()
		return nil
	}
	task.State, task.UpdatedAt = TaskPaused, time.Now().UnixMilli()
	m.generations[id]++
	cancel := m.cancels[id]
	m.saveStateLocked()
	copy := *task
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.publish(copy)
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		return fmt.Errorf("download task %q not found", id)
	}
	if task.State != TaskPaused && task.State != TaskFailed {
		m.mu.Unlock()
		return nil
	}
	task.State, task.Error, task.UpdatedAt = TaskQueued, "", time.Now().UnixMilli()
	m.generations[id]++
	generation := m.generations[id]
	m.saveStateLocked()
	m.mu.Unlock()
	m.launch(id, generation)
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		return fmt.Errorf("download task %q not found", id)
	}
	if task.State != TaskQueued && task.State != TaskDownloading && task.State != TaskPaused && task.State != TaskVerifying && task.State != TaskInstalling {
		m.mu.Unlock()
		return nil
	}
	task.State, task.UpdatedAt = TaskCancelled, time.Now().UnixMilli()
	m.generations[id]++
	cancel := m.cancels[id]
	m.saveStateLocked()
	copy := *task
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.publish(copy)
	return nil
}

func (m *Manager) launch(id string, generation uint64) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.mu.Lock()
	if m.generations[id] != generation {
		m.mu.Unlock()
		cancel()
		close(done)
		return
	}
	waitFor := m.workerDone[id]
	m.cancels[id] = cancel
	m.cancelGenerations[id] = generation
	m.workerDone[id] = done
	m.mu.Unlock()
	go func() {
		var err error
		defer func() {
			close(done)
			m.finishWorker(id, generation, done, err)
		}()
		if waitFor != nil {
			select {
			case <-waitFor:
			case <-ctx.Done():
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		err = m.run(ctx, id, generation)
	}()
}

func (m *Manager) finishWorker(id string, generation uint64, done chan struct{}, err error) {
	m.mu.Lock()
	if m.workerDone[id] != done {
		m.mu.Unlock()
		return
	}
	delete(m.workerDone, id)
	if m.cancelGenerations[id] == generation {
		delete(m.cancels, id)
		delete(m.cancelGenerations, id)
	}
	task := m.tasks[id]
	var publishTask DownloadTask
	shouldPublish := false
	if task != nil && m.generations[id] == generation && err != nil && task.State != TaskPaused && task.State != TaskCancelled {
		task.State, task.Error, task.UpdatedAt = TaskFailed, err.Error(), time.Now().UnixMilli()
		publishTask = *task
		shouldPublish = true
	}
	if task != nil && m.generations[id] == generation {
		m.saveStateLocked()
	}
	m.mu.Unlock()
	if shouldPublish {
		m.publish(publishTask)
	}
}

var errRunSuperseded = errors.New("download run superseded")

func (m *Manager) run(ctx context.Context, id string, generation uint64) error {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		return fmt.Errorf("task disappeared")
	}
	if m.generations[id] != generation || task.State != TaskQueued {
		m.mu.Unlock()
		return nil
	}
	task.State, task.UpdatedAt = TaskDownloading, time.Now().UnixMilli()
	kind, targetID := task.Kind, task.TargetID
	m.saveStateLocked()
	m.mu.Unlock()

	artifacts, ok := m.resolveArtifacts(kind, targetID)
	if !ok {
		return fmt.Errorf("unknown download target %q", targetID)
	}
	taskDir := filepath.Join(m.downloads, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return err
	}
	var completed int64
	for _, artifact := range artifacts {
		part := filepath.Join(taskDir, artifact.Name+".part")
		if ok, _ := fileMatches(part, artifact); ok {
			completed += artifact.Size
			continue
		}
		if err := m.downloadArtifactRun(ctx, id, generation, artifact, part, completed); err != nil {
			return err
		}
		completed += artifact.Size
	}
	if !m.transitionTask(id, generation, TaskDownloading, TaskVerifying, "") {
		return errRunSuperseded
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ok, err := fileMatches(filepath.Join(taskDir, artifact.Name+".part"), artifact); !ok {
			if err == nil {
				err = fmt.Errorf("checksum mismatch")
			}
			return fmt.Errorf("verify %s: %w", artifact.Name, err)
		}
	}
	if !m.transitionTask(id, generation, TaskVerifying, TaskInstalling, "") {
		return errRunSuperseded
	}
	if kind == TaskModel {
		if err := m.installModel(ctx, id, generation, targetID, taskDir, artifacts); err != nil {
			return err
		}
	} else if err := m.installRuntime(ctx, id, generation, targetID, taskDir, artifacts); err != nil {
		return err
	}
	return nil
}

func (m *Manager) downloadArtifact(ctx context.Context, id string, artifact Artifact, part string, completed int64) error {
	return m.downloadArtifactRun(ctx, id, 0, artifact, part, completed)
}

func (m *Manager) downloadArtifactRun(ctx context.Context, id string, generation uint64, artifact Artifact, part string, completed int64) error {
	var lastErr error
	for _, source := range artifact.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.downloadFromRun(ctx, id, generation, source, artifact, part, completed); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("all mirrors failed for %s: %w", artifact.Name, lastErr)
}

func (m *Manager) downloadFrom(ctx context.Context, id, source string, artifact Artifact, part string, completed int64) error {
	return m.downloadFromRun(ctx, id, 0, source, artifact, part, completed)
}

func (m *Manager) downloadFromRun(ctx context.Context, id string, generation uint64, source string, artifact Artifact, part string, completed int64) error {
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return err
	}
	offset := int64(0)
	if info, err := os.Stat(part); err == nil {
		offset = info.Size()
		if offset >= artifact.Size {
			_ = os.Remove(part)
			offset = 0
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Orca/3.0 local-model-manager")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 256*1024)
	lastTick, lastBytes := time.Now(), offset
	written := offset
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
		}
		now := time.Now()
		if now.Sub(lastTick) >= 250*time.Millisecond || readErr == io.EOF {
			deltaSec := now.Sub(lastTick).Seconds()
			speed := int64(0)
			if deltaSec > 0 {
				speed = int64(float64(written-lastBytes) / deltaSec)
			}
			m.updateProgressForRun(id, generation, artifact.Name, source, completed+written, speed)
			lastTick, lastBytes = now, written
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if written != artifact.Size {
		return fmt.Errorf("downloaded %d bytes, expected %d", written, artifact.Size)
	}
	return f.Sync()
}

func (m *Manager) updateProgress(id, artifact, source string, downloaded, speed int64) {
	m.updateProgressForRun(id, 0, artifact, source, downloaded, speed)
}

func (m *Manager) updateProgressForRun(id string, generation uint64, artifact, source string, downloaded, speed int64) {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		return
	}
	if generation != 0 && (m.generations[id] != generation || task.State != TaskDownloading) {
		m.mu.Unlock()
		return
	}
	task.Artifact, task.Source, task.DownloadedBytes, task.BytesPerSecond, task.UpdatedAt = artifact, source, downloaded, speed, time.Now().UnixMilli()
	remaining := task.TotalBytes - downloaded
	if speed > 0 && remaining > 0 {
		task.ETASeconds = remaining / speed
	} else {
		task.ETASeconds = 0
	}
	copy := *task
	m.mu.Unlock()
	m.publish(copy)
}

func (m *Manager) transitionTask(id string, generation uint64, expected, state TaskState, errText string) bool {
	m.mu.Lock()
	if task := m.tasks[id]; task != nil && m.generations[id] == generation && task.State == expected {
		task.State, task.Error, task.UpdatedAt = state, errText, time.Now().UnixMilli()
		copy := *task
		m.saveStateLocked()
		m.mu.Unlock()
		m.publish(copy)
		return true
	}
	m.mu.Unlock()
	return false
}

func (m *Manager) installModel(ctx context.Context, taskID string, generation uint64, id, taskDir string, artifacts []Artifact) error {
	target := filepath.Join(m.models, id)
	stage := target + ".installing"
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
		source := filepath.Join(taskDir, artifact.Name+".part")
		dest := filepath.Join(stage, artifact.Name)
		if err := copyFile(source, dest); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
	}
	spec, _ := ModelByID(id)
	if err := writeJSONAtomic(filepath.Join(stage, "installation.json"), ModelInstallation{ID: id, Name: spec.Name, Path: target, InstalledAt: time.Now().UTC(), Vision: spec.Vision, ToolUse: spec.ToolUse}); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}

	// Commit the prepared directory only while this generation is still the
	// installing run. A cancellation that wins this lock leaves both the old
	// model and the download parts untouched.
	m.mu.Lock()
	if !m.currentRunLocked(taskID, generation, TaskInstalling) || ctx.Err() != nil {
		m.mu.Unlock()
		_ = os.RemoveAll(stage)
		return errRunSuperseded
	}
	backup := target + ".previous-" + fmt.Sprint(time.Now().UnixNano())
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			m.mu.Unlock()
			_ = os.RemoveAll(stage)
			return err
		}
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.Rename(backup, target)
		m.mu.Unlock()
		_ = os.RemoveAll(stage)
		return err
	}
	completed, ok := m.markCompletedLocked(taskID, generation)
	m.mu.Unlock()
	_ = os.RemoveAll(backup)
	if !ok {
		return errRunSuperseded
	}
	m.publish(completed)
	return nil
}

func (m *Manager) installRuntime(ctx context.Context, taskID string, generation uint64, id, taskDir string, artifacts []Artifact) error {
	target := filepath.Join(m.runtimes, id)
	stage := target + ".installing"
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
		if err := extractZip(filepath.Join(taskDir, artifact.Name+".part"), stage); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
	}
	server, err := findFile(stage, "llama-server.exe")
	if err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	rel, _ := filepath.Rel(stage, server)
	spec, _ := RuntimeByID(id)
	if err := writeJSONAtomic(filepath.Join(stage, "installation.json"), RuntimeInstallation{ID: id, Backend: spec.Backend, Version: spec.Version, Path: target, ServerPath: filepath.Join(target, rel), InstalledAt: time.Now().UTC()}); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	m.mu.Lock()
	if !m.currentRunLocked(taskID, generation, TaskInstalling) || ctx.Err() != nil {
		m.mu.Unlock()
		_ = os.RemoveAll(stage)
		return errRunSuperseded
	}
	backup := target + ".previous-" + fmt.Sprint(time.Now().UnixNano())
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			m.mu.Unlock()
			_ = os.RemoveAll(stage)
			return err
		}
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.Rename(backup, target)
		m.mu.Unlock()
		_ = os.RemoveAll(stage)
		return err
	}
	completed, ok := m.markCompletedLocked(taskID, generation)
	m.mu.Unlock()
	_ = os.RemoveAll(backup)
	if !ok {
		return errRunSuperseded
	}
	m.publish(completed)
	return nil
}

func (m *Manager) currentRunLocked(id string, generation uint64, state TaskState) bool {
	task := m.tasks[id]
	return task != nil && m.generations[id] == generation && task.State == state
}

func (m *Manager) markCompletedLocked(id string, generation uint64) (DownloadTask, bool) {
	task := m.tasks[id]
	if task == nil || m.generations[id] != generation || task.State != TaskInstalling {
		return DownloadTask{}, false
	}
	task.State, task.Error, task.DownloadedBytes, task.BytesPerSecond, task.ETASeconds, task.UpdatedAt = TaskCompleted, "", task.TotalBytes, 0, 0, time.Now().UnixMilli()
	m.saveStateLocked()
	return *task, true
}

func (m *Manager) InstalledModels() []ModelInstallation {
	entries, _ := os.ReadDir(m.models)
	var out []ModelInstallation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var item ModelInstallation
		if readJSON(filepath.Join(m.models, entry.Name(), "installation.json"), &item) != nil {
			continue
		}
		spec, ok := ModelByID(item.ID)
		if !ok {
			continue
		}
		valid := true
		for _, artifact := range spec.Artifacts {
			if ok, _ := fileMatches(filepath.Join(item.Path, artifact.Name), artifact); !ok {
				valid = false
				break
			}
			item.Size += artifact.Size
			if strings.Contains(strings.ToLower(artifact.Name), "mmproj") {
				item.MMProjPath = filepath.Join(item.Path, artifact.Name)
			} else {
				item.ModelPath = filepath.Join(item.Path, artifact.Name)
			}
		}
		if valid {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) InstalledRuntime() (RuntimeInstallation, bool) {
	entries, _ := os.ReadDir(m.runtimes)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var item RuntimeInstallation
		if readJSON(filepath.Join(m.runtimes, entry.Name(), "installation.json"), &item) == nil {
			if info, err := os.Stat(item.ServerPath); err == nil && !info.IsDir() {
				return item, true
			}
		}
	}
	return RuntimeInstallation{}, false
}

func (m *Manager) DeleteModel(id string) error {
	if _, ok := ModelByID(id); !ok {
		return fmt.Errorf("unknown local model %q", id)
	}
	return removeOwnedDir(m.models, filepath.Join(m.models, id))
}

func (m *Manager) UninstallRuntime() error {
	if err := removeOwnedDir(m.root, m.runtimes); err != nil {
		return err
	}
	return os.MkdirAll(m.runtimes, 0o755)
}

func (m *Manager) ensureDiskSpace(required int64) error {
	free := m.diskFree
	if free == nil {
		free = diskFreeBytes
	}
	paths := []string{m.downloads}
	if strings.TrimSpace(m.models) != "" {
		paths = append(paths, m.models)
	}
	return ensureDiskSpaceForPaths(required, paths, free, func(path string) string {
		volume := strings.ToLower(filepath.VolumeName(filepath.Clean(path)))
		if volume != "" {
			return volume
		}
		return string(filepath.Separator)
	})
}

func ensureDiskSpaceForPaths(required int64, paths []string, free func(string) int64, volume func(string) string) error {
	if required <= 0 || free == nil || volume == nil {
		return nil
	}
	checked := map[string]string{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		key := volume(path)
		if _, seen := checked[key]; seen {
			continue
		}
		checked[key] = path
		available := free(path)
		if available > 0 && available < required {
			return fmt.Errorf("insufficient disk space on %s: need %.1f GB including safety margin, have %.1f GB", path, float64(required)/(1<<30), float64(available)/(1<<30))
		}
	}
	return nil
}

func (m *Manager) loadState() {
	var state persistedState
	if readJSON(filepath.Join(m.root, "downloads.json"), &state) != nil {
		return
	}
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.State == TaskDownloading || task.State == TaskQueued || task.State == TaskVerifying || task.State == TaskInstalling {
			task.State = TaskPaused
			task.Error = "应用已重启，可继续下载"
		}
		copy := task
		m.tasks[copy.ID] = &copy
	}
}

func (m *Manager) saveStateLocked() {
	state := persistedState{Tasks: make([]DownloadTask, 0, len(m.tasks))}
	for _, task := range m.tasks {
		state.Tasks = append(state.Tasks, *task)
	}
	_ = writeJSONAtomic(filepath.Join(m.root, "downloads.json"), state)
}

func (m *Manager) publish(task DownloadTask) {
	if m.emit != nil {
		m.emit(task)
	}
}

func fileMatches(path string, artifact Artifact) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Size() != artifact.Size {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), artifact.SHA256), nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func promoteFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.Remove(target)
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target + ".tmp")
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(target+".tmp", target); err != nil {
		return err
	}
	return os.Remove(source)
}

func extractZip(path, target string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	root, _ := filepath.Abs(target)
	for _, file := range r.File {
		dest := filepath.Join(target, filepath.FromSlash(file.Name))
		abs, _ := filepath.Abs(dest)
		if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry escapes install root: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		_ = in.Close()
		_ = out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			found = path
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("%s not found in runtime package", name)
	}
	return found, nil
}

func removeOwnedDir(root, target string) error {
	rootAbs, _ := filepath.Abs(root)
	targetAbs, _ := filepath.Abs(target)
	if targetAbs == rootAbs || !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to remove path outside local AI root")
	}
	return os.RemoveAll(targetAbs)
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func readJSON(path string, target any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}
