//go:build windows

package computeruse

import (
	"fmt"
	"syscall"
	"unsafe"

	uia "github.com/auuunya/go-element"
)

// COM interfaces share the IUnknown prefix. go-element v1.0.1's typed
// Release methods pass the vtable as this; always release the object itself.
func releaseCOM(object unsafe.Pointer) {
	if object != nil {
		uia.NewIUnKnown(object).Release()
	}
}

func elementFromPoint(auto *uia.IUIAutomation, x, y int) (*uia.IUIAutomationElement, error) {
	if auto == nil {
		return nil, fmt.Errorf("UI Automation is unavailable")
	}
	vtbl := *(**uia.IUIAutomationVtbl)(unsafe.Pointer(auto))
	if vtbl == nil || vtbl.ElementFromPoint == 0 {
		return nil, fmt.Errorf("UI Automation point lookup is unavailable")
	}
	var result *uia.IUIAutomationElement
	// POINT is passed by value, packed into one register on 64-bit Windows.
	var ret uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		packed := uint64(uint32(int32(x))) | uint64(uint32(int32(y)))<<32
		ret, _, _ = syscall.SyscallN(vtbl.ElementFromPoint, uintptr(unsafe.Pointer(auto)), uintptr(packed), uintptr(unsafe.Pointer(&result)))
	} else {
		ret, _, _ = syscall.SyscallN(vtbl.ElementFromPoint, uintptr(unsafe.Pointer(auto)), uintptr(int32(x)), uintptr(int32(y)), uintptr(unsafe.Pointer(&result)))
	}
	if int32(ret) < 0 {
		return nil, uia.HResult(ret)
	}
	if result == nil {
		return nil, fmt.Errorf("UI Automation returned no element")
	}
	return result, nil
}
