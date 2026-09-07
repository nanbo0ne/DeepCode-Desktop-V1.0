package control

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestExplicitAttachmentRootsRemainIsolatedConcurrently(t *testing.T) {
	launchRoot := t.TempDir()
	t.Chdir(launchRoot)
	rootA := filepath.Join(t.TempDir(), "project-a")
	rootB := filepath.Join(t.TempDir(), "project-b")
	const rounds = 32

	dataURL := "data:image/png;base64," + tinyPNG
	type result struct {
		root string
		path string
		err  error
	}
	results := make(chan result, rounds*2)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, root := range []string{rootA, rootB} {
			wg.Add(1)
			go func(root string) {
				defer wg.Done()
				path, err := SaveImageDataURLAt(root, dataURL)
				results <- result{root: root, path: path, err: err}
			}(root)
		}
	}
	wg.Wait()
	close(results)

	for got := range results {
		if got.err != nil {
			t.Fatalf("SaveImageDataURLAt(%q): %v", got.root, got.err)
		}
		if !strings.HasPrefix(got.path, ".orca/attachments/") || !strings.HasSuffix(got.path, ".png") {
			t.Fatalf("relative attachment path = %q", got.path)
		}
		absolute := filepath.Join(got.root, filepath.FromSlash(got.path))
		if _, err := os.Stat(absolute); err != nil {
			t.Fatalf("attachment %q was not written under %q: %v", got.path, got.root, err)
		}
		if _, err := ImageDataURLAt(got.path, got.root); err != nil {
			t.Fatalf("ImageDataURLAt(%q, %q): %v", got.path, got.root, err)
		}
	}
	if got, _ := os.Getwd(); filepath.Clean(got) != filepath.Clean(launchRoot) {
		t.Fatalf("explicit attachment operations changed cwd to %q", got)
	}
}

func TestExplicitAttachmentRootPreservesSnapshotAndFileSemantics(t *testing.T) {
	launchRoot := t.TempDir()
	t.Chdir(launchRoot)
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	source := filepath.Join(t.TempDir(), "source.png")
	if err := os.WriteFile(source, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := SnapshotImageFile(source, workspaceRoot)
	if err != nil {
		t.Fatalf("SnapshotImageFile: %v", err)
	}
	if !strings.HasPrefix(path, ".orca/attachments/") || !strings.HasSuffix(path, ".png") {
		t.Fatalf("snapshot path = %q", path)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, filepath.FromSlash(path))); err != nil {
		t.Fatalf("snapshot not under explicit workspace root: %v", err)
	}
	if _, err := ImageDataURLAt(path, workspaceRoot); err != nil {
		t.Fatalf("ImageDataURLAt snapshot: %v", err)
	}
	if got, _ := os.Getwd(); filepath.Clean(got) != filepath.Clean(launchRoot) {
		t.Fatalf("snapshot operations changed cwd to %q", got)
	}
}
