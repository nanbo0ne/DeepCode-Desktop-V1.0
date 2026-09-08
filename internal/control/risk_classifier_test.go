package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type fakeRiskClassifier struct {
	assessment permission.RiskAssessment
	err        error
	calls      int
	input      permission.RiskInput
}

func (f *fakeRiskClassifier) Assess(_ context.Context, input permission.RiskInput) (permission.RiskAssessment, error) {
	f.calls++
	f.input = input
	return f.assessment, f.err
}

type riskAuditRecorder struct {
	mu        sync.Mutex
	audits    []event.RiskReviewAudit
	approvals chan string
}

func newRiskAuditRecorder() *riskAuditRecorder {
	return &riskAuditRecorder{approvals: make(chan string, 4)}
}

func (r *riskAuditRecorder) Emit(e event.Event) {
	if e.Kind != event.ApprovalRequest || e.Approval.ID == "" {
		return
	}
	select {
	case r.approvals <- e.Approval.ID:
	default:
	}
}

func (r *riskAuditRecorder) RecordRiskReviewAudit(a event.RiskReviewAudit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audits = append(r.audits, a)
}

func (r *riskAuditRecorder) snapshot() []event.RiskReviewAudit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.RiskReviewAudit(nil), r.audits...)
}

func TestProviderRiskClassifierUsesIsolatedStrictRequest(t *testing.T) {
	p := &classifierProvider{text: `{"level":"medium","reason":"bounded write"}`}
	c := NewProviderRiskClassifier(p)
	got, err := c.Assess(context.Background(), permission.RiskInput{Tool: "write_file", Operation: "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != permission.RiskMedium || got.Reason != "bounded write" {
		t.Fatalf("assessment = %+v", got)
	}
	if len(p.req.Messages) != 2 || len(p.req.Tools) != 0 || p.req.Temperature != 0 || p.req.MaxTokens != 256 || !p.req.DisableThinking {
		t.Fatalf("classifier request leaked execution context: %+v", p.req)
	}
}

func TestProviderRiskClassifierEmitsAttributedUsageReceipt(t *testing.T) {
	p := &classifierProvider{
		text:  `{"level":"low","reason":"read only"}`,
		usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13},
	}
	var got event.Event
	sink := event.FuncSink(func(e event.Event) { got = e })
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	c := NewProviderRiskClassifier(p).WithTelemetry(sink, pricing, "https://api.deepseek.com")
	if _, err := c.AssessWithParentTurn(context.Background(), permission.RiskInput{Tool: "read_file"}, "turn-parent"); err != nil {
		t.Fatal(err)
	}
	if got.Kind != event.Usage || got.RequestID == "" || got.ParentTurnID != "turn-parent" || got.ProviderEndpoint != "https://api.deepseek.com" || got.Usage == nil {
		t.Fatalf("risk usage receipt = %+v", got)
	}
}

type riskStreamProvider struct{ chunks chan provider.Chunk }

func (p riskStreamProvider) Name() string { return "risk-stream" }
func (p riskStreamProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return p.chunks, nil
}

func TestRiskClassifierRejectsIncompleteOrOversizedStream(t *testing.T) {
	for _, value := range []string{`{"level":"low","reason":"ok"}`, strings.Repeat("x", 2049)} {
		ch := make(chan provider.Chunk, 1)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: value}
		close(ch)
		if _, err := NewProviderRiskClassifier(riskStreamProvider{ch}).Assess(context.Background(), permission.RiskInput{}); err == nil {
			t.Fatal("invalid stream accepted")
		}
	}
}

func TestRiskClassifierCancellationDoesNotWaitForProviderClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewProviderRiskClassifier(riskStreamProvider{make(chan provider.Chunk)}).Assess(ctx, permission.RiskInput{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestProviderRiskClassifierRejectsMalformedOutput(t *testing.T) {
	for _, response := range []string{"", "```json\n{\"level\":\"low\",\"reason\":\"x\"}\n```", `{"level":"critical","reason":"x"}`, `{"level":"low"}`} {
		p := &classifierProvider{text: response}
		if _, err := NewProviderRiskClassifier(p).Assess(context.Background(), permission.RiskInput{Tool: "x"}); err == nil {
			t.Fatalf("response %q should fail strict decoding", response)
		}
	}
}

func TestAutoRiskReviewLowMediumHighAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name            string
		level           permission.RiskLevel
		err             error
		wantApproval    bool
		wantAutoAllow   bool
		wantAuditResult string
	}{
		{name: "low", level: permission.RiskLow, wantAutoAllow: true, wantAuditResult: "auto_allow"},
		{name: "medium", level: permission.RiskMedium, wantAutoAllow: true, wantAuditResult: "auto_allow"},
		{name: "high", level: permission.RiskHigh, wantApproval: true, wantAutoAllow: true, wantAuditResult: "manual_review"},
		{name: "failure fallback", err: errors.New("invalid json"), wantAutoAllow: true, wantAuditResult: "fallback_allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			classifier := &fakeRiskClassifier{assessment: permission.RiskAssessment{Level: tc.level, Reason: "test"}, err: tc.err}
			recorder := newRiskAuditRecorder()
			c := New(Options{Policy: permission.New("ask", nil, nil, nil), Sink: recorder, RiskClassifier: classifier, RiskModel: "review/model"})
			c.SetToolApprovalMode(ToolApprovalAuto)
			gate := c.newInteractiveGate()

			type result struct {
				allow bool
				err   error
			}
			done := make(chan result, 1)
			go func() {
				allow, _, err := gate.Check(context.Background(), "write_file", json.RawMessage(`{"path":"README.md"}`), false)
				done <- result{allow: allow, err: err}
			}()

			if tc.wantApproval {
				select {
				case approvalID := <-recorder.approvals:
					c.Approve(approvalID, true, false, false)
				case <-time.After(2 * time.Second):
					t.Fatal("high risk review did not request approval")
				}
			}
			select {
			case got := <-done:
				if got.err != nil || got.allow != tc.wantAutoAllow {
					t.Fatalf("gate result allow=%v err=%v", got.allow, got.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("gate did not finish")
			}
			audits := recorder.snapshot()
			if len(audits) != 1 || audits[0].Result != tc.wantAuditResult || audits[0].Model != "review/model" {
				t.Fatalf("audits = %+v", audits)
			}
		})
	}
}

func TestAutoRiskReviewDoesNotRunForExplicitRules(t *testing.T) {
	classifier := &fakeRiskClassifier{assessment: permission.RiskAssessment{Level: permission.RiskLow, Reason: "x"}}
	recorder := newRiskAuditRecorder()
	c := New(Options{Policy: permission.New("ask", nil, []string{"bash(git commit*)"}, []string{"bash(rm*)"}), Sink: recorder, RiskClassifier: classifier})
	c.SetToolApprovalMode(ToolApprovalAuto)
	gate := c.newInteractiveGate()

	allow, _, err := gate.Check(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf build"}`), false)
	if err != nil || allow || classifier.calls != 0 {
		t.Fatalf("deny precedence allow=%v err=%v calls=%d", allow, err, classifier.calls)
	}

	type result struct {
		allow bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		allow, _, err := gate.Check(context.Background(), "bash", json.RawMessage(`{"command":"git commit -m x"}`), false)
		done <- result{allow: allow, err: err}
	}()
	select {
	case approvalID := <-recorder.approvals:
		c.Approve(approvalID, false, false, false)
	case <-time.After(2 * time.Second):
		t.Fatal("explicit ask rule did not request approval")
	}
	select {
	case got := <-done:
		if got.err != nil || got.allow {
			t.Fatalf("explicit ask result allow=%v err=%v", got.allow, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("explicit ask rule did not finish")
	}
	if classifier.calls != 0 {
		t.Fatalf("explicit ask called classifier %d times", classifier.calls)
	}
}

func TestRiskReviewErrorType(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{err: context.DeadlineExceeded, want: "timeout"},
		{err: context.Canceled, want: "cancelled"},
		{err: errors.New("decode risk classifier response: invalid json"), want: "invalid_response"},
		{err: errors.New("risk classifier is not configured"), want: "unavailable"},
		{err: errors.New("connection reset"), want: "provider_error"},
	} {
		if got := riskReviewErrorType(tc.err); got != tc.want {
			t.Fatalf("riskReviewErrorType(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
