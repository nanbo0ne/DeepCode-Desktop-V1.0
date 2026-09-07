package artifact

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateEditPreviewAndValidateArtifacts(t *testing.T) {
	tests := []struct {
		format string
		model  Model
		part   string
	}{
		{format: "docx", model: Model{Title: "项目报告", Paragraphs: []string{"第一段", strings.Repeat("长文本", 80)}}, part: "word/document.xml"},
		{format: "xlsx", model: Model{Title: "数据", Sheets: []WorkbookSheet{{Name: "汇总", Rows: [][]string{{"项目", "数值"}, {"A", "12"}, {"合计", "=SUM(B2:B2)"}}}, {Name: "明细", Rows: [][]string{{"中文"}}}}}, part: "xl/workbook.xml"},
		{format: "pptx", model: Model{Title: "演示", Slides: []Slide{{Title: "第一页", Bullets: []string{"要点一", "要点二"}}, {Title: "第二页", Bullets: []string{"结论"}}}}, part: "ppt/presentation.xml"},
		{format: "pdf", model: Model{Title: "中文 PDF", Paragraphs: []string{"正文完整性验证"}}, part: ""},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact."+tc.format)
			model := tc.model
			model.Format = tc.format
			result, err := Create(path, model)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if !result.Valid || result.TextBlocks == 0 && tc.format != "pdf" {
				t.Fatalf("validation = %+v", result)
			}
			if _, err := os.Stat(SidecarPath(path)); err != nil {
				t.Fatalf("sidecar: %v", err)
			}
			if tc.part != "" {
				zr, err := zip.OpenReader(path)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, file := range zr.File {
					found = found || file.Name == tc.part
				}
				zr.Close()
				if !found {
					t.Fatalf("missing required part %s", tc.part)
				}
			}
			if _, err := Edit(path, func(m *Model) error {
				switch m.Format {
				case "docx", "pdf":
					m.Paragraphs = append(m.Paragraphs, "后续结构化修改")
				case "xlsx":
					m.Sheets[0].Rows = append(m.Sheets[0].Rows, []string{"后续结构化修改"})
				case "pptx":
					m.Slides = append(m.Slides, Slide{Title: "后续结构化修改"})
				}
				return nil
			}); err != nil {
				t.Fatalf("Edit: %v", err)
			}
			preview, err := Preview(path, "")
			_, rendererErr := exec.LookPath("pdftoppm")
			if tc.format != "pdf" || rendererErr != nil {
				if err == nil || !strings.Contains(err.Error(), "unavailable") {
					t.Fatalf("expected explicit unavailable preview, got %q, %v", preview, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if info, err := os.Stat(preview); err != nil || info.Size() == 0 {
				t.Fatalf("preview invalid: info=%v err=%v", info, err)
			}
		})
	}
}

func TestLoadRejectsThirdPartyArtifactWithoutSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "third-party.docx")
	if err := os.WriteFile(path, []byte("not an Orca artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "sidecar") {
		t.Fatalf("Load error = %v, want explicit sidecar limitation", err)
	}
}
