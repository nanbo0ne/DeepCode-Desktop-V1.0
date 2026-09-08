package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func qualificationAnswer(t *testing.T, r provider.Request) string {
	t.Helper()
	if !r.DisableThinking || len(r.Tools) != 1 || len(r.Messages) != 1 || len(r.Messages[0].Images) != 1 {
		t.Fatal("qualification not isolated image/function call")
	}
	data, err := base64.StdEncoding.DecodeString(r.Messages[0].Images[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	names := map[[3]uint32]string{{230, 30, 30}: "red", {20, 190, 20}: "green", {25, 45, 230}: "blue", {240, 220, 20}: "yellow", {220, 20, 210}: "magenta", {20, 215, 220}: "cyan"}
	read := func(x, y int) string {
		r, g, b, _ := img.At(x, y).RGBA()
		return names[[3]uint32{r >> 8, g >> 8, b >> 8}]
	}
	shape := "square"
	if read(20, 20) == "" {
		shape = "circle"
	}
	body, _ := json.Marshal(map[string]string{"colors": read(40, 40) + "," + read(120, 40), "shape": shape})
	return string(body)
}

func TestComputerQualificationRequiresImageAndFunction(t *testing.T) {
	for _, mode := range []string{"pass", "spaced-colors", "reversed-colors", "text-only", "wrong-image", "batch", "truncated", "failed"} {
		t.Run(mode, func(t *testing.T) {
			p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
				args := qualificationAnswer(t, r)
				switch mode {
				case "spaced-colors", "reversed-colors":
					var answer map[string]string
					if err := json.Unmarshal([]byte(args), &answer); err != nil {
						t.Fatal(err)
					}
					parts := strings.Split(answer["colors"], ",")
					if mode == "reversed-colors" {
						parts[0], parts[1] = parts[1], parts[0]
					}
					answer["colors"] = strings.Join(parts, ", ")
					body, _ := json.Marshal(answer)
					args = string(body)
				case "text-only":
					return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: args}), nil
				case "wrong-image":
					args = `{"colors":"wrong","shape":"square"}`
				case "batch":
					return controlChunks(provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{Name: "report_image", Arguments: args}}, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{Name: "report_image", Arguments: args}}), nil
				case "truncated":
					ch := make(chan provider.Chunk, 1)
					ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{Name: "report_image", Arguments: args}}
					close(ch)
					return ch, nil
				case "failed":
					return nil, errors.New("synthetic network failure")
				}
				return controlCall("report_image", args), nil
			}}
			err := probeComputerProvider(context.Background(), p)
			if (err == nil) != (mode == "pass" || mode == "spaced-colors") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}

func TestComputerQualificationCacheIdentityAndFailures(t *testing.T) {
	entry := (&config.ProviderEntry{Name: t.Name(), Kind: "openai", BaseURL: "https://synthetic.invalid", Model: "vision", APIKeyEnv: "SLOT_A"}).WithAPIKey("synthetic-key")
	key := computerQualificationKey(entry)
	computerQualifications.Lock()
	delete(computerQualifications.passed, key)
	computerQualifications.Unlock()
	t.Cleanup(func() {
		computerQualifications.Lock()
		delete(computerQualifications.passed, key)
		computerQualifications.Unlock()
	})
	for _, change := range []func(*config.ProviderEntry){func(e *config.ProviderEntry) { e.Name = "different" }, func(e *config.ProviderEntry) { e.Kind = "anthropic" }, func(e *config.ProviderEntry) { e.BaseURL += "/v1" }, func(e *config.ProviderEntry) { e.Model = "other" }, func(e *config.ProviderEntry) { e.APIKeyEnv = "SLOT_B" }, func(e *config.ProviderEntry) { *e = *e.WithAPIKey("rotated") }} {
		copy := *entry
		change(&copy)
		if computerQualificationKey(&copy) == key {
			t.Fatal("cache aliases different endpoint/model/credential slot")
		}
	}
	calls := 0
	pass := false
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		calls++
		if !pass {
			return nil, errors.New("synthetic failure")
		}
		return controlCall("report_image", qualificationAnswer(t, r)), nil
	}}
	for i := 0; i < 2; i++ {
		if err := qualifyComputerProvider(context.Background(), p, entry); err == nil {
			t.Fatal("failure passed")
		}
	}
	if calls != 2 {
		t.Fatal("failure cached")
	}
	pass = true
	for i := 0; i < 2; i++ {
		if err := qualifyComputerProvider(context.Background(), p, entry); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal("successful qualification not cached")
	}
}
