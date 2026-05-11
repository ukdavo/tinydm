package meta

import (
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
