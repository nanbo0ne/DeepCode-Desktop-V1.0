package localai

import (
	"strings"
	"testing"
)

func TestQwen38CatalogPinsVerifiedUpstreamRevisions(t *testing.T) {
	spec, ok := ModelByID("qwen3.8-27b-iq3-xxs")
	if !ok || len(spec.Artifacts) != 2 {
		t.Fatalf("qwen3.8 catalog = %+v", spec)
	}
	wants := []struct {
		name, sha, hfRevision, modelScopeRevision string
		size                                      int64
	}{
		{"Qwen3.8-27B-UD-IQ3_XXS.gguf", "c0b7c3038681ed2e3040456c1dd45f9858b6c2290bed172c70388a94874f3eee", "4ca720788d1e01f1bff70c033e0d0028fd02e502", "cda69804e9a0bf6546a3adefb63a771c37e50a5d", 10_934_860_704},
		{"mmproj-F16.gguf", "cbb841a9ee0636b2ec172f5bb8df2ea8dfeb01e90fe7c6126581d662a0b4e43e", "4ca720788d1e01f1bff70c033e0d0028fd02e502", "276faa3e9be1b3b57954c1eec3b5e993802a880f", 927_607_488},
	}
	for i, want := range wants {
		artifact := spec.Artifacts[i]
		if artifact.Name != want.name || artifact.Size != want.size || artifact.SHA256 != want.sha {
			t.Fatalf("artifact %d = %+v, want %+v", i, artifact, want)
		}
		if len(artifact.Sources) != 3 || strings.Contains(artifact.Sources[0], "/master/") || strings.Contains(artifact.Sources[1], "/main/") || strings.Contains(artifact.Sources[2], "/main/") {
			t.Fatalf("artifact %s is not fully revision-pinned: %v", artifact.Name, artifact.Sources)
		}
		if !strings.Contains(artifact.Sources[0], "/resolve/"+want.modelScopeRevision+"/") || !strings.Contains(artifact.Sources[1], "/resolve/"+want.hfRevision+"/") || !strings.Contains(artifact.Sources[2], "/resolve/"+want.hfRevision+"/") {
			t.Fatalf("artifact %s revisions = %v", artifact.Name, artifact.Sources)
		}
	}
}
