package artifact

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Preview(path, output string) (string, error) {
	return PreviewContext(context.Background(), path, output)
}

// PreviewContext renders only the first PDF page. It never treats a successful
// renderer invocation as visual verification of the document or remaining pages.
func PreviewContext(ctx context.Context, path, output string) (string, error) {
	v, err := Validate(path)
	if err != nil {
		return "", err
	}
	if v.Format != "pdf" {
		return "", fmt.Errorf("preview unavailable for %s: no Office renderer is configured; structural validation is not visual verification", v.Format)
	}
	bin, err := exec.LookPath("pdftoppm")
	if err != nil {
		return "", fmt.Errorf("PDF preview unavailable: install Poppler pdftoppm on PATH: %w", err)
	}
	if output == "" {
		output = path + ".preview.png"
	}
	if !strings.EqualFold(filepath.Ext(output), ".png") {
		return "", fmt.Errorf("preview output must have a .png extension")
	}
	source, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(source, destination) || strings.EqualFold(SidecarPath(source), destination) {
		return "", fmt.Errorf("preview output must not overwrite the artifact or sidecar")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return "", fmt.Errorf("preview output already exists or is inaccessible; choose a new output path")
	}
	tmp, err := os.MkdirTemp("", "orca-pdf-preview-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := filepath.Join(tmp, "page")
	cmd := exec.CommandContext(ctx, bin, "-f", "1", "-l", "1", "-singlefile", "-scale-to", "1600", "-png", source, prefix)
	if diagnostic, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("PDF preview rendering failed: %w: %s", err, strings.TrimSpace(string(diagnostic)))
	}
	data, err := os.ReadFile(prefix + ".png")
	if err != nil {
		return "", fmt.Errorf("PDF renderer produced no PNG: %w", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("PDF renderer produced invalid PNG: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return output, nil
}
