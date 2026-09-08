//go:build windows

package main

// This file is deliberately opt-in. The unit tests exercise only the local web
// fixture and the target guard. Native cases require explicit opt-in; cases
// using a live provider additionally require DEEPSEEK_API_KEY and a batch budget.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/boot"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

const (
	computerE2EMaxModelRequests = int64(200)
	computerE2EMaxSteps         = 40
	computerE2ECaseTimeout      = 5 * time.Minute
	computerE2ETitlePrefix      = "O.R.C.A Computer Fixture"
)

type computerFixtureExpected struct {
	RunID        string `json:"runId"`
	TargetName   string `json:"targetName"`
	TargetRegion string `json:"targetRegion"`
	TargetItem   string `json:"targetItem"`
	MarkerX      int    `json:"markerX"`
	MarkerY      int    `json:"markerY"`
	ZoneX        int    `json:"zoneX"`
	ZoneY        int    `json:"zoneY"`
}

type computerFixtureObserved struct {
	Name       string `json:"name"`
	Region     string `json:"region"`
	Item       string `json:"item"`
	Checked    bool   `json:"checked"`
	FilterUsed bool   `json:"filterUsed"`
	Scrolled   bool   `json:"scrolled"`
	Dragged    bool   `json:"dragged"`
	Submitted  bool   `json:"submitted"`
	Success    bool   `json:"success"`
}

type computerFixtureStateResponse struct {
	Expected computerFixtureExpected `json:"expected"`
	Observed computerFixtureObserved `json:"observed"`
}

type computerFixture struct {
	server       *httptest.Server
	expected     computerFixtureExpected
	validatorURL string
	mu           sync.Mutex
	observed     computerFixtureObserved
}

func newComputerFixture(t *testing.T) *computerFixture {
	t.Helper()
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	runID := hex.EncodeToString(raw[:6])
	regions := []string{"华东", "华南", "西北", "东北"}
	region := regions[int(raw[6])%len(regions)]
	expected := computerFixtureExpected{
		RunID:        runID,
		TargetName:   "受试者-" + hex.EncodeToString(raw[7:10]),
		TargetRegion: region,
		TargetItem:   "目标条目-" + hex.EncodeToString(raw[10:13]),
		MarkerX:      52 + int(raw[13])%90,
		MarkerY:      42 + int(raw[14])%90,
		ZoneX:        300 + int(raw[15])%75,
		ZoneY:        120 + int(raw[16])%55,
	}
	f := &computerFixture{expected: expected}
	f.server = httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	f.validatorURL = f.server.URL + "/__validator/" + hex.EncodeToString(raw[3:7])
	t.Cleanup(f.server.Close)
	return f
}

func (f *computerFixture) pageURL() string {
	return f.server.URL + "/"
}

func (f *computerFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := computerFixtureTemplate.Execute(w, f.templateData()); err != nil {
			http.Error(w, "fixture render failed", http.StatusInternalServerError)
		}
	case r.Method == http.MethodPost && r.URL.Path == "/__fixture/event":
		f.recordEvent(w, r)
	case r.Method == http.MethodGet && r.URL.String() == strings.TrimPrefix(f.validatorURL, f.server.URL):
		f.writeValidatorState(w)
	default:
		http.NotFound(w, r)
	}
}

func (f *computerFixture) templateData() struct {
	Title      string
	RunID      string
	TargetName string
	Region     string
	TargetItem string
	MarkerX    int
	MarkerY    int
	ZoneX      int
	ZoneY      int
	Items      []string
} {
	items := make([]string, 0, 28)
	for i := 1; i <= 22; i++ {
		items = append(items, fmt.Sprintf("普通条目-%02d", i))
	}
	items = append(items, f.expected.TargetItem)
	for i := 23; i <= 28; i++ {
		items = append(items, fmt.Sprintf("备用条目-%02d", i))
	}
	return struct {
		Title      string
		RunID      string
		TargetName string
		Region     string
		TargetItem string
		MarkerX    int
		MarkerY    int
		ZoneX      int
		ZoneY      int
		Items      []string
	}{
		Title:      computerE2ETitlePrefix + " " + f.expected.RunID,
		RunID:      f.expected.RunID,
		TargetName: f.expected.TargetName,
		Region:     f.expected.TargetRegion,
		TargetItem: f.expected.TargetItem,
		MarkerX:    f.expected.MarkerX,
		MarkerY:    f.expected.MarkerY,
		ZoneX:      f.expected.ZoneX,
		ZoneY:      f.expected.ZoneY,
		Items:      items,
	}
}

func (f *computerFixture) recordEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var next computerFixtureObserved
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	if err := decoder.Decode(&next); err != nil {
		http.Error(w, "bad fixture event", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.observed = next
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *computerFixture) writeValidatorState(w http.ResponseWriter) {
	f.mu.Lock()
	response := computerFixtureStateResponse{Expected: f.expected, Observed: f.observed}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (f *computerFixture) readValidatorState(ctx context.Context) (computerFixtureStateResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.validatorURL, nil)
	if err != nil {
		return computerFixtureStateResponse{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return computerFixtureStateResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return computerFixtureStateResponse{}, fmt.Errorf("fixture validator status %s", response.Status)
	}
	var state computerFixtureStateResponse
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return computerFixtureStateResponse{}, err
	}
	return state, nil
}

var computerFixtureTemplate = template.Must(template.New("orca-computer-fixture").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
:root { color-scheme: light; font-family: "Microsoft YaHei", sans-serif; }
body { margin: 0; background: #f4f7fb; color: #172033; }
main { width: min(940px, calc(100vw - 48px)); margin: 24px auto 80px; }
h1 { font-size: 25px; margin: 0 0 8px; } h2 { font-size: 17px; margin: 0 0 12px; }
.notice { border-left: 4px solid #2563eb; background: white; padding: 14px 16px; margin-bottom: 16px; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
section { background: white; border: 1px solid #d9e1ef; padding: 16px; }
label { display: block; margin: 10px 0; } input, select, button { font: inherit; }
input[type=text], select { width: 100%; box-sizing: border-box; padding: 8px; border: 1px solid #9aa9bf; }
input[type=checkbox] { width: 18px; height: 18px; vertical-align: middle; }
#filter { margin-bottom: 8px; } #items { height: 174px; overflow: auto; border: 1px solid #c6d1e1; }
.item { display: flex; gap: 8px; align-items: center; padding: 7px 9px; min-height: 25px; border-bottom: 1px solid #edf1f7; }
#items .item[hidden] { display: none; }
.item input { width: 18px; height: 18px; } .muted { color: #59677d; font-size: 13px; }
canvas { width: 100%; height: 220px; border: 1px solid #95a4bb; background: #f9fbff; touch-action: none; }
button { margin-top: 14px; padding: 9px 18px; background: #155eef; color: white; border: 0; cursor: pointer; }
#status { min-height: 26px; margin-top: 12px; font-weight: 700; } .success { color: #087443; }
@media (max-width: 760px) { .grid { grid-template-columns: 1fr; } main { width: calc(100vw - 24px); margin-top: 12px; } }
</style></head>
<body data-target-name="{{.TargetName}}" data-target-region="{{.Region}}" data-target-item="{{.TargetItem}}">
<main>
<div class="notice"><h1>O.R.C.A 计算机使用真实测试夹具</h1>
<div>请完成本页任务。随机目标只显示在屏幕上，不能通过 HTTP、文件、开发者工具或脚本读取。</div>
<div class="muted">目标姓名：<strong>{{.TargetName}}</strong>　目标地区：<strong>{{.Region}}</strong>　目标条目：<strong>{{.TargetItem}}</strong></div></div>
<div class="grid">
<section aria-label="中文表单"><h2>一、填写资料</h2>
<label for="name">姓名<input id="name" aria-label="姓名" type="text" autocomplete="off"></label>
<label for="region">地区<select id="region" aria-label="地区"><option value="">请选择</option><option>华东</option><option>华南</option><option>西北</option><option>东北</option></select></label>
<label><input id="checked" aria-label="已核对" type="checkbox"> 我已核对资料</label></section>
<section aria-label="筛选和滚动条目"><h2>二、滚动并筛选条目</h2>
<label for="filter">筛选<input id="filter" aria-label="筛选条目" type="text" autocomplete="off"></label>
<div id="items" role="listbox" aria-label="条目列表" tabindex="0">{{range .Items}}<label class="item"><input type="checkbox" aria-label="选择 {{.}}" data-item="{{.}}"><span>{{.}}</span></label>{{end}}</div>
<div class="muted">请先滚动长列表，再用筛选框定位目标条目并选择。</div></section>
</div>
<section aria-label="拖动区域" style="margin-top:16px"><h2>三、拖动画布标记</h2>
<div class="muted">把蓝色标记拖入绿色目标区。</div><canvas id="canvas" width="440" height="220" aria-label="拖动画布"></canvas>
<button id="submit" type="button">提交测试</button><div id="status" role="status" aria-live="polite"></div></section>
</main>
<script>
const body = document.body, expected = {name: body.dataset.targetName, region: body.dataset.targetRegion, item: body.dataset.targetItem};
const current = {name:"", region:"", item:"", checked:false, filterUsed:false, scrolled:false, dragged:false, submitted:false, success:false};
let syncChain = Promise.resolve();
function sync() { const payload=JSON.stringify(current); syncChain=syncChain.then(()=>fetch('/__fixture/event', {method:'POST', headers:{'Content-Type':'application/json'}, body:payload})).catch(()=>{}); }
const name = document.getElementById('name'), region = document.getElementById('region'), checked = document.getElementById('checked');
name.addEventListener('input', () => { current.name=name.value; sync(); });
region.addEventListener('change', () => { current.region=region.value; sync(); });
checked.addEventListener('change', () => { current.checked=checked.checked; sync(); });
const filter = document.getElementById('filter'), items = document.getElementById('items');
filter.addEventListener('input', () => { current.filterUsed=true; const q=filter.value.trim(); items.querySelectorAll('.item').forEach(row => row.hidden=q && !row.textContent.includes(q)); sync(); });
items.addEventListener('scroll', () => { if (items.scrollTop + items.clientHeight >= items.scrollHeight - 2) current.scrolled=true; sync(); });
items.querySelectorAll('input[data-item]').forEach(box => box.addEventListener('change', () => { if (box.checked) { current.item=box.dataset.item; items.querySelectorAll('input[data-item]').forEach(other => { if (other!==box) other.checked=false; }); } else if (current.item===box.dataset.item) current.item=''; sync(); }));
const canvas=document.getElementById('canvas'), ctx=canvas.getContext('2d'); let marker={x:{{.MarkerX}},y:{{.MarkerY}}}, dragging=false;
function draw() { ctx.clearRect(0,0,440,220); ctx.fillStyle='#d1fae5'; ctx.fillRect({{.ZoneX}},{{.ZoneY}},82,60); ctx.strokeStyle='#087443'; ctx.strokeRect({{.ZoneX}},{{.ZoneY}},82,60); ctx.fillStyle='#2563eb'; ctx.beginPath(); ctx.arc(marker.x,marker.y,18,0,Math.PI*2); ctx.fill(); }
draw();
function point(e) { const r=canvas.getBoundingClientRect(); return {x:(e.clientX-r.left)*canvas.width/r.width,y:(e.clientY-r.top)*canvas.height/r.height}; }
canvas.addEventListener('pointerdown', e => { const p=point(e); if (Math.hypot(p.x-marker.x,p.y-marker.y)<30) { dragging=true; canvas.setPointerCapture(e.pointerId); } });
canvas.addEventListener('pointermove', e => { if (dragging) { marker=point(e); draw(); } });
canvas.addEventListener('pointerup', e => { if (dragging) { dragging=false; current.dragged = marker.x>{{.ZoneX}} && marker.x<{{.ZoneX}}+82 && marker.y>{{.ZoneY}} && marker.y<{{.ZoneY}}+60; sync(); } });
document.getElementById('submit').addEventListener('click', () => { current.submitted=true; current.success=current.name===expected.name && current.region===expected.region && current.item===expected.item && current.checked && current.filterUsed && current.scrolled && current.dragged; const status=document.getElementById('status'); status.textContent=current.success?'提交成功：全部条件已满足':'尚未满足全部条件，请继续检查页面'; status.className=current.success?'success':''; sync(); });
</script></body></html>`))

type computerE2ERequestBudget struct {
	used   atomic.Int64
	limit  int64
	cancel context.CancelFunc
}

var computerE2ECumulativeRequests atomic.Int64
var computerE2EBudgetMu sync.Mutex
var computerE2EBudgetFile string

func loadComputerE2EBudget(t *testing.T) {
	t.Helper()
	computerE2EBudgetFile = os.Getenv("ORCA_E2E_BUDGET_FILE")
	if computerE2EBudgetFile == "" {
		t.Fatal("live test requires a persistent batch request budget")
	}
	data, err := os.ReadFile(computerE2EBudgetFile)
	if err != nil {
		t.Fatal("cannot read live test batch budget")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || n < 0 || n >= computerE2EMaxModelRequests {
		t.Fatal("live test batch budget invalid or exhausted")
	}
	computerE2ECumulativeRequests.Store(n)
	t.Logf("persistent batch budget: %d/%d attempts already reserved", n, computerE2EMaxModelRequests)
}

func (b *computerE2ERequestBudget) reserve() error {
	computerE2EBudgetMu.Lock()
	defer computerE2EBudgetMu.Unlock()
	b.used.Add(1)
	n := computerE2ECumulativeRequests.Add(1)
	if computerE2EBudgetFile != "" {
		path := computerE2EBudgetFile + ".next"
		err := os.WriteFile(path, []byte(strconv.FormatInt(n, 10)), 0600)
		if err == nil {
			err = os.Rename(path, computerE2EBudgetFile)
		}
		if err != nil {
			if b.cancel != nil {
				b.cancel()
			}
			return fmt.Errorf("persist live test request reservation: %w", err)
		}
	}
	if n > b.limit {
		if b.cancel != nil {
			b.cancel()
		}
		return fmt.Errorf("computer E2E model-request budget exceeded (%d)", b.limit)
	}
	return nil
}

type computerE2EEventSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *computerE2EEventSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *computerE2EEventSink) snapshot() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.events...)
}

func hasComputerTaskDispatch(events []event.Event) bool {
	for _, e := range events {
		if e.Kind == event.ToolDispatch && e.Tool.Name == "computer_task" && !e.Tool.Partial {
			return true
		}
	}
	return false
}

// targetGuardBackend checks the foreground HWND before every capture and input
// call. The production backend still performs its own trust checks; this extra
// test-only boundary prevents a stale or changed foreground window from making
// the real-use fixture test touch the user's desktop.
type targetGuardBackend struct {
	inner         computeruse.Backend
	targetHWND    uintptr
	targetPID     uint32
	targetID      string
	foreground    func() uintptr
	windowPID     func(uintptr) uint32
	stripElements bool
	mu            sync.Mutex
	observes      int
	actions       int
	attempts      int
	uiElements    int
	screenshots   int
	log           func(string, ...any)
}

func (b *targetGuardBackend) Capabilities() computeruse.Capabilities { return b.inner.Capabilities() }

func (b *targetGuardBackend) assertTarget() error {
	hwnd := b.foreground()
	if hwnd == 0 || hwnd != b.targetHWND {
		return fmt.Errorf("computer E2E target foreground changed: hwnd=%x want=%x", hwnd, b.targetHWND)
	}
	if pid := b.windowPID(hwnd); pid == 0 || pid != b.targetPID {
		return fmt.Errorf("computer E2E target PID changed: pid=%d want=%d", pid, b.targetPID)
	}
	return nil
}

func (b *targetGuardBackend) Observe(ctx context.Context, sessionID string, generation uint64) (computeruse.Observation, error) {
	if err := b.assertTarget(); err != nil {
		return computeruse.Observation{}, err
	}
	obs, err := b.inner.Observe(ctx, sessionID, generation)
	if err != nil {
		return computeruse.Observation{}, err
	}
	if obs.Foreground.ProcessID != b.targetPID || obs.Foreground.ID != b.targetID {
		return computeruse.Observation{}, fmt.Errorf("computer E2E observation escaped target window: %+v", obs.Foreground)
	}
	obs.Windows = []computeruse.Window{obs.Foreground}
	if b.stripElements {
		obs = computerE2EVisualOnlyObservation(obs)
	}
	b.mu.Lock()
	b.observes++
	if len(obs.Elements) > b.uiElements {
		b.uiElements = len(obs.Elements)
	}
	if obs.Screenshot != "" {
		b.screenshots++
	}
	b.mu.Unlock()
	return obs, nil
}

func (b *targetGuardBackend) Execute(ctx context.Context, obs computeruse.Observation, action computeruse.Action) error {
	b.mu.Lock()
	b.attempts++
	attempt := b.attempts
	b.mu.Unlock()
	if attempt > computerE2EMaxSteps {
		return fmt.Errorf("fixture case reached its 40-action limit")
	}
	if b.log != nil {
		b.log("native action %d: %s (generation %d)", attempt, action.Type, action.Generation)
	}
	if err := b.assertTarget(); err != nil {
		return err
	}
	if err := validateComputerFixtureAction(action); err != nil {
		return err
	}
	if obs.Foreground.ProcessID != b.targetPID || obs.Foreground.ID != b.targetID {
		return fmt.Errorf("computer E2E action observation is outside target window")
	}
	if action.WindowID != "" && action.WindowID != b.targetID {
		return fmt.Errorf("computer E2E action selected non-target window %q", action.WindowID)
	}
	if err := b.inner.Execute(ctx, obs, action); err != nil {
		if b.log != nil {
			b.log("native action error: %s", sanitizeComputerE2EError(err))
		}
		return err
	}
	if err := b.assertTarget(); err != nil {
		return err
	}
	b.mu.Lock()
	b.actions++
	b.mu.Unlock()
	return nil
}

func (b *targetGuardBackend) StartSafetyHooks(emergency, userInput func()) error {
	return b.inner.StartSafetyHooks(emergency, userInput)
}
func (b *targetGuardBackend) StopSafetyHooks()      { b.inner.StopSafetyHooks() }
func (b *targetGuardBackend) ReleaseInjectedInput() { b.inner.ReleaseInjectedInput() }
func (b *targetGuardBackend) ReleaseInjectedInputError() error {
	if checked, ok := b.inner.(interface{ ReleaseInjectedInputError() error }); ok {
		return checked.ReleaseInjectedInputError()
	}
	b.inner.ReleaseInjectedInput()
	return nil
}
func (b *targetGuardBackend) StopSafetyHooksError() error {
	if checked, ok := b.inner.(interface{ StopSafetyHooksError() error }); ok {
		return checked.StopSafetyHooksError()
	}
	b.inner.StopSafetyHooks()
	return nil
}
func (b *targetGuardBackend) ShowOverlay(s computeruse.OverlayState) error {
	return b.inner.ShowOverlay(s)
}
func (b *targetGuardBackend) UpdateOverlay(s computeruse.OverlayState) { b.inner.UpdateOverlay(s) }
func (b *targetGuardBackend) HideOverlay()                             { b.inner.HideOverlay() }

func (b *targetGuardBackend) stats() (observes, actions, uiElements, screenshots int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.observes, b.actions, b.uiElements, b.screenshots
}

func computerE2EVisualOnlyObservation(obs computeruse.Observation) computeruse.Observation {
	foreground := obs.Foreground
	obs.Elements = nil
	obs.Windows = []computeruse.Window{foreground}
	obs.Summary = fmt.Sprintf(
		"Foreground title=%q bounds=%d,%d %dx%d crop=%d,%d %dx%d",
		foreground.Title,
		foreground.Bounds.X, foreground.Bounds.Y, foreground.Bounds.Width, foreground.Bounds.Height,
		obs.Crop.X, obs.Crop.Y, obs.Crop.Width, obs.Crop.Height,
	)
	return obs
}

// The browser PID is not enough to protect the fixture: Ctrl+L, browser
// history keys, and devtools can leave the app window while keeping the same
// process. The fixture task has no legitimate reason to use window-management
// actions or system/navigation hotkeys, so reject them before injection.
func validateComputerFixtureAction(action computeruse.Action) error {
	switch strings.ToLower(strings.TrimSpace(action.Type)) {
	case "activate_window", "minimize_window", "maximize_window", "restore_window", "close_window", "move_window", "resize_window":
		return fmt.Errorf("computer E2E fixture forbids window/navigation action %q", action.Type)
	case "key", "key_combo":
		keys := append([]string{action.Key}, action.Keys...)
		for _, key := range keys {
			normalized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), " ", ""), "+", ""))
			switch normalized {
			case "CTRL", "CONTROL", "ALT", "WIN", "LWIN", "RWIN", "F4", "F12", "ESC", "ESCAPE", "TAB", "HOME", "END", "PGUP", "PAGEDOWN", "PGDN", "BROWSERBACK", "BROWSERFORWARD", "CTRLL":
				return fmt.Errorf("computer E2E fixture forbids system/navigation key %q", key)
			}
		}
	}
	return nil
}

type fixtureGuardStubBackend struct {
	observeCalls int
	executeCalls int
}

type fixtureVisualObservationBackend struct {
	fixtureGuardStubBackend
}

func (*fixtureVisualObservationBackend) Observe(context.Context, string, uint64) (computeruse.Observation, error) {
	return computeruse.Observation{
		Foreground: computeruse.Window{
			ID: "window-abc", Title: "O.R.C.A Computer Fixture target", ProcessID: 123,
			Bounds: computeruse.Rect{X: 10, Y: 20, Width: 900, Height: 700},
		},
		Windows: []computeruse.Window{
			{ID: "window-abc", Title: "O.R.C.A Computer Fixture target", ProcessID: 123},
			{ID: "window-private", Title: "PRIVATE USER WINDOW", ProcessID: 456},
		},
		Elements:   []computeruse.Element{{ID: "element-secret", Name: "PRIVATE UIA TEXT"}},
		Screenshot: "data:image/jpeg;base64,fixture",
		Crop:       computeruse.Rect{X: 1, Y: 2, Width: 800, Height: 600},
		Summary:    "PRIVATE UIA TEXT element-secret",
	}, nil
}

func (*fixtureGuardStubBackend) Capabilities() computeruse.Capabilities {
	return computeruse.Capabilities{Supported: true, ScreenCapture: true, UIAutomation: true, InputInjection: true}
}
func (b *fixtureGuardStubBackend) Observe(context.Context, string, uint64) (computeruse.Observation, error) {
	b.observeCalls++
	return computeruse.Observation{Foreground: computeruse.Window{ID: "window-abc", ProcessID: 123}}, nil
}
func (b *fixtureGuardStubBackend) Execute(context.Context, computeruse.Observation, computeruse.Action) error {
	b.executeCalls++
	return nil
}
func (*fixtureGuardStubBackend) StartSafetyHooks(func(), func()) error { return nil }
func (*fixtureGuardStubBackend) StopSafetyHooks()                      {}
func (*fixtureGuardStubBackend) ReleaseInjectedInput()                 {}
func (*fixtureGuardStubBackend) ShowOverlay(computeruse.OverlayState) error {
	return nil
}
func (*fixtureGuardStubBackend) UpdateOverlay(computeruse.OverlayState) {}
func (*fixtureGuardStubBackend) HideOverlay()                           {}

func TestComputerFixtureHTMLAndValidator(t *testing.T) {
	f := newComputerFixture(t)
	response, err := http.Get(f.pageURL())
	if err != nil {
		t.Fatal(err)
	}
	page, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fixture page status = %s", response.Status)
	}
	text := string(page)
	for _, marker := range []string{"姓名", "地区", "筛选条目", "条目列表", "拖动画布", "提交测试", f.expected.TargetName, f.expected.TargetItem} {
		if !strings.Contains(text, marker) {
			t.Fatalf("fixture page missing screen marker %q", marker)
		}
	}
	if strings.Contains(text, f.validatorURL) || strings.Contains(text, "/__validator/") {
		t.Fatal("validator endpoint leaked into the model-facing page")
	}
	state, err := f.readValidatorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Expected.RunID != f.expected.RunID || state.Observed.Success {
		t.Fatalf("unexpected initial validator state: %+v", state)
	}
}

func TestComputerFixtureTargetGuardBlocksWrongWindow(t *testing.T) {
	inner := &fixtureGuardStubBackend{}
	foreground := uintptr(0xabc)
	guard := &targetGuardBackend{
		inner:      inner,
		targetHWND: foreground,
		targetPID:  123,
		targetID:   "window-abc",
		foreground: func() uintptr { return foreground },
		windowPID:  func(uintptr) uint32 { return 123 },
	}
	if _, err := guard.Observe(context.Background(), "session", 1); err != nil {
		t.Fatal(err)
	}
	foreground = 0xdef
	if _, err := guard.Observe(context.Background(), "session", 2); err == nil {
		t.Fatal("guard allowed capture after foreground HWND changed")
	}
	if inner.observeCalls != 1 {
		t.Fatalf("inner observe calls = %d, want 1", inner.observeCalls)
	}
	foreground = guard.targetHWND
	obs := computeruse.Observation{Foreground: computeruse.Window{ID: guard.targetID, ProcessID: guard.targetPID}}
	if err := guard.Execute(context.Background(), obs, computeruse.Action{Type: "click", Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if inner.executeCalls != 1 {
		t.Fatalf("inner execute calls = %d, want 1", inner.executeCalls)
	}
	for _, action := range []computeruse.Action{
		{Type: "key_combo", Keys: []string{"CTRL", "L"}},
		{Type: "key", Key: "F12"},
		{Type: "activate_window"},
	} {
		if err := guard.Execute(context.Background(), obs, action); err == nil {
			t.Fatalf("target guard allowed unsafe fixture action %+v", action)
		}
	}
	if inner.executeCalls != 1 {
		t.Fatalf("unsafe actions reached inner backend: %d calls", inner.executeCalls)
	}
}

func TestComputerFixtureVisualOnlyObservationIsScreenshotOnly(t *testing.T) {
	inner := &fixtureVisualObservationBackend{}
	guard := &targetGuardBackend{
		inner:         inner,
		targetHWND:    uintptr(0xabc),
		targetPID:     123,
		targetID:      "window-abc",
		foreground:    func() uintptr { return uintptr(0xabc) },
		windowPID:     func(uintptr) uint32 { return 123 },
		stripElements: true,
	}
	obs, err := guard.Observe(context.Background(), "session", 1)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Screenshot == "" || len(obs.Elements) != 0 {
		t.Fatalf("visual-only observation is not screenshot-only: screenshot=%t elements=%v", obs.Screenshot != "", obs.Elements)
	}
	if len(obs.Windows) != 1 || obs.Windows[0].ID != "window-abc" {
		t.Fatalf("visual-only windows = %+v, want only target window", obs.Windows)
	}
	if strings.Contains(obs.Summary, "PRIVATE") || strings.Contains(obs.Summary, "element-secret") || strings.Contains(obs.Summary, "UIA") {
		t.Fatalf("visual-only summary leaked UIA/private data: %q", obs.Summary)
	}
	for _, marker := range []string{"O.R.C.A Computer Fixture target", "bounds=10,20 900x700", "crop=1,2 800x600"} {
		if !strings.Contains(obs.Summary, marker) {
			t.Fatalf("visual-only summary missing allowed marker %q: %q", marker, obs.Summary)
		}
	}
}

func TestComputerE2ERequestTraceStopsBeforeDial(t *testing.T) {
	computerE2ECumulativeRequests.Store(computerE2EMaxModelRequests)
	t.Cleanup(func() { computerE2ECumulativeRequests.Store(0) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget := &computerE2ERequestBudget{limit: computerE2EMaxModelRequests, cancel: cancel}
	var dials atomic.Int64
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("dial must not be reached after budget overflow")
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(withComputerE2ERequestTrace(ctx, budget), http.MethodGet, "https://api.deepseek.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("budget-overflow request unexpectedly succeeded")
	}
	if dials.Load() != 0 {
		t.Fatalf("transport dialed after budget overflow: %d", dials.Load())
	}
	if got := computerE2ECumulativeRequests.Load(); got != computerE2EMaxModelRequests+1 {
		t.Fatalf("request trace count = %d, want %d", got, computerE2EMaxModelRequests+1)
	}
}

func TestComputerE2EContract(t *testing.T) {
	if computerControlMaxSteps != computerE2EMaxSteps {
		t.Fatalf("production computer step limit = %d, want %d", computerControlMaxSteps, computerE2EMaxSteps)
	}
	goal := computerE2ENaturalGoal()
	if strings.Contains(goal, "/__validator/") || strings.Contains(goal, "DEEPSEEK_API_KEY") || !strings.Contains(goal, "开发者工具") {
		t.Fatal("natural goal must keep validator, credential and developer-tool state out of the model task")
	}
}

func TestComputerE2ENativeObservation(t *testing.T) {
	if os.Getenv("ORCA_COMPUTER_E2E") != "1" {
		t.Skip("native observation is opt-in")
	}
	f := newComputerFixture(t)
	cmd, hwnd, pid := startComputerFixtureBrowser(t, f.pageURL(), computerE2ETitlePrefix+" "+f.expected.RunID)
	defer stopComputerFixtureBrowser(cmd)
	if err := setFixtureForeground(hwnd, pid); err != nil {
		t.Fatal(err)
	}
	backend := computeruse.NewPlatformBackend()
	defer backend.HideOverlay()
	defer backend.ReleaseInjectedInput()
	for generation := uint64(1); generation <= 3; generation++ {
		obs, err := backend.Observe(context.Background(), "native-observation", generation)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Foreground.ProcessID != pid || len(obs.Elements) == 0 || obs.Screenshot == "" {
			t.Fatalf("invalid native observation: foreground=%d elements=%d screenshot=%t", obs.Foreground.ProcessID, len(obs.Elements), obs.Screenshot != "")
		}
		t.Logf("native observation %d: elements=%d screenshot=%dx%d", generation, len(obs.Elements), obs.ScreenshotWidth, obs.ScreenshotHeight)
		t.Logf("physical geometry: crop=%+v window=%+v firstElement=%+v", obs.Crop, obs.Foreground.Bounds, obs.Elements[0].Bounds)
		if err := backend.Execute(context.Background(), obs, computeruse.Action{Generation: generation, Type: "hover", X: 0.5, Y: 0.5}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestComputerE2ELiveQualification(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if os.Getenv("ORCA_COMPUTER_E2E") != "1" || key == "" {
		t.Skip("live synthetic qualification is opt-in")
	}
	loadComputerE2EBudget(t)
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", key)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "orca.toml"), []byte(computerE2EProjectConfig), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.ResolveModel("deepseek-flash/deepseek-v4-flash-vision-exp")
	if !ok {
		t.Fatal("fixture model not found")
	}
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	budget := &computerE2ERequestBudget{limit: computerE2EMaxModelRequests, cancel: cancel}
	defer func() { t.Logf("qualification attempts=%d", budget.used.Load()) }()
	if err := probeComputerProvider(withComputerE2ERequestTrace(ctx, budget), prov); err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
}

func TestComputerE2ENativeController(t *testing.T) {
	// Capture the credential before test isolation changes the process's config
	// environment. It is restored only to the isolated test process and never
	// included in diagnostics, fixture HTML, or persisted files.
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if strings.TrimSpace(os.Getenv("ORCA_COMPUTER_E2E")) != "1" {
		t.Skip("set ORCA_COMPUTER_E2E=1 to run the real model/native Computer Use case")
	}
	if strings.TrimSpace(apiKey) == "" {
		t.Skip("DEEPSEEK_API_KEY is required; the harness never prints or persists it")
	}
	loadComputerE2EBudget(t)
	for repetition := 1; repetition <= 3; repetition++ {
		for _, stripElements := range []bool{false, true} {
			mode := "uia"
			if stripElements {
				mode = "visual-only"
			}
			t.Run(fmt.Sprintf("repeat-%d-%s", repetition, mode), func(t *testing.T) {
				runComputerE2ENativeCase(t, apiKey, stripElements)
			})
		}
	}
}

func runComputerE2ENativeCase(t *testing.T, apiKey string, stripElements bool) {
	t.Helper()
	// Keep every credential/config lookup inside per-test temporary user dirs.
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", apiKey)

	fixture := newComputerFixture(t)
	cmd, hwnd, pid := startComputerFixtureBrowser(t, fixture.pageURL(), computerE2ETitlePrefix+" "+fixture.expected.RunID)
	t.Cleanup(func() { stopComputerFixtureBrowser(cmd) })
	if err := setFixtureForeground(hwnd, pid); err != nil {
		t.Fatal(err)
	}

	budgetCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget := &computerE2ERequestBudget{limit: computerE2EMaxModelRequests, cancel: cancel}
	sink := &computerE2EEventSink{}
	guard := &targetGuardBackend{
		inner:         computeruse.NewPlatformBackend(),
		targetHWND:    hwnd,
		targetPID:     pid,
		targetID:      fmt.Sprintf("window-%x", hwnd),
		foreground:    fixtureForegroundWindow,
		windowPID:     fixtureWindowPID,
		stripElements: stripElements,
		log:           t.Logf,
	}
	app := NewApp()
	app.computerUse = computeruse.NewService(guard, app.onComputerUseEvent)
	t.Cleanup(func() {
		session := app.computerUse.Current()
		fixture.mu.Lock()
		observed := fixture.observed
		fixture.mu.Unlock()
		t.Logf("independent fixture result: %+v", observed)
		observes, actions, elements, screenshots := guard.stats()
		t.Logf("native evidence: state=%s actions=%d observations=%d UIA=%d screenshots=%d request_attempts=%d batch_attempts=%d", session.State, actions, observes, elements, screenshots, budget.used.Load(), computerE2ECumulativeRequests.Load())
		for _, e := range sink.snapshot() {
			if e.Kind == event.ToolResult && e.Tool.Name == "computer_task" {
				t.Logf("computer task result: %s", sanitizeComputerE2EError(errors.New(e.Tool.Err+" "+e.Tool.Output)))
			}
		}
		_ = app.computerUse.StopSession(session.ID, "fixture cleanup")
	})
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "orca.toml"), []byte(computerE2EProjectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	ctrl, err := app.buildController(budgetCtx, boot.Options{
		Model:          "deepseek-flash",
		RequireKey:     true,
		Sink:           sink,
		WorkspaceRoot:  workspace,
		SessionDir:     filepath.Join(workspace, "sessions"),
		RuntimeProfile: boot.RuntimeProfileOrca,
	}, "test")
	if err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
	defer ctrl.Close()
	ctrl.SetSessionPath(filepath.Join(workspace, "sessions", "fixture-parent.jsonl"))
	app.setTestCtrl(ctrl, "deepseek-flash/deepseek-v4-flash")
	testTab := app.tabs["test"]
	testTab.WorkspaceRoot = workspace
	if testTab.WorkspaceRoot != workspace {
		t.Fatalf("test tab workspace root = %q, want fixture workspace %q", testTab.WorkspaceRoot, workspace)
	}
	// The owner-aware production buildController path registered the tools with
	// the real test tab. Keep only the task and emergency stop surfaces for this
	// fixture; the status surface is intentionally outside this test contract.
	restrictComputerE2ERegistry(t, ctrl)

	caseCtx, caseCancel := context.WithTimeout(budgetCtx, computerE2ECaseTimeout)
	defer caseCancel()
	caseCtx = withComputerE2ERequestTrace(caseCtx, budget)
	if err := ctrl.RunTurn(caseCtx, computerE2ENaturalGoal()); err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
	state, err := waitForComputerFixtureSuccess(caseCtx, fixture)
	if err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
	if !state.Observed.Success || !state.Observed.Submitted || state.Observed.Name != state.Expected.TargetName || state.Observed.Region != state.Expected.TargetRegion || state.Observed.Item != state.Expected.TargetItem || !state.Observed.Checked || !state.Observed.FilterUsed || !state.Observed.Scrolled || !state.Observed.Dragged {
		t.Fatalf("fixture validator did not confirm all independent conditions: %+v", state)
	}
	events := sink.snapshot()
	if !hasComputerTaskDispatch(events) {
		t.Fatal("main Controller did not dispatch computer_task")
	}
	session := app.computerUse.Current()
	if session.State != computeruse.StateSucceeded || session.ModelRef != "deepseek-flash/deepseek-v4-flash-vision-exp" {
		t.Fatalf("computer session did not complete through the vision subagent: %+v", session)
	}
	observes, actions, elements, screenshots := guard.stats()
	if observes < 2 || actions == 0 || screenshots == 0 {
		t.Fatalf("native evidence incomplete: observes=%d actions=%d maxUIAElements=%d screenshots=%d", observes, actions, elements, screenshots)
	}
	if stripElements && elements != 0 {
		t.Fatalf("visual-only guard returned UIA elements: %d", elements)
	}
	if !stripElements && elements == 0 {
		t.Fatalf("UIA fixture run returned no elements")
	}
	if used := budget.used.Load(); used > computerE2EMaxModelRequests {
		t.Fatalf("request budget exceeded: %d", used)
	}
	if used := computerE2ECumulativeRequests.Load(); used > computerE2EMaxModelRequests {
		t.Fatalf("cumulative request budget exceeded: %d", used)
	}
}

func withComputerE2ERequestTrace(ctx context.Context, budget *computerE2ERequestBudget) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			if strings.EqualFold(hostPort, "api.deepseek.com:443") {
				_ = budget.reserve()
			}
		},
	})
}

func restrictComputerE2ERegistry(t *testing.T, controller *control.Controller) {
	t.Helper()
	if controller == nil {
		t.Fatal("production controller is nil")
	}
	// Controller intentionally exposes ToolNames read-only. The test must
	// filter the already-built production registry rather than merely assert a
	// prompt restriction, so use this Windows-only adapter until Controller has
	// an exported test-safe registry filter. A field rename fails loudly.
	field := reflect.ValueOf(controller).Elem().FieldByName("reg")
	if !field.IsValid() || field.Type() != reflect.TypeOf((*tool.Registry)(nil)) {
		t.Fatal("production controller registry field changed; refusing unfiltered E2E")
	}
	registry := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*tool.Registry)
	allowed := map[string]bool{"computer_task": true, "computer_stop": true}
	for _, name := range controller.ToolNames() {
		if !allowed[name] {
			registry.Remove(name)
		}
	}
	got := controller.ToolNames()
	if len(got) != len(allowed) {
		t.Fatalf("computer fixture controller tools = %v, want only computer_task and computer_stop", got)
	}
	for _, name := range got {
		if !allowed[name] {
			t.Fatalf("forbidden tool remained in computer fixture controller registry: %q", name)
		}
	}
}

func computerE2ENaturalGoal() string {
	return "请在当前前台的 O.R.C.A 计算机使用测试页面完成全部任务：只根据屏幕上可见的中文内容填写姓名和地区，勾选已核对；先把长条目列表滚动到底部，再使用筛选框定位页面上显示的目标条目并选择；把画布中的蓝色标记拖入绿色目标区，然后提交。成功条件是页面明确显示‘提交成功：全部条件已满足’。只能使用当前屏幕和 UI Automation，不得读取任何 HTTP 状态端点、文件、开发者工具或凭据；不要离开目标测试窗口。"
}

const computerE2EProjectConfig = `default_model = "deepseek-flash"

[desktop]
vision_mode = "on"
computer_control_model = "deepseek-flash/deepseek-v4-flash-vision-exp"
computer_use_full_access_approved = true
computer_use_consent_version = 1

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
models = ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"]
api_key_env = "DEEPSEEK_API_KEY"
`

func waitForComputerFixtureSuccess(ctx context.Context, fixture *computerFixture) (computerFixtureStateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := fixture.readValidatorState(ctx)
		if err == nil && state.Observed.Success {
			return state, nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return computerFixtureStateResponse{}, ctx.Err()
			}
			return state, fmt.Errorf("fixture did not reach success after the main Agent finished: %+v", state.Observed)
		case <-ticker.C:
		}
	}
}

func sanitizeComputerE2EError(err error) error {
	if err == nil {
		return nil
	}
	secret := os.Getenv("DEEPSEEK_API_KEY")
	message := strings.ReplaceAll(err.Error(), secret, "[redacted]")
	if strings.TrimSpace(message) == "" {
		message = "computer E2E failed"
	}
	return errors.New(message)
}

var (
	fixtureUser32                   = syscall.NewLazyDLL("user32.dll")
	fixtureEnumWindows              = fixtureUser32.NewProc("EnumWindows")
	fixtureIsWindowVisible          = fixtureUser32.NewProc("IsWindowVisible")
	fixtureGetWindowTextLengthW     = fixtureUser32.NewProc("GetWindowTextLengthW")
	fixtureGetWindowTextW           = fixtureUser32.NewProc("GetWindowTextW")
	fixtureGetWindowThreadProcessID = fixtureUser32.NewProc("GetWindowThreadProcessId")
	fixtureGetForegroundWindow      = fixtureUser32.NewProc("GetForegroundWindow")
	fixtureSetForegroundWindow      = fixtureUser32.NewProc("SetForegroundWindow")
)

func fixtureWindowText(hwnd uintptr) string {
	length, _, _ := fixtureGetWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, int(length)+1)
	fixtureGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func fixtureWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	fixtureGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func fixtureForegroundWindow() uintptr {
	hwnd, _, _ := fixtureGetForegroundWindow.Call()
	return hwnd
}

func findFixtureWindow(title string) (uintptr, uint32) {
	var found uintptr
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		visible, _, _ := fixtureIsWindowVisible.Call(hwnd)
		if visible != 0 && fixtureWindowText(hwnd) == title {
			found = hwnd
			return 0
		}
		return 1
	})
	fixtureEnumWindows.Call(callback, 0)
	if found == 0 {
		return 0, 0
	}
	return found, fixtureWindowPID(found)
}

func startComputerFixtureBrowser(t *testing.T, pageURL, title string) (*exec.Cmd, uintptr, uint32) {
	t.Helper()
	browser := findFixtureBrowser()
	if browser == "" {
		t.Skip("Microsoft Edge or Chrome is required for the opt-in native fixture")
	}
	profile := filepath.Join(t.TempDir(), "browser-profile")
	cmd := exec.Command(browser, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--disable-sync", "--new-window", "--window-size=1100,850", "--app="+pageURL)
	if err := cmd.Start(); err != nil {
		t.Fatal(sanitizeComputerE2EError(err))
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if hwnd, pid := findFixtureWindow(title); hwnd != 0 && pid != 0 {
			return cmd, hwnd, pid
		}
		time.Sleep(200 * time.Millisecond)
	}
	stopComputerFixtureBrowser(cmd)
	t.Fatalf("fixture browser window %q did not appear", title)
	return nil, 0, 0
}

func findFixtureBrowser() string {
	var candidates []string
	for _, name := range []string{"msedge.exe", "chrome.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	// PATH is commonly absent in desktop-launched test shells, so include the
	// standard absolute Edge/Chrome installs as a deterministic fallback.
	for _, path := range []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if path != "" {
			candidates = append(candidates, path)
		}
	}
	seen := map[string]bool{}
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func stopComputerFixtureBrowser(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill.exe", "/PID", pid, "/T", "/F").Run()
	_, _ = cmd.Process.Wait()
}

func setFixtureForeground(hwnd uintptr, pid uint32) error {
	if hwnd == 0 || pid == 0 {
		return fmt.Errorf("invalid fixture target hwnd=%x pid=%d", hwnd, pid)
	}
	fixtureSetForegroundWindow.Call(hwnd)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fixtureForegroundWindow() == hwnd && fixtureWindowPID(hwnd) == pid {
			return nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	return fmt.Errorf("could not foreground fixture target hwnd=%x pid=%d; actual=%x actualPID=%d", hwnd, pid, fixtureForegroundWindow(), fixtureWindowPID(fixtureForegroundWindow()))
}
