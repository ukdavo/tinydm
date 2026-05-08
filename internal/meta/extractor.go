// Package meta extracts metadata from document content during upload.
// It uses only the Go standard library. Properties are stored under
// namespaced keys (e.g. "image.width") and later available via the
// document properties API.
package meta

import (
	"bytes"
	"image"
	// Register standard decoders.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"
)

// Extract inspects up to the first 64 KiB of content (passed as a byte slice)
// and returns a map of extracted properties. The map may be empty but is never nil.
//
// contentType is the detected MIME type of the file (e.g. "image/jpeg").
// header is a leading slice of the file content (the caller reads ahead before
// seeking back to store the full file).
func Extract(contentType string, header []byte) map[string]string {
	props := make(map[string]string)

	switch {
	case strings.HasPrefix(contentType, "image/"):
		extractImage(header, props)
	case contentType == "application/pdf":
		extractPDF(header, props)
	case isOfficeFormat(contentType):
		extractOffice(header, props)
	}

	return props
}

// ─── Image ────────────────────────────────────────────────────────────────────

func extractImage(data []byte, props map[string]string) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Not a recognised image or truncated header — skip silently.
		return
	}
	props["image.width"] = strconv.Itoa(cfg.Width)
	props["image.height"] = strconv.Itoa(cfg.Height)
	props["image.format"] = format
}

// ─── PDF ──────────────────────────────────────────────────────────────────────

// extractPDF reads the PDF version string from the header ("%PDF-1.x").
// Full page count extraction requires a proper PDF parser and is left for a
// future iteration once an appropriate library is vetted.
func extractPDF(data []byte, props map[string]string) {
	// "%PDF-" signature is at offset 0 for well-formed PDFs.
	if len(data) < 8 {
		return
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return
	}
	// Extract version token: "%PDF-1.7\n" → "1.7"
	rest := data[5:]
	end := bytes.IndexAny(rest, "\r\n \x00")
	if end < 0 {
		end = len(rest)
	}
	if end > 0 {
		props["pdf.version"] = string(rest[:end])
	}
}

// ─── Office formats ───────────────────────────────────────────────────────────

func isOfficeFormat(ct string) bool {
	switch ct {
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document",   // .docx
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",          // .xlsx
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",  // .pptx
		"application/msword",       // .doc
		"application/vnd.ms-excel", // .xls
		"application/vnd.ms-powerpoint": // .ppt
		return true
	}
	return false
}

// extractOffice detects whether the file uses the OOXML (ZIP-based) or legacy
// OLE2 compound document format and records this as a property.
// Full content extraction (author, title, page count) requires additional
// libraries and is deferred to a later phase.
func extractOffice(data []byte, props map[string]string) {
	if len(data) < 4 {
		return
	}
	// OOXML files are ZIP archives — PK signature.
	if data[0] == 0x50 && data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
		props["office.container"] = "ooxml"
		return
	}
	// Legacy OLE2 compound document — magic D0 CF 11 E0.
	if data[0] == 0xD0 && data[1] == 0xCF && data[2] == 0x11 && data[3] == 0xE0 {
		props["office.container"] = "ole2"
		return
	}
}
