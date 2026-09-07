//go:build windows

package localai

import (
	"testing"
)

func TestDecodeWindowsVideoControllersAcceptsObjectAndArray(t *testing.T) {
	object, err := decodeWindowsVideoControllers([]byte(`{"Name":"Intel(R) UHD Graphics","AdapterRAM":2147483648,"DriverVersion":"31.0"}`))
	if err != nil || len(object) != 1 || object[0].Name != "Intel(R) UHD Graphics" {
		t.Fatalf("single controller = %+v, err=%v", object, err)
	}
	array, err := decodeWindowsVideoControllers([]byte(`[{"Name":"AMD Radeon","AdapterRAM":4294967296},{"Name":"Virtual Display","AdapterRAM":536870912}]`))
	if err != nil || len(array) != 2 || array[1].Name != "Virtual Display" {
		t.Fatalf("controller array = %+v, err=%v", array, err)
	}
	empty, err := decodeWindowsVideoControllers([]byte(`[]`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty controller list = %+v, err=%v", empty, err)
	}
}

func TestDecodeWindowsVideoControllersRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeWindowsVideoControllers([]byte(`not-json`)); err == nil {
		t.Fatal("invalid controller JSON must be reported as detection failure")
	}
}

func TestMergeGPUAdaptersKeepsIntegratedAndDiscreteAdapters(t *testing.T) {
	got := mergeGPUAdapters(
		[]GPUAdapter{{Name: "NVIDIA GeForce RTX", Vendor: "NVIDIA", Backend: "cuda", DedicatedMiB: 16 * 1024, AvailableMiB: 12 * 1024}},
		[]GPUAdapter{
			{Name: "Intel Graphics", Vendor: "Intel", Backend: "vulkan", DedicatedMiB: 1024},
			{Name: "NVIDIA GeForce RTX", Vendor: "NVIDIA", Backend: "cuda", DedicatedMiB: 4095},
		},
	)
	if len(got) != 2 {
		t.Fatalf("merged adapters = %+v", got)
	}
	if got[0].Name != "NVIDIA GeForce RTX" || got[0].DedicatedMiB != 16*1024 {
		t.Fatalf("best adapter was not preserved: %+v", got)
	}
	if got[1].Name != "Intel Graphics" {
		t.Fatalf("integrated adapter was lost: %+v", got)
	}
}
