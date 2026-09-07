package hosttools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/artifact"
)

func TestArtifactRejectsMalformedEditsWithoutChangingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.xlsx")
	if _, err := artifact.Create(path, artifact.Model{Sheets: []artifact.WorkbookSheet{{Name: "Data", Rows: [][]string{{"original"}}}}}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	sidecar, _ := os.ReadFile(artifact.SidecarPath(path))
	tool := artifactEditTool{workDir: dir}
	for _, op := range []string{
		`{"op":"set_cell","sheet":0,"row":-1,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":0,"column":-1,"text":"bad"}`,
		`{"op":"set_cell","sheet":-1,"row":0,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":2147483647,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":0,"column":2147483647,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":10000,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":0,"column":1024,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":null,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":0.5,"column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":"0","column":0,"text":"bad"}`,
		`{"op":"set_cell","sheet":0,"row":0,"column":0}`,
		`{"op":"append_text","text":"would disappear"}`,
		`{"op":"add_slide","title":"would disappear"}`,
		`{"op":"replace_text","find":""}`,
		`{"op":"replace_text","find":"original"}`,
		`{"op":"unknown"}`,
		`{"op":"set_cell","sheet":0,"row":0,"column":0,"text":"bad","typo":1}`,
	} {
		raw := json.RawMessage(fmt.Sprintf(`{"path":"data.xlsx","operations":[%s]}`, op))
		if _, err := tool.Execute(context.Background(), raw); err == nil {
			t.Errorf("accepted %s", op)
		}
		after, _ := os.ReadFile(path)
		afterSidecar, _ := os.ReadFile(artifact.SidecarPath(path))
		if !bytes.Equal(original, after) || !bytes.Equal(sidecar, afterSidecar) {
			t.Fatalf("failed edit changed data: %s", op)
		}
	}
	for _, raw := range []string{`null`, `{`, `[]`, `{"path":"data.xlsx"}`, `{"path":"data.xlsx","operations":[]}`, `{"path":"data.xlsx","operations":[null]}`, `{} {}`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	// A valid mutation preceding a rejected mutation must also leave disk untouched.
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"data.xlsx","operations":[{"op":"set_cell","sheet":0,"row":0,"column":0,"text":"changed"},{"op":"set_cell","sheet":0,"row":-1,"column":0,"text":"bad"}]}`))
	if err == nil {
		t.Fatal("expected batch rejection")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("partially applied rejected batch")
	}
}

func TestArtifactToolRealUseWorkflow(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	for _, format := range []string{"docx", "pdf", "xlsx", "pptx"} {
		t.Run(format, func(t *testing.T) {
			name := "workflow." + format
			raw := json.RawMessage(fmt.Sprintf(`{"path":%q,"format":%q,"title":"Original"}`, name, format))
			out, err := (artifactCreateTool{dir}).Execute(ctx, raw)
			if err != nil || !strings.Contains(out, `"visualVerified":false`) {
				t.Fatalf("create = %s, %v", out, err)
			}
			op := `{"op":"append_text","text":"New content"}`
			if format == "xlsx" {
				op = `{"op":"set_cell","sheet":0,"row":3,"column":2,"text":"12"}`
			}
			if format == "pptx" {
				op = `{"op":"add_slide","title":"New slide","bullets":["New content"]}`
			}
			_, err = (artifactEditTool{dir}).Execute(ctx, json.RawMessage(fmt.Sprintf(`{"path":%q,"operations":[%s,{"op":"replace_text","find":"Original","replace":"Updated"}]}`, name, op)))
			if err != nil {
				t.Fatal(err)
			}
			m, err := artifact.Load(filepath.Join(dir, name))
			if err != nil || m.Title != "Updated" {
				t.Fatalf("load = %+v, %v", m, err)
			}
			out, err = (artifactValidateTool{dir}).Execute(ctx, json.RawMessage(fmt.Sprintf(`{"path":%q}`, name)))
			if err != nil || !strings.Contains(out, `"visualVerified":false`) {
				t.Fatalf("validate = %s, %v", out, err)
			}
			out, err = (artifactPreviewTool{dir}).Execute(ctx, json.RawMessage(fmt.Sprintf(`{"path":%q}`, name)))
			if err != nil {
				if !strings.Contains(err.Error(), "unavailable") {
					t.Fatal(err)
				}
			} else if format != "pdf" || !strings.Contains(out, `"scope":"first_page_only"`) || !strings.Contains(out, `"visualVerified":false`) {
				t.Fatalf("misleading preview: %s", out)
			}
		})
	}
}

func TestArtifactCreateMalformedAndMixedInput(t *testing.T) {
	tool := artifactCreateTool{workDir: t.TempDir()}
	for _, raw := range []string{
		`{"path":"file.pdf"}`,
		`{"path":"file.xlsx","format":"xlsx","rows":[]}`,
		`{"path":"file.xlsx","format":"xlsx","sheets":[{"rows":[[12]]}]}`,
		`{"path":"file.xlsx","format":"xlsx","paragraphs":["must not vanish"]}`,
		`{"path":"../outside.pdf","format":"pdf"}`,
		`{"path":"file.pdf","format":"pdf","paragraphs":["\ud83d\ude00"]}`,
	} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestArtifactEditSchemaExplainsZeroBasedCoordinates(t *testing.T) {
	var schema struct {
		Properties struct {
			Operations struct {
				Items struct {
					Properties map[string]struct {
						Description string `json:"description"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((artifactEditTool{}).Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"sheet", "row", "column"} {
		description := schema.Properties.Operations.Items.Properties[field].Description
		for _, required := range []string{"0-based", "B2", "row=1, column=1"} {
			if !strings.Contains(description, required) {
				t.Errorf("%s description missing %q: %s", field, required, description)
			}
		}
	}
}

func TestArtifactLegacyPDFToolPreviewReportsUnknownConsistency(t *testing.T) {
	dir := t.TempDir()
	name := "legacy.pdf"
	for _, suffix := range []string{"", ".orca-artifact.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "artifact", "testdata", "legacy-v1", name+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+suffix), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	out, err := (artifactValidateTool{dir}).Execute(ctx, json.RawMessage(`{"path":"legacy.pdf"}`))
	if err != nil || !strings.Contains(out, "structure_only_sidecar_consistency_unknown") {
		t.Fatalf("legacy validate: %s, %v", out, err)
	}
	out, err = (artifactPreviewTool{dir}).Execute(ctx, json.RawMessage(`{"path":"legacy.pdf"}`))
	if err != nil {
		if !strings.Contains(err.Error(), "unavailable") {
			t.Fatal(err)
		}
	} else if !strings.Contains(out, "unknown_legacy_sidecar") || !strings.Contains(out, `"visualVerified":false`) {
		t.Fatalf("legacy preview hid uncertainty: %s", out)
	}
	if _, err := (artifactEditTool{dir}).Execute(ctx, json.RawMessage(`{"path":"legacy.pdf","operations":[{"op":"append_text","text":"unsafe"}]}`)); err == nil {
		t.Fatal("legacy tool edit overwrote source")
	}
}
