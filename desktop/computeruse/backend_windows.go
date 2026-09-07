//go:build windows

package computeruse

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	uia "github.com/auuunya/go-element"
	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

const (
	maxObservationEdge = 1600
	maxUIAElements     = 320
	injectedMarker     = uintptr(0x4f524341) // ORCA

	whKeyboardLL  = 13
	whMouseLL     = 14
	hcAction      = 0
	llkhfInjected = 0x10
	llmhfInjected = 0x01
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMouseWheel  = 0x020a
	wmMouseHWheel = 0x020e
	vkEscape      = 0x1b

	inputMouse             = 0
	inputKeyboard          = 1
	keyeventfKeyUp         = 0x0002
	keyeventfUnicode       = 0x0004
	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfWheel       = 0x0800
	mouseeventfHWheel      = 0x01000
	mouseeventfAbsolute    = 0x8000
	mouseeventfVirtualDesk = 0x4000

	swHide     = 0
	swRestore  = 9
	swMinimize = 6
	swMaximize = 3

	gaRoot = 2
)

var (
	user32DLL   = windows.NewLazySystemDLL("user32.dll")
	kernel32DLL = windows.NewLazySystemDLL("kernel32.dll")

	procEnumWindows               = user32DLL.NewProc("EnumWindows")
	procIsWindowVisible           = user32DLL.NewProc("IsWindowVisible")
	procGetWindowTextLengthW      = user32DLL.NewProc("GetWindowTextLengthW")
	procGetWindowTextW            = user32DLL.NewProc("GetWindowTextW")
	procGetWindowRect             = user32DLL.NewProc("GetWindowRect")
	procGetForegroundWindow       = user32DLL.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID  = user32DLL.NewProc("GetWindowThreadProcessId")
	procIsIconic                  = user32DLL.NewProc("IsIconic")
	procSetForegroundWindow       = user32DLL.NewProc("SetForegroundWindow")
	procShowWindow                = user32DLL.NewProc("ShowWindow")
	procPostMessageW              = user32DLL.NewProc("PostMessageW")
	procMoveWindow                = user32DLL.NewProc("MoveWindow")
	procSendInput                 = user32DLL.NewProc("SendInput")
	procSetCursorPos              = user32DLL.NewProc("SetCursorPos")
	procSetWindowsHookExW         = user32DLL.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx       = user32DLL.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx            = user32DLL.NewProc("CallNextHookEx")
	procGetMessageW               = user32DLL.NewProc("GetMessageW")
	procPeekMessageW              = user32DLL.NewProc("PeekMessageW")
	procPostThreadMessageW        = user32DLL.NewProc("PostThreadMessageW")
	procGetAncestor               = user32DLL.NewProc("GetAncestor")
	procOpenInputDesktop          = user32DLL.NewProc("OpenInputDesktop")
	procCloseDesktop              = user32DLL.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32DLL.NewProc("GetUserObjectInformationW")
	procGetCurrentThreadID        = kernel32DLL.NewProc("GetCurrentThreadId")
	procSysFreeString             = windows.NewLazySystemDLL("oleaut32.dll").NewProc("SysFreeString")
)

type winRect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type message struct {
	HWND           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type keyboardHook struct {
	VKCode, ScanCode, Flags, Time uint32
	ExtraInfo                     uintptr
}
type mouseHook struct {
	Pt                     point
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}

type mouseInput struct {
	DX, DY                 int32
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}

type keyboardInput struct {
	VK, Scan    uint16
	Flags, Time uint32
	ExtraInfo   uintptr
}

type input struct {
	Type uint32
	_    uint32
	Data [32]byte
}

type elementTarget struct {
	Generation uint64
	Bounds     Rect
	Password   bool
	Name       string
}

type WindowsBackend struct {
	mu                 sync.Mutex
	observationRequest uint64
	targets            map[string]elementTarget
	displays           map[string]Rect
	windows            map[string]uintptr
	pressedKeys        map[uint16]bool
	pressedButtons     map[string]bool
	hookThread         uint32
	keyboardHook       uintptr
	mouseHook          uintptr
	hookGeneration     uint64
	hookDone           chan struct{}
	hookLifecycle      sync.Mutex
	postThreadMessage  func(uint32) error
	onEmergency        func()
	onUserInput        func()
	overlay            *nativeOverlay
}

func NewPlatformBackend() Backend {
	return &WindowsBackend{
		targets: map[string]elementTarget{}, displays: map[string]Rect{}, windows: map[string]uintptr{},
		pressedKeys: map[uint16]bool{}, pressedButtons: map[string]bool{}, overlay: newNativeOverlay(),
	}
}

func (b *WindowsBackend) Capabilities() Capabilities {
	return Capabilities{Platform: "windows", Supported: true, ScreenCapture: true, UIAutomation: true, InputInjection: true, Overlay: true, EmergencyStop: true}
}

func (b *WindowsBackend) Observe(ctx context.Context, sessionID string, generation uint64) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	b.mu.Lock()
	b.observationRequest++
	request := b.observationRequest
	b.mu.Unlock()
	secure := !onDefaultDesktop()
	obs := Observation{SessionID: sessionID, Generation: generation, ObservedAt: time.Now().UTC(), SecureDesktop: secure}
	if secure {
		return obs, nil
	}

	displays := enumerateDisplays()
	windowsList, handles := enumerateWindows()
	foreground := uintptr(0)
	if h, _, _ := procGetForegroundWindow.Call(); h != 0 {
		foreground = h
	}
	for i := range windowsList {
		windowsList[i].Foreground = handles[windowsList[i].ID] == foreground
		if windowsList[i].Foreground {
			obs.Foreground = windowsList[i]
		}
	}
	obs.Windows = windowsList
	if obs.Foreground.ID == "" && len(windowsList) > 0 {
		obs.Foreground = windowsList[0]
		foreground = handles[windowsList[0].ID]
	}

	crop, displayID := captureBounds(obs.Foreground.Bounds, displays)
	img, err := screenshot.CaptureRect(image.Rect(crop.X, crop.Y, crop.X+crop.Width, crop.Y+crop.Height))
	if err != nil {
		return Observation{}, fmt.Errorf("capture screen: %w", err)
	}
	img = resizeRGBA(img, maxObservationEdge)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 84}); err != nil {
		return Observation{}, err
	}
	obs.DisplayID, obs.Crop = displayID, crop
	obs.Screenshot = base64.StdEncoding.EncodeToString(encoded.Bytes())
	obs.ScreenshotMIME = "image/jpeg"
	obs.ScreenshotWidth, obs.ScreenshotHeight = img.Bounds().Dx(), img.Bounds().Dy()

	elements, targets := collectUIAElements(foreground, generation)
	obs.Elements = elements
	obs.Summary = summarizeObservation(obs)
	b.mu.Lock()
	if request != b.observationRequest || ctx.Err() != nil {
		b.mu.Unlock()
		return Observation{}, ErrStaleObservation
	}
	b.targets, b.windows, b.displays = targets, handles, map[string]Rect{}
	for _, d := range displays {
		b.displays[d.ID] = d.Bounds
	}
	b.mu.Unlock()
	return obs, nil
}

func (b *WindowsBackend) Execute(ctx context.Context, observation Observation, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !onDefaultDesktop() {
		return ErrProtectedSurface
	}
	if action.Generation != observation.Generation {
		return ErrStaleObservation
	}
	target, x, y, err := b.resolveTarget(observation, action)
	if err != nil {
		return err
	}
	if target.Password || sensitiveText(target.Name) {
		return fmt.Errorf("%w: credential fields cannot be automated", ErrProtectedSurface)
	}
	if observation.Foreground.HigherTrust || sensitiveText(observation.Foreground.Title) {
		return fmt.Errorf("%w: the foreground window has a protected trust or credential boundary", ErrProtectedSurface)
	}
	protected, reason, inspectErr := protectedElementAt(x, y)
	if err := protectedElementDecision(protected, reason, inspectErr); err != nil {
		return err
	}

	switch action.Type {
	case "click", "double_click", "right_click", "hover", "mouse_down", "mouse_up", "drag":
		return b.pointerAction(ctx, action, x, y, observation)
	case "scroll":
		return b.scroll(action, x, y)
	case "key", "key_combo":
		return b.keyboardAction(ctx, action)
	case "type_text":
		if action.Text == "" {
			return nil
		}
		return b.typeText(ctx, action.Text)
	case "activate_window", "minimize_window", "maximize_window", "restore_window", "close_window", "move_window", "resize_window":
		return b.windowAction(ctx, observation, action)
	case "invoke", "toggle", "select", "expand", "collapse", "set_value":
		return b.uiaAction(action, x, y)
	case "wait":
		d := time.Duration(action.TimeoutMS) * time.Millisecond
		if d <= 0 {
			d = 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	default:
		return fmt.Errorf("unsupported computer action %q", action.Type)
	}
}

func (b *WindowsBackend) resolveTarget(observation Observation, action Action) (elementTarget, int, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if action.ElementID != "" {
		t, ok := b.targets[action.ElementID]
		if !ok || t.Generation != action.Generation {
			return elementTarget{}, 0, 0, ErrStaleObservation
		}
		return t, t.Bounds.X + t.Bounds.Width/2, t.Bounds.Y + t.Bounds.Height/2, nil
	}
	bounds := observation.Crop
	if action.DisplayID != "" {
		var ok bool
		bounds, ok = b.displays[action.DisplayID]
		if !ok {
			return elementTarget{}, 0, 0, fmt.Errorf("unknown display %q", action.DisplayID)
		}
	}
	x := bounds.X + int(math.Round(clamp(action.X, 0, 1)*float64(max(0, bounds.Width-1))))
	y := bounds.Y + int(math.Round(clamp(action.Y, 0, 1)*float64(max(0, bounds.Height-1))))
	return elementTarget{Generation: action.Generation, Bounds: Rect{X: x, Y: y, Width: 1, Height: 1}}, x, y, nil
}

func (b *WindowsBackend) pointerAction(ctx context.Context, action Action, x, y int, observation Observation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y)); err != syscall.Errno(0) {
		return err
	}
	b.UpdateOverlay(OverlayState{State: StateRunning, App: observation.Foreground.Title, Action: action.Description, PointerX: x, PointerY: y, ClickKind: action.Type})
	switch action.Type {
	case "hover":
		return nil
	case "click":
		if err := sendMouse(mouseeventfLeftDown, 0); err != nil {
			return err
		}
		b.setButton("left", true)
		if err := sendMouse(mouseeventfLeftUp, 0); err != nil {
			return err
		}
		b.setButton("left", false)
		return nil
	case "double_click":
		if err := b.pointerAction(ctx, Action{Type: "click"}, x, y, observation); err != nil {
			return err
		}
		if err := waitInputDelay(ctx, 70*time.Millisecond); err != nil {
			return err
		}
		return b.pointerAction(ctx, Action{Type: "click"}, x, y, observation)
	case "right_click":
		if err := sendMouse(mouseeventfRightDown, 0); err != nil {
			return err
		}
		b.setButton("right", true)
		if err := sendMouse(mouseeventfRightUp, 0); err != nil {
			return err
		}
		b.setButton("right", false)
		return nil
	case "mouse_down":
		if err := sendMouse(mouseeventfLeftDown, 0); err != nil {
			return err
		}
		b.setButton("left", true)
		return nil
	case "mouse_up":
		if err := sendMouse(mouseeventfLeftUp, 0); err != nil {
			return err
		}
		b.setButton("left", false)
		return nil
	case "drag":
		if err := sendMouse(mouseeventfLeftDown, 0); err != nil {
			return err
		}
		b.setButton("left", true)
		bounds := observation.Crop
		ex := bounds.X + int(clamp(action.EndX, 0, 1)*float64(max(0, bounds.Width-1)))
		ey := bounds.Y + int(clamp(action.EndY, 0, 1)*float64(max(0, bounds.Height-1)))
		for i := 1; i <= 8; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			procSetCursorPos.Call(uintptr(x+(ex-x)*i/8), uintptr(y+(ey-y)*i/8))
			if err := waitInputDelay(ctx, 18*time.Millisecond); err != nil {
				return err
			}
		}
		if err := sendMouse(mouseeventfLeftUp, 0); err != nil {
			return err
		}
		b.setButton("left", false)
		return nil
	}
	return nil
}

func (b *WindowsBackend) scroll(action Action, x, y int) error {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
	if action.DeltaY != 0 {
		if err := sendMouse(mouseeventfWheel, uint32(int32(action.DeltaY))); err != nil {
			return err
		}
	}
	if action.DeltaX != 0 {
		return sendMouse(mouseeventfHWheel, uint32(int32(action.DeltaX)))
	}
	return nil
}

func (b *WindowsBackend) keyboardAction(ctx context.Context, action Action) error {
	keys := append([]string(nil), action.Keys...)
	if action.Key != "" {
		keys = append(keys, action.Key)
	}
	if len(keys) == 0 {
		return fmt.Errorf("keyboard action has no key")
	}
	var codes []uint16
	for _, key := range keys {
		code, ok := virtualKey(key)
		if !ok {
			return fmt.Errorf("unsupported key %q", key)
		}
		codes = append(codes, code)
	}
	for _, code := range codes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sendKey(code, false); err != nil {
			return err
		}
		b.setKey(code, true)
	}
	for i := len(codes) - 1; i >= 0; i-- {
		if err := sendKey(codes[i], true); err != nil {
			return err
		}
		b.setKey(codes[i], false)
	}
	return nil
}

func (b *WindowsBackend) typeText(ctx context.Context, text string) error {
	for _, unit := range utf16.Encode([]rune(text)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sendUnicode(unit, false); err != nil {
			return err
		}
		if err := sendUnicode(unit, true); err != nil {
			return err
		}
	}
	return nil
}

func (b *WindowsBackend) windowAction(ctx context.Context, observation Observation, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var target *Window
	for i := range observation.Windows {
		if observation.Windows[i].ID == action.WindowID {
			target = &observation.Windows[i]
			break
		}
	}
	if target == nil {
		return ErrStaleObservation
	}
	if target.HigherTrust || sensitiveText(target.Title) {
		return ErrProtectedSurface
	}
	b.mu.Lock()
	hwnd := b.windows[action.WindowID]
	b.mu.Unlock()
	if hwnd == 0 {
		return fmt.Errorf("unknown window %q", action.WindowID)
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid != target.ProcessID {
		return ErrStaleObservation
	}
	if isHigherIntegrity(pid) {
		return ErrProtectedSurface
	}
	switch action.Type {
	case "activate_window":
		procShowWindow.Call(hwnd, swRestore)
		procSetForegroundWindow.Call(hwnd)
	case "minimize_window":
		procShowWindow.Call(hwnd, swMinimize)
	case "maximize_window":
		procShowWindow.Call(hwnd, swMaximize)
	case "restore_window":
		procShowWindow.Call(hwnd, swRestore)
	case "close_window":
		procPostMessageW.Call(hwnd, 0x0010, 0, 0)
	case "move_window", "resize_window":
		var r winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		x, y, w, h := int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top)
		if action.Type == "move_window" {
			x, y = action.DeltaX, action.DeltaY
		} else {
			w, h = max(200, action.DeltaX), max(120, action.DeltaY)
		}
		procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
	}
	return nil
}

func (b *WindowsBackend) uiaAction(action Action, x, y int) error {
	return withUIAutomation(func(auto *uia.IUIAutomation) error {
		raw, err := auto.ElementFromPoint(&uia.TagPoint{X: int32(x), Y: int32(y)})
		if err != nil {
			return err
		}
		if raw == nil {
			return protectedElementDecision(false, "", fmt.Errorf("UI Automation returned no element"))
		}
		defer raw.Release()
		protected, reason, err := currentElementProtection(raw)
		if err != nil {
			return protectedElementDecision(false, "", err)
		}
		if err := protectedElementDecision(protected, reason, nil); err != nil {
			return err
		}
		elem := uia.NewElement(raw)
		switch action.Type {
		case "invoke":
			p, err := elem.GetInvokePattern()
			if err != nil {
				return err
			}
			defer p.Release()
			return p.Invoke()
		case "toggle":
			p, err := elem.GetTogglePattern()
			if err != nil {
				return err
			}
			defer p.Release()
			return p.Toggle()
		case "select":
			p, err := elem.GetSelectionItemPattern()
			if err != nil {
				return err
			}
			defer p.Release()
			return p.Select()
		case "expand", "collapse":
			p, err := elem.GetExpandCollapsePattern()
			if err != nil {
				return err
			}
			defer p.Release()
			if action.Type == "expand" {
				return p.Expand()
			}
			return p.Collapse()
		case "set_value":
			p, err := elem.GetValuePattern()
			if err != nil {
				return err
			}
			defer p.Release()
			return p.SetValue(action.Text)
		}
		return nil
	})
}

func collectUIAElements(hwnd uintptr, generation uint64) ([]Element, map[string]elementTarget) {
	out, targets := []Element{}, map[string]elementTarget{}
	if hwnd == 0 {
		return out, targets
	}
	_ = withUIAutomation(func(auto *uia.IUIAutomation) error {
		root, err := uia.ElementFromHandle(auto, hwnd)
		if err != nil {
			return err
		}
		defer root.Release()
		tree := uia.TraverseUIElementTree(auto, root)
		var walk func(*uia.Element)
		walk = func(node *uia.Element) {
			if node == nil || len(out) >= maxUIAElements {
				return
			}
			node.BoundingRectangle()
			node.IsPassword()
			node.HasKeyboardFocus()
			node.IsEnabled()
			r := node.CurrentBoundingRectangle
			if r != nil && r.Right > r.Left && r.Bottom > r.Top && (node.CurrentName != "" || node.CurrentAutomationId != "" || len(node.SupportedPatterns) > 0) {
				id := fmt.Sprintf("g%d-e%d", generation, len(out)+1)
				bounds := Rect{X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}
				patterns := patternNames(node.SupportedPatterns)
				name := strings.TrimSpace(node.CurrentName)
				password := node.CurrentIsPassword != 0
				if password {
					name = "[受保护输入框]"
				}
				out = append(out, Element{ID: id, Name: name, Role: node.CurrentLocalizedControlType, AutomationID: node.CurrentAutomationId, Bounds: bounds, Enabled: node.CurrentIsEnabled != 0, Focused: node.CurrentHasKeyboardFocus != 0, Password: password, Patterns: patterns})
				targets[id] = elementTarget{Generation: generation, Bounds: bounds, Password: password, Name: name}
			}
			for _, child := range node.Child {
				walk(child)
				if len(out) >= maxUIAElements {
					break
				}
			}
		}
		walk(tree)
		return nil
	})
	return out, targets
}

func withUIAutomation(fn func(*uia.IUIAutomation) error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := uia.CoInitialize(); err != nil {
			done <- err
			return
		}
		defer uia.CoUninitialize()
		instance, err := uia.CreateInstance(uia.CLSID_CUIAutomation, uia.IID_IUIAutomation, uia.CLSCTX_INPROC_SERVER)
		if err != nil {
			done <- err
			return
		}
		auto := uia.NewIUIAutomation(uia.NewIUnKnown(instance))
		defer auto.Release()
		done <- fn(auto)
	}()
	return <-done
}

func enumerateDisplays() []Display {
	count := screenshot.NumActiveDisplays()
	out := make([]Display, 0, count)
	for i := 0; i < count; i++ {
		r := screenshot.GetDisplayBounds(i)
		out = append(out, Display{ID: fmt.Sprintf("display-%d", i), Bounds: Rect{X: r.Min.X, Y: r.Min.Y, Width: r.Dx(), Height: r.Dy()}, Primary: r.Min.X == 0 && r.Min.Y == 0, Scale: 100})
	}
	return out
}

func enumerateWindows() ([]Window, map[string]uintptr) {
	var out []Window
	handles := map[string]uintptr{}
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, int(length)+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		title := windows.UTF16ToString(buf)
		if strings.TrimSpace(title) == "" {
			return 1
		}
		var r winRect
		ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		if ok == 0 || r.Right <= r.Left || r.Bottom <= r.Top {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		id := fmt.Sprintf("window-%x", hwnd)
		process := processName(pid)
		iconic, _, _ := procIsIconic.Call(hwnd)
		out = append(out, Window{ID: id, Title: title, Process: process, ProcessID: pid, Bounds: Rect{X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}, Minimized: iconic != 0, HigherTrust: isHigherIntegrity(pid)})
		handles[id] = hwnd
		return 1
	})
	procEnumWindows.Call(callback, 0)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, handles
}

func captureBounds(foreground Rect, displays []Display) (Rect, string) {
	if foreground.Width > 0 && foreground.Height > 0 {
		for _, d := range displays {
			if intersects(foreground, d.Bounds) {
				return intersection(foreground, d.Bounds), d.ID
			}
		}
	}
	if len(displays) > 0 {
		return displays[0].Bounds, displays[0].ID
	}
	return Rect{Width: 1, Height: 1}, "display-0"
}

func summarizeObservation(obs Observation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Foreground: %s (%s), bounds=%d,%d %dx%d\n", obs.Foreground.Title, obs.Foreground.Process, obs.Foreground.Bounds.X, obs.Foreground.Bounds.Y, obs.Foreground.Bounds.Width, obs.Foreground.Bounds.Height)
	for _, e := range obs.Elements {
		fmt.Fprintf(&b, "[%s] %s %q bounds=%d,%d %dx%d patterns=%s\n", e.ID, e.Role, e.Name, e.Bounds.X, e.Bounds.Y, e.Bounds.Width, e.Bounds.Height, strings.Join(e.Patterns, ","))
	}
	return b.String()
}

func resizeRGBA(src *image.RGBA, maxEdge int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	edge := max(w, h)
	if edge <= maxEdge {
		return src
	}
	scale := float64(maxEdge) / float64(edge)
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := src.Bounds().Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := src.Bounds().Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func patternNames(ids []uia.PatternId) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		switch id {
		case uia.UIA_ValuePatternId:
			out = append(out, "value")
		case uia.UIA_InvokePatternId:
			out = append(out, "invoke")
		case uia.UIA_SelectionItemPatternId:
			out = append(out, "select")
		case uia.UIA_ExpandCollapsePatternId:
			out = append(out, "expand-collapse")
		case uia.UIA_TogglePatternId:
			out = append(out, "toggle")
		}
	}
	return out
}

func onDefaultDesktop() bool {
	h, _, _ := procOpenInputDesktop.Call(0, 0, 0x0001)
	if h == 0 {
		return false
	}
	defer procCloseDesktop.Call(h)
	var needed uint32
	procGetUserObjectInformationW.Call(h, 2, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return true
	}
	buf := make([]uint16, needed/2+1)
	ok, _, _ := procGetUserObjectInformationW.Call(h, 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	return ok != 0 && strings.EqualFold(windows.UTF16ToString(buf), "Default")
}

func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(buf[:size]))
}

func isHigherIntegrity(pid uint32) bool {
	target, err := processIntegrity(pid)
	if err != nil {
		return true
	}
	current, err := processIntegrity(uint32(windows.GetCurrentProcessId()))
	return err != nil || target > current
}

func processIntegrity(pid uint32) (uint32, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err = windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return 0, err
	}
	defer tok.Close()
	var n uint32
	if err = windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, nil, 0, &n); err != windows.ERROR_INSUFFICIENT_BUFFER {
		return 0, err
	}
	if n < uint32(unsafe.Sizeof(windows.Tokenmandatorylabel{})) {
		return 0, fmt.Errorf("integrity label is truncated")
	}
	buf := make([]byte, n)
	if err = windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, &buf[0], n, &n); err != nil {
		return 0, err
	}
	return integrityLevelFromTokenInfo(buf)
}

func integrityLevelFromTokenInfo(buf []byte) (uint32, error) {
	if len(buf) < int(unsafe.Sizeof(windows.Tokenmandatorylabel{})) {
		return 0, fmt.Errorf("integrity label is truncated")
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
	return integrityLevelFromSID(label.Label.Sid)
}

func integrityLevelFromSID(sid *windows.SID) (uint32, error) {
	if sid == nil || !sid.IsValid() {
		return 0, fmt.Errorf("integrity SID is invalid")
	}
	count := sid.SubAuthorityCount()
	if count == 0 {
		return 0, fmt.Errorf("integrity SID has no sub-authorities")
	}
	return sid.SubAuthority(uint32(count - 1)), nil
}

func sensitiveText(s string) bool {
	s = strings.ToLower(s)
	for _, word := range []string{"password", "passwd", "密码", "验证码", "captcha", "payment password", "支付密码"} {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}

func protectedElementAt(x, y int) (bool, string, error) {
	protected, reason := false, ""
	err := withUIAutomation(func(auto *uia.IUIAutomation) error {
		raw, err := auto.ElementFromPoint(&uia.TagPoint{X: int32(x), Y: int32(y)})
		if err != nil {
			return err
		}
		if raw == nil {
			return fmt.Errorf("UI Automation returned no element")
		}
		defer raw.Release()
		protected, reason, err = currentElementProtection(raw)
		return err
	})
	return protected, reason, err
}

// go-element's element property helpers discard HRESULTs. Read the two
// security-relevant properties through the same COM vtable so a provider
// failure cannot be interpreted as an unprotected target.
type uiAutomationElementObject struct {
	vtbl *uia.IUnKnown
}

func currentElementProtection(raw *uia.IUIAutomationElement) (bool, string, error) {
	obj := (*uiAutomationElementObject)(unsafe.Pointer(raw))
	if obj.vtbl == nil {
		return false, "", fmt.Errorf("UI Automation element has no vtable")
	}
	vtbl := (*uia.IUIAutomationElementVtbl)(unsafe.Pointer(obj.vtbl))
	password, err := currentElementPassword(raw, vtbl.Get_CurrentIsPassword)
	if err != nil {
		return false, "", fmt.Errorf("read UI Automation password property: %w", err)
	}
	if password {
		return true, "password fields cannot be automated", nil
	}
	name, err := currentElementName(raw, vtbl.Get_CurrentName)
	if err != nil {
		return false, "", fmt.Errorf("read UI Automation name property: %w", err)
	}
	if sensitiveText(name) {
		return true, "credential or CAPTCHA controls cannot be automated", nil
	}
	return false, "", nil
}

func currentElementPassword(raw *uia.IUIAutomationElement, method uintptr) (bool, error) {
	if method == 0 {
		return false, fmt.Errorf("UI Automation password getter is unavailable")
	}
	var value int32
	ret, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(raw)), uintptr(unsafe.Pointer(&value)))
	if ret != 0 {
		return false, uia.HResult(ret)
	}
	return value != 0, nil
}

func currentElementName(raw *uia.IUIAutomationElement, method uintptr) (string, error) {
	if method == 0 {
		return "", fmt.Errorf("UI Automation name getter is unavailable")
	}
	var bstr uintptr
	ret, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(raw)), uintptr(unsafe.Pointer(&bstr)))
	if bstr != 0 {
		defer procSysFreeString.Call(bstr)
	}
	if ret != 0 {
		return "", uia.HResult(ret)
	}
	if bstr == 0 {
		return "", nil
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(bstr))), nil
}

func protectedElementDecision(protected bool, reason string, inspectErr error) error {
	if inspectErr != nil {
		return fmt.Errorf("%w: inspect target: %v", ErrProtectedSurface, inspectErr)
	}
	if protected {
		return fmt.Errorf("%w: %s", ErrProtectedSurface, reason)
	}
	return nil
}

func (b *WindowsBackend) StartSafetyHooks(emergency, userInput func()) error {
	b.hookLifecycle.Lock()
	defer b.hookLifecycle.Unlock()
	if err := b.stopSafetyHooks(); err != nil {
		return err
	}
	b.mu.Lock()
	b.hookGeneration++
	generation := b.hookGeneration
	b.onEmergency, b.onUserInput = emergency, userInput
	b.hookDone = make(chan struct{})
	done := b.hookDone
	ready := make(chan error, 1)
	b.mu.Unlock()
	go b.hookLoop(ready, generation, done)
	return <-ready
}

func (b *WindowsBackend) hookLoop(ready chan<- error, generation uint64, done chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)
	thread, _, _ := procGetCurrentThreadID.Call()
	b.mu.Lock()
	if generation != b.hookGeneration {
		b.mu.Unlock()
		ready <- ErrStopPending
		return
	}
	b.hookThread = uint32(thread)
	b.mu.Unlock()
	kcb := syscall.NewCallback(func(code int, wp, lp uintptr) uintptr {
		if code >= hcAction {
			ev := (*keyboardHook)(unsafe.Pointer(lp))
			if ev.ExtraInfo != injectedMarker && ev.Flags&llkhfInjected == 0 && (wp == wmKeyDown || wp == wmSysKeyDown) {
				b.mu.Lock()
				em, user := b.onEmergency, b.onUserInput
				b.mu.Unlock()
				if ev.VKCode == vkEscape {
					if em != nil {
						go em()
					}
				} else if user != nil {
					go user()
				}
			}
		}
		r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, lp)
		return r
	})
	mcb := syscall.NewCallback(func(code int, wp, lp uintptr) uintptr {
		if code >= hcAction {
			ev := (*mouseHook)(unsafe.Pointer(lp))
			if ev.ExtraInfo != injectedMarker && ev.Flags&llmhfInjected == 0 && wp != wmMouseMove {
				b.mu.Lock()
				user := b.onUserInput
				b.mu.Unlock()
				if user != nil {
					go user()
				}
			}
		}
		r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, lp)
		return r
	})
	k, _, ke := procSetWindowsHookExW.Call(whKeyboardLL, kcb, 0, 0)
	m, _, me := procSetWindowsHookExW.Call(whMouseLL, mcb, 0, 0)
	if k == 0 || m == 0 {
		if k != 0 {
			procUnhookWindowsHookEx.Call(k)
		}
		if m != 0 {
			procUnhookWindowsHookEx.Call(m)
		}
		b.clearHookState(generation)
		ready <- fmt.Errorf("install safety hooks: keyboard=%v mouse=%v", ke, me)
		return
	}
	b.mu.Lock()
	b.keyboardHook, b.mouseHook = k, m
	b.mu.Unlock()
	// PostThreadMessageW cannot target a thread until it has a message queue.
	// PM_NOREMOVE creates the queue without consuming a message.
	var msg message
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
	ready <- nil
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
	}
	procUnhookWindowsHookEx.Call(k)
	procUnhookWindowsHookEx.Call(m)
	b.clearHookState(generation)
}

func (b *WindowsBackend) StopSafetyHooks() {
	b.hookLifecycle.Lock()
	defer b.hookLifecycle.Unlock()
	_ = b.stopSafetyHooks()
}

func (b *WindowsBackend) StopSafetyHooksError() error {
	b.hookLifecycle.Lock()
	defer b.hookLifecycle.Unlock()
	return b.stopSafetyHooks()
}

func (b *WindowsBackend) stopSafetyHooks() error {
	b.mu.Lock()
	thread := b.hookThread
	done := b.hookDone
	b.hookGeneration++
	b.onEmergency, b.onUserInput = nil, nil
	b.mu.Unlock()
	if thread != 0 {
		if err := b.postHookStop(thread); err != nil {
			return fmt.Errorf("stop safety hooks: %w", err)
		}
	}
	if done != nil {
		<-done
		b.mu.Lock()
		if b.hookDone == done {
			b.keyboardHook, b.mouseHook, b.hookThread = 0, 0, 0
			b.hookDone = nil
		}
		b.mu.Unlock()
	}
	return nil
}

func (b *WindowsBackend) postHookStop(thread uint32) error {
	if b.postThreadMessage != nil {
		return b.postThreadMessage(thread)
	}
	ret, _, err := procPostThreadMessageW.Call(uintptr(thread), 0x0012, 0, 0)
	if ret == 0 {
		if err != syscall.Errno(0) {
			return err
		}
		return fmt.Errorf("PostThreadMessageW returned false")
	}
	return nil
}

func (b *WindowsBackend) clearHookState(generation uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hookGeneration == generation {
		b.keyboardHook, b.mouseHook, b.hookThread = 0, 0, 0
		b.hookDone = nil
	}
}

func (b *WindowsBackend) ReleaseInjectedInput() {
	_ = b.releaseInjectedInput(
		func(code uint16) error { return sendKey(code, true) },
		func(button string) error {
			switch button {
			case "left":
				return sendMouse(mouseeventfLeftUp, 0)
			case "right":
				return sendMouse(mouseeventfRightUp, 0)
			default:
				return fmt.Errorf("unknown pressed button %q", button)
			}
		},
	)
}

func (b *WindowsBackend) releaseInjectedInput(keyUp func(uint16) error, buttonUp func(string) error) error {
	b.mu.Lock()
	keys := make([]uint16, 0, len(b.pressedKeys))
	for k, v := range b.pressedKeys {
		if v {
			keys = append(keys, k)
		}
	}
	left, right := b.pressedButtons["left"], b.pressedButtons["right"]
	b.mu.Unlock()
	var firstErr error
	for _, k := range keys {
		if err := keyUp(k); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		b.mu.Lock()
		if b.pressedKeys[k] {
			delete(b.pressedKeys, k)
		}
		b.mu.Unlock()
	}
	for button, pressed := range map[string]bool{"left": left, "right": right} {
		if !pressed {
			continue
		}
		if err := buttonUp(button); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		b.mu.Lock()
		if b.pressedButtons[button] {
			delete(b.pressedButtons, button)
		}
		b.mu.Unlock()
	}
	return firstErr
}

func (b *WindowsBackend) ShowOverlay(state OverlayState) error { return b.overlay.Show(state) }
func (b *WindowsBackend) UpdateOverlay(state OverlayState)     { b.overlay.Update(state) }
func (b *WindowsBackend) HideOverlay()                         { b.overlay.Hide() }

func (b *WindowsBackend) setKey(code uint16, down bool) {
	b.mu.Lock()
	b.pressedKeys[code] = down
	b.mu.Unlock()
}
func (b *WindowsBackend) setButton(name string, down bool) {
	b.mu.Lock()
	b.pressedButtons[name] = down
	b.mu.Unlock()
}

func sendMouse(flags, data uint32) error {
	mi := mouseInput{MouseData: data, Flags: flags, ExtraInfo: injectedMarker}
	return sendInput(inputMouse, unsafe.Pointer(&mi))
}
func sendKey(code uint16, up bool) error {
	flags := uint32(0)
	if up {
		flags = keyeventfKeyUp
	}
	ki := keyboardInput{VK: code, Flags: flags, ExtraInfo: injectedMarker}
	return sendInput(inputKeyboard, unsafe.Pointer(&ki))
}
func sendUnicode(scan uint16, up bool) error {
	flags := uint32(keyeventfUnicode)
	if up {
		flags |= keyeventfKeyUp
	}
	ki := keyboardInput{Scan: scan, Flags: flags, ExtraInfo: injectedMarker}
	return sendInput(inputKeyboard, unsafe.Pointer(&ki))
}
func sendInput(kind uint32, data unsafe.Pointer) error {
	var in input
	in.Type = kind
	size := unsafe.Sizeof(mouseInput{})
	if kind == inputKeyboard {
		size = unsafe.Sizeof(keyboardInput{})
	}
	copy(in.Data[:], unsafe.Slice((*byte)(data), size))
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r != 1 {
		if err != syscall.Errno(0) {
			return err
		}
		return fmt.Errorf("SendInput rejected the event")
	}
	return nil
}

func virtualKey(key string) (uint16, bool) {
	k := strings.ToUpper(strings.TrimSpace(key))
	table := map[string]uint16{"CTRL": 0x11, "CONTROL": 0x11, "SHIFT": 0x10, "ALT": 0x12, "WIN": 0x5b, "ENTER": 0x0d, "ESC": 0x1b, "ESCAPE": 0x1b, "TAB": 0x09, "BACKSPACE": 0x08, "DELETE": 0x2e, "SPACE": 0x20, "LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28, "HOME": 0x24, "END": 0x23, "PAGEUP": 0x21, "PAGEDOWN": 0x22, "F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74, "F6": 0x75, "F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79, "F11": 0x7a, "F12": 0x7b}
	if v, ok := table[k]; ok {
		return v, true
	}
	if len(k) == 1 {
		c := k[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint16(c), true
		}
	}
	return 0, false
}

func intersects(a, c Rect) bool {
	return a.X < c.X+c.Width && a.X+a.Width > c.X && a.Y < c.Y+c.Height && a.Y+a.Height > c.Y
}
func intersection(a, c Rect) Rect {
	x1, y1 := max(a.X, c.X), max(a.Y, c.Y)
	x2, y2 := min(a.X+a.Width, c.X+c.Width), min(a.Y+a.Height, c.Y+c.Height)
	return Rect{X: x1, Y: y1, Width: max(1, x2-x1), Height: max(1, y2-y1)}
}
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
