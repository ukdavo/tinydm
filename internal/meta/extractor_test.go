package meta

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// makePNG returns a minimal valid PNG with the given dimensions.
func makePNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// makeGIF returns a minimal valid GIF87a byte slice (1×1 pixel).
func makeGIF() []byte {
	// GIF87a 1×1 black pixel — hand-crafted minimal stream.
	return []byte{
		0x47, 0x49, 0x46, 0x38, 0x37, 0x61, // GIF87a
		0x01, 0x00, 0x01, 0x00, // width=1, height=1
		0x80, 0x00, 0x00, // GCT flag + background + aspect
		0x00, 0x00, 0x00, // colour 0 = black
		0x2C,             // image descriptor
		0x00, 0x00, 0x00, 0x00, // left, top = 0,0
		0x01, 0x00, 0x01, 0x00, // width=1, height=1
		0x00, // no local colour table
		0x02, // min LZW code size
		0x02, 0x4C, 0x01, 0x00, // LZW compressed data
		0x3B, // trailer
	}
}

// makeJPEG returns a minimal valid JPEG header that image.DecodeConfig can parse.
func makeJPEG(width, height uint16) []byte {
	// SOI + APP0 + SOF0 — the minimal set image.DecodeConfig needs.
	var buf bytes.Buffer

	// SOI
	buf.Write([]byte{0xFF, 0xD8})

	// APP0 (JFIF)
	app0 := []byte{
		0xFF, 0xE0,
		0x00, 0x10, // length = 16
		0x4A, 0x46, 0x49, 0x46, 0x00, // "JFIF\0"
		0x01, 0x01, // version 1.1
		0x00,       // aspect ratio units
		0x00, 0x01, // X density
		0x00, 0x01, // Y density
		0x00, 0x00, // thumbnail size
	}
	buf.Write(app0)

	// SOF0 (baseline DCT)
	sof0 := make([]byte, 19)
	sof0[0] = 0xFF
	sof0[1] = 0xC0
	binary.BigEndian.PutUint16(sof0[2:], 17)  // length
	sof0[4] = 8                                // precision
	binary.BigEndian.PutUint16(sof0[5:], height)
	binary.BigEndian.PutUint16(sof0[7:], width)
	sof0[9] = 3 // components
	// component 1
	sof0[10] = 1
	sof0[11] = 0x11
	sof0[12] = 0
	// component 2
	sof0[13] = 2
	sof0[14] = 0x11
	sof0[15] = 1
	// component 3
	sof0[16] = 3
	sof0[17] = 0x11
	sof0[18] = 1
	buf.Write(sof0)

	return buf.Bytes()
}

// ─── Image tests ──────────────────────────────────────────────────────────────

func TestExtract_PNG(t *testing.T) {
	data := makePNG(120, 80)
	props := Extract("image/png", bytes.NewReader(data))

	if props["image.width"] != "120" {
		t.Errorf("image.width: got %q, want %q", props["image.width"], "120")
	}
	if props["image.height"] != "80" {
		t.Errorf("image.height: got %q, want %q", props["image.height"], "80")
	}
	if props["image.format"] != "png" {
		t.Errorf("image.format: got %q, want %q", props["image.format"], "png")
	}
}

func TestExtract_GIF(t *testing.T) {
	data := makeGIF()
	props := Extract("image/gif", bytes.NewReader(data))

	if props["image.width"] != "1" {
		t.Errorf("image.width: got %q, want %q", props["image.width"], "1")
	}
	if props["image.height"] != "1" {
		t.Errorf("image.height: got %q, want %q", props["image.height"], "1")
	}
	if props["image.format"] != "gif" {
		t.Errorf("image.format: got %q, want %q", props["image.format"], "gif")
	}
}

func TestExtract_JPEG(t *testing.T) {
	data := makeJPEG(320, 240)
	props := Extract("image/jpeg", bytes.NewReader(data))

	if props["image.width"] != "320" {
		t.Errorf("image.width: got %q, want %q", props["image.width"], "320")
	}
	if props["image.height"] != "240" {
		t.Errorf("image.height: got %q, want %q", props["image.height"], "240")
	}
	if props["image.format"] != "jpeg" {
		t.Errorf("image.format: got %q, want %q", props["image.format"], "jpeg")
	}
}

func TestExtract_Image_TruncatedHeader(t *testing.T) {
	// Truncated data — should not panic and should return empty props.
	props := Extract("image/png", bytes.NewReader([]byte{0x89, 0x50, 0x4E, 0x47}))
	// No panic is the main assertion; we may or may not get properties.
	_ = props
}

func TestExtract_Image_GarbageData(t *testing.T) {
	props := Extract("image/jpeg", bytes.NewReader([]byte("this is not an image")))
	if len(props) != 0 {
		t.Errorf("expected empty props for garbage image data, got %v", props)
	}
}

// ─── JPEG EXIF tests ──────────────────────────────────────────────────────────

func TestExtract_JPEG_EXIF_Make(t *testing.T) {
	data, err := os.ReadFile("testdata/with_exif.jpg")
	if err != nil {
		t.Skip("testdata/with_exif.jpg not found — skipping EXIF test")
	}
	props := Extract("image/jpeg", bytes.NewReader(data))
	if _, ok := props["image.make"]; !ok {
		t.Error("expected image.make property from EXIF JPEG")
	}
}

func TestExtract_JPEG_EXIF_Dimensions(t *testing.T) {
	data, err := os.ReadFile("testdata/with_exif.jpg")
	if err != nil {
		t.Skip("testdata/with_exif.jpg not found — skipping EXIF test")
	}
	props := Extract("image/jpeg", bytes.NewReader(data))
	if props["image.width"] == "" {
		t.Error("expected image.width from EXIF JPEG")
	}
	if props["image.height"] == "" {
		t.Error("expected image.height from EXIF JPEG")
	}
}

func TestExtract_JPEG_NoEXIF_NoCrash(t *testing.T) {
	// A minimal JPEG without EXIF should still return width/height, no crash.
	data := makeJPEG(100, 80)
	props := Extract("image/jpeg", bytes.NewReader(data))
	if props["image.width"] != "100" {
		t.Errorf("image.width: got %q, want %q", props["image.width"], "100")
	}
	if _, ok := props["image.make"]; ok {
		t.Error("expected no image.make for JPEG without EXIF")
	}
}

// ─── PDF tests ────────────────────────────────────────────────────────────────

func TestExtract_PDF_Version(t *testing.T) {
	tests := []struct {
		name    string
		header  []byte
		want    string
	}{
		{
			name:   "PDF 1.4",
			header: []byte("%PDF-1.4\nrest of file..."),
			want:   "1.4",
		},
		{
			name:   "PDF 2.0",
			header: []byte("%PDF-2.0\r\nrest of file..."),
			want:   "2.0",
		},
		{
			name:   "PDF 1.7 with space",
			header: []byte("%PDF-1.7 rest of file"),
			want:   "1.7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := Extract("application/pdf", bytes.NewReader(tc.header))
			if props["pdf.version"] != tc.want {
				t.Errorf("pdf.version: got %q, want %q", props["pdf.version"], tc.want)
			}
		})
	}
}

func TestExtract_PDF_InvalidHeader(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
	}{
		{"not a PDF", []byte("This is a text file, not a PDF")},
		{"too short", []byte("%PDF")},
		{"empty", []byte{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			props := Extract("application/pdf", bytes.NewReader(tc.header))
			if _, ok := props["pdf.version"]; ok {
				t.Error("expected no pdf.version property for invalid PDF header")
			}
		})
	}
}

// ─── PDF deep tests ───────────────────────────────────────────────────────────

func TestExtract_PDF_Pages(t *testing.T) {
	data, err := os.ReadFile("testdata/test.pdf")
	if err != nil {
		t.Skip("testdata/test.pdf not found")
	}
	props := Extract("application/pdf", bytes.NewReader(data))
	if props["pdf.pages"] == "" {
		t.Error("expected pdf.pages from test PDF")
	}
	if props["pdf.version"] == "" {
		t.Error("expected pdf.version from test PDF")
	}
}

func TestExtract_PDF_TitleAuthor(t *testing.T) {
	data, err := os.ReadFile("testdata/test_with_info.pdf")
	if err != nil {
		t.Skip("testdata/test_with_info.pdf not found")
	}
	props := Extract("application/pdf", bytes.NewReader(data))
	if props["pdf.title"] == "" {
		t.Error("expected pdf.title from PDF with info dict")
	}
	if props["pdf.author"] == "" {
		t.Error("expected pdf.author from PDF with info dict")
	}
}

func TestExtract_PDF_Malformed_NoCrash(t *testing.T) {
	// Garbage after header — pdfcpu should handle gracefully.
	data := []byte("%PDF-1.4\ngarbage garbage garbage")
	props := Extract("application/pdf", bytes.NewReader(data))
	if props["pdf.version"] != "1.4" {
		t.Errorf("pdf.version: got %q, want %q", props["pdf.version"], "1.4")
	}
	_ = props["pdf.pages"]
}

// ─── Office tests ─────────────────────────────────────────────────────────────

func TestExtract_Office_OOXML(t *testing.T) {
	// PK ZIP signature.
	header := []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00}

	ooxmlTypes := []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	for _, ct := range ooxmlTypes {
		props := Extract(ct, bytes.NewReader(header))
		if props["office.container"] != "ooxml" {
			t.Errorf("content-type %q: office.container = %q, want %q", ct, props["office.container"], "ooxml")
		}
	}
}

func TestExtract_Office_OLE2(t *testing.T) {
	// OLE2 magic.
	header := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

	legacyTypes := []string{
		"application/msword",
		"application/vnd.ms-excel",
		"application/vnd.ms-powerpoint",
	}
	for _, ct := range legacyTypes {
		props := Extract(ct, bytes.NewReader(header))
		if props["office.container"] != "ole2" {
			t.Errorf("content-type %q: office.container = %q, want %q", ct, props["office.container"], "ole2")
		}
	}
}

func TestExtract_Office_TooShort(t *testing.T) {
	// With the new implementation OLE2 content type always sets office.container=ole2
	// before attempting further parsing. Truncated data is fine; container is still set.
	props := Extract("application/msword", bytes.NewReader([]byte{0xD0, 0xCF}))
	if props["office.container"] != "ole2" {
		t.Errorf("expected office.container=ole2 for OLE2 content type, got %q", props["office.container"])
	}
}

func TestExtract_Office_UnknownMagic(t *testing.T) {
	// OLE2 content type always sets office.container=ole2 regardless of magic bytes.
	props := Extract("application/msword", bytes.NewReader([]byte{0x00, 0x01, 0x02, 0x03}))
	if props["office.container"] != "ole2" {
		t.Errorf("expected office.container=ole2 for OLE2 content type, got %q", props["office.container"])
	}
}

// ─── OOXML deep tests ─────────────────────────────────────────────────────────────

// makeOOXML creates a minimal OOXML (ZIP) file with the given docProps/core.xml
// and docProps/app.xml content.
func makeOOXML(coreXML, appXML string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	addFile := func(name, content string) {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}

	addFile("[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`)
	if coreXML != "" {
		addFile("docProps/core.xml", coreXML)
	}
	if appXML != "" {
		addFile("docProps/app.xml", appXML)
	}
	w.Close()
	return buf.Bytes()
}

func TestExtract_OOXML_Docx_TitleAuthorWordCount(t *testing.T) {
	core := `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>My Doc</dc:title><dc:creator>Alice</dc:creator></cp:coreProperties>`
	app := `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Words>500</Words></Properties>`
	data := makeOOXML(core, app)
	ct := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	props := Extract(ct, bytes.NewReader(data))

	if props["office.container"] != "ooxml" {
		t.Errorf("office.container: got %q, want %q", props["office.container"], "ooxml")
	}
	if props["office.title"] != "My Doc" {
		t.Errorf("office.title: got %q, want %q", props["office.title"], "My Doc")
	}
	if props["office.author"] != "Alice" {
		t.Errorf("office.author: got %q, want %q", props["office.author"], "Alice")
	}
	if props["office.word_count"] != "500" {
		t.Errorf("office.word_count: got %q, want %q", props["office.word_count"], "500")
	}
}

func TestExtract_OOXML_Pptx_SlideCount(t *testing.T) {
	app := `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Slides>12</Slides></Properties>`
	data := makeOOXML("", app)
	ct := "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	props := Extract(ct, bytes.NewReader(data))
	if props["office.slide_count"] != "12" {
		t.Errorf("office.slide_count: got %q, want %q", props["office.slide_count"], "12")
	}
}

func TestExtract_OOXML_Xlsx_SheetCount(t *testing.T) {
	app := `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Sheets>3</Sheets></Properties>`
	data := makeOOXML("", app)
	ct := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	props := Extract(ct, bytes.NewReader(data))
	if props["office.sheet_count"] != "3" {
		t.Errorf("office.sheet_count: got %q, want %q", props["office.sheet_count"], "3")
	}
}

// ─── OLE2 deep tests ──────────────────────────────────────────────────────────

func TestExtract_OLE2_Container(t *testing.T) {
	// OLE2 magic bytes — container type always set even if property parsing fails.
	header := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	props := Extract("application/msword", bytes.NewReader(header))
	if props["office.container"] != "ole2" {
		t.Errorf("office.container: got %q, want %q", props["office.container"], "ole2")
	}
}

// ─── Unknown / unhandled content types ────────────────────────────────────────

func TestExtract_UnknownContentType(t *testing.T) {
	props := Extract("application/octet-stream", bytes.NewReader([]byte("hello world")))
	if len(props) != 0 {
		t.Errorf("expected empty props for application/octet-stream, got %v", props)
	}
}

func TestExtract_NeverNil(t *testing.T) {
	// Extract must always return a non-nil map.
	props := Extract("application/octet-stream", bytes.NewReader([]byte{}))
	if props == nil {
		t.Error("Extract should never return nil")
	}
}

// ─── Audio tests ──────────────────────────────────────────────────────────────

func TestExtract_Audio_MP3(t *testing.T) {
	data, err := os.ReadFile("testdata/test.mp3")
	if err != nil {
		t.Skip("testdata/test.mp3 not found")
	}
	props := Extract("audio/mpeg", bytes.NewReader(data))
	if props["audio.format"] == "" {
		t.Error("expected audio.format from MP3")
	}
}

func TestExtract_Audio_GarbageData_NoCrash(t *testing.T) {
	props := Extract("audio/mpeg", bytes.NewReader([]byte("not an mp3")))
	_ = props
}

// ─── Video tests ──────────────────────────────────────────────────────────────

func TestExtract_Video_MP4(t *testing.T) {
	data, err := os.ReadFile("testdata/test.mp4")
	if err != nil {
		t.Skip("testdata/test.mp4 not found")
	}
	props := Extract("video/mp4", bytes.NewReader(data))
	if props["video.duration_s"] == "" {
		t.Error("expected video.duration_s from MP4")
	}
	if props["video.width"] == "" {
		t.Error("expected video.width from MP4")
	}
}

func TestExtract_Video_GarbageData_NoCrash(t *testing.T) {
	props := Extract("video/mp4", bytes.NewReader([]byte("not a video")))
	_ = props
}

// ─── Text tests ───────────────────────────────────────────────────────────────

func TestExtract_Text_UTF8_Lines(t *testing.T) {
	data := []byte("line one\nline two\nline three\n")
	props := Extract("text/plain", bytes.NewReader(data))
	if props["text.encoding"] != "utf-8" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-8")
	}
	if props["text.lines"] != "3" {
		t.Errorf("text.lines: got %q, want %q", props["text.lines"], "3")
	}
}

func TestExtract_Text_UTF8BOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello\nworld\n")...)
	props := Extract("text/plain", bytes.NewReader(data))
	if props["text.encoding"] != "utf-8-bom" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-8-bom")
	}
}

func TestExtract_Text_UTF16LE(t *testing.T) {
	data := []byte{0xFF, 0xFE, 0x68, 0x00, 0x69, 0x00} // BOM + "hi" in UTF-16 LE
	props := Extract("text/plain", bytes.NewReader(data))
	if props["text.encoding"] != "utf-16-le" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-16-le")
	}
}

func TestExtract_Text_UTF16BE(t *testing.T) {
	data := []byte{0xFE, 0xFF, 0x00, 0x68, 0x00, 0x69} // BOM + "hi" in UTF-16 BE
	props := Extract("text/plain", bytes.NewReader(data))
	if props["text.encoding"] != "utf-16-be" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-16-be")
	}
}

func TestExtract_Text_Binary_Skipped(t *testing.T) {
	// Binary data — not valid UTF-8, no BOM — should produce no props.
	data := []byte{0x80, 0x81, 0x82, 0x83, 0xFF, 0xFD}
	props := Extract("text/plain", bytes.NewReader(data))
	if _, ok := props["text.encoding"]; ok {
		t.Error("expected no props for binary data passed as text/plain")
	}
}

func TestExtract_Text_JSON(t *testing.T) {
	data := []byte(`{"key":"value"}` + "\n")
	props := Extract("application/json", bytes.NewReader(data))
	if props["text.encoding"] != "utf-8" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-8")
	}
	if props["text.lines"] != "1" {
		t.Errorf("text.lines: got %q, want %q", props["text.lines"], "1")
	}
}

func TestExtract_Text_EmptyFile(t *testing.T) {
	props := Extract("text/plain", bytes.NewReader([]byte{}))
	if props["text.encoding"] != "utf-8" {
		t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-8")
	}
	if props["text.lines"] != "0" {
		t.Errorf("text.lines: got %q, want %q", props["text.lines"], "0")
	}
}
