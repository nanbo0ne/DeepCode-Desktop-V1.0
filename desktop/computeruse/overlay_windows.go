//go:build windows

package computeruse

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

const (
	wsPopup               = 0x80000000
	wsExLayered           = 0x00080000
	wsExTransparent       = 0x00000020
	wsExTopmost           = 0x00000008
	wsExToolWindow        = 0x00000080
	wsExNoActivate        = 0x08000000
	swpNoActivate         = 0x0010
	swpNoOwnerZOrder      = 0x0200
	swpShowWindow         = 0x0040
	hwndTopmost           = ^uintptr(0)
	lwaColorKey           = 0x00000001
	wdaExcludeFromCapture = 0x11
	wmPaint               = 0x000f
	wmEraseBkgnd          = 0x0014
	wmDestroy             = 0x0002
	psSolid               = 0
	transparentBk         = 1
)

var (
	procRegisterClassExW           = user32DLL.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32DLL.NewProc("CreateWindowExW")
	procDestroyWindow              = user32DLL.NewProc("DestroyWindow")
	procSetWindowPos               = user32DLL.NewProc("SetWindowPos")
	procSetLayeredWindowAttributes = user32DLL.NewProc("SetLayeredWindowAttributes")
	procSetWindowDisplayAffinity   = user32DLL.NewProc("SetWindowDisplayAffinity")
	procInvalidateRect             = user32DLL.NewProc("InvalidateRect")
	procUpdateWindow               = user32DLL.NewProc("UpdateWindow")
	procBeginPaint                 = user32DLL.NewProc("BeginPaint")
	procEndPaint                   = user32DLL.NewProc("EndPaint")
	procDefWindowProcW             = user32DLL.NewProc("DefWindowProcW")
	procTranslateMessage           = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW           = user32DLL.NewProc("DispatchMessageW")
	procFillRect                   = user32DLL.NewProc("FillRect")

	gdi32DLL             = windows.NewLazySystemDLL("gdi32.dll")
	procSetBkMode        = gdi32DLL.NewProc("SetBkMode")
	procSetTextColor     = gdi32DLL.NewProc("SetTextColor")
	procTextOutW         = gdi32DLL.NewProc("TextOutW")
	procMoveToEx         = gdi32DLL.NewProc("MoveToEx")
	procLineTo           = gdi32DLL.NewProc("LineTo")
	procEllipse          = gdi32DLL.NewProc("Ellipse")
	procCreatePen        = gdi32DLL.NewProc("CreatePen")
	procCreateSolidBrush = gdi32DLL.NewProc("CreateSolidBrush")
	procSelectObject     = gdi32DLL.NewProc("SelectObject")
	procDeleteObject     = gdi32DLL.NewProc("DeleteObject")
	procGetModuleHandleW = kernel32DLL.NewProc("GetModuleHandleW")

	overlayClassOnce sync.Once
	overlayClassErr  error
	overlayClassName = "ORCAComputerUseOverlay"
	overlayWindowsMu sync.Mutex
	overlayWindows   = map[uintptr]*overlayWindow{}
	overlayWndProc   = syscall.NewCallback(overlayWindowProc)
)

type paintStruct struct {
	HDC       uintptr
	Erase     int32
	Paint     winRect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type wndClassEx struct {
	Size        uint32
	Style       uint32
	WndProc     uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
	SmallIcon   uintptr
}

type overlayWindow struct {
	hwnd   uintptr
	bounds Rect
	state  OverlayState
}

type nativeOverlay struct {
	mu       sync.Mutex
	visible  bool
	commands chan overlayCommand
	done     chan struct{}
}

type overlayCommand struct {
	kind  string
	state OverlayState
	ack   chan error
}

func newNativeOverlay() *nativeOverlay { return &nativeOverlay{} }

func (o *nativeOverlay) Show(state OverlayState) error {
	o.mu.Lock()
	if o.visible {
		commands := o.commands
		o.mu.Unlock()
		commands <- overlayCommand{kind: "update", state: state}
		return nil
	}
	if err := registerOverlayClass(); err != nil {
		o.mu.Unlock()
		return err
	}
	o.visible, o.commands, o.done = true, make(chan overlayCommand, 8), make(chan struct{})
	commands, done := o.commands, o.done
	o.mu.Unlock()
	ready := make(chan error, 1)
	go o.loop(commands, done, state, ready)
	return <-ready
}

func (o *nativeOverlay) Update(state OverlayState) {
	o.mu.Lock()
	if !o.visible {
		o.mu.Unlock()
		return
	}
	commands := o.commands
	o.mu.Unlock()
	select {
	case commands <- overlayCommand{kind: "update", state: state}:
	default:
	}
}

func (o *nativeOverlay) Hide() {
	o.mu.Lock()
	if !o.visible {
		o.mu.Unlock()
		return
	}
	commands, done := o.commands, o.done
	o.visible = false
	o.mu.Unlock()
	ack := make(chan error, 1)
	select {
	case commands <- overlayCommand{kind: "hide", ack: ack}:
	case <-done:
		return
	}
	select {
	case <-ack:
	case <-done:
	}
}

func (o *nativeOverlay) loop(commands <-chan overlayCommand, done chan<- struct{}, state OverlayState, ready chan<- error) {
	defer close(done)
	restoreDPI, err := physicalCoordinateScope()
	if err != nil {
		ready <- err
		return
	}
	defer restoreDPI()
	list, err := createOverlayWindows(state)
	if err != nil {
		ready <- err
		return
	}
	ready <- nil
	pump := time.NewTicker(8 * time.Millisecond)
	defer pump.Stop()
	for {
		select {
		case command := <-commands:
			switch command.kind {
			case "update":
				for _, window := range list {
					window.state = command.state
					procInvalidateRect.Call(window.hwnd, 0, 0)
					procUpdateWindow.Call(window.hwnd)
				}
			case "hide":
				for _, window := range list {
					procDestroyWindow.Call(window.hwnd)
				}
				if command.ack != nil {
					command.ack <- nil
				}
				return
			}
		case <-pump.C:
		}
		// A channel alone does not service cross-thread SendMessage calls from
		// window discovery/UI Automation. Keep this window-owning thread pumping.
		var message message
		for {
			available, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
			if available == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

func registerOverlayClass() error {
	overlayClassOnce.Do(func() {
		name, err := windows.UTF16PtrFromString(overlayClassName)
		if err != nil {
			overlayClassErr = err
			return
		}
		instance, _, err := procGetModuleHandleW.Call(0)
		if instance == 0 {
			overlayClassErr = err
			return
		}
		class := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: overlayWndProc, Instance: instance, ClassName: name}
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 && callErr != syscall.Errno(1410) {
			overlayClassErr = fmt.Errorf("register computer overlay: %w", callErr)
		}
	})
	return overlayClassErr
}

func createOverlayWindows(state OverlayState) ([]*overlayWindow, error) {
	className, _ := windows.UTF16PtrFromString(overlayClassName)
	instance, _, _ := procGetModuleHandleW.Call(0)
	count := screenshot.NumActiveDisplays()
	list := make([]*overlayWindow, 0, count)
	for i := 0; i < count; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		hwnd, _, err := procCreateWindowExW.Call(
			wsExLayered|wsExTransparent|wsExTopmost|wsExToolWindow|wsExNoActivate,
			uintptr(unsafe.Pointer(className)), 0, wsPopup,
			uintptr(bounds.Min.X), uintptr(bounds.Min.Y), uintptr(bounds.Dx()), uintptr(bounds.Dy()),
			0, 0, instance, 0,
		)
		if hwnd == 0 {
			for _, old := range list {
				procDestroyWindow.Call(old.hwnd)
			}
			return nil, fmt.Errorf("create computer overlay: %w", err)
		}
		window := &overlayWindow{hwnd: hwnd, bounds: Rect{X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy()}, state: state}
		overlayWindowsMu.Lock()
		overlayWindows[hwnd] = window
		overlayWindowsMu.Unlock()
		procSetLayeredWindowAttributes.Call(hwnd, 0, 0, lwaColorKey)
		procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture)
		procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(bounds.Min.X), uintptr(bounds.Min.Y), uintptr(bounds.Dx()), uintptr(bounds.Dy()), swpNoActivate|swpNoOwnerZOrder|swpShowWindow)
		procInvalidateRect.Call(hwnd, 0, 0)
		procUpdateWindow.Call(hwnd)
		list = append(list, window)
	}
	return list, nil
}

func overlayWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		overlayWindowsMu.Lock()
		window := overlayWindows[hwnd]
		overlayWindowsMu.Unlock()
		if window != nil {
			paintOverlayWindow(window)
		}
		return 0
	case wmDestroy:
		overlayWindowsMu.Lock()
		delete(overlayWindows, hwnd)
		overlayWindowsMu.Unlock()
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

func paintOverlayWindow(window *overlayWindow) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(window.hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(window.hwnd, uintptr(unsafe.Pointer(&ps)))
	r := winRect{Left: 0, Top: 0, Right: int32(window.bounds.Width), Bottom: int32(window.bounds.Height)}
	black, _, _ := procCreateSolidBrush.Call(0)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), black)
	procDeleteObject.Call(black)
	color := uintptr(0x00ff8a19)
	if window.state.State == StatePaused {
		color = 0x0017b7d6
	}
	if window.state.State == StateStopping || window.state.State == StateFailed {
		color = 0x004646e8
	}
	pen, _, _ := procCreatePen.Call(psSolid, 4, color)
	old, _, _ := procSelectObject.Call(hdc, pen)
	defer func() { procSelectObject.Call(hdc, old); procDeleteObject.Call(pen) }()
	line(hdc, 2, 2, window.bounds.Width-3, 2)
	line(hdc, window.bounds.Width-3, 2, window.bounds.Width-3, window.bounds.Height-3)
	line(hdc, window.bounds.Width-3, window.bounds.Height-3, 2, window.bounds.Height-3)
	line(hdc, 2, window.bounds.Height-3, 2, 2)
	procSetBkMode.Call(hdc, transparentBk)
	procSetTextColor.Call(hdc, color)
	status := window.state.App
	if window.state.Action != "" {
		if status != "" {
			status += "  -  "
		}
		status += window.state.Action
	}
	if status != "" {
		status += "  |  Esc 停止"
	} else {
		status = "O.R.C.A. 正在控制此屏幕  |  Esc 停止"
	}
	chars, _ := windows.UTF16PtrFromString(status)
	procTextOutW.Call(hdc, 16, 12, uintptr(unsafe.Pointer(chars)), uintptr(len([]rune(status))))
	if window.state.PointerX >= window.bounds.X && window.state.PointerX < window.bounds.X+window.bounds.Width && window.state.PointerY >= window.bounds.Y && window.state.PointerY < window.bounds.Y+window.bounds.Height {
		x, y := window.state.PointerX-window.bounds.X, window.state.PointerY-window.bounds.Y
		procEllipse.Call(hdc, uintptr(x-15), uintptr(y-15), uintptr(x+15), uintptr(y+15))
		if window.state.ClickKind != "" {
			procEllipse.Call(hdc, uintptr(x-24), uintptr(y-24), uintptr(x+24), uintptr(y+24))
		}
	}
}

func line(hdc uintptr, x1, y1, x2, y2 int) {
	procMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(hdc, uintptr(x2), uintptr(y2))
}
