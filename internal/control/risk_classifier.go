package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/nilutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const riskClassifierPrompt = `You are an isolated tool-operation risk classifier.
You receive only a redacted tool name, operation summary, arguments, read-only flag, and host security context.
Classify operational risk, not task quality.
Return exactly one JSON object with no markdown or extra text: {"level":"low|medium|high","reason":"short reason"}. Keep the reason under 20 words; do not include analysis.
Use high for destructive, irreversible, security-sensitive, privilege-changing, credential-related, financial, broad data deletion, external publishing, or materially ambiguous operations.
Use medium for bounded writes or process/network changes that are reversible and scoped.
Use low for read-only or routine, narrowly-scoped, easily reversible operations.`

type ProviderRiskClassifier struct {
	prov provider.Provider
}

func NewProviderRiskClassifier(prov provider.Provider) *ProviderRiskClassifier {
	if nilutil.IsNil(prov) {
		return nil
	}
	return &ProviderRiskClassifier{prov: prov}
}

func (c *ProviderRiskClassifier) Assess(ctx context.Context, input permission.RiskInput) (permission.RiskAssessment, error) {
	if c == nil || nilutil.IsNil(c.prov) {
		return permission.RiskAssessment{}, fmt.Errorf("risk classifier is not initialized")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	payload, err := json.Marshal(input)
	if err != nil {
		return permission.RiskAssessment{}, fmt.Errorf("encode risk input: %w", err)
	}
	ch, err := c.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: riskClassifierPrompt},
			{Role: provider.RoleUser, Content: string(payload)},
		},
		Temperature:     0,
		MaxTokens:       256,
		DisableThinking: true,
	})
	if err != nil {
		return permission.RiskAssessment{}, err
	}

	var text strings.Builder
	completed := false
read:
	for {
		var chunk provider.Chunk
		select {
		case <-ctx.Done():
			return permission.RiskAssessment{}, ctx.Err()
		case next, ok := <-ch:
			if !ok {
				break read
			}
			chunk = next
		}
		switch chunk.Type {
		case provider.ChunkText:
			if text.Len()+len(chunk.Text) > 2048 {
				return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: output exceeds limit")
			}
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err == nil {
				return permission.RiskAssessment{}, fmt.Errorf("risk classifier stream failed")
			}
			return permission.RiskAssessment{}, chunk.Err
		case provider.ChunkDone:
			completed = true
		}
	}
	if !completed {
		return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: incomplete stream")
	}

	var out struct {
		Level  permission.RiskLevel `json:"level"`
		Reason string               `json:"reason"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text.String())))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: trailing JSON")
		}
		return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: %w", err)
	}
	switch out.Level {
	case permission.RiskLow, permission.RiskMedium, permission.RiskHigh:
	default:
		return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: invalid level %q", out.Level)
	}
	reason := strings.TrimSpace(out.Reason)
	if reason == "" {
		return permission.RiskAssessment{}, fmt.Errorf("decode risk classifier response: missing reason")
	}
	if runes := []rune(reason); len(runes) > 240 {
		reason = string(runes[:240])
	}
	return permission.RiskAssessment{Level: out.Level, Reason: reason}, nil
}
