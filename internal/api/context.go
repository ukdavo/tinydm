package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

type ctxKey string

const (
	projectKey  ctxKey = "project"
	bucketKey   ctxKey = "bucket"
	documentKey ctxKey = "document"
	versionKey  ctxKey = "version"
	rightsKey   ctxKey = "rights"
)

// ─── Resource context middleware ──────────────────────────────────────────────
// Each middleware resolves a URL parameter to a DB record, validates
// parent ownership, and stores the record in the request context.
// A 404 is returned immediately if the resource does not exist.

// RightsCtx loads the authenticated principal's rights into the context.
// It must run after RequireAuth.
func RightsCtx(authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok || p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			var rights []auth.Right
			if p.AuthMethod == auth.AuthMethodAPIKey {
				rights, _ = authStore.GetAPIKeyRights(r.Context(), p.ID)
			} else {
				rights, _ = authStore.GetUserRights(r.Context(), p.ID)
			}
			ctx := contextWith(r.Context(), rightsKey, rights)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CanMiddleware returns middleware that checks whether the authenticated
// principal may perform action on the given resource. getResourceID and
// getAncestors are called at request time to extract the resource ID and
// ancestor chain from URL params and context.
func CanMiddleware(
	mode auth.PermMode,
	action auth.Action,
	resourceType auth.ResourceType,
	getResourceID func(*http.Request) string,
	getAncestors func(*http.Request) []auth.ResourceAncestor,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			rights, _ := r.Context().Value(rightsKey).([]auth.Right)
			resourceID := "*"
			if getResourceID != nil {
				resourceID = getResourceID(r)
			}
			var ancestors []auth.ResourceAncestor
			if getAncestors != nil {
				ancestors = getAncestors(r)
			}
			if !auth.Can(p, rights, mode, action, resourceType, resourceID, ancestors...) {
				writeError(w, http.StatusForbidden, "permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ProjectCtx loads the project identified by {projectID} into the context.
func ProjectCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "projectID")
			project, err := store.GetProject(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if project == nil {
				writeError(w, http.StatusNotFound, "project not found")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWith(r.Context(), projectKey, project)))
		})
	}
}

// BucketCtx loads the bucket identified by {bucketID} and verifies it
// belongs to the project already in context.
func BucketCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			project := projectFromCtx(r)
			id := chi.URLParam(r, "bucketID")
			bucket, err := store.GetBucket(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if bucket == nil || bucket.ProjectID != project.ID {
				writeError(w, http.StatusNotFound, "bucket not found")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWith(r.Context(), bucketKey, bucket)))
		})
	}
}

// DocumentCtx loads the document identified by {documentID} and verifies it
// belongs to the bucket already in context.
func DocumentCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bucket := bucketFromCtx(r)
			id := chi.URLParam(r, "documentID")
			doc, err := store.GetDocument(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if doc == nil || doc.BucketID != bucket.ID {
				writeError(w, http.StatusNotFound, "document not found")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWith(r.Context(), documentKey, doc)))
		})
	}
}

// VersionCtx loads the document version identified by {versionID} and verifies
// it belongs to the document already in context.
func VersionCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			doc := documentFromCtx(r)
			id := chi.URLParam(r, "versionID")
			v, err := store.GetDocumentVersion(r.Context(), id, doc.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if v == nil {
				writeError(w, http.StatusNotFound, "version not found")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWith(r.Context(), versionKey, v)))
		})
	}
}

// ─── Context accessors ────────────────────────────────────────────────────────

func projectFromCtx(r *http.Request) *repo.Project {
	p, _ := r.Context().Value(projectKey).(*repo.Project)
	return p
}

func bucketFromCtx(r *http.Request) *repo.Bucket {
	b, _ := r.Context().Value(bucketKey).(*repo.Bucket)
	return b
}

func documentFromCtx(r *http.Request) *repo.Document {
	d, _ := r.Context().Value(documentKey).(*repo.Document)
	return d
}

func versionFromCtx(r *http.Request) *repo.DocumentVersion {
	v, _ := r.Context().Value(versionKey).(*repo.DocumentVersion)
	return v
}

func contextWith(ctx context.Context, key ctxKey, val any) context.Context {
	return context.WithValue(ctx, key, val)
}
