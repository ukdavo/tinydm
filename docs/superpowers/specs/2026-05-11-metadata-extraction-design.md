# Automated Metadata Extraction — Design

**Date:** 2026-05-11

---

## Goal

Extend TinyDM's metadata extraction to cover EXIF data from JPEG images, deep PDF properties (page count, title, author), Office document properties for both OOXML and OLE2 formats, audio tags, MP4/QuickTime video properties, and basic text file properties. Each type is implemented as a self-contained stage so the system remains useful and testable between merges.

---

## Architecture

### Interface change

The current extractor reads only the first 64 KB of a file:

```go
func Extract(contentType string, header []byte) map[string]string
```

This is insufficient for EXIF data (can appear anywhere in a JPEG), PDF page count (cross-reference table is at end-of-file), and audio/video tags (often at end-of-file). The interface changes to:

```go
func Extract(contentType string, r io.ReadSeeker) map[string]string
```

Each sub-extractor seeks to the position it needs and reads only what is required. The caller is responsible for providing an `io.ReadSeeker` seeked to offset 0.

### Upload handler changes

Both the API upload handler (`internal/api/documents.go`) and the web upload handler (`internal/web/handlers.go`) currently:

1. Read a 64 KB header from the multipart file
2. Store the full file via the storage layer
3. Call `meta.Extract(contentType, header)`

The new flow:

1. Store the full file via the storage layer
2. Seek the `multipart.File` back to offset 0 (it implements `io.ReadSeeker`)
3. Call `meta.Extract(contentType, f)`

Extraction is synchronous and inline with the upload path. Typical document parsing (PDF info dict, EXIF segment, audio tags) completes in under 10 ms for files under 100 MB. No separate worker, queue, or job table is needed.

### Error handling

Any sub-extractor that fails returns without writing any properties for that type. A failed extraction never fails the upload. This is consistent with the existing behaviour.

### Property storage

Unchanged. Extracted properties are stored via `repo.MergeDocumentProperties`, which upserts key/value pairs. Re-uploading a document overwrites existing extracted properties with fresh values.

---

## Extraction stages

### Stage 1 — JPEG EXIF

**MIME types:** `image/jpeg`

**Library:** `github.com/rwcarlsen/goexif` (MIT, pure Go)

**Properties:**

| Key | Example value |
|---|---|
| `image.make` | `"Apple"` |
| `image.model` | `"iPhone 15 Pro"` |
| `image.datetime` | `"2024-06-01T14:32:00"` (ISO 8601, best-effort from EXIF DateTime) |
| `image.orientation` | `"1"` (EXIF orientation tag value, 1–8) |
| `image.gps_lat` | `"51.5074"` (decimal degrees, positive = N) |
| `image.gps_lon` | `"-0.1278"` (decimal degrees, positive = E) |

GPS and datetime are omitted if not present in the EXIF block. PNG and GIF do not carry EXIF and are unchanged.

---

### Stage 2 — PDF deep extraction

**MIME types:** `application/pdf`

**Library:** `github.com/pdfcpu/pdfcpu` (MIT, pure Go)

**Properties:**

| Key | Example value |
|---|---|
| `pdf.pages` | `"42"` |
| `pdf.title` | `"Annual Report 2024"` |
| `pdf.author` | `"Jane Smith"` |

`pdf.version` (the header-byte extraction) is retained unchanged. Title and author are omitted if not present in the PDF info dictionary. Malformed or encrypted PDFs produce no new properties (pdfcpu handles these gracefully).

---

### Stage 3 — Office OOXML deep extraction

**MIME types:** `.docx`, `.xlsx`, `.pptx`  
(`application/vnd.openxmlformats-officedocument.*`)

**Library:** stdlib `archive/zip` + `encoding/xml` (OOXML is a ZIP archive)

**Properties:**

| Key | Applies to | Example value |
|---|---|---|
| `office.title` | docx, xlsx, pptx | `"Q3 Budget"` |
| `office.author` | docx, xlsx, pptx | `"John Doe"` |
| `office.word_count` | docx only | `"3421"` |
| `office.slide_count` | pptx only | `"18"` |
| `office.sheet_count` | xlsx only | `"4"` |

`office.container` (`"ooxml"`) is retained from the existing extractor. Title and author come from `docProps/core.xml`; word count from `word/settings.xml` or `docProps/app.xml`; slide/sheet counts from `docProps/app.xml`.

---

### Stage 4 — Office OLE2 deep extraction

**MIME types:** `.doc`, `.xls`, `.ppt`  
(`application/msword`, `application/vnd.ms-excel`, `application/vnd.ms-powerpoint`)

**Library:** `github.com/richardlehane/mscfb` (MIT, pure Go OLE2/CFB parser)

**Properties:**

| Key | Example value |
|---|---|
| `office.title` | `"Project Plan"` |
| `office.author` | `"Alice Jones"` |

Title and author come from the OLE2 Summary Information stream (`\x05SummaryInformation`). `office.container` (`"ole2"`) is retained. OLE2 does not expose reliable word/slide/sheet counts without deep format parsing, so those are omitted for legacy formats.

---

### Stage 5 — Audio tags

**MIME types:** `audio/mpeg`, `audio/mp4`, `audio/x-m4a`, `audio/flac`, `audio/x-flac`, `audio/ogg`

**Library:** `github.com/dhowden/tag` (BSD, pure Go; handles ID3v1/v2, MP4, FLAC, Ogg Vorbis)

**Properties:**

| Key | Example value |
|---|---|
| `audio.title` | `"Bohemian Rhapsody"` |
| `audio.artist` | `"Queen"` |
| `audio.album` | `"A Night at the Opera"` |
| `audio.year` | `"1975"` |
| `audio.format` | `"ID3v2.4"` |

Each property is omitted if blank in the tag.

---

### Stage 6 — Video (MP4 / QuickTime)

**MIME types:** `video/mp4`, `video/quicktime`

**Library:** `github.com/abema/go-mp4` (MIT, pure Go MP4 box parser)

**Properties:**

| Key | Example value |
|---|---|
| `video.duration_s` | `"127"` (integer seconds, rounded) |
| `video.width` | `"1920"` |
| `video.height` | `"1080"` |

Duration is extracted from the `mvhd` box. Width/height come from the first `tkhd` box with non-zero dimensions. MKV, WebM, and AVI are not parsed (pure-Go support is immature); they receive no video properties.

---

### Stage 7 — Text files

**MIME types:** `text/plain`, `text/csv`, `text/html`, `text/css`, `text/javascript`, `application/json`, `application/xml`, `text/xml`

**Library:** stdlib only

**Properties:**

| Key | Example value |
|---|---|
| `text.lines` | `"512"` |
| `text.encoding` | `"utf-8"` |

Line count is a newline count over the full file. Encoding detection:
- UTF-8 BOM (`EF BB BF`) → `"utf-8-bom"`
- UTF-16 LE BOM (`FF FE`) → `"utf-16-le"`
- UTF-16 BE BOM (`FE FF`) → `"utf-16-be"`
- Valid UTF-8 (no BOM) → `"utf-8"`
- Otherwise → `"binary"` (properties skipped)

---

## File structure

| File | Change |
|---|---|
| `internal/meta/extractor.go` | Change `Extract` signature; add stages 1–7 as sub-functions |
| `internal/meta/extractor_test.go` | New tests for each stage; existing tests updated for new signature |
| `internal/api/documents.go` | Seek multipart.File to 0, pass `io.ReadSeeker` to Extract |
| `internal/web/handlers.go` | Same as above for web upload handler |
| `go.mod` / `go.sum` | Add five new dependencies |

No new tables, migrations, or API endpoints are required.

---

## Testing

Each stage has dedicated tests using real file fixtures stored in `internal/meta/testdata/`:

| Fixture | Tests |
|---|---|
| `test.jpg` (JPEG with EXIF) | make, model, datetime, orientation, GPS |
| `test_no_exif.jpg` (JPEG without EXIF) | no image.make etc. produced |
| `test.pdf` | pages, title, author |
| `test.docx` | title, author, word_count |
| `test.xlsx` | title, author, sheet_count |
| `test.pptx` | title, author, slide_count |
| `test.doc` | title, author (OLE2) |
| `test.mp3` | title, artist, album, year, format |
| `test.m4a` | same fields, MP4 container |
| `test.mp4` | duration_s, width, height |
| `test.txt` (UTF-8) | lines, encoding=utf-8 |
| `test_bom.txt` (UTF-8 BOM) | encoding=utf-8-bom |

Existing tests for image dimensions, PDF version, and office container type are updated to pass an `io.ReadSeeker` (via `bytes.NewReader`) instead of a `[]byte`.
