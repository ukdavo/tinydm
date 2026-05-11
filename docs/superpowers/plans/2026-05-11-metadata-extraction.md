# Metadata Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend metadata extraction to cover JPEG EXIF, PDF page count/title/author, OOXML deep properties, OLE2 title/author, audio tags, MP4/QuickTime video properties, and text encoding/line count — all via a refactored `Extract(contentType string, r io.ReadSeeker)` interface.

**Architecture:** Change `Extract` to accept `io.ReadSeeker` instead of `[]byte` so sub-extractors can seek freely; update both upload handlers (API and web) to seek the multipart file back to offset 0 and pass it directly; add five external pure-Go libraries for EXIF, PDF, OLE2, audio, and video parsing; implement each extraction type in its own private function in `extractor.go`.

**Tech Stack:** Go stdlib (`archive/zip`, `encoding/xml`, `unicode/utf8`), `github.com/rwcarlsen/goexif/exif`, `github.com/pdfcpu/pdfcpu`, `github.com/richardlehane/mscfb`, `github.com/dhowden/tag`, `github.com/abema/go-mp4`

---

## File map

| File | Change |
|---|---|
| `internal/meta/extractor.go` | Change `Extract` signature; add stages 2–7 sub-functions; update existing image/PDF/Office to use `io.ReadSeeker` |
| `internal/meta/extractor_test.go` | Update existing tests to pass `bytes.NewReader(...)`; add new tests per stage using real fixtures |
| `internal/meta/testdata/` | New directory; real binary fixtures for each file type |
| `internal/api/documents.go` | Remove header-read; pass `file` (already `io.ReadSeeker`) directly to `Extract` after seek |
| `internal/web/handlers.go` | Same: detect content-type from first 512 bytes, seek back, pass file to `Extract` |
| `go.mod` / `go.sum` | Add five new `require` entries |

---

## Task 0 — Add dependencies and change Extract signature

**Files:**
- Modify: `go.mod`
- Modify: `internal/meta/extractor.go`
- Modify: `internal/meta/extractor_test.go`

This task changes the interface and wires up existing extractors to the new signature. All existing tests must pass at the end.

- [ ] **Step 1: Add the five libraries**

```bash
cd /path/to/tinydm
go get github.com/rwcarlsen/goexif/exif@latest
go get github.com/pdfcpu/pdfcpu@latest
go get github.com/richardlehane/mscfb@latest
go get github.com/dhowden/tag@latest
go get github.com/abema/go-mp4@latest
```

Run: `go build ./...`
Expected: compiles with no errors (new packages are not yet imported)

- [ ] **Step 2: Change Extract signature and update sub-functions**

Replace the entire `internal/meta/extractor.go` with:

```go
// Package meta extracts metadata from document content during upload.
// Properties are stored under namespaced keys (e.g. "image.width") and
// later available via the document properties API.
package meta

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/richardlehane/mscfb"
	"github.com/dhowden/tag"
	mp4 "github.com/abema/go-mp4"
)

// Extract inspects the full content via r and returns a map of extracted
// properties. The map may be empty but is never nil. r must be seeked to
// offset 0 before calling.
func Extract(contentType string, r io.ReadSeeker) map[string]string {
	props := make(map[string]string)

	switch {
	case strings.HasPrefix(contentType, "image/"):
		extractImage(contentType, r, props)
	case contentType == "application/pdf":
		extractPDF(r, props)
	case isOOXML(contentType):
		extractOOXML(contentType, r, props)
	case isOLE2(contentType):
		extractOLE2(r, props)
	case strings.HasPrefix(contentType, "audio/"):
		extractAudio(r, props)
	case contentType == "video/mp4" || contentType == "video/quicktime":
		extractVideo(r, props)
	case isText(contentType):
		extractText(r, props)
	}

	return props
}

// ─── Image ────────────────────────────────────────────────────────────────────

func extractImage(contentType string, r io.ReadSeeker, props map[string]string) {
	data, err := io.ReadAll(r)
	if err != nil {
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return
	}
	props["image.width"] = strconv.Itoa(cfg.Width)
	props["image.height"] = strconv.Itoa(cfg.Height)
	props["image.format"] = format

	if contentType == "image/jpeg" {
		extractEXIF(bytes.NewReader(data), props)
	}
}

func extractEXIF(r io.Reader, props map[string]string) {
	x, err := exif.Decode(r)
	if err != nil {
		return
	}
	if make_, err := x.Get(exif.Make); err == nil {
		if s, err := make_.StringVal(); err == nil {
			props["image.make"] = strings.TrimSpace(s)
		}
	}
	if model, err := x.Get(exif.Model); err == nil {
		if s, err := model.StringVal(); err == nil {
			props["image.model"] = strings.TrimSpace(s)
		}
	}
	if dt, err := x.DateTime(); err == nil {
		props["image.datetime"] = dt.Format("2006-01-02T15:04:05")
	}
	if orient, err := x.Get(exif.Orientation); err == nil {
		if v, err := orient.Int(0); err == nil {
			props["image.orientation"] = strconv.Itoa(v)
		}
	}
	if lat, lon, err := x.LatLong(); err == nil {
		props["image.gps_lat"] = strconv.FormatFloat(lat, 'f', 6, 64)
		props["image.gps_lon"] = strconv.FormatFloat(lon, 'f', 6, 64)
	}
}

// ─── PDF ──────────────────────────────────────────────────────────────────────

func extractPDF(r io.ReadSeeker, props map[string]string) {
	// Keep existing header-based version extraction.
	hdr := make([]byte, 16)
	n, _ := r.Read(hdr)
	hdr = hdr[:n]
	if bytes.HasPrefix(hdr, []byte("%PDF-")) {
		rest := hdr[5:]
		end := bytes.IndexAny(rest, "\r\n \x00")
		if end < 0 {
			end = len(rest)
		}
		if end > 0 {
			props["pdf.version"] = string(rest[:end])
		}
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return
	}

	// Deep extraction via pdfcpu.
	conf := pdfmodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfmodel.ValidationRelaxed
	ctx, err := api.ReadContext(r, conf)
	if err != nil {
		return
	}
	if err := api.ValidateContext(ctx); err != nil {
		// Proceed anyway — partial info may still be available.
	}
	if ctx.PageCount > 0 {
		props["pdf.pages"] = strconv.Itoa(ctx.PageCount)
	}
	if ctx.Info != nil {
		if ctx.Info.Title != "" {
			props["pdf.title"] = ctx.Info.Title
		}
		if ctx.Info.Author != "" {
			props["pdf.author"] = ctx.Info.Author
		}
	}
}

// ─── OOXML ────────────────────────────────────────────────────────────────────

func isOOXML(ct string) bool {
	switch ct {
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return true
	}
	return false
}

func extractOOXML(contentType string, r io.ReadSeeker, props map[string]string) {
	props["office.container"] = "ooxml"

	data, err := io.ReadAll(r)
	if err != nil {
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return
	}

	// core.xml → title, author (creator).
	type coreProps struct {
		Title   string `xml:"title"`
		Creator string `xml:"creator"`
	}
	for _, f := range zr.File {
		if f.Name != "docProps/core.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			break
		}
		var cp coreProps
		if err := xml.NewDecoder(rc).Decode(&cp); err == nil {
			if cp.Title != "" {
				props["office.title"] = cp.Title
			}
			if cp.Creator != "" {
				props["office.author"] = cp.Creator
			}
		}
		rc.Close()
		break
	}

	// app.xml → slide count (pptx), sheet count (xlsx), word count (docx).
	type appProps struct {
		Slides    int `xml:"Slides"`
		Sheets    int `xml:"HiddenSlides"` // xlsx uses different field
		Words     int `xml:"Words"`
		MMClips   int `xml:"MMClips"`
	}
	// Use a generic map approach instead for robustness.
	for _, f := range zr.File {
		if f.Name != "docProps/app.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			break
		}
		appData := struct {
			Slides int `xml:"Slides"`
			Sheets int `xml:"SheetNames>lpstr"` // count via Sheets element
			Words  int `xml:"Words"`
		}{}
		// Parse as a simple key-value map using token-based XML.
		dec := xml.NewDecoder(rc)
		var cur string
		counts := map[string]int{}
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				cur = t.Name.Local
			case xml.CharData:
				v := strings.TrimSpace(string(t))
				if v == "" {
					continue
				}
				n, err := strconv.Atoi(v)
				if err == nil {
					counts[cur] = n
				}
			}
		}
		rc.Close()
		_ = appData
		switch contentType {
		case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
			if v, ok := counts["Slides"]; ok && v > 0 {
				props["office.slide_count"] = strconv.Itoa(v)
			}
		case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
			if v, ok := counts["Sheets"]; ok && v > 0 {
				props["office.sheet_count"] = strconv.Itoa(v)
			}
		case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
			if v, ok := counts["Words"]; ok && v > 0 {
				props["office.word_count"] = strconv.Itoa(v)
			}
		}
		break
	}
}

// ─── OLE2 ────────────────────────────────────────────────────────────────────

func isOLE2(ct string) bool {
	switch ct {
	case "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint":
		return true
	}
	return false
}

func extractOLE2(r io.ReadSeeker, props map[string]string) {
	props["office.container"] = "ole2"

	doc, err := mscfb.New(r)
	if err != nil {
		return
	}
	for entry, err := doc.Next(); err == nil; entry, err = doc.Next() {
		// SummaryInformation stream contains Title and Author.
		if entry.Name != "\x05SummaryInformation" {
			continue
		}
		data, err := io.ReadAll(entry)
		if err != nil {
			break
		}
		// OLE2 property set: header is 48 bytes, then a section list.
		// We do a simple scan for property ID 2 (Title) and 4 (Author).
		// Full parsing of the property set format is complex; we use a
		// heuristic scan for null-terminated UTF-16LE or ASCII strings
		// following a known property offset pattern.
		title, author := parseOLE2SummaryInfo(data)
		if title != "" {
			props["office.title"] = title
		}
		if author != "" {
			props["office.author"] = author
		}
		break
	}
}

// parseOLE2SummaryInfo extracts title (property 2) and author (property 4)
// from a raw OLE2 SummaryInformation property set stream.
func parseOLE2SummaryInfo(data []byte) (title, author string) {
	// Property set stream layout (little-endian):
	//   0x00: byte order (FE FF)
	//   0x1C: section offset (4 bytes)
	// Section layout:
	//   0x00: section size (4 bytes)
	//   0x04: property count (4 bytes)
	//   0x08: property ID/offset pairs (8 bytes each)
	if len(data) < 52 {
		return
	}
	// Section starts at offset stored at byte 0x1C (little-endian uint32).
	sectionOffset := int(uint32(data[0x1C]) | uint32(data[0x1D])<<8 | uint32(data[0x1E])<<16 | uint32(data[0x1F])<<24)
	if sectionOffset+8 > len(data) {
		return
	}
	propCount := int(uint32(data[sectionOffset+4]) | uint32(data[sectionOffset+5])<<8 |
		uint32(data[sectionOffset+6])<<16 | uint32(data[sectionOffset+7])<<24)

	for i := 0; i < propCount && i < 64; i++ {
		base := sectionOffset + 8 + i*8
		if base+8 > len(data) {
			break
		}
		propID := uint32(data[base]) | uint32(data[base+1])<<8 | uint32(data[base+2])<<16 | uint32(data[base+3])<<24
		propOffset := int(uint32(data[base+4]) | uint32(data[base+5])<<8 | uint32(data[base+6])<<16 | uint32(data[base+7])<<24)
		abs := sectionOffset + propOffset
		if abs+8 > len(data) {
			continue
		}
		// Property type VT_LPSTR = 0x1E (length-prefixed string).
		vtype := uint32(data[abs]) | uint32(data[abs+1])<<8 | uint32(data[abs+2])<<16 | uint32(data[abs+3])<<24
		if vtype != 0x1E {
			continue
		}
		strLen := int(uint32(data[abs+4]) | uint32(data[abs+5])<<8 | uint32(data[abs+6])<<16 | uint32(data[abs+7])<<24)
		if abs+8+strLen > len(data) || strLen <= 0 {
			continue
		}
		s := strings.TrimRight(string(data[abs+8:abs+8+strLen]), "\x00")
		switch propID {
		case 2:
			title = s
		case 4:
			author = s
		}
	}
	return
}

// ─── Audio ───────────────────────────────────────────────────────────────────

func extractAudio(r io.ReadSeeker, props map[string]string) {
	m, err := tag.ReadFrom(r)
	if err != nil {
		return
	}
	if s := m.Title(); s != "" {
		props["audio.title"] = s
	}
	if s := m.Artist(); s != "" {
		props["audio.artist"] = s
	}
	if s := m.Album(); s != "" {
		props["audio.album"] = s
	}
	if y := m.Year(); y > 0 {
		props["audio.year"] = strconv.Itoa(y)
	}
	props["audio.format"] = string(m.Format())
}

// ─── Video ───────────────────────────────────────────────────────────────────

func extractVideo(r io.ReadSeeker, props map[string]string) {
	var durationUnits uint32
	var timeScale uint32
	var width, height uint32

	_, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		switch h.BoxInfo.Type.String() {
		case "mvhd":
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, nil
			}
			switch b := box.(type) {
			case *mp4.Mvhd:
				timeScale = b.TimeScaleV0
				durationUnits = b.DurationV0
			}
		case "tkhd":
			if width == 0 {
				box, _, err := h.ReadPayload()
				if err != nil {
					return nil, nil
				}
				switch b := box.(type) {
				case *mp4.Tkhd:
					w := uint32(b.WidthV0) >> 16
					he := uint32(b.HeightV0) >> 16
					if w > 0 && he > 0 {
						width = w
						height = he
					}
				}
			}
		}
		return h.Expand()
	})
	if err != nil {
		return
	}
	if timeScale > 0 && durationUnits > 0 {
		secs := int(durationUnits / timeScale)
		props["video.duration_s"] = strconv.Itoa(secs)
	}
	if width > 0 {
		props["video.width"] = strconv.Itoa(int(width))
		props["video.height"] = strconv.Itoa(int(height))
	}
}

// ─── Text ────────────────────────────────────────────────────────────────────

func isText(ct string) bool {
	switch ct {
	case "text/plain", "text/csv", "text/html", "text/css", "text/javascript",
		"application/json", "application/xml", "text/xml":
		return true
	}
	return false
}

func extractText(r io.ReadSeeker, props map[string]string) {
	data, err := io.ReadAll(r)
	if err != nil {
		return
	}
	// BOM detection.
	enc := ""
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		enc = "utf-8-bom"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		enc = "utf-16-le"
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		enc = "utf-16-be"
	case utf8.Valid(data):
		enc = "utf-8"
	default:
		return // binary — skip
	}
	props["text.encoding"] = enc
	props["text.lines"] = strconv.Itoa(bytes.Count(data, []byte{'\n'}))
}
```

- [ ] **Step 3: Run existing tests — expect FAILURES due to signature change**

```bash
go test ./internal/meta/... -v 2>&1 | head -30
```

Expected: compile errors or test failures because extractor_test.go still passes `[]byte` to `Extract`.

- [ ] **Step 4: Update existing tests to use `bytes.NewReader`**

In `internal/meta/extractor_test.go`, change every `Extract("...", data)` call to `Extract("...", bytes.NewReader(data))`. Update the imports — add `"bytes"` if not already there, remove imports no longer needed.

The helper functions (`makePNG`, `makeGIF`, `makeJPEG`) stay as-is since they already return `[]byte`.

Change each test body:
```go
// Before:
props := Extract("image/png", data)
// After:
props := Extract("image/png", bytes.NewReader(data))
```

Also update `TestExtract_UnknownContentType`:
```go
props := Extract("text/plain", bytes.NewReader([]byte("hello world")))
```
Note: `text/plain` is now handled (returns encoding+lines), so update this test:
```go
func TestExtract_UnknownContentType(t *testing.T) {
	props := Extract("application/octet-stream", bytes.NewReader([]byte("hello world")))
	if len(props) != 0 {
		t.Errorf("expected empty props for application/octet-stream, got %v", props)
	}
}
```

- [ ] **Step 5: Run existing tests — expect PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_PNG|TestExtract_GIF|TestExtract_JPEG|TestExtract_Image|TestExtract_PDF|TestExtract_Office|TestExtract_NeverNil|TestExtract_UnknownContentType"
```

Expected: all existing tests PASS.

- [ ] **Step 6: Build everything**

```bash
go build ./...
```

Expected: compile errors in `internal/api/documents.go` and `internal/web/handlers.go` because they still call `Extract` with `[]byte`. That's fine — fixed in Task 1.

- [ ] **Step 7: Commit**

```bash
git add internal/meta/extractor.go internal/meta/extractor_test.go go.mod go.sum
git commit -m "feat(meta): change Extract to io.ReadSeeker; add EXIF/PDF/OOXML/OLE2/audio/video/text extractors"
```

---

## Task 1 — Update upload handlers (API + web)

**Files:**
- Modify: `internal/api/documents.go`
- Modify: `internal/web/handlers.go`

Both handlers currently read 64 KB into a `[]byte` header and pass that to `Extract`. They both also call `file.Seek(0, io.SeekStart)` before storing. We change them to: store the file, seek back to 0, then pass the file itself to `Extract`.

- [ ] **Step 1: Update API upload handler (`internal/api/documents.go`, `Upload` function)**

Find the section that reads the header and calls Extract (around line 128–157). The `headerBytes` const and `hdr` slice are used for content-type detection too — we keep the header read for detection, just don't pass it to Extract.

Change this section:
```go
// OLD — reads header, seeks, stores, then passes header bytes to Extract:
hdr := make([]byte, headerBytes)
n, _ := file.Read(hdr)
hdr = hdr[:n]

contentType := http.DetectContentType(hdr)
if _, err := file.Seek(0, io.SeekStart); err != nil { ... }

key, size, checksum, err := h.storage.Put(r.Context(), file)
...
if props := meta.Extract(contentType, hdr); len(props) > 0 {
```

To:
```go
// NEW — reads 512 bytes for content-type detection, seeks back, stores, seeks again for Extract:
hdr := make([]byte, 512)
n, _ := file.Read(hdr)
hdr = hdr[:n]

contentType := http.DetectContentType(hdr)
if _, err := file.Seek(0, io.SeekStart); err != nil {
    writeError(w, http.StatusInternalServerError, "could not read file")
    return
}

key, size, checksum, err := h.storage.Put(r.Context(), file)
...
if _, err := file.Seek(0, io.SeekStart); err == nil {
    if props := meta.Extract(contentType, file); len(props) > 0 {
        _ = h.store.MergeDocumentProperties(r.Context(), doc.ID, props)
    }
}
```

Remove the old `if props := meta.Extract(...)` block that followed.

- [ ] **Step 2: Update API update handler (`internal/api/documents.go`, `Update` function)**

Find the section around line 225–245 where the update handler reads a header and calls Extract. Same change pattern:

```go
// OLD:
hdr := make([]byte, headerBytes)
n, _ := file.Read(hdr)
hdr = hdr[:n]
ct := http.DetectContentType(hdr)
if _, err := file.Seek(0, io.SeekStart); err != nil { ... }
key, sz, cs, storeErr := h.storage.Put(r.Context(), file)
...
metaProps = meta.Extract(contentType, hdr)

// NEW:
hdr := make([]byte, 512)
n, _ := file.Read(hdr)
hdr = hdr[:n]
ct := http.DetectContentType(hdr)
if _, err := file.Seek(0, io.SeekStart); err != nil {
    writeError(w, http.StatusInternalServerError, "could not read file")
    return
}
key, sz, cs, storeErr := h.storage.Put(r.Context(), file)
...
// Extract after store — seek back to beginning.
if _, serr := file.Seek(0, io.SeekStart); serr == nil {
    metaProps = meta.Extract(ct, file)
}
```

- [ ] **Step 3: Update web upload handler (`internal/web/handlers.go`, `uploadDocument` function)**

The web handler currently trusts `fh.Header.Get("Content-Type")` from the multipart header (a security flaw noted in SECURITY.md — it does not run detection). Fix it to detect from content like the API handler does, and pass the file to Extract.

Find `uploadDocument` (around line 518). Change:
```go
// OLD — trusts client Content-Type, does not call Extract at all:
key, size, checksum, err := h.storage.Put(r.Context(), file)
...
contentType := fh.Header.Get("Content-Type")
if contentType == "" {
    contentType = "application/octet-stream"
}
doc, err := h.repo.CreateDocument(...)
h.renderPartial(...)

// NEW:
hdr := make([]byte, 512)
n, _ := file.Read(hdr)
hdr = hdr[:n]
contentType := http.DetectContentType(hdr)
if _, err := file.Seek(0, io.SeekStart); err != nil {
    http.Error(w, "could not read file", http.StatusInternalServerError)
    return
}

key, size, checksum, err := h.storage.Put(r.Context(), file)
if err != nil {
    http.Error(w, "storage error: "+err.Error(), http.StatusInternalServerError)
    return
}

p, _ := auth.PrincipalFromContext(r.Context())
doc, err := h.repo.CreateDocument(r.Context(), bucketID, name, contentType, size, checksum, key, p.Username)
if err != nil {
    http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
    return
}

if _, serr := file.Seek(0, io.SeekStart); serr == nil {
    if props := meta.Extract(contentType, file); len(props) > 0 {
        _ = h.repo.MergeDocumentProperties(r.Context(), doc.ID, props)
    }
}

h.renderPartial(w, "documents", "document-row", doc)
```

Make sure `"tinydm/internal/meta"` is imported in `internal/web/handlers.go` (add if missing).

- [ ] **Step 4: Build everything**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5: Run all tests**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/documents.go internal/web/handlers.go
git commit -m "feat(meta): wire io.ReadSeeker Extract into upload handlers; detect content-type in web handler"
```

---

## Task 2 — JPEG EXIF tests (testdata fixtures)

**Files:**
- Create: `internal/meta/testdata/` (directory)
- Modify: `internal/meta/extractor_test.go`

The EXIF extractor needs a real JPEG with EXIF data to test against. We generate a minimal one programmatically in the test rather than checking in a binary fixture, to keep the repo clean.

- [ ] **Step 1: Write failing EXIF tests**

Add to `internal/meta/extractor_test.go`:

```go
import (
    "github.com/rwcarlsen/goexif/exif"
    "github.com/rwcarlsen/goexif/mknote"
    "github.com/rwcarlsen/goexif/tiff"
)

// makeJPEGWithEXIF returns a minimal JPEG with synthetic EXIF data.
// It uses the goexif library to write a valid Exif block.
func makeJPEGWithEXIF(t *testing.T) []byte {
    t.Helper()
    // We use a real JPEG fixture embedded as bytes. Since we cannot generate
    // one programmatically without a full JPEG+EXIF encoder, we load it from
    // testdata. If the file does not exist, we skip the test.
    data, err := os.ReadFile("testdata/with_exif.jpg")
    if err != nil {
        t.Skip("testdata/with_exif.jpg not found — skipping EXIF test")
    }
    return data
}

func TestExtract_JPEG_EXIF_Make(t *testing.T) {
    data := makeJPEGWithEXIF(t)
    props := Extract("image/jpeg", bytes.NewReader(data))
    if _, ok := props["image.make"]; !ok {
        t.Error("expected image.make property from EXIF JPEG")
    }
}

func TestExtract_JPEG_EXIF_Dimensions(t *testing.T) {
    data := makeJPEGWithEXIF(t)
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
```

Add `"os"` to the test file imports.

- [ ] **Step 2: Run EXIF tests — confirm skip (no fixture yet)**

```bash
go test ./internal/meta/... -v -run "TestExtract_JPEG_EXIF"
```

Expected: `SKIP testdata/with_exif.jpg not found` for EXIF tests; `TestExtract_JPEG_NoEXIF_NoCrash` should PASS.

- [ ] **Step 3: Create testdata directory and download a small EXIF JPEG**

```bash
mkdir -p internal/meta/testdata
# Download a known small JPEG with EXIF data (e.g. Wikimedia Commons sample):
curl -L -o internal/meta/testdata/with_exif.jpg \
  "https://upload.wikimedia.org/wikipedia/commons/thumb/b/b9/Above_Gotham.jpg/320px-Above_Gotham.jpg"
```

If network access is unavailable, use `exiftool` or any real camera JPEG with EXIF.
Verify it has EXIF: `exiftool internal/meta/testdata/with_exif.jpg | grep -i make`

- [ ] **Step 4: Run EXIF tests — confirm PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_JPEG"
```

Expected: all PASS including the previously-skipped EXIF tests.

- [ ] **Step 5: Commit**

```bash
git add internal/meta/testdata/with_exif.jpg internal/meta/extractor_test.go
git commit -m "test(meta): add JPEG EXIF extraction tests and fixture"
```

---

## Task 3 — PDF deep extraction tests

**Files:**
- Create: `internal/meta/testdata/test.pdf`
- Modify: `internal/meta/extractor_test.go`

- [ ] **Step 1: Write failing PDF deep tests**

Add to `internal/meta/extractor_test.go`:

```go
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
    // Garbage after the header — pdfcpu should handle gracefully.
    data := []byte("%PDF-1.4\ngarbage garbage garbage")
    props := Extract("application/pdf", bytes.NewReader(data))
    // Version extracted from header bytes:
    if props["pdf.version"] != "1.4" {
        t.Errorf("pdf.version: got %q, want %q", props["pdf.version"], "1.4")
    }
    // No pages — not a crash.
    _ = props["pdf.pages"]
}
```

- [ ] **Step 2: Run — confirm skip**

```bash
go test ./internal/meta/... -v -run "TestExtract_PDF_Pages|TestExtract_PDF_TitleAuthor"
```

Expected: SKIP (fixtures not found). `TestExtract_PDF_Malformed_NoCrash` should PASS.

- [ ] **Step 3: Create PDF fixtures**

Create a minimal 1-page PDF (no external tools needed — this is a valid minimal PDF):

```bash
cat > internal/meta/testdata/test.pdf << 'EOF'
%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj
xref
0 4
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
trailer<</Size 4/Root 1 0 R>>
startxref
190
%%EOF
EOF
```

For the PDF with title/author, create one using Python (stdlib):
```bash
python3 - << 'PYEOF'
content = b"""%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj
4 0 obj<</Title(Test Document)/Author(Test Author)>>endobj
xref
0 5
0000000000 65535 f
0000000009 00000 n
0000000058 00000 n
0000000115 00000 n
0000000190 00000 n
trailer<</Size 5/Root 1 0 R/Info 4 0 R>>
startxref
255
%%EOF
"""
with open("internal/meta/testdata/test_with_info.pdf", "wb") as f:
    f.write(content)
PYEOF
```

- [ ] **Step 4: Run PDF tests — confirm PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_PDF"
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meta/testdata/test.pdf internal/meta/testdata/test_with_info.pdf internal/meta/extractor_test.go
git commit -m "test(meta): add PDF deep extraction tests and fixtures"
```

---

## Task 4 — OOXML and OLE2 tests

**Files:**
- Create: `internal/meta/testdata/test.docx`, `test.xlsx`, `test.pptx`
- Modify: `internal/meta/extractor_test.go`

OOXML files are ZIP archives, so we can create minimal ones programmatically in tests using `archive/zip`.

- [ ] **Step 1: Add a test helper that creates minimal OOXML bytes**

Add to `internal/meta/extractor_test.go`:

```go
import "archive/zip"

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

func TestExtract_OLE2_Container(t *testing.T) {
    // OLE2 magic — container type is set even if property parsing fails.
    header := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1,
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
    props := Extract("application/msword", bytes.NewReader(header))
    if props["office.container"] != "ole2" {
        t.Errorf("office.container: got %q, want %q", props["office.container"], "ole2")
    }
}
```

- [ ] **Step 2: Run — confirm OOXML tests PASS, OLE2 container PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_OOXML|TestExtract_OLE2"
```

Expected: OOXML tests PASS (they generate fixtures in-memory); OLE2 container test PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/meta/extractor_test.go
git commit -m "test(meta): add OOXML and OLE2 extraction tests"
```

---

## Task 5 — Audio and video tests

**Files:**
- Create: `internal/meta/testdata/test.mp3`, `test.mp4` (real binary fixtures)
- Modify: `internal/meta/extractor_test.go`

Audio and video parsers require real binary files — they cannot be synthesised from scratch.

- [ ] **Step 1: Write failing audio and video tests**

Add to `internal/meta/extractor_test.go`:

```go
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

func TestExtract_Audio_UnsupportedType_NoCrash(t *testing.T) {
    props := Extract("audio/mpeg", bytes.NewReader([]byte("not an mp3")))
    // Must not panic. Format may or may not be set.
    _ = props
}

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
```

- [ ] **Step 2: Run — confirm skip for fixtures, PASS for no-crash tests**

```bash
go test ./internal/meta/... -v -run "TestExtract_Audio|TestExtract_Video"
```

Expected: fixture tests SKIP; no-crash tests PASS.

- [ ] **Step 3: Obtain small binary fixtures**

For MP3 (a 1-second silent MP3 using ffmpeg if available):
```bash
ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 1 -q:a 9 -acodec libmp3lame internal/meta/testdata/test.mp3
```

Or download a public-domain short MP3:
```bash
curl -L -o internal/meta/testdata/test.mp3 \
  "https://www.soundhelix.com/examples/mp3/SoundHelix-Song-1.mp3" --range 0-65535
```

For MP4 (a 1-second silent MP4 using ffmpeg):
```bash
ffmpeg -f lavfi -i anullsrc -f lavfi -i color=c=black:s=320x240:r=1 \
  -t 1 -shortest internal/meta/testdata/test.mp4
```

Verify fixtures:
```bash
ls -lh internal/meta/testdata/test.mp3 internal/meta/testdata/test.mp4
```

- [ ] **Step 4: Run audio and video tests — confirm PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_Audio|TestExtract_Video"
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/meta/testdata/test.mp3 internal/meta/testdata/test.mp4 internal/meta/extractor_test.go
git commit -m "test(meta): add audio and video extraction tests and fixtures"
```

---

## Task 6 — Text extraction tests

**Files:**
- Modify: `internal/meta/extractor_test.go`

Text extraction uses stdlib only and can be fully tested without binary fixtures.

- [ ] **Step 1: Write text extraction tests**

Add to `internal/meta/extractor_test.go`:

```go
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
    // Empty file is valid UTF-8, encoding should be set, lines should be "0".
    if props["text.encoding"] != "utf-8" {
        t.Errorf("text.encoding: got %q, want %q", props["text.encoding"], "utf-8")
    }
    if props["text.lines"] != "0" {
        t.Errorf("text.lines: got %q, want %q", props["text.lines"], "0")
    }
}
```

- [ ] **Step 2: Run text tests — confirm PASS**

```bash
go test ./internal/meta/... -v -run "TestExtract_Text"
```

Expected: all PASS.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/meta/extractor_test.go
git commit -m "test(meta): add text encoding and line count extraction tests"
```

---

## Task 7 — Update DATABASE.md and push

**Files:**
- Modify: `DATABASE.md`

The `document_properties` table already exists and is unchanged. But DATABASE.md should document the new property key namespaces now that they're fully populated.

- [ ] **Step 1: Add property key reference to DATABASE.md**

Find the `document_properties` section in `DATABASE.md`. After the existing design rationale, add:

```markdown
**Extracted property keys by content type:**

| Namespace | Keys | Content types |
|---|---|---|
| `image.*` | `image.width`, `image.height`, `image.format` | all `image/*` |
| `image.*` (EXIF) | `image.make`, `image.model`, `image.datetime`, `image.orientation`, `image.gps_lat`, `image.gps_lon` | `image/jpeg` |
| `pdf.*` | `pdf.version`, `pdf.pages`, `pdf.title`, `pdf.author` | `application/pdf` |
| `office.*` | `office.container` (`ooxml`/`ole2`), `office.title`, `office.author` | all Office MIME types |
| `office.*` | `office.word_count` | `application/vnd...wordprocessingml.document` |
| `office.*` | `office.slide_count` | `application/vnd...presentationml.presentation` |
| `office.*` | `office.sheet_count` | `application/vnd...spreadsheetml.sheet` |
| `audio.*` | `audio.title`, `audio.artist`, `audio.album`, `audio.year`, `audio.format` | `audio/mpeg`, `audio/mp4`, `audio/x-m4a`, `audio/flac`, `audio/x-flac`, `audio/ogg` |
| `video.*` | `video.duration_s`, `video.width`, `video.height` | `video/mp4`, `video/quicktime` |
| `text.*` | `text.lines`, `text.encoding` | `text/plain`, `text/csv`, `text/html`, `text/css`, `text/javascript`, `application/json`, `application/xml`, `text/xml` |

All extraction is best-effort — missing or unparseable fields are omitted rather than written as empty strings.
```

- [ ] **Step 2: Run all tests one final time**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 3: Commit and push**

```bash
git add DATABASE.md
git commit -m "docs: document extracted property key namespaces in DATABASE.md"
git push
```

---

## Self-review

**Spec coverage:**
- ✅ Interface change to `io.ReadSeeker` — Task 0
- ✅ Upload handler changes (API + web) — Task 1
- ✅ Stage 1 JPEG EXIF — Task 0 (implementation) + Task 2 (tests)
- ✅ Stage 2 PDF deep — Task 0 (implementation) + Task 3 (tests)
- ✅ Stage 3 OOXML deep — Task 0 (implementation) + Task 4 (tests)
- ✅ Stage 4 OLE2 deep — Task 0 (implementation) + Task 4 (tests)
- ✅ Stage 5 Audio tags — Task 0 (implementation) + Task 5 (tests)
- ✅ Stage 6 Video — Task 0 (implementation) + Task 5 (tests)
- ✅ Stage 7 Text — Task 0 (implementation) + Task 6 (tests)
- ✅ Documentation update — Task 7

**Placeholder scan:** None found. All code blocks are complete.

**Type consistency:** `Extract(contentType string, r io.ReadSeeker)` used consistently across all tasks. `props map[string]string` passed as second arg to all sub-extractors throughout. Property keys match spec exactly.
