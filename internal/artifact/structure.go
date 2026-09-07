package artifact

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type packageRelationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr"`
}

func validateOOXML(data []byte, format string) (int, int, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, 0, err
	}
	parts := map[string][]byte{}
	roots := map[string]string{}
	counts := map[string]int{}
	relationships := map[string][]packageRelationship{}
	total := uint64(0)
	for _, file := range zr.File {
		total += file.UncompressedSize64
		if total > 128<<20 || len(parts) >= 10000 {
			return 0, 0, fmt.Errorf("OOXML package exceeds validation resource limits")
		}
		if _, exists := parts[file.Name]; exists {
			return 0, 0, fmt.Errorf("duplicate OOXML part %s", file.Name)
		}
		r, err := file.Open()
		if err != nil {
			return 0, 0, err
		}
		content, err := io.ReadAll(io.LimitReader(r, (128<<20)+1))
		r.Close()
		if err != nil || len(content) > 128<<20 {
			return 0, 0, fmt.Errorf("cannot read OOXML part %s", file.Name)
		}
		parts[file.Name] = content
		if !strings.HasSuffix(file.Name, ".xml") && !strings.HasSuffix(file.Name, ".rels") {
			continue
		}
		decoder := xml.NewDecoder(bytes.NewReader(content))
		depth, documents := 0, 0
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, 0, fmt.Errorf("invalid XML part %s: %w", file.Name, err)
			}
			switch token := token.(type) {
			case xml.StartElement:
				if depth == 0 {
					roots[file.Name] = token.Name.Local
					documents++
				}
				depth++
				if token.Name.Local == "p" && format == "docx" || token.Name.Local == "c" && format == "xlsx" || token.Name.Local == "t" && format == "pptx" {
					counts[file.Name]++
				}
			case xml.EndElement:
				depth--
			}
		}
		if documents != 1 || depth != 0 {
			return 0, 0, fmt.Errorf("XML part %s must have one root", file.Name)
		}
		if strings.HasSuffix(file.Name, ".rels") {
			var rels struct {
				Items []packageRelationship `xml:"Relationship"`
			}
			if err := xml.Unmarshal(content, &rels); err != nil {
				return 0, 0, err
			}
			relationships[file.Name] = rels.Items
		}
	}
	if roots["[Content_Types].xml"] != "Types" || roots["_rels/.rels"] != "Relationships" {
		return 0, 0, fmt.Errorf("OOXML content types or root relationships missing")
	}
	for file, rels := range relationships {
		seen := map[string]bool{}
		for _, rel := range rels {
			if rel.ID == "" || seen[rel.ID] || rel.Type == "" {
				return 0, 0, fmt.Errorf("invalid relationship in %s", file)
			}
			seen[rel.ID] = true
			if rel.Mode == "External" {
				continue
			}
			if _, ok := parts[relationshipTarget(file, rel.Target)]; !ok {
				return 0, 0, fmt.Errorf("missing relationship target in %s: %s", file, rel.Target)
			}
		}
	}
	main := map[string]string{"docx": "word/document.xml", "xlsx": "xl/workbook.xml", "pptx": "ppt/presentation.xml"}[format]
	rootName := map[string]string{"docx": "document", "xlsx": "workbook", "pptx": "presentation"}[format]
	if roots[main] != rootName {
		return 0, 0, fmt.Errorf("missing or invalid main %s part", format)
	}
	linked := false
	for _, rel := range relationships["_rels/.rels"] {
		linked = linked || rel.Mode != "External" && strings.HasSuffix(rel.Type, "/officeDocument") && relationshipTarget("_rels/.rels", rel.Target) == main
	}
	if !linked {
		return 0, 0, fmt.Errorf("OOXML root does not reference the main document")
	}
	if format == "docx" {
		return 1, counts[main], nil
	}
	var list struct {
		Sheets []struct {
			ID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
		Slides []struct {
			ID string `xml:"id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err := xml.Unmarshal(parts[main], &list); err != nil {
		return 0, 0, err
	}
	ids := []string{}
	for _, item := range list.Sheets {
		ids = append(ids, item.ID)
	}
	for _, item := range list.Slides {
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return 0, 0, fmt.Errorf("%s contains no sheets/slides", format)
	}
	relFile := path.Join(path.Dir(main), "_rels", path.Base(main)+".rels")
	blocks := 0
	seen := map[string]bool{}
	for _, id := range ids {
		found := false
		for _, rel := range relationships[relFile] {
			if rel.ID != id || rel.Mode == "External" {
				continue
			}
			target := relationshipTarget(relFile, rel.Target)
			want := "worksheet"
			if format == "pptx" {
				want = "sld"
			}
			if roots[target] != want || seen[target] {
				return 0, 0, fmt.Errorf("invalid or repeated sheet/slide target")
			}
			seen[target] = true
			blocks += counts[target]
			found = true
		}
		if !found {
			return 0, 0, fmt.Errorf("missing sheet/slide relationship %s", id)
		}
	}
	return len(ids), blocks, nil
}

func relationshipTarget(relsFile, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/"))
	}
	base := path.Dir(path.Dir(relsFile))
	return path.Clean(path.Join(base, target))
}

var pdfPagesPattern = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
var pdfStartXRef = regexp.MustCompile(`startxref\s+(\d+)\s+%%EOF\s*$`)
var pdfPageObject = regexp.MustCompile(`/Type\s*/Page\b`)

func validatePDFStructure(_ string, data []byte) (int, string, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return 0, "", fmt.Errorf("invalid PDF header")
	}
	bin, err := exec.LookPath("pdfinfo")
	if err == nil {
		// Validate the same snapshot that was hashed, not a second read of a file
		// which another process might have replaced in between.
		dir, err := os.MkdirTemp("", "orca-pdf-validate-*")
		if err != nil {
			return 0, "", err
		}
		defer os.RemoveAll(dir)
		input := filepath.Join(dir, "document.pdf")
		if err := os.WriteFile(input, data, 0o600); err != nil {
			return 0, "", err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, input)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err := cmd.CombinedOutput()
		if err != nil || bytes.Contains(output, []byte("Syntax Error:")) {
			return 0, "", fmt.Errorf("PDF structural parsing failed: %v: %s", err, strings.TrimSpace(string(output)))
		}
		match := pdfPagesPattern.FindSubmatch(output)
		if len(match) != 2 {
			return 0, "", fmt.Errorf("pdfinfo did not report page count")
		}
		pages, err := strconv.Atoi(string(match[1]))
		if err != nil || pages < 1 {
			return 0, "", fmt.Errorf("PDF has no readable pages")
		}
		return pages, "PDF structure and page count parsed with Poppler pdfinfo; content fidelity is not verified.", nil
	}
	// Dependency-free checks for the classic cross-reference format generated by
	// this package. This is intentionally not a general PDF parser/conformance test.
	match := pdfStartXRef.FindSubmatch(data)
	if len(match) != 2 {
		return 0, "", fmt.Errorf("PDF trailer/startxref missing; pdfinfo is unavailable")
	}
	offset, err := strconv.Atoi(string(match[1]))
	if err != nil || offset < 0 || offset >= len(data) {
		return 0, "", fmt.Errorf("invalid PDF cross-reference offset")
	}
	r := bufio.NewReader(bytes.NewReader(data[offset:]))
	var marker string
	var first, count int
	if _, err := fmt.Fscan(r, &marker, &first, &count); err != nil || marker != "xref" || first != 0 || count < 2 || count > 100000 {
		return 0, "", fmt.Errorf("unsupported PDF cross-reference structure; install Poppler pdfinfo")
	}
	pages := 0
	for i := 0; i < count; i++ {
		var positionText, generationText, status string
		if _, err := fmt.Fscan(r, &positionText, &generationText, &status); err != nil {
			return 0, "", fmt.Errorf("incomplete PDF cross-reference table")
		}
		position, positionErr := strconv.Atoi(positionText)
		generation, generationErr := strconv.Atoi(generationText)
		if positionErr != nil || generationErr != nil {
			return 0, "", fmt.Errorf("invalid PDF cross-reference numbers")
		}
		if i == 0 && status == "f" {
			continue
		}
		if status != "n" || generation != 0 || position < 0 || position >= offset {
			return 0, "", fmt.Errorf("unsupported PDF cross-reference entry")
		}
		body := data[position:offset]
		if !bytes.HasPrefix(body, []byte(fmt.Sprintf("%d 0 obj", i))) {
			return 0, "", fmt.Errorf("PDF object offset mismatch")
		}
		end := bytes.Index(body, []byte("endobj"))
		if end < 0 {
			return 0, "", fmt.Errorf("unterminated PDF object")
		}
		body = body[:end]
		if stream := bytes.Index(body, []byte("stream")); stream >= 0 {
			body = body[:stream]
		}
		if pdfPageObject.Match(body) {
			pages++
		}
	}
	if pages == 0 {
		return 0, "", fmt.Errorf("PDF contains no page objects")
	}
	return pages, "Poppler pdfinfo unavailable: only classic PDF cross-reference/object sanity was checked; full PDF parsing and content fidelity remain unverified.", nil
}
