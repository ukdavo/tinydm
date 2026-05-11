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

	"github.com/abema/go-mp4"
	"github.com/dhowden/tag"
	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/richardlehane/mscfb"
	"github.com/rwcarlsen/goexif/exif"
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

	conf := pdfmodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfmodel.ValidationRelaxed
	ctx, err := pdfapi.ReadContext(r, conf)
	if err != nil {
		return
	}
	_ = pdfapi.ValidateContext(ctx)
	if ctx.PageCount > 0 {
		props["pdf.pages"] = strconv.Itoa(ctx.PageCount)
	}
	if ctx.Title != "" {
		props["pdf.title"] = ctx.Title
	}
	if ctx.Author != "" {
		props["pdf.author"] = ctx.Author
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

	for _, f := range zr.File {
		if f.Name != "docProps/core.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			break
		}
		type coreProps struct {
			Title   string `xml:"title"`
			Creator string `xml:"creator"`
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

	for _, f := range zr.File {
		if f.Name != "docProps/app.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			break
		}
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
				if n, err := strconv.Atoi(v); err == nil {
					counts[cur] = n
				}
			}
		}
		rc.Close()
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

	data, err := io.ReadAll(r)
	if err != nil {
		return
	}
	doc, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return
	}
	for entry, err := doc.Next(); err == nil; entry, err = doc.Next() {
		if entry.Name != "\x05SummaryInformation" {
			continue
		}
		data, err := io.ReadAll(entry)
		if err != nil {
			break
		}
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

func parseOLE2SummaryInfo(data []byte) (title, author string) {
	if len(data) < 52 {
		return
	}
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
	var durationUnits uint64
	var timeScale uint32
	var width, height uint16

	_, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
		switch h.BoxInfo.Type.String() {
		case "mvhd":
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, nil
			}
			if b, ok := box.(*mp4.Mvhd); ok {
				timeScale = b.Timescale
				durationUnits = b.GetDuration()
			}
		case "tkhd":
			if width == 0 {
				box, _, err := h.ReadPayload()
				if err != nil {
					return nil, nil
				}
				if b, ok := box.(*mp4.Tkhd); ok {
					w := b.GetWidthInt()
					he := b.GetHeightInt()
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
		props["video.duration_s"] = strconv.Itoa(int(durationUnits / uint64(timeScale)))
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
	var enc string
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
		return
	}
	props["text.encoding"] = enc
	props["text.lines"] = strconv.Itoa(bytes.Count(data, []byte{'\n'}))
}
