//go:build windows

package computeruse

import (
	"fmt"
	"runtime"
)

var procSetThreadDpiAwarenessContext = user32DLL.NewProc("SetThreadDpiAwarenessContext")

// UIA reports physical pixels. Keep Win32 rects, GDI captures and SendInput in
// that same space, including test binaries without Wails' Per-Monitor manifest.
func physicalCoordinateScope() (func(), error) {
	runtime.LockOSThread()
	if err := procSetThreadDpiAwarenessContext.Find(); err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("physical desktop coordinates unavailable: %w", err)
	}
	old, _, err := procSetThreadDpiAwarenessContext.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2
	if old == 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("set physical desktop coordinates: %w", err)
	}
	return func() {
		procSetThreadDpiAwarenessContext.Call(old)
		runtime.UnlockOSThread()
	}, nil
}
