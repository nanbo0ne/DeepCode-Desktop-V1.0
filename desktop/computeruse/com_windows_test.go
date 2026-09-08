//go:build windows

package computeruse

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	uia "github.com/auuunya/go-element"
)

func TestCOMReleasePassesObjectNotVtable(t *testing.T) {
	var received uintptr
	vtbl := uia.IUnKnownVtbl{Release: syscall.NewCallback(func(this uintptr) uintptr { received = this; return 0 })}
	object := uia.IUnKnown{Vtbl: &vtbl}
	releaseCOM(unsafe.Pointer(&object))
	if received != uintptr(unsafe.Pointer(&object)) {
		t.Fatalf("Release this = %x, want object address", received)
	}
	releaseCOM(nil)
	runtime.KeepAlive(object)
}

func TestElementFromPointPassesPOINTByValue(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("64-bit ABI regression")
	}
	var receivedThis, receivedPoint uintptr
	element := uia.IUnKnown{}
	vtbl := uia.IUIAutomationVtbl{ElementFromPoint: syscall.NewCallback(func(this, packed, result uintptr) uintptr {
		receivedThis, receivedPoint = this, packed
		*(**uia.IUIAutomationElement)(unsafe.Pointer(result)) = (*uia.IUIAutomationElement)(unsafe.Pointer(&element))
		return 0
	})}
	object := struct{ vtbl *uia.IUIAutomationVtbl }{&vtbl}
	auto := (*uia.IUIAutomation)(unsafe.Pointer(&object))
	got, err := elementFromPoint(auto, -1920, 340)
	if err != nil || got == nil {
		t.Fatalf("point lookup: %v", err)
	}
	if receivedThis != uintptr(unsafe.Pointer(auto)) || int32(receivedPoint) != -1920 || int32(uint64(receivedPoint)>>32) != 340 {
		t.Fatalf("wrong COM point ABI: this=%x point=%x", receivedThis, receivedPoint)
	}
	runtime.KeepAlive(object)
	runtime.KeepAlive(element)
}
