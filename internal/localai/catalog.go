package localai

const (
	RuntimeVersion = "b10453"
	ProviderID     = "orca-local"
	ProviderBase   = "http://127.0.0.1:39291/v1"
	ProviderKeyEnv = "ORCA_LOCAL_API_KEY"
)

type Artifact struct {
	Name    string   `json:"name"`
	Size    int64    `json:"size"`
	SHA256  string   `json:"sha256"`
	Sources []string `json:"sources"`
}

type ModelSpec struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	License         string     `json:"license"`
	MinVRAMGiB      int        `json:"minVramGiB"`
	RecommendedVRAM int        `json:"recommendedVramGiB"`
	ContextSize     int        `json:"contextSize"`
	ContextFallback []int      `json:"contextFallback"`
	Vision          bool       `json:"vision"`
	ToolUse         bool       `json:"toolUse"`
	Artifacts       []Artifact `json:"artifacts"`
}

type RuntimeSpec struct {
	ID        string     `json:"id"`
	Backend   string     `json:"backend"`
	Version   string     `json:"version"`
	Artifacts []Artifact `json:"artifacts"`
}

func ModelCatalog() []ModelSpec {
	return []ModelSpec{
		{
			ID: "qwen3.8-27b-iq3-xxs", Name: "Qwen3.8 27B · IQ3_XXS", Description: "首选本地视觉与工具模型，适合 16GB 级独显", License: "Apache-2.0",
			MinVRAMGiB: 12, RecommendedVRAM: 16, ContextSize: 25_600, ContextFallback: []int{16_384, 8_192}, Vision: true, ToolUse: true,
			Artifacts: []Artifact{
				modelArtifactAtRevision("Qwen3.8-27B-UD-IQ3_XXS.gguf", 10_934_860_704, "c0b7c3038681ed2e3040456c1dd45f9858b6c2290bed172c70388a94874f3eee", "unsloth/Qwen3.8-27B-GGUF", "4ca720788d1e01f1bff70c033e0d0028fd02e502", "cda69804e9a0bf6546a3adefb63a771c37e50a5d"),
				modelArtifactAtRevision("mmproj-F16.gguf", 927_607_488, "cbb841a9ee0636b2ec172f5bb8df2ea8dfeb01e90fe7c6126581d662a0b4e43e", "unsloth/Qwen3.8-27B-GGUF", "4ca720788d1e01f1bff70c033e0d0028fd02e502", "276faa3e9be1b3b57954c1eec3b5e993802a880f"),
			},
		},
		{
			ID: "qwen3.5-9b-q4-k-m", Name: "Qwen3.5 9B · Q4_K_M", Description: "适合 10–15GB 独显的轻量视觉模型", License: "Apache-2.0",
			MinVRAMGiB: 8, RecommendedVRAM: 10, ContextSize: 16_384, ContextFallback: []int{8_192}, Vision: true, ToolUse: true,
			Artifacts: []Artifact{
				modelArtifact("Qwen3.5-9B-Q4_K_M.gguf", 5_680_522_464, "03b74727a860a56338e042c4420bb3f04b2fec5734175f4cb9fa853daf52b7e8", "unsloth/Qwen3.5-9B-GGUF"),
				modelArtifact("mmproj-F16.gguf", 918_166_080, "f70dc3509053962b0d0d3ee8a7eacebf5d60aa560cad78254ae8698516ae029f", "unsloth/Qwen3.5-9B-GGUF"),
			},
		},
		{
			ID: "qwen3.5-4b-q4-k-m", Name: "Qwen3.5 4B · Q4_K_M", Description: "适合 6–9GB 独显或 CPU 混合加载", License: "Apache-2.0",
			MinVRAMGiB: 4, RecommendedVRAM: 6, ContextSize: 8_192, ContextFallback: []int{4_096}, Vision: true, ToolUse: true,
			Artifacts: []Artifact{
				modelArtifact("Qwen3.5-4B-Q4_K_M.gguf", 2_740_937_888, "00fe7986ff5f6b463e62455821146049db6f9313603938a70800d1fb69ef11a4", "unsloth/Qwen3.5-4B-GGUF"),
				modelArtifact("mmproj-F16.gguf", 672_423_616, "cd88edcf8d031894960bb0c9c5b9b7e1fea6ebee02b9f7ce925a00d12891f864", "unsloth/Qwen3.5-4B-GGUF"),
			},
		},
	}
}

func RuntimeCatalog() []RuntimeSpec {
	base := "https://github.com/ggml-org/llama.cpp/releases/download/" + RuntimeVersion + "/"
	return []RuntimeSpec{
		{ID: "cuda-12.4-x64", Backend: "cuda", Version: RuntimeVersion, Artifacts: []Artifact{
			{Name: "llama-" + RuntimeVersion + "-bin-win-cuda-12.4-x64.zip", Size: 250_790_655, SHA256: "84b863f70a8b4c2873e93385d0b208f24776ecd1b946a2cb6d5cda863d143c3d", Sources: []string{base + "llama-" + RuntimeVersion + "-bin-win-cuda-12.4-x64.zip"}},
			{Name: "cudart-llama-bin-win-cuda-12.4-x64.zip", Size: 391_443_627, SHA256: "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6", Sources: []string{base + "cudart-llama-bin-win-cuda-12.4-x64.zip"}},
		}},
		{ID: "vulkan-x64", Backend: "vulkan", Version: RuntimeVersion, Artifacts: []Artifact{{Name: "llama-" + RuntimeVersion + "-bin-win-vulkan-x64.zip", Size: 34_807_257, SHA256: "123001c3e3918f29420f622431b06dfc5e09ef4d6aff366860d3fd5b9f3418d8", Sources: []string{base + "llama-" + RuntimeVersion + "-bin-win-vulkan-x64.zip"}}}},
		{ID: "cpu-x64", Backend: "cpu", Version: RuntimeVersion, Artifacts: []Artifact{{Name: "llama-" + RuntimeVersion + "-bin-win-cpu-x64.zip", Size: 18_464_078, SHA256: "70c07211d0027305f0be09cd755d79641ebb0bb646590ff3d498c66b22df29b0", Sources: []string{base + "llama-" + RuntimeVersion + "-bin-win-cpu-x64.zip"}}}},
	}
}

func modelArtifact(name string, size int64, sha, repo string) Artifact {
	return modelArtifactAtRevision(name, size, sha, repo, "main", "master")
}

func modelArtifactAtRevision(name string, size int64, sha, repo, hfRevision, modelScopeRevision string) Artifact {
	hfPath := repo + "/resolve/" + hfRevision + "/" + name
	return Artifact{Name: name, Size: size, SHA256: sha, Sources: []string{
		"https://modelscope.cn/models/" + repo + "/resolve/" + modelScopeRevision + "/" + name,
		"https://hf-mirror.com/" + hfPath,
		"https://huggingface.co/" + hfPath,
	}}
}

func ModelByID(id string) (ModelSpec, bool) {
	for _, model := range ModelCatalog() {
		if model.ID == id {
			return model, true
		}
	}
	return ModelSpec{}, false
}

func RuntimeByID(id string) (RuntimeSpec, bool) {
	for _, runtime := range RuntimeCatalog() {
		if runtime.ID == id {
			return runtime, true
		}
	}
	return RuntimeSpec{}, false
}
