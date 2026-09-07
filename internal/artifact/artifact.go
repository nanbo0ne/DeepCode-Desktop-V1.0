package artifact

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const sidecarVersion = 1

type WorkbookSheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

type Slide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

type Model struct {
	Version        int             `json:"version"`
	Format         string          `json:"format"`
	Title          string          `json:"title"`
	Paragraphs     []string        `json:"paragraphs,omitempty"`
	Sheets         []WorkbookSheet `json:"sheets,omitempty"`
	Slides         []Slide         `json:"slides,omitempty"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	ArtifactSHA256 string          `json:"artifactSha256,omitempty"`
	SidecarSHA256  string          `json:"sidecarSha256,omitempty"`
}

type Validation struct {
	Valid          bool     `json:"valid"`
	Scope          string   `json:"scope"`
	VisualVerified bool     `json:"visualVerified"`
	Format         string   `json:"format"`
	Units          int      `json:"units"`
	TextBlocks     int      `json:"textBlocks"`
	Warnings       []string `json:"warnings,omitempty"`
}

func Normalize(m Model, path string) Model {
	m.Version = sidecarVersion
	m.Format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(m.Format), "."))
	if m.Format == "" {
		m.Format = strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	}
	if strings.TrimSpace(m.Title) == "" {
		m.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if (m.Format == "pdf" || m.Format == "docx") && len(m.Paragraphs) == 0 && len(m.Sheets) == 0 && len(m.Slides) == 0 {
		m.Paragraphs = []string{m.Title}
	}
	if m.Format == "xlsx" && len(m.Sheets) == 0 {
		m.Sheets = []WorkbookSheet{{Name: "Sheet1", Rows: [][]string{{m.Title}}}}
	}
	if m.Format == "pptx" && len(m.Slides) == 0 {
		m.Slides = []Slide{{Title: m.Title}}
	}
	m.UpdatedAt = time.Now().UTC()
	return m
}

func SidecarPath(path string) string { return path + ".orca-artifact.json" }

func Create(path string, model Model) (Validation, error) {
	model = Normalize(model, path)
	data, err := renderModel(model)
	if err != nil {
		return Validation{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Validation{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Validation{}, err
	}
	model.ArtifactSHA256 = digest(data)
	model.SidecarSHA256 = sidecarDigest(model)
	sidecar, _ := json.MarshalIndent(model, "", "  ")
	if err := os.WriteFile(SidecarPath(path), append(sidecar, '\n'), 0o644); err != nil {
		return Validation{}, err
	}
	return Validate(path)
}

func Load(path string) (Model, error) {
	b, err := os.ReadFile(SidecarPath(path))
	if err != nil {
		return Model{}, fmt.Errorf("structured editing requires an Orca sidecar: %w", err)
	}
	var model Model
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&model); err != nil {
		return Model{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Model{}, fmt.Errorf("artifact sidecar contains trailing JSON")
	}
	if model.Version != sidecarVersion {
		return Model{}, fmt.Errorf("unsupported artifact sidecar version %d", model.Version)
	}
	if (model.ArtifactSHA256 == "") != (model.SidecarSHA256 == "") ||
		model.SidecarSHA256 != "" && model.SidecarSHA256 != sidecarDigest(model) {
		return Model{}, fmt.Errorf("artifact sidecar integrity check failed; refusing to discard or regenerate changed metadata")
	}
	// Preserve the stored timestamp and fields on reads. Creation already writes
	// normalized sidecars, including those produced by the original v1 generator.
	return model, nil
}

func Edit(path string, mutate func(*Model) error) (Validation, error) {
	model, err := Load(path)
	if err != nil {
		return Validation{}, err
	}
	if model.ArtifactSHA256 == "" {
		return Validation{}, fmt.Errorf("legacy sidecar has no artifact checksum; consistency is unknown, so editing is refused without overwriting the file; explicitly recreate from reviewed content")
	}
	if _, err := Validate(path); err != nil {
		return Validation{}, err
	}
	if err := mutate(&model); err != nil {
		return Validation{}, err
	}
	return Create(path, model)
}

func Validate(path string) (Validation, error) {
	model, err := Load(path)
	if err != nil {
		return Validation{}, err
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return Validation{}, err
	}
	if model.ArtifactSHA256 != "" && model.ArtifactSHA256 != digest(actual) {
		return Validation{}, fmt.Errorf("artifact SHA256 mismatch; file changed or is corrupt, refusing to overwrite external edits")
	}
	result := Validation{Valid: true, Scope: "artifact_sha256_and_structure", Format: model.Format, Warnings: []string{"Structural checks only; not visual verification or formula evaluation."}}
	if model.ArtifactSHA256 == "" {
		result.Scope = "structure_only_sidecar_consistency_unknown"
		result.Warnings = append(result.Warnings, "Legacy sidecar has no artifact SHA256. Content consistency with the sidecar is unknown; edits are disabled. Structural validity does not establish that all source content was rendered.")
	}
	switch model.Format {
	case "docx", "xlsx", "pptx":
		result.Units, result.TextBlocks, err = validateOOXML(actual, model.Format)
	case "pdf":
		var warning string
		result.Units, warning, err = validatePDFStructure(path, actual)
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
		result.Warnings = append(result.Warnings, "PDF text block count is unavailable (reported as 0), not inferred from the sidecar.")
		result.Warnings = append(result.Warnings, "PDF font is not embedded; viewer CJK font substitution is required. Supported text is printable ASCII and basic GB2312 characters.")
	default:
		return Validation{}, fmt.Errorf("unsupported artifact format %q", model.Format)
	}
	if err != nil {
		result.Valid = false
		return result, err
	}
	return result, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// This detects accidental sidecar changes, not an attacker who can rewrite
// both hashes. It is independent of document templates and rendering code.
func sidecarDigest(model Model) string {
	model.SidecarSHA256 = ""
	data, _ := json.Marshal(model)
	return digest(data)
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func zipPackage(parts map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(parts[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderDOCX(m Model) ([]byte, error) {
	var body strings.Builder
	for _, p := range append([]string{m.Title}, m.Paragraphs...) {
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">` + xmlText(p) + `</w:t></w:r></w:p>`)
	}
	return zipPackage(map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
	})
}

func columnName(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}

func renderXLSX(m Model) ([]byte, error) {
	parts := map[string]string{
		"_rels/.rels":   `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/styles.xml": `<?xml version="1.0"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font><sz val="11"/><name val="Arial"/><family val="2"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`,
	}
	var sheets, rels, overrides strings.Builder
	for i, sheet := range m.Sheets {
		id := i + 1
		name := strings.TrimSpace(sheet.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", id)
		}
		sheets.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlText(name), id, id))
		rels.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, id, id))
		overrides.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, id))
		var rows strings.Builder
		for ri, row := range sheet.Rows {
			rows.WriteString(fmt.Sprintf(`<row r="%d">`, ri+1))
			for ci, value := range row {
				ref := columnName(ci+1) + strconv.Itoa(ri+1)
				if strings.HasPrefix(value, "=") {
					rows.WriteString(`<c r="` + ref + `"><f>` + xmlText(strings.TrimPrefix(value, "=")) + `</f></c>`)
				} else if numericCell(value) {
					rows.WriteString(`<c r="` + ref + `" t="n"><v>` + value + `</v></c>`)
				} else {
					rows.WriteString(`<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">` + xmlText(value) + `</t></is></c>`)
				}
			}
			rows.WriteString(`</row>`)
		}
		parts[fmt.Sprintf("xl/worksheets/sheet%d.xml", id)] = `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + rows.String() + `</sheetData></worksheet>`
	}
	parts["xl/workbook.xml"] = `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + sheets.String() + `</sheets><calcPr calcMode="auto" fullCalcOnLoad="1" forceFullCalc="1"/></workbook>`
	parts["xl/_rels/workbook.xml.rels"] = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
	parts["[Content_Types].xml"] = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` + overrides.String() + `<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`
	return zipPackage(parts)
}

func renderPPTX(m Model) ([]byte, error) {
	parts := map[string]string{"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`}
	var ids, rels, overrides strings.Builder
	for i, slide := range m.Slides {
		id := i + 1
		ids.WriteString(fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 255+id, id))
		rels.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, id, id))
		overrides.WriteString(fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, id))
		text, err := slideLines(slide)
		if err != nil {
			return nil, fmt.Errorf("slide %d: %w", id, err)
		}
		var shapes strings.Builder
		for j, line := range text {
			shapes.WriteString(fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="914400" y="%d"/><a:ext cx="7315200" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="none" lIns="0" tIns="0" rIns="0" bIns="0"/><a:lstStyle/><a:p><a:r><a:rPr lang="zh-CN" sz="%d"><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`, j+2, j+1, line.y, line.height, line.size, xmlText(line.text)))
		}
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", id)] = `<?xml version="1.0" encoding="UTF-8"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` + shapes.String() + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", id)] = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`
	}
	masterRelID := len(m.Slides) + 1
	parts["ppt/presentation.xml"] = fmt.Sprintf(`<?xml version="1.0"?><p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId%d"/></p:sldMasterIdLst><p:sldIdLst>%s</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`, masterRelID, ids.String())
	parts["ppt/_rels/presentation.xml.rels"] = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`, masterRelID) + `</Relationships>`
	parts["ppt/slideMasters/slideMaster1.xml"] = `<?xml version="1.0"?><p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld><p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/><p:sldLayoutIdLst><p:sldLayoutId id="1" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`
	parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/></Relationships>`
	parts["ppt/slideLayouts/slideLayout1.xml"] = `<?xml version="1.0"?><p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank"><p:cSld name="Blank"><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`
	parts["ppt/theme/theme1.xml"] = `<?xml version="1.0"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Orca"><a:themeElements><a:clrScheme name="Orca"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F2937"/></a:dk2><a:lt2><a:srgbClr val="F3F6FB"/></a:lt2><a:accent1><a:srgbClr val="3471E8"/></a:accent1><a:accent2><a:srgbClr val="18A999"/></a:accent2><a:accent3><a:srgbClr val="F97316"/></a:accent3><a:accent4><a:srgbClr val="8B5CF6"/></a:accent4><a:accent5><a:srgbClr val="EC4899"/></a:accent5><a:accent6><a:srgbClr val="64748B"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme><a:fontScheme name="Orca"><a:majorFont><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface="Arial"/></a:majorFont><a:minorFont><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface="Arial"/></a:minorFont></a:fontScheme><a:fmtScheme name="Orca"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`
	parts["[Content_Types].xml"] = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/><Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/><Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` + overrides.String() + `</Types>`
	return zipPackage(parts)
}

func pdfHex(s string) string {
	units := utf16.Encode([]rune(s))
	var b strings.Builder
	for _, u := range units {
		b.WriteString(fmt.Sprintf("%04X", u))
	}
	return b.String()
}

func renderPDF(m Model) ([]byte, error) {
	lines, err := pdfLines(m)
	if err != nil {
		return nil, err
	}
	objects := []string{"", `<< /Type /Catalog /Pages 2 0 R >>`, "", `<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [4 0 R] >>`, `<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /DW 1000 /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> >>`}
	var kids strings.Builder
	for start := 0; start < len(lines); start += pdfLinesPerPage {
		pageID := len(objects)
		fmt.Fprintf(&kids, "%d 0 R ", pageID)
		var content strings.Builder
		content.WriteString("BT /F1 14 Tf 60 780 Td ")
		for i, line := range lines[start:min(start+pdfLinesPerPage, len(lines))] {
			if i > 0 {
				content.WriteString("0 -22 Td ")
			}
			content.WriteString("<" + pdfHex(line) + "> Tj ")
		}
		content.WriteString("ET")
		objects = append(objects,
			fmt.Sprintf(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>`, pageID+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()))
	}
	objects[2] = fmt.Sprintf(`<< /Type /Pages /Kids [%s] /Count %d >>`, kids.String(), (len(lines)+pdfLinesPerPage-1)/pdfLinesPerPage)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i := 1; i < len(objects); i++ {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i, objects[i])
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects))
	for i := 1; i < len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects), xref)
	return b.Bytes(), nil
}

const pdfLinesPerPage = 33

func pdfLines(m Model) ([]string, error) {
	var lines []string
	encoder := simplifiedchinese.GBK.NewEncoder()
	for _, paragraph := range append([]string{m.Title}, m.Paragraphs...) {
		paragraph = strings.ReplaceAll(strings.ReplaceAll(paragraph, "\r\n", "\n"), "\t", "    ")
		for _, r := range paragraph {
			if r == '\n' || r >= 32 && r <= 126 {
				continue
			}
			// Restrict the unembedded GB1 font to the basic GB2312 repertoire.
			encoded, err := encoder.String(string(r))
			if err != nil || len(encoded) != 2 || encoded[0] < 0xA1 || encoded[0] > 0xF7 || encoded[1] < 0xA1 || encoded[1] > 0xFE {
				return nil, fmt.Errorf("PDF unsupported glyph U+%04X; use DOCX or an external renderer with an embedded font", r)
			}
		}
		for _, line := range strings.Split(paragraph, "\n") {
			lines = append(lines, wrapRunes(line, 33)...)
		}
	}
	return lines, nil
}

// The PDF font has an explicit 1000-unit advance, so 33 glyphs at 14pt
// occupy 462pt within the 475pt page text area, including unbroken tokens.
func wrapRunes(text string, width int) []string {
	runes := []rune(text)
	var lines []string
	for len(runes) > width {
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	return append(lines, string(runes))
}
