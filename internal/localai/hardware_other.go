//go:build !windows

package localai

import "runtime"

func diskFreeBytes(string) int64 { return 0 }

func DetectHardware(dataRoot string) HardwareProfile {
	return HardwareProfile{Platform: runtime.GOOS, Supported: false, CPULogicalCores: runtime.NumCPU(), GPUs: []GPUAdapter{}, Warning: "本地运行器首版仅在 Windows 提供。"}
}
