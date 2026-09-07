package localai

import "strings"

type GPUAdapter struct {
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	Backend       string `json:"backend"`
	DedicatedMiB  int64  `json:"dedicatedMiB"`
	AvailableMiB  int64  `json:"availableMiB"`
	DriverVersion string `json:"driverVersion,omitempty"`
}

type HardwareProfile struct {
	Platform           string       `json:"platform"`
	Supported          bool         `json:"supported"`
	CPULogicalCores    int          `json:"cpuLogicalCores"`
	MemoryTotalMiB     int64        `json:"memoryTotalMiB"`
	MemoryFreeMiB      int64        `json:"memoryFreeMiB"`
	DiskFreeBytes      int64        `json:"diskFreeBytes"`
	GPUs               []GPUAdapter `json:"gpus"`
	GPUDetectionFailed bool         `json:"gpuDetectionFailed,omitempty"`
	RecommendedModel   string       `json:"recommendedModel,omitempty"`
	RecommendedRuntime string       `json:"recommendedRuntime,omitempty"`
	Warning            string       `json:"warning,omitempty"`
}

func applyRecommendation(profile *HardwareProfile) {
	if profile == nil || !profile.Supported {
		return
	}
	profile.GPUs = nonNilGPUs(profile.GPUs)
	best := bestGPU(profile.GPUs)
	switch {
	case best.DedicatedMiB >= 16*1024:
		profile.RecommendedModel = "qwen3.8-27b-iq3-xxs"
	case best.DedicatedMiB >= 10*1024:
		profile.RecommendedModel = "qwen3.5-9b-q4-k-m"
	case best.DedicatedMiB >= 6*1024:
		profile.RecommendedModel = "qwen3.5-4b-q4-k-m"
	default:
		profile.Warning = "当前显存低于自动推荐阈值，可手动选择 CPU/混合加载。"
	}
	if best.Backend == "cuda" {
		profile.RecommendedRuntime = "cuda-12.4-x64"
	} else if best.Name != "" {
		profile.RecommendedRuntime = "vulkan-x64"
	} else {
		profile.RecommendedRuntime = "cpu-x64"
	}
	if best.DedicatedMiB >= 16*1024 && best.AvailableMiB > 0 && best.AvailableMiB < 12*1024 {
		profile.Warning = "检测到 16GB 级显卡，但当前空闲显存不足 12GB；启动时会自动降低上下文或 GPU 卸载层数。"
	}
}

func nonNilGPUs(gpus []GPUAdapter) []GPUAdapter {
	if gpus == nil {
		return []GPUAdapter{}
	}
	return gpus
}

func bestGPU(gpus []GPUAdapter) GPUAdapter {
	var best GPUAdapter
	for _, gpu := range gpus {
		if gpu.DedicatedMiB > best.DedicatedMiB ||
			(gpu.DedicatedMiB == best.DedicatedMiB && gpuPreference(gpu) > gpuPreference(best)) {
			best = gpu
		}
	}
	return best
}

func gpuPreference(gpu GPUAdapter) int {
	switch strings.ToLower(strings.TrimSpace(gpu.Backend)) {
	case "cuda":
		return 3
	case "vulkan":
		return 2
	default:
		return 1
	}
}
