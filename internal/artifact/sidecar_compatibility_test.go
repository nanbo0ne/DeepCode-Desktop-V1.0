package artifact

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func copyLegacyFixture(t *testing.T, name string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), name)
	for _, suffix := range []string{"", ".orca-artifact.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "legacy-v1", name+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target+suffix, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return target
}

func TestLegacyV1SidecarsReadValidatePreviewButDoNotOverwrite(t *testing.T) {
	for _, name := range []string{"legacy.pdf", "legacy.docx", "legacy.xlsx", "legacy.pptx", "legacy-default.xlsx", "legacy-default.pptx"} {
		t.Run(name, func(t *testing.T) {
			path := copyLegacyFixture(t, name)
			original, _ := os.ReadFile(path)
			sidecar, _ := os.ReadFile(SidecarPath(path))
			var stored Model
			if err := json.Unmarshal(sidecar, &stored); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.ArtifactSHA256 != "" || !loaded.UpdatedAt.Equal(stored.UpdatedAt) || len(loaded.Paragraphs) != len(stored.Paragraphs) {
				t.Fatalf("read changed legacy fields: %+v", loaded)
			}
			v, err := Validate(path)
			if err != nil || !v.Valid || v.Scope != "structure_only_sidecar_consistency_unknown" || v.VisualVerified {
				t.Fatalf("legacy validation = %+v, %v", v, err)
			}
			if !strings.Contains(strings.Join(v.Warnings, " "), "unknown") {
				t.Fatal("missing consistency warning")
			}
			if name == "legacy.pdf" && v.Units != 1 {
				t.Fatal("legacy truncated PDF must report actual 1 page, not sidecar-derived pages")
			}
			if _, err := Edit(path, func(m *Model) error { m.Title = "Do not overwrite"; return nil }); err == nil || !strings.Contains(err.Error(), "no artifact checksum") {
				t.Fatalf("legacy edit = %v", err)
			}
			after, _ := os.ReadFile(path)
			afterSidecar, _ := os.ReadFile(SidecarPath(path))
			if !bytes.Equal(original, after) || !bytes.Equal(sidecar, afterSidecar) {
				t.Fatal("read/validate/rejected edit changed legacy files")
			}
			if name == "legacy.pdf" {
				if _, err := exec.LookPath("pdftoppm"); err == nil {
					preview, err := Preview(path, "")
					if err != nil {
						t.Fatalf("legacy real preview: %v", err)
					}
					if dir := os.Getenv("ORCA_ARTIFACT_EVIDENCE_DIR"); dir != "" {
						png, err := os.ReadFile(preview)
						if err != nil {
							t.Fatal(err)
						}
						for file, data := range map[string][]byte{"legacy-first-page.png": png, "legacy.pdf": original, "legacy.pdf.orca-artifact.json": sidecar} {
							if err := os.WriteFile(filepath.Join(dir, file), data, 0o644); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		})
	}
}

func TestNewSidecarIntegrityAndStableReads(t *testing.T) {
	for _, format := range []string{"docx", "xlsx", "pptx", "pdf"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current."+format)
			if _, err := Create(path, Model{Title: "Original"}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(SidecarPath(path))
			first, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Load(path)
			if err != nil || !first.UpdatedAt.Equal(second.UpdatedAt) {
				t.Fatal("read resets timestamp")
			}
			data, _ := os.ReadFile(path)
			if first.ArtifactSHA256 != digest(data) || first.SidecarSHA256 != sidecarDigest(first) {
				t.Fatal("missing or incorrect checksums")
			}
			if _, err := Edit(path, func(m *Model) error { m.Title = "Updated"; return nil }); err != nil {
				t.Fatal(err)
			}
			updated, err := Load(path)
			if err != nil || updated.Title != "Updated" {
				t.Fatalf("edit round-trip: %+v, %v", updated, err)
			}
			if _, err := Validate(path); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(SidecarPath(path))
			if bytes.Equal(before, after) {
				t.Fatal("edit did not update sidecar")
			}
		})
	}
}

func TestSidecarAccidentalChangesAreRejected(t *testing.T) {
	for _, change := range []string{"title", "hash", "missing_one_hash", "extra_field", "trailing"} {
		t.Run(change, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "guard.docx")
			if _, err := Create(path, Model{Title: "Original"}); err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(path)
			raw, _ := os.ReadFile(SidecarPath(path))
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			switch change {
			case "title":
				fields["title"] = "Accidental change"
			case "hash":
				fields["artifactSha256"] = strings.Repeat("0", 64)
			case "missing_one_hash":
				delete(fields, "artifactSha256")
			case "extra_field":
				fields["unknown_metadata"] = "must not silently disappear"
			}
			raw, _ = json.Marshal(fields)
			if change == "trailing" {
				raw = append(raw, []byte(" {}")...)
			}
			if err := os.WriteFile(SidecarPath(path), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("changed sidecar accepted")
			}
			if _, err := Edit(path, func(m *Model) error { return nil }); err == nil {
				t.Fatal("edit accepted changed sidecar")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(original, after) {
				t.Fatal("changed sidecar caused overwrite")
			}
		})
	}
}

func TestChecksummedFilesDoNotDependOnCurrentTemplates(t *testing.T) {
	for _, name := range []string{"legacy.pdf", "legacy.xlsx", "legacy.pptx"} {
		t.Run(name, func(t *testing.T) {
			path := copyLegacyFixture(t, name)
			data, _ := os.ReadFile(path)
			model, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate another template version that emitted this exact snapshot.
			model.ArtifactSHA256 = digest(data)
			model.SidecarSHA256 = sidecarDigest(model)
			sidecar, _ := json.Marshal(model)
			if err := os.WriteFile(SidecarPath(path), sidecar, 0o644); err != nil {
				t.Fatal(err)
			}
			v, err := Validate(path)
			if err != nil || !v.Valid || v.Scope != "artifact_sha256_and_structure" {
				t.Fatalf("template-dependent validation: %+v, %v", v, err)
			}
			// Editing may deliberately repair formerly overflowing text; validation
			// must not invoke today's renderer before the mutation is applied.
			_, err = Edit(path, func(m *Model) error {
				if m.Format == "pptx" {
					m.Slides[0].Bullets = []string{"Now fits"}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStructuralValidationRejectsMissingOOXMLTargets(t *testing.T) {
	path := copyLegacyFixture(t, "legacy.xlsx")
	data, _ := os.ReadFile(path)
	// A legacy file cannot pass on its ZIP header/main XML alone.
	parts := map[string]string{
		"[Content_Types].xml":        readPart(t, path, "[Content_Types].xml"),
		"_rels/.rels":                readPart(t, path, "_rels/.rels"),
		"xl/workbook.xml":            readPart(t, path, "xl/workbook.xml"),
		"xl/_rels/workbook.xml.rels": readPart(t, path, "xl/_rels/workbook.xml.rels"),
	}
	broken, err := zipPackage(parts)
	if err != nil || bytes.Equal(data, broken) {
		t.Fatal("fixture mutation failed")
	}
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(path); err == nil {
		t.Fatal("missing worksheet/styles accepted")
	}
}

func TestPDFClassicStructureWithoutPoppler(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := copyLegacyFixture(t, "legacy.pdf")
	v, err := Validate(path)
	if err != nil || v.Units != 1 || !strings.Contains(strings.Join(v.Warnings, " "), "sanity") {
		t.Fatalf("fallback validation: %+v, %v", v, err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4\nnot a PDF\n%%EOF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(path); err == nil {
		t.Fatal("header/EOF-only fake PDF accepted")
	}
}
