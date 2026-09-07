package artifact

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestPDFPaginationWrappingAndGlyphs(t *testing.T) {
	m := Model{Title: "Pagination"}
	for i := 0; i < 80; i++ {
		m.Paragraphs = append(m.Paragraphs, fmt.Sprintf("LINE-%03d", i))
	}
	m.Paragraphs = append(m.Paragraphs, strings.Repeat("W", 200), strings.Repeat("\u4e2d\u6587", 50), "LAST-PARAGRAPH")
	data, err := renderPDF(m)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pdfLines(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if len([]rune(line)) > 33 {
			t.Fatalf("line overflows: %q", line)
		}
	}
	if !bytes.Contains(data, []byte("/Count 3")) || !bytes.Contains(data, []byte(pdfHex("LAST-PARAGRAPH"))) {
		t.Fatal("missing pages or final paragraph")
	}
	if !strings.Contains(strings.Join(lines, ""), strings.Repeat("\u4e2d\u6587", 50)) {
		t.Fatal("Chinese content lost")
	}
	for _, bad := range []string{"\U0001f600", "\u0378", "\x00", "\r"} {
		if _, err := renderPDF(Model{Title: bad}); err == nil || !strings.Contains(err.Error(), "unsupported glyph") {
			t.Fatalf("glyph %q: %v", bad, err)
		}
	}
}
