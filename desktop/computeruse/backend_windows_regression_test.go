//go:build windows

package computeruse

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	uia "github.com/auuunya/go-element"
	"golang.org/x/sys/windows"
)

func TestCancelledNativeActionsDoNotInspectOrInject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &WindowsBackend{}
	obs := Observation{}
	for _, kind := range []string{"click", "scroll", "key_combo", "type_text", "invoke", "set_value"} {
		if err := b.Execute(ctx, obs, Action{Type: kind}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled %s: %v", kind, err)
		}
	}
	if err := b.uiaAction(ctx, obs, Action{Type: "invoke"}, 0, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled UIA action: %v", err)
	}
}

func TestIntegrityLevelFromTokenInfoReadsSIDSubAuthority(t *testing.T) {
	labelSize := int(unsafe.Sizeof(windows.Tokenmandatorylabel{}))
	buf := make([]byte, labelSize+12)
	sid := buf[labelSize:]
	sid[0] = 1 // revision
	sid[1] = 1 // sub-authority count
	sid[7] = 5 // SECURITY_NT_AUTHORITY
	sid[8] = 0x20
	sid[9] = 0x03
	sid[10] = 0
	sid[11] = 0 // SECURITY_MANDATORY_MEDIUM_RID
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
	label.Label.Sid = (*windows.SID)(unsafe.Pointer(&buf[labelSize]))

	level, err := integrityLevelFromTokenInfo(buf)
	if err != nil {
		t.Fatal(err)
	}
	if level != 0x320 {
		t.Fatalf("integrity level = %#x, want %#x", level, uint32(0x320))
	}
}

func TestReleaseInjectedInputRetriesFailedEvents(t *testing.T) {
	b := &WindowsBackend{
		pressedKeys:    map[uint16]bool{0x41: true},
		pressedButtons: map[string]bool{"left": true},
	}
	keyAttempts, buttonAttempts := 0, 0
	keyUp := func(uint16) error {
		keyAttempts++
		if keyAttempts == 1 {
			return errors.New("synthetic key-up failure")
		}
		return nil
	}
	buttonUp := func(string) error {
		buttonAttempts++
		if buttonAttempts == 1 {
			return errors.New("synthetic button-up failure")
		}
		return nil
	}

	if err := b.releaseInjectedInput(keyUp, buttonUp); err == nil {
		t.Fatal("first release unexpectedly succeeded")
	}
	if !b.pressedKeys[0x41] || !b.pressedButtons["left"] {
		t.Fatal("failed releases were forgotten")
	}
	if err := b.releaseInjectedInput(keyUp, buttonUp); err != nil {
		t.Fatal(err)
	}
	if len(b.pressedKeys) != 0 || len(b.pressedButtons) != 0 {
		t.Fatalf("released state remains: keys=%v buttons=%v", b.pressedKeys, b.pressedButtons)
	}
	if keyAttempts != 2 || buttonAttempts != 2 {
		t.Fatalf("attempts = key %d, button %d; want two each", keyAttempts, buttonAttempts)
	}
}

func TestUnicodeReleaseRetainsFailedUnits(t *testing.T) {
	b := &WindowsBackend{pressedUnicode: map[uint16]bool{0x4e2d: true}}
	if err := b.releaseUnicodeInput(func(uint16) error { return errors.New("synthetic failure") }); err == nil || len(b.pressedUnicode) != 1 {
		t.Fatal("failed Unicode release must remain pending")
	}
	if err := b.releaseUnicodeInput(func(uint16) error { return nil }); err != nil || len(b.pressedUnicode) != 0 {
		t.Fatal("successful Unicode release must clear pending state")
	}
}

func TestProtectedElementDecisionFailsClosed(t *testing.T) {
	err := protectedElementDecision(false, "", errors.New("UIA unavailable"))
	if !errors.Is(err, ErrProtectedSurface) || !strings.Contains(err.Error(), "UIA unavailable") {
		t.Fatalf("inspection error = %v", err)
	}
	if err := protectedElementDecision(true, "password field", nil); !errors.Is(err, ErrProtectedSurface) {
		t.Fatalf("protected element error = %v", err)
	}
	if err := protectedElementDecision(false, "", nil); err != nil {
		t.Fatalf("unprotected decision = %v", err)
	}
}

func TestCurrentElementProtectionFailsClosedOnPropertyHRESULT(t *testing.T) {
	tests := []struct {
		name         string
		passwordCode uintptr
		nameCode     uintptr
	}{
		{name: "password", passwordCode: uintptr(0x80004005)},
		{name: "name", nameCode: uintptr(0x80004005)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := syntheticElementForPropertyTest(tt.passwordCode, tt.nameCode)
			protected, _, err := currentElementProtection(raw)
			if err == nil {
				t.Fatal("property failure was ignored")
			}
			if protected {
				t.Fatal("property failure was reported as a positive protection result")
			}
			if !strings.Contains(err.Error(), "COM Error") {
				t.Fatalf("property error = %v", err)
			}
		})
	}
}

func syntheticElementForPropertyTest(passwordCode, nameCode uintptr) *uia.IUIAutomationElement {
	vtbl := &uia.IUIAutomationElementVtbl{}
	passwordGetter := syscall.NewCallback(func(_ uintptr, out uintptr) uintptr {
		*(*int32)(unsafe.Pointer(out)) = 0
		return passwordCode
	})
	nameGetter := syscall.NewCallback(func(_ uintptr, _ uintptr) uintptr {
		return nameCode
	})
	vtbl.Get_CurrentIsPassword = passwordGetter
	vtbl.Get_CurrentName = nameGetter
	obj := &uiAutomationElementObject{vtbl: (*uia.IUnKnown)(unsafe.Pointer(vtbl))}
	return (*uia.IUIAutomationElement)(unsafe.Pointer(obj))
}

func TestStaleHookCleanupCannotClearReplacement(t *testing.T) {
	b := &WindowsBackend{
		hookGeneration: 2,
		hookThread:     22,
		keyboardHook:   33,
		mouseHook:      44,
		hookDone:       make(chan struct{}),
	}
	b.clearHookState(1)
	if b.hookThread != 22 || b.keyboardHook != 33 || b.mouseHook != 44 || b.hookDone == nil {
		t.Fatalf("stale cleanup changed replacement state: %+v", b)
	}
	b.clearHookState(2)
	if b.hookThread != 0 || b.keyboardHook != 0 || b.mouseHook != 0 || b.hookDone != nil {
		t.Fatalf("current cleanup did not clear state: %+v", b)
	}
}

func TestStopSafetyHooksReportsPostFailureWithoutWaiting(t *testing.T) {
	b := &WindowsBackend{
		hookThread:        22,
		hookDone:          make(chan struct{}),
		postThreadMessage: func(uint32) error { return errors.New("synthetic PostThreadMessage failure") },
	}
	result := make(chan error, 1)
	go func() { result <- b.StopSafetyHooksError() }()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "synthetic PostThreadMessage failure") {
			t.Fatalf("stop error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stop waited for hook completion after PostThreadMessage failure")
	}
}

func TestDisplayTargetCannotExpandObservedCrop(t *testing.T) {
	b := &WindowsBackend{displays: map[string]Rect{"display": {X: 0, Y: 0, Width: 1920, Height: 1080}}}
	o := Observation{Generation: 3, DisplayID: "display", Crop: Rect{X: 100, Y: 200, Width: 101, Height: 101}}
	_, x, y, err := b.resolveTarget(o, Action{Generation: 3, DisplayID: "display", X: .5, Y: .5})
	if err != nil || x != 150 || y != 250 {
		t.Fatalf("coordinates escaped crop: %d,%d err=%v", x, y, err)
	}
	if _, _, _, err := b.resolveTarget(o, Action{Generation: 3, DisplayID: "unobserved"}); !errors.Is(err, ErrStaleObservation) {
		t.Fatal(err)
	}
}
