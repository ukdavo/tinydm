package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
)

// maxUploadBytes is the hard cap on file upload body size (512 MiB).
// ParseMultipartForm keeps 32 MiB in memory; the rest spills to temp files.
// This limit prevents unbounded disk exhaustion from a single request.
const maxUploadBytes = 512 << 20

// maxJSONBytes is the maximum size accepted for JSON request bodies (1 MiB).
// Prevents memory exhaustion from oversized payloads on non-upload endpoints.
const maxJSONBytes = 1 << 20

// ─── Security headers ─────────────────────────────────────────────────────────

// SecurityHeaders is middleware that adds defensive HTTP response headers to
// every response. These headers are a cheap, high-value defence-in-depth layer.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Prevent MIME-type sniffing by the browser.
		h.Set("X-Content-Type-Options", "nosniff")
		// Deny framing — protects against clickjacking.
		h.Set("X-Frame-Options", "DENY")
		// Minimal referrer information sent on cross-origin requests.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Restrict powerful browser features.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Content-Security-Policy — tight default for the admin UI and REST API.
		// HTMX is loaded from unpkg.com. The /api/docs handler overrides this
		// header with a looser policy that also allows cdn.jsdelivr.net, which
		// is required by Swagger UI.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' unpkg.com; "+ // HTMX via unpkg
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// ─── Filename sanitization ────────────────────────────────────────────────────

// sanitizeFilename strips path components and control characters from a
// client-supplied filename, returning a safe basename.
//
// Protections applied:
//   - filepath.Base strips directory traversal (e.g. "../../etc/passwd" → "passwd")
//   - Backslashes replaced with underscores (Windows path separators)
//   - Null bytes and other control characters replaced with underscores
//   - Length capped at 255 bytes (Linux filename limit)
//   - Reserved names (".", "..") replaced with "unnamed"
func sanitizeFilename(name string) string {
	// Normalise Windows-style separators before taking the base.
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)

	// Replace control characters and null bytes.
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)

	// Enforce a safe maximum length.
	if len(name) > 255 {
		name = name[:255]
	}

	// Disallow reserved names.
	if name == "" || name == "." || name == ".." {
		return "unnamed"
	}
	return name
}
