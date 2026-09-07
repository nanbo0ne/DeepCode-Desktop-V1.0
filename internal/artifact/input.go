package artifact

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Resource limits are deliberately below the file format maxima: the sidecar
// uses dense slices and each edit regenerates the complete in-memory package.
const (
	MaxRows      = 10000
	MaxColumns   = 1024
	MaxCells     = 100000
	maxTextBytes = 4 << 20
)

func renderModel(m Model) ([]byte, error) {
	if err := validateModel(m); err != nil {
		return nil, err
	}
	switch m.Format {
	case "docx":
		return renderDOCX(m)
	case "xlsx":
		return renderXLSX(m)
	case "pptx":
		return renderPPTX(m)
	case "pdf":
		return renderPDF(m)
	default:
		return nil, fmt.Errorf("unsupported artifact format %q", m.Format)
	}
}

func validateModel(m Model) error {
	if len(m.Paragraphs) > 10000 || len(m.Sheets) > 100 || len(m.Slides) > 1000 {
		return fmt.Errorf("artifact exceeds paragraph (10000), sheet (100), or slide (1000) limit")
	}
	if (m.Format != "docx" && m.Format != "pdf" && len(m.Paragraphs) > 0) ||
		(m.Format != "xlsx" && len(m.Sheets) > 0) || (m.Format != "pptx" && len(m.Slides) > 0) {
		return fmt.Errorf("content does not match artifact format %q; refusing to discard it", m.Format)
	}
	total := 0
	check := func(s string) error {
		total += len(s)
		if total > maxTextBytes {
			return fmt.Errorf("artifact text exceeds 4 MiB limit")
		}
		if !utf8.ValidString(s) {
			return fmt.Errorf("artifact text is not valid UTF-8")
		}
		for _, r := range s {
			if r < 32 && r != '\n' && r != '\r' && r != '\t' || r == 0xFFFE || r == 0xFFFF {
				return fmt.Errorf("artifact text contains unsupported control character U+%04X", r)
			}
		}
		return nil
	}
	if err := check(m.Title); err != nil {
		return err
	}
	for _, p := range m.Paragraphs {
		if err := check(p); err != nil {
			return err
		}
	}
	names := make(map[string]bool)
	cells := 0
	for i, sheet := range m.Sheets {
		name := strings.TrimSpace(sheet.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		if err := check(name); err != nil {
			return err
		}
		key := strings.ToLower(name)
		if len(utf16.Encode([]rune(name))) > 31 || strings.ContainsAny(name, "[]:*?/\\\r\n\t") || strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") || names[key] {
			return fmt.Errorf("invalid or duplicate worksheet name %q", name)
		}
		names[key] = true
		if len(sheet.Rows) > MaxRows {
			return fmt.Errorf("worksheet exceeds %d row limit", MaxRows)
		}
		for _, row := range sheet.Rows {
			cells += len(row)
			if len(row) > MaxColumns || cells > MaxCells {
				return fmt.Errorf("worksheet exceeds column (%d) or workbook cell (%d) limit", MaxColumns, MaxCells)
			}
			for _, value := range row {
				if err := check(value); err != nil {
					return err
				}
				if len(utf16.Encode([]rune(value))) > 32767 {
					return fmt.Errorf("worksheet cell exceeds 32767 UTF-16 units")
				}
				if strings.HasPrefix(value, "=") && (strings.TrimSpace(value[1:]) == "" || len(value) > 8192) {
					return fmt.Errorf("formula is empty or exceeds 8192 bytes; formulas are stored, not evaluated")
				}
			}
		}
	}
	for _, slide := range m.Slides {
		if len(slide.Bullets) > 1000 {
			return fmt.Errorf("slide exceeds bullet limit")
		}
		for _, text := range append([]string{slide.Title}, slide.Bullets...) {
			if err := check(text); err != nil {
				return err
			}
		}
	}
	return nil
}

var decimalNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func numericCell(value string) bool {
	if !decimalNumber.MatchString(value) {
		return false
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return false
	}
	mantissa := strings.FieldsFunc(value, func(r rune) bool { return r == 'e' || r == 'E' })[0]
	digits := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(mantissa, "-"), ".", ""), "0")
	// Preserve identifiers, leading zeroes, and numbers Excel would round.
	return len(digits) <= 15 && (n != 0 || len(digits) == 0)
}
