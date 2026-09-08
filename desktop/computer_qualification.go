package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/localai"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Qualification is deliberately independent of official capability metadata and
// manual overrides. Nothing is persisted, including credential-derived keys.
var computerQualifications = struct {
	sync.Mutex
	passed map[[32]byte]time.Time
}{passed: make(map[[32]byte]time.Time)}

func computerQualificationKey(entry *config.ProviderEntry) [32]byte {
	fields := []string{entry.Kind, entry.BaseURL, entry.Model, entry.Name, entry.APIKeyEnv, entry.APIKey(), entry.ReasoningProtocol, fmt.Sprint(entry.NoProxy)}
	body, _ := json.Marshal(fields)
	return sha256.Sum256(body)
}

func qualifyComputerProvider(ctx context.Context, prov provider.Provider, entry *config.ProviderEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.Name == localai.ProviderID {
		return nil
	}
	key := computerQualificationKey(entry)
	computerQualifications.Lock()
	until := computerQualifications.passed[key]
	computerQualifications.Unlock()
	if time.Now().Before(until) {
		return nil
	}
	if err := probeComputerProvider(ctx, prov); err != nil {
		computerQualifications.Lock()
		delete(computerQualifications.passed, key)
		computerQualifications.Unlock()
		return fmt.Errorf("computer model image/function qualification failed: %w", err)
	}
	computerQualifications.Lock()
	// Bound cache size and lifetime; a changed credential requires a new probe.
	for k, expiry := range computerQualifications.passed {
		if time.Now().After(expiry) {
			delete(computerQualifications.passed, k)
		}
	}
	if len(computerQualifications.passed) >= 64 {
		clear(computerQualifications.passed)
	}
	computerQualifications.passed[key] = time.Now().Add(15 * time.Minute)
	computerQualifications.Unlock()
	return nil
}

// This challenge contains no desktop content. Both randomized shape and color
// order must be read from the image and returned in exactly one schema call.
func computerQualificationImage() (string, string, string, error) {
	var random [3]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", "", err
	}
	names := []string{"red", "green", "blue", "yellow", "magenta", "cyan"}
	colors := []color.RGBA{{230, 30, 30, 255}, {20, 190, 20, 255}, {25, 45, 230, 255}, {240, 220, 20, 255}, {220, 20, 210, 255}, {20, 215, 220, 255}}
	left := int(random[0]) % len(names)
	right := (left + 1 + int(random[1])%(len(names)-1)) % len(names)
	shape := "square"
	if random[2]%2 != 0 {
		shape = "circle"
	}
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			cx, shade := 40, colors[left]
			if x >= 80 {
				cx, shade = 120, colors[right]
			}
			dx, dy := x-cx, y-40
			inside := dx >= -24 && dx <= 24 && dy >= -24 && dy <= 24
			if shape == "circle" {
				inside = dx*dx+dy*dy <= 24*24
			}
			if inside {
				img.SetRGBA(x, y, shade)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), names[left] + "," + names[right], shape, nil
}

func probeComputerProvider(ctx context.Context, prov provider.Provider) error {
	data, order, shape, err := computerQualificationImage()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stream, err := prov.Stream(ctx, provider.Request{DisableThinking: true, Temperature: 0, MaxTokens: 200,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "Inspect the attached test image. Call report_image once with the two colors in left-to-right order (comma separated names chosen from red, green, blue, yellow, magenta, cyan) and the shape (square or circle). No prose.", Images: []provider.ImageContent{{Name: "qualification.png", MediaType: "image/png", Data: data}}}},
		Tools:    []provider.ToolSchema{{Name: "report_image", Description: "Report the observed colors and shape.", Parameters: json.RawMessage(`{"type":"object","required":["colors","shape"],"properties":{"colors":{"type":"string"},"shape":{"type":"string","enum":["square","circle"]}},"additionalProperties":false}`)}},
	})
	if err != nil {
		return err
	}
	var call *provider.ToolCall
	done := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				if !done || call == nil {
					return io.ErrUnexpectedEOF
				}
				var answer struct {
					Colors string `json:"colors"`
					Shape  string `json:"shape"`
				}
				decoder := json.NewDecoder(strings.NewReader(call.Arguments))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&answer); err != nil {
					return err
				}
				var extra any
				colors := strings.Split(answer.Colors, ",")
				for i := range colors {
					colors[i] = strings.ToLower(strings.TrimSpace(colors[i]))
				}
				if decoder.Decode(&extra) != io.EOF || strings.Join(colors, ",") != order || strings.ToLower(strings.TrimSpace(answer.Shape)) != shape {
					return fmt.Errorf("synthetic image evidence does not match: got colors=%q shape=%q, expected colors=%q shape=%q", boundedComputerText(answer.Colors, 100), boundedComputerText(answer.Shape, 40), order, shape)
				}
				return ctx.Err()
			}
			switch chunk.Type {
			case provider.ChunkDone:
				done = true
			case provider.ChunkError:
				return fmt.Errorf("qualification stream error: %v", chunk.Err)
			case provider.ChunkToolCall:
				if call != nil || chunk.ToolCall == nil || chunk.ToolCall.Name != "report_image" || len(chunk.ToolCall.Arguments) > 1024 {
					return fmt.Errorf("qualification requires exactly one valid function call")
				}
				copy := *chunk.ToolCall
				call = &copy
			case provider.ChunkText:
				if strings.TrimSpace(chunk.Text) != "" {
					return fmt.Errorf("qualification requires a function, not prose")
				}
			}
		}
	}
}
