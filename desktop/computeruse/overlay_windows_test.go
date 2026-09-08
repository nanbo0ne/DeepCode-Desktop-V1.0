//go:build windows

package computeruse

import (
	"golang.org/x/sys/windows"
	"os"
	"testing"
	"time"
)

func TestOverlayWindowsProceduresResolve(t *testing.T) {
	for _, proc := range []*windows.LazyProc{
		procRegisterClassExW, procCreateWindowExW, procDestroyWindow,
		procSetWindowPos, procSetLayeredWindowAttributes, procSetWindowDisplayAffinity,
		procInvalidateRect, procUpdateWindow, procBeginPaint, procEndPaint,
		procDefWindowProcW, procFillRect, procSetBkMode, procSetTextColor,
		procTextOutW, procMoveToEx, procLineTo, procEllipse, procCreatePen,
		procCreateSolidBrush, procSelectObject, procDeleteObject, procGetModuleHandleW,
		procTranslateMessage, procDispatchMessageW,
	} {
		if err := proc.Find(); err != nil {
			t.Errorf("Windows overlay API %s: %v", proc.Name, err)
		}
	}
}

func TestNativeOverlayMessagePump(t *testing.T) {
	if os.Getenv("ORCA_COMPUTER_E2E") != "1" {
		t.Skip("native overlay test is opt-in")
	}
	overlay := newNativeOverlay()
	if err := overlay.Show(OverlayState{State: StateRunning}); err != nil {
		t.Fatal(err)
	}
	defer overlay.Hide()
	overlayWindowsMu.Lock()
	var hwnd uintptr
	for window := range overlayWindows {
		hwnd = window
		break
	}
	overlayWindowsMu.Unlock()
	if hwnd == 0 {
		t.Fatal("overlay window was not created")
	}
	done := make(chan struct{})
	go func() { procGetWindowTextLengthW.Call(hwnd); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("overlay did not service cross-thread window discovery")
	}
}

func TestNativeInputDesktopAvailable(t *testing.T) {
	if os.Getenv("ORCA_COMPUTER_E2E") != "1" {
		t.Skip("native desktop probe is opt-in")
	}
	if !onDefaultDesktop() {
		t.Fatal("input desktop is protected or unavailable; native interaction is blocked")
	}
}
