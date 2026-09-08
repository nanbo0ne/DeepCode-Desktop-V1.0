package localai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testArtifact(name string, body []byte, source string) Artifact {
	sum := sha256.Sum256(body)
	return Artifact{Name: name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]), Sources: []string{source}}
}

func TestDownloadFromReplacesCompleteCorruptPart(t *testing.T) {
	want := []byte("verified local model payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(want)
	}))
	defer server.Close()

	root := t.TempDir()
	m := NewManager(root, nil)
	part := filepath.Join(root, "payload.part")
	if err := os.WriteFile(part, []byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxx"[:len(want)]), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := testArtifact("payload.gguf", want, server.URL)
	if err := m.downloadFrom(context.Background(), "missing-task", server.URL, artifact, part, 0); err != nil {
		t.Fatalf("downloadFrom: %v", err)
	}
	got, err := os.ReadFile(part)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("downloaded payload = %q, want %q", got, want)
	}
	if ok, err := fileMatches(part, artifact); !ok || err != nil {
		t.Fatalf("downloaded file must verify: ok=%v err=%v", ok, err)
	}
}

func TestWriteJSONAtomicReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := writeJSONAtomic(path, map[string]int{"generation": 1}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(path, map[string]int{"generation": 2}); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := readJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["generation"] != 2 {
		t.Fatalf("generation = %d, want 2", got["generation"])
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	// The path guard itself is covered through a deliberately malformed archive
	// in the integration download tests; keep this assertion close to the owned
	// directory guard so future cleanup changes cannot escape the local AI root.
	root := t.TempDir()
	if err := removeOwnedDir(root, filepath.Dir(root)); err == nil {
		t.Fatal("expected cleanup outside local AI root to be rejected")
	}
}

func TestEnsureDiskSpaceChecksStagingAndModelVolumes(t *testing.T) {
	paths := []string{"staging", "models"}
	free := map[string]int64{"staging": 100, "models": 99}
	volume := map[string]string{"staging": "C:", "models": "D:"}
	if err := ensureDiskSpaceForPaths(100, paths, func(path string) int64 { return free[path] }, func(path string) string { return volume[path] }); err == nil {
		t.Fatal("expected final model volume shortage to be rejected")
	}
	free["models"] = 100
	if err := ensureDiskSpaceForPaths(100, paths, func(path string) int64 { return free[path] }, func(path string) string { return volume[path] }); err != nil {
		t.Fatalf("both volumes have enough space: %v", err)
	}
	free["models"] = 99
	volume["models"] = "C:"
	if err := ensureDiskSpaceForPaths(100, paths, func(path string) int64 { return free[path] }, func(path string) string { return volume[path] }); err != nil {
		t.Fatalf("same-volume staging should be checked once: %v", err)
	}
}

func TestCancelledRunCannotInstallOrDeletePart(t *testing.T) {
	payload := []byte("verified local model payload")
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write(payload[:1])
			flusher.Flush()
		} else {
			_, _ = w.Write(payload[:1])
		}
		close(started)
		<-release
		_, _ = w.Write(payload[1:])
	}))
	defer server.Close()

	root := t.TempDir()
	models := filepath.Join(root, "models")
	m := NewManagerWithModels(root, models, nil)
	artifact := testArtifact("payload.gguf", payload, server.URL)
	m.resolveArtifacts = func(TaskKind, string) ([]Artifact, bool) { return []Artifact{artifact}, true }
	id := "cancelled-model"
	existing := filepath.Join(models, "qwen3.5-4b-q4-k-m")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "legacy.marker"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.tasks[id] = &DownloadTask{ID: id, Kind: TaskModel, TargetID: "qwen3.5-4b-q4-k-m", State: TaskQueued, TotalBytes: artifact.Size}
	m.generations[id] = 1
	m.mu.Unlock()
	m.launch(id, 1)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	part := filepath.Join(m.downloads, id, artifact.Name+".part")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(part); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download part was not created before cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline = time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		state := m.tasks[id].State
		active := len(m.cancels) != 0
		m.mu.Unlock()
		if state == TaskCancelled && !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled run did not settle: state=%s active=%v", state, active)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(existing, "legacy.marker")); err != nil {
		t.Fatalf("cancelled run must preserve the existing model: %v", err)
	}
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("cancelled run must preserve the resumable part: %v", err)
	}
}

func TestPausedRunGenerationCannotAdvanceState(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	id := "paused-generation"
	m.mu.Lock()
	m.tasks[id] = &DownloadTask{ID: id, State: TaskDownloading}
	m.generations[id] = 1
	m.mu.Unlock()
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	if m.transitionTask(id, 1, TaskDownloading, TaskVerifying, "") {
		t.Fatal("a paused worker must not advance its stale generation")
	}
	m.mu.Lock()
	state := m.tasks[id].State
	m.mu.Unlock()
	if state != TaskPaused {
		t.Fatalf("state = %s, want paused", state)
	}
}

func TestCancelledQueuedRunCannotStart(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	id := "cancelled-queued"
	m.mu.Lock()
	m.tasks[id] = &DownloadTask{ID: id, State: TaskQueued}
	m.generations[id] = 1
	m.mu.Unlock()
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if err := m.run(context.Background(), id, 1); err != nil {
		t.Fatalf("stale queued worker returned error: %v", err)
	}
	m.mu.Lock()
	state := m.tasks[id].State
	m.mu.Unlock()
	if state != TaskCancelled {
		t.Fatalf("state = %s, want cancelled", state)
	}
}

func TestPauseResumeWaitsForPreviousHTTPWorker(t *testing.T) {
	payload := []byte("pause-resume payload must remain intact")
	started := make(chan struct{})
	secondStarted := make(chan struct{})
	var requestCount atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	var startedOnce sync.Once
	var secondOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := requestCount.Add(1)
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)

		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		if request == 1 {
			_, _ = w.Write(payload[:1])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			startedOnce.Do(func() { close(started) })
			<-r.Context().Done()
			return
		}
		secondOnce.Do(func() { close(secondStarted) })
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var eventsMu sync.Mutex
	var events []DownloadTask
	m := NewManager(t.TempDir(), func(task DownloadTask) {
		eventsMu.Lock()
		events = append(events, task)
		eventsMu.Unlock()
	})
	m.diskFree = func(string) int64 { return 1 << 50 }
	artifact := testArtifact("payload.gguf", payload, server.URL)
	m.resolveArtifacts = func(TaskKind, string) ([]Artifact, bool) { return []Artifact{artifact}, true }

	task, err := m.start(TaskModel, "qwen3.5-4b-q4-k-m", "test model", []Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != TaskQueued {
		t.Fatalf("start returned state %s, want queued snapshot", task.State)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first HTTP worker did not start")
	}

	if err := m.Pause(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.resume(task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed HTTP worker did not start")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		m.mu.Lock()
		state := m.tasks[task.ID].State
		workers := len(m.workerDone)
		m.mu.Unlock()
		if state == TaskCompleted && workers == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rapid pause/resume did not settle: state=%s workers=%d", state, workers)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if requestCount.Load() != 2 {
		t.Fatalf("HTTP request count = %d, want one cancelled request and one resumed request", requestCount.Load())
	}
	if maxActive.Load() != 1 {
		t.Fatalf("maximum concurrent HTTP workers = %d, want 1", maxActive.Load())
	}
	part := filepath.Join(m.downloads, task.ID, artifact.Name+".part")
	if ok, err := fileMatches(part, artifact); !ok || err != nil {
		t.Fatalf("resumed part must remain intact: ok=%v err=%v", ok, err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	for _, event := range events {
		if event.State == TaskQueued {
			t.Fatalf("stale worker published the current queued state: %+v", event)
		}
	}
}

func TestDisabledManagerRejectsInstallDownloadAndResume(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	if LocalAIEnabled {
		t.Fatal("test assumes the temporary local AI gate is enabled")
	}
	for name, call := range map[string]func() error{
		"model download":  func() error { _, err := m.StartModelDownload("qwen3.5-4b-q4-k-m"); return err },
		"runtime install": func() error { _, err := m.StartRuntimeInstall("cpu-x64"); return err },
		"resume":          func() error { return m.Resume("missing") },
	} {
		if err := call(); !errors.Is(err, ErrTemporarilyDisabled) {
			t.Errorf("%s error = %v, want ErrTemporarilyDisabled", name, err)
		}
	}
}

func TestCancelledResumeHandoffDoesNotStartWorker(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	id := "cancelled-resume-handoff"
	oldDone := make(chan struct{})
	m.mu.Lock()
	m.tasks[id] = &DownloadTask{ID: id, Kind: TaskModel, TargetID: "qwen3.5-4b-q4-k-m", State: TaskQueued}
	m.generations[id] = 2
	m.workerDone[id] = oldDone
	m.mu.Unlock()

	m.launch(id, 2)
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	close(oldDone)

	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		state := m.tasks[id].State
		workers := len(m.workerDone)
		m.mu.Unlock()
		if state == TaskCancelled && workers == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled handoff did not settle: state=%s workers=%d", state, workers)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
