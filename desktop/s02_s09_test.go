package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const s02TinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestAttachmentBridgeUsesEachWorkspaceWithoutChangingCWD(t *testing.T) {
	launchRoot := t.TempDir()
	t.Chdir(launchRoot)
	rootA := filepath.Join(t.TempDir(), "project-a")
	rootB := filepath.Join(t.TempDir(), "project-b")
	appA := &App{tabs: map[string]*WorkspaceTab{
		"a": {ID: "a", WorkspaceRoot: rootA},
	}, activeTabID: "a"}
	appB := &App{tabs: map[string]*WorkspaceTab{
		"b": {ID: "b", WorkspaceRoot: rootB},
	}, activeTabID: "b"}

	const rounds = 24
	dataURL := "data:image/png;base64," + s02TinyPNG
	paths := make(chan string, rounds*2)
	errs := make(chan error, rounds*2)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, app := range []*App{appA, appB} {
			wg.Add(1)
			go func(app *App) {
				defer wg.Done()
				path, err := app.SavePastedImage(dataURL)
				if err != nil {
					errs <- err
					return
				}
				paths <- filepath.Join(app.activeWorkspaceRoot(), filepath.FromSlash(path))
			}(app)
		}
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("attachment bridge output %q is missing: %v", path, err)
		}
	}
	if got, _ := os.Getwd(); filepath.Clean(got) != filepath.Clean(launchRoot) {
		t.Fatalf("attachment bridge changed cwd to %q", got)
	}
	for _, root := range []string{rootA, rootB} {
		matches, err := filepath.Glob(filepath.Join(root, ".orca", "attachments", "*.png"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("workspace %q received no attachments", root)
		}
	}
}

func TestSubmitReturnsWorkspaceNotReadyErrorWithoutController(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{
		"starting": {ID: "starting"},
		"failed":   {ID: "failed", StartupErr: "provider unavailable"},
	}}

	if err := app.SubmitToTab("starting", "draft"); err == nil || !strings.Contains(err.Error(), "still starting") {
		t.Fatalf("SubmitToTab starting error = %v, want not-ready error", err)
	}
	if err := app.SubmitDisplayToTab("failed", "draft", "expanded draft"); err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("SubmitDisplayToTab failed error = %v, want startup error", err)
	}
}

func TestSubmitRejectsReadOnlyHistory(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{"history": {ID: "history", ReadOnly: true}}, activeTabID: "history"}
	for _, send := range []func() error{
		func() error { return app.Submit("draft") },
		func() error { return app.SubmitDisplay("draft", "expanded draft") },
	} {
		if err := send(); err == nil {
			t.Fatal("read-only history must reject a send, not acknowledge and clear its draft")
		}
	}
}
