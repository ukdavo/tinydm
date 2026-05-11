package audit

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"tinydm/internal/auth"
)

// Middleware returns a Chi middleware that appends an audit event after every
// successful mutating request (POST, PUT, DELETE, PATCH).
//
// Events are written in a background goroutine so that a slow or failing
// database write never delays the HTTP response.
func Middleware(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap the ResponseWriter so we can inspect the status code after
			// the handler returns.
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			// Only record mutating verbs.
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return
			}

			// Only record successful responses (2xx).
			status := ww.Status()
			if status < 200 || status >= 300 {
				return
			}

			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				return
			}

			action := deriveAction(r.Method, r)
			resource := deriveResource(r)

			// Fire and forget — a failed audit write must not surface to the caller.
			go func() {
				if err := store.Record(context.Background(),
					p.Username, action, resource, ""); err != nil {
					slog.Warn("audit: failed to record event",
						"action", action,
						"resource", resource,
						"error", err,
					)
				}
			}()
		})
	}
}

// ─── Action derivation ────────────────────────────────────────────────────────

// deriveAction maps an HTTP method and the matched Chi route pattern to a
// dot-namespaced action string, e.g. "document.create" or "document.tag.add".
func deriveAction(method string, r *http.Request) string {
	pattern := ""
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		pattern = rctx.RoutePattern()
	}

	// Handle suffix-keyed special cases first.
	switch {
	case strings.HasSuffix(pattern, "/restore"):
		return "document.version.restore"
	case strings.HasSuffix(pattern, "/login"):
		return "auth.login"
	}

	// Walk the pattern segments from the right, skipping URL parameter tokens.
	// The first concrete segment we hit tells us the resource type; whether
	// a variable segment follows it tells us if this is a collection or item op.
	parts := strings.Split(pattern, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := parts[i]
		if seg == "" || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			continue
		}
		switch seg {
		case "tags":
			// Collection op (PUT /tags — no trailing {tag})?
			if i == len(parts)-1 {
				return "document.tag.replace"
			}
			if method == http.MethodPost {
				return "document.tag.add"
			}
			return "document.tag.remove"
		case "properties":
			// Collection op (PUT /properties — no trailing {key})?
			if i == len(parts)-1 {
				return "document.property.replace"
			}
			if method == http.MethodPut {
				return "document.property.set"
			}
			return "document.property.delete"
		default:
			return singularize(seg) + "." + methodVerb(method)
		}
	}
	return strings.ToLower(method)
}

// deriveResource returns the most specific resource identifier present in the
// URL parameters, walking from most-specific to least-specific.
func deriveResource(r *http.Request) string {
	for _, param := range []string{
		"key", "tag", "versionID", "documentID",
		"bucketID", "projectID", "tenantID",
	} {
		if id := chi.URLParam(r, param); id != "" {
			return id
		}
	}
	return ""
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func methodVerb(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodDelete:
		return "delete"
	case http.MethodPatch:
		return "patch"
	default:
		return strings.ToLower(method)
	}
}

func singularize(s string) string {
	switch s {
	case "tenants":
		return "tenant"
	case "projects":
		return "project"
	case "buckets":
		return "bucket"
	case "documents":
		return "document"
	case "versions":
		return "version"
	case "tags":
		return "tag"
	case "properties":
		return "property"
	default:
		return s
	}
}
