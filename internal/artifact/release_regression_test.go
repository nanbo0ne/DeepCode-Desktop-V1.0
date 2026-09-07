package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readPart(t *testing.T, path, name string) string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatalf("missing %s", name)
	return ""
}

func TestXLSXNumericAndFormulaStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbers.xlsx")
	_, err := Create(path, Model{Sheets: []WorkbookSheet{{Name: "Numbers", Rows: [][]string{{"12", "-3.5", "1e2", "0012", "1234567890123456", "=SUM(A1:C1)", "NaN", "1e-999"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	sheet := readPart(t, path, "xl/worksheets/sheet1.xml")
	for _, want := range []string{`<c r="A1" t="n"><v>12</v>`, `<c r="B1" t="n"><v>-3.5</v>`, `<c r="C1" t="n"><v>1e2</v>`, `<c r="D1" t="inlineStr">`, `<c r="E1" t="inlineStr">`, `<c r="F1"><f>SUM(A1:C1)</f></c>`, `<c r="G1" t="inlineStr">`, `<c r="H1" t="inlineStr">`} {
		if !strings.Contains(sheet, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(readPart(t, path, "xl/workbook.xml"), `forceFullCalc="1"`) {
		t.Fatal("missing recalc flag")
	}
}

func TestModelBoundsAndMismatchedContent(t *testing.T) {
	for name, model := range map[string]Model{
		"wrong format":     {Format: "xlsx", Paragraphs: []string{"must not disappear"}},
		"duplicate sheets": {Format: "xlsx", Sheets: []WorkbookSheet{{Name: "Data"}, {Name: "data"}}},
		"invalid sheet":    {Format: "xlsx", Sheets: []WorkbookSheet{{Name: "a/b"}}},
		"huge rows":        {Format: "xlsx", Sheets: []WorkbookSheet{{Rows: make([][]string, MaxRows+1)}}},
		"huge columns":     {Format: "xlsx", Sheets: []WorkbookSheet{{Rows: [][]string{make([]string, MaxColumns+1)}}}},
		"huge cell":        {Format: "xlsx", Sheets: []WorkbookSheet{{Rows: [][]string{{strings.Repeat("X", 32768)}}}}},
		"empty formula":    {Format: "xlsx", Sheets: []WorkbookSheet{{Rows: [][]string{{"= "}}}}},
		"invalid control":  {Format: "docx", Paragraphs: []string{"before\x00after"}},
		"invalid utf8":     {Format: "docx", Paragraphs: []string{string([]byte{0xff})}},
		"oversize title":   {Format: "pptx", Slides: []Slide{{Title: strings.Repeat("W", 41)}}},
		"oversize bullet":  {Format: "pptx", Slides: []Slide{{Title: "Title", Bullets: []string{strings.Repeat("W", 391)}}}},
		"many bullets":     {Format: "pptx", Slides: []Slide{{Title: "Title", Bullets: make([]string, 14)}}},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rejected."+model.Format)
			if _, err := Create(path, model); err == nil {
				t.Fatal("expected rejection")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("rejected input created artifact: %v", err)
			}
		})
	}
}

func TestPPTXWrappedLinesStayInsideSlide(t *testing.T) {
	slide := Slide{Title: strings.Repeat("W", 40), Bullets: []string{strings.Repeat("\u4e2d", 390)}}
	lines, err := slideLines(slide)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 15 {
		t.Fatalf("got %d lines", len(lines))
	}
	lastBottom := 0
	for _, line := range lines {
		if line.y < lastBottom || line.y+line.height > 6858000 {
			t.Fatalf("overlap/overflow: %+v", line)
		}
		lastBottom = line.y + line.height
	}
	var text strings.Builder
	for _, line := range lines[2:] {
		text.WriteString(line.text)
	}
	if text.String() != slide.Bullets[0] {
		t.Fatal("wrapped text was lost")
	}
}

func TestValidationAndEditRejectDrift(t *testing.T) {
	for _, format := range []string{"docx", "xlsx", "pptx", "pdf"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "drift."+format)
			v, err := Create(path, Model{Title: "Original"})
			if err != nil {
				t.Fatal(err)
			}
			if v.VisualVerified || v.Scope == "" || len(v.Warnings) == 0 {
				t.Fatalf("misleading validation: %+v", v)
			}
			data, _ := os.ReadFile(path)
			data = append(data, []byte("externally changed")...)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Validate(path); err == nil {
				t.Fatal("corruption accepted")
			}
			if _, err := Edit(path, func(m *Model) error { m.Title = "Lost"; return nil }); err == nil {
				t.Fatal("external change overwritten")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, data) {
				t.Fatal("failed edit changed original")
			}
		})
	}
}

func TestPreviewUnavailableAndSourceProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	if _, err := Create(path, Model{Title: "Actual source"}); err != nil {
		t.Fatal(err)
	}
	t.Run("missing renderer", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		output := path + ".missing.png"
		if _, err := Preview(path, output); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("error = %v", err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("fake preview created")
		}
	})
	for _, output := range []string{path, SidecarPath(path)} {
		before, _ := os.ReadFile(output)
		if _, err := Preview(path, output); err == nil {
			t.Fatal("source overwrite permitted")
		}
		after, _ := os.ReadFile(output)
		if !bytes.Equal(before, after) {
			t.Fatal("source overwritten")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreviewContext(ctx, path, path+".cancel.png"); err == nil {
		t.Fatal("cancelled render succeeded")
	}
}

func TestPreviewDependsOnDocumentContent(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("Poppler unavailable")
	}
	dir := t.TempDir()
	var previews [][]byte
	for i, title := range []string{"AAAAAA", "ZZZZZZ"} {
		path := filepath.Join(dir, fmt.Sprintf("content%d.pdf", i))
		if _, err := Create(path, Model{Title: title, Paragraphs: []string{"same number of text blocks"}}); err != nil {
			t.Fatal(err)
		}
		output, err := Preview(path, "")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		previews = append(previews, data)
	}
	if bytes.Equal(previews[0], previews[1]) {
		t.Fatal("preview ignores the actual text")
	}
}

func TestPreviewBrokenRendererDoesNotCreateOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.pdf")
	if _, err := Create(path, Model{Title: "Render failure"}); err != nil {
		t.Fatal(err)
	}
	name := "pdftoppm"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("invalid executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := Preview(path, ""); err == nil || !strings.Contains(err.Error(), "rendering failed") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(path + ".preview.png"); !os.IsNotExist(err) {
		t.Fatal("failed renderer left preview")
	}
}

// Opt-in retained fixtures for independent parsing and actual renderer review.
func TestReleaseEvidenceFixtures(t *testing.T) {
	dir := os.Getenv("ORCA_ARTIFACT_EVIDENCE_DIR")
	if dir == "" {
		t.Skip("set ORCA_ARTIFACT_EVIDENCE_DIR to retain generated review fixtures")
	}
	m := Model{Title: "PDF pagination / \u4e2d\u6587\u6d4b\u8bd5"}
	for i := 0; i < 80; i++ {
		m.Paragraphs = append(m.Paragraphs, fmt.Sprintf("LINE-%03d: retained content", i))
	}
	m.Paragraphs = append(m.Paragraphs, strings.Repeat("W", 200), strings.Repeat("\u4e2d\u6587\u957f\u6587\u672c", 20), "LAST-PARAGRAPH: END-OF-DOCUMENT")
	models := map[string]Model{
		"pagination.pdf": m,
		"numbers.xlsx":   {Title: "Formula typing", Sheets: []WorkbookSheet{{Name: "Numbers", Rows: [][]string{{"Value", "Notes"}, {"12", "numeric"}, {"-3.5", "numeric"}, {"1e2", "numeric"}, {"=SUM(A2:A4)", "must recalculate to 108.5"}, {"0012", "identifier"}, {"1234567890123456", "precision preserved"}, {"\u4e2d\u6587", "Unicode text"}}}}},
		"bounded.pptx":   {Title: "Slides", Slides: []Slide{{Title: "Bounded slide review", Bullets: []string{strings.Repeat("W", 90), strings.Repeat("\u4e2d\u6587", 90), "LAST-BULLET"}}, {Title: "Final slide", Bullets: []string{"All text retained"}}}},
		"document.docx":  {Title: "Generated document", Paragraphs: []string{"\u4e2d\u6587\u5185\u5bb9\u5b8c\u6574", strings.Repeat("Long paragraph content. ", 100), "FINAL-DOCX-PARAGRAPH"}},
	}
	results := make(map[string]Validation)
	for name, model := range models {
		path := filepath.Join(dir, name)
		v, err := Create(path, model)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		results[name] = v
		if filepath.Ext(name) == ".pdf" {
			preview, err := Preview(path, filepath.Join(t.TempDir(), "pdf-first-page.png"))
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(preview)
			if err := os.WriteFile(filepath.Join(dir, "pdf-first-page.png"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil || img.Bounds().Dx() == 0 {
				t.Fatalf("preview invalid: %v", err)
			}
		}
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "validation-results.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
