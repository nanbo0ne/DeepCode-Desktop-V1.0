//go:build windows

package computeruse

import (
	"runtime"
	"testing"
)

func TestPhysicalCoordinateScopeRestoresCallerContext(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	get := user32DLL.NewProc("GetThreadDpiAwarenessContext")
	equal := user32DLL.NewProc("AreDpiAwarenessContextsEqual")
	before, _, _ := get.Call()
	restore, err := physicalCoordinateScope()
	if err != nil {
		t.Fatal(err)
	}
	during, _, _ := get.Call()
	same, _, _ := equal.Call(during, ^uintptr(3))
	restore()
	after, _, _ := get.Call()
	restored, _, _ := equal.Call(before, after)
	if same == 0 || restored == 0 {
		t.Fatalf("DPI scope mismatch: before=%x during=%x after=%x", before, during, after)
	}
}
