package localai

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestDisabledRuntimeRejectsStart(t *testing.T) {
	s := NewRuntimeServer(nil)
	_, err := s.Start(context.Background(), RuntimeInstallation{ServerPath: `C:\llama-server.exe`}, ModelInstallation{ID: "model", ModelPath: `C:\model.gguf`}, ModelSpec{ID: "model", ContextSize: 8192}, HardwareProfile{}, 0, 0)
	if !errors.Is(err, ErrTemporarilyDisabled) {
		t.Fatalf("runtime start error = %v, want ErrTemporarilyDisabled", err)
	}
	if status := s.Status(); status.State != RuntimeStopped || status.Supported {
		t.Fatalf("disabled runtime status = %+v", status)
	}
}

func TestLoadProfilesUseSafeFallbackLadder(t *testing.T) {
	profiles := loadProfiles(ModelSpec{ContextSize: 25_600, ContextFallback: []int{16_384, 8_192}}, 12, 2048)
	if len(profiles) != 4 {
		t.Fatalf("profiles = %d, want 4", len(profiles))
	}
	contexts := []int{profiles[0].ContextSize, profiles[1].ContextSize, profiles[2].ContextSize, profiles[3].ContextSize}
	if !slices.Equal(contexts, []int{25_600, 16_384, 8_192, 8_192}) {
		t.Fatalf("contexts = %v", contexts)
	}
	if profiles[0].GPULayers != "auto" || profiles[3].GPULayers == "auto" {
		t.Fatalf("unexpected GPU fallback: first=%q last=%q", profiles[0].GPULayers, profiles[3].GPULayers)
	}
	if profiles[2].UBatchSize != 256 || profiles[3].BatchSize != 1024 {
		t.Fatalf("low-memory batches not reduced: %+v %+v", profiles[2], profiles[3])
	}
}

func TestServerArgsAreLoopbackAuthenticatedAndSingleSlot(t *testing.T) {
	model := ModelInstallation{ID: "qwen", ModelPath: `C:\models\qwen.gguf`, MMProjPath: `C:\models\mmproj.gguf`}
	profile := LoadProfile{ContextSize: 8192, BatchSize: 1024, UBatchSize: 256, GPULayers: "auto", Threads: 8, VRAMReserve: 2048}
	args := serverArgs(model, profile, 41234, "secret")
	joined := " "
	for _, arg := range args {
		joined += arg + " "
	}
	for _, required := range []string{"--host 127.0.0.1", "--port 41234", "--api-key secret", "--parallel 1", "--kv-unified", "--cache-type-k q8_0", "--cache-type-v q8_0", "--mmproj C:\\models\\mmproj.gguf"} {
		if !containsArgument(joined, required) {
			t.Fatalf("args missing %q: %s", required, joined)
		}
	}
}

func TestRecommendedThreadsKeepsDesktopResponsive(t *testing.T) {
	for logical, want := range map[int]int{2: 4, 8: 6, 16: 12, 64: 12} {
		if got := recommendedThreads(logical); got != want {
			t.Fatalf("recommendedThreads(%d) = %d, want %d", logical, got, want)
		}
	}
}

func containsArgument(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
