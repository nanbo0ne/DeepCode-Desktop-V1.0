package main

import (
	"runtime"
	"slices"
	"testing"
)

func TestWorkspaceGitDisablesDaemonSpawns(t *testing.T) {
	cmd := workspaceGit("-C", "repo", "status", "--porcelain=v1")
	want := []string{"git", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-C", "repo", "status", "--porcelain=v1"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	if runtime.GOOS == "windows" && cmd.SysProcAttr == nil {
		t.Fatal("workspaceGit must hide the console window on Windows")
	}
}

func TestWorkspaceGitPathsUseRepositoryPrefix(t *testing.T) {
	for _, tc := range []struct{ prefix, path, want string }{
		{"", "tracked.txt", "tracked.txt"},
		{"sub/", "sub/tracked.txt", "tracked.txt"},
		{"sub/", "outside.txt", ""},
		{"sub/", "submarine/file.txt", ""},
		{"sub/", "sub/deleted.txt", "deleted.txt"},
		{"space dir/", "space dir/ leading.txt", " leading.txt"},
		{"", "../escape.txt", ""},
		{"sub/", "", ""},
	} {
		if got := workspaceRelPathFromGitStatus(tc.prefix, tc.path); got != tc.want {
			t.Errorf("prefix %q path %q: got %q, want %q", tc.prefix, tc.path, got, tc.want)
		}
	}
	if got := normalizeWorkspaceRelPath(".", " leading.txt"); got != " leading.txt" {
		t.Fatalf("workspace file name changed: %q", got)
	}
}
