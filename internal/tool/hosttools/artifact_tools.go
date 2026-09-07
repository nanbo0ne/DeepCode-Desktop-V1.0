package hosttools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/artifact"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func decodeArtifactInput(raw json.RawMessage, target any) error {
	if len(raw) > 8<<20 {
		return fmt.Errorf("artifact input exceeds 8 MiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("artifact input must contain exactly one JSON object")
	}
	return nil
}

// ArtifactTools returns the bundled Work artifact surface. It is registered by
// profile-aware boot code for Assistant and Orca only.
func ArtifactTools(workDir string) []tool.Tool {
	return []tool.Tool{
		artifactCreateTool{workDir: workDir}, artifactEditTool{workDir: workDir},
		artifactPreviewTool{workDir: workDir}, artifactValidateTool{workDir: workDir},
	}
}

func artifactPath(root, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(rootAbs, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path is outside the workspace")
	}
	return target, nil
}

type artifactCreateTool struct{ workDir string }

func (artifactCreateTool) Name() string { return "artifact_create" }
func (artifactCreateTool) Description() string {
	return "Create a plain-text DOCX, XLSX, PPTX, or PDF with a sidecar. Checks generated structure, not visual layout. XLSX decimal strings become numbers (leading-zero identifiers and >15-digit values stay text); formulas require a spreadsheet application to recalculate. PDF supports ASCII/basic GB2312, not arbitrary Unicode. PPTX rejects content beyond its bounded text layout."
}
func (artifactCreateTool) ReadOnly() bool { return false }
func (artifactCreateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"format":{"type":"string","enum":["docx","xlsx","pptx","pdf"]},"title":{"type":"string"},"paragraphs":{"type":"array","items":{"type":"string"}},"sheets":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"rows":{"type":"array","items":{"type":"array","items":{"type":"string"}}}}}},"slides":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"bullets":{"type":"array","items":{"type":"string"}}}}}},"required":["path","format"]}`)
}
func (t artifactCreateTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Path, Format, Title string
		Paragraphs          []string
		Sheets              []artifact.WorkbookSheet
		Slides              []artifact.Slide
	}
	if err := decodeArtifactInput(raw, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Format) == "" {
		return "", fmt.Errorf("format is required")
	}
	path, err := artifactPath(t.workDir, p.Path)
	if err != nil {
		return "", err
	}
	result, err := artifact.Create(path, artifact.Model{Format: p.Format, Title: p.Title, Paragraphs: p.Paragraphs, Sheets: p.Sheets, Slides: p.Slides})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"status": "done", "path": path, "sidecar": artifact.SidecarPath(path), "validation": result})
	return string(b), nil
}

type artifactEditTool struct{ workDir string }

func (artifactEditTool) Name() string { return "artifact_edit" }
func (artifactEditTool) Description() string {
	return "Edit an Orca-created artifact with a checksum-protected sidecar, then regenerate and validate it. Legacy sidecars without checksums are readable but cannot be edited because file consistency is unknown."
}
func (artifactEditTool) ReadOnly() bool { return false }
func (artifactEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"operations":{"type":"array","items":{"type":"object","properties":{"op":{"type":"string","enum":["append_text","replace_text","set_cell","add_slide"]},"text":{"type":"string"},"find":{"type":"string"},"replace":{"type":"string"},"sheet":{"type":"integer","minimum":0,"description":"0-based worksheet index: the first worksheet is sheet=0. To edit B2 on the first worksheet, use sheet=0, row=1, column=1."},"row":{"type":"integer","minimum":0,"description":"0-based row index, not the displayed Excel row number: Excel row 1 is row=0; cell B2 requires row=1, column=1."},"column":{"type":"integer","minimum":0,"description":"0-based column index: A is column=0, B is column=1; cell B2 requires row=1, column=1."},"title":{"type":"string"},"bullets":{"type":"array","items":{"type":"string"}}},"required":["op"]}}},"required":["path","operations"]}`)
}
func (t artifactEditTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	type operation struct {
		Op, Find, Title    string
		Text, Replace      *string
		Sheet, Row, Column *int
		Bullets            []string
	}
	var p struct {
		Path       string
		Operations []operation
	}
	if err := decodeArtifactInput(raw, &p); err != nil {
		return "", err
	}
	if len(p.Operations) == 0 || len(p.Operations) > 1000 {
		return "", fmt.Errorf("operations must contain between 1 and 1000 edits")
	}
	path, err := artifactPath(t.workDir, p.Path)
	if err != nil {
		return "", err
	}
	result, err := artifact.Edit(path, func(m *artifact.Model) error {
		for _, op := range p.Operations {
			switch op.Op {
			case "append_text":
				if op.Text == nil {
					return fmt.Errorf("append_text requires text")
				}
				if m.Format != "docx" && m.Format != "pdf" {
					return fmt.Errorf("append_text requires DOCX or PDF")
				}
				m.Paragraphs = append(m.Paragraphs, *op.Text)
			case "replace_text":
				if op.Find == "" || op.Replace == nil {
					return fmt.Errorf("replace_text requires non-empty find and explicit replace")
				}
				m.Title = strings.ReplaceAll(m.Title, op.Find, *op.Replace)
				for i, v := range m.Paragraphs {
					m.Paragraphs[i] = strings.ReplaceAll(v, op.Find, *op.Replace)
				}
				for si := range m.Sheets {
					for ri := range m.Sheets[si].Rows {
						for ci := range m.Sheets[si].Rows[ri] {
							m.Sheets[si].Rows[ri][ci] = strings.ReplaceAll(m.Sheets[si].Rows[ri][ci], op.Find, *op.Replace)
						}
					}
				}
				for si := range m.Slides {
					m.Slides[si].Title = strings.ReplaceAll(m.Slides[si].Title, op.Find, *op.Replace)
					for bi := range m.Slides[si].Bullets {
						m.Slides[si].Bullets[bi] = strings.ReplaceAll(m.Slides[si].Bullets[bi], op.Find, *op.Replace)
					}
				}
			case "set_cell":
				if m.Format != "xlsx" || op.Sheet == nil || op.Row == nil || op.Column == nil || op.Text == nil {
					return fmt.Errorf("set_cell requires XLSX and explicit text, sheet, row, column indices")
				}
				si, ri, ci := *op.Sheet, *op.Row, *op.Column
				if si < 0 || si >= len(m.Sheets) {
					return fmt.Errorf("sheet index out of range")
				}
				if ri < 0 || ri >= artifact.MaxRows || ci < 0 || ci >= artifact.MaxColumns {
					return fmt.Errorf("cell index out of range: row must be 0..%d, column 0..%d", artifact.MaxRows-1, artifact.MaxColumns-1)
				}
				cells := 0
				for _, sheet := range m.Sheets {
					for _, row := range sheet.Rows {
						cells += len(row)
					}
				}
				oldColumns := 0
				if ri < len(m.Sheets[si].Rows) {
					oldColumns = len(m.Sheets[si].Rows[ri])
				}
				if cells+max(0, ci+1-oldColumns) > artifact.MaxCells {
					return fmt.Errorf("workbook exceeds %d cell limit", artifact.MaxCells)
				}
				for len(m.Sheets[si].Rows) <= ri {
					m.Sheets[si].Rows = append(m.Sheets[si].Rows, []string{})
				}
				for len(m.Sheets[si].Rows[ri]) <= ci {
					m.Sheets[si].Rows[ri] = append(m.Sheets[si].Rows[ri], "")
				}
				m.Sheets[si].Rows[ri][ci] = *op.Text
			case "add_slide":
				if m.Format != "pptx" {
					return fmt.Errorf("add_slide requires PPTX")
				}
				m.Slides = append(m.Slides, artifact.Slide{Title: op.Title, Bullets: op.Bullets})
			default:
				return fmt.Errorf("unsupported edit operation %q", op.Op)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"status": "done", "path": path, "validation": result})
	return string(b), nil
}

type artifactPreviewTool struct{ workDir string }

func (artifactPreviewTool) Name() string { return "artifact_preview" }
func (artifactPreviewTool) Description() string {
	return "Render the first PDF page to PNG using optional Poppler pdftoppm on PATH. Office previews are unavailable. Rendering is not full-document visual verification. Output must be a new .png path."
}
func (artifactPreviewTool) ReadOnly() bool { return false }
func (artifactPreviewTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"output":{"type":"string"}},"required":["path"]}`)
}
func (t artifactPreviewTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct{ Path, Output string }
	if err := decodeArtifactInput(raw, &p); err != nil {
		return "", err
	}
	path, err := artifactPath(t.workDir, p.Path)
	if err != nil {
		return "", err
	}
	output := ""
	if strings.TrimSpace(p.Output) != "" {
		output, err = artifactPath(t.workDir, p.Output)
		if err != nil {
			return "", err
		}
	}
	result, err := artifact.PreviewContext(ctx, path, output)
	if err != nil {
		return "", err
	}
	model, err := artifact.Load(path)
	if err != nil {
		return "", err
	}
	consistency := "artifact_sha256_checked"
	warnings := []string{"PDF font is not embedded; renderer font substitution may occur. Inspect the image; other pages are not included."}
	if model.ArtifactSHA256 == "" {
		consistency = "unknown_legacy_sidecar"
		warnings = append(warnings, "Legacy sidecar has no checksum; this renders the actual file but does not establish that all sidecar content appears in it. Editing is disabled.")
	}
	b, _ := json.Marshal(map[string]any{"status": "rendered", "preview": result, "page": 1, "scope": "first_page_only", "sidecarConsistency": consistency, "visualVerified": false, "warnings": warnings})
	return string(b), nil
}

type artifactValidateTool struct{ workDir string }

func (artifactValidateTool) Name() string { return "artifact_validate" }
func (artifactValidateTool) Description() string {
	return "Check artifact structure and optional artifact/sidecar SHA256 checksums. Legacy sidecars receive read-only structural checks with unknown content consistency. No visual verification or formula calculation. Units come from the file; DOCX pagination and PDF text block counts are unknown."
}
func (artifactValidateTool) ReadOnly() bool { return true }
func (artifactValidateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (t artifactValidateTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct{ Path string }
	if err := decodeArtifactInput(raw, &p); err != nil {
		return "", err
	}
	path, err := artifactPath(t.workDir, p.Path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	result, err := artifact.Validate(path)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}
