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
	tenantKey   ctxKey = "tenant"
	projectKey  ctxKey = "project"
	bucketKey   ctxKey = "bucket"
	documentKey ctxKey = "document"
	versionKey  ctxKey = "version"
	rightsKey   ctxKey = "rights"
	permModeKey ctxKey = "permMode"
)

// ─── Resource context middleware ──────────────────────────────────────────────
// Each middleware resolves a URL parameter to a DB record, validates
// parent ownership, and stores the record in the request context.
// A 404 is returned immediately if the resource does not exist.

// TenantCtx loads the tenant identified by {tenantID} into the context,
// and also stores its perm_mode for use by RightsCtx and CanMiddleware.
func TenantCtx(repoStore *repo.Store, authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "tenantID")
			tenant, err := repoStore.GetTenant(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if tenant == nil {
				writeError(w, http.StatusNotFound, "tenant not found")
				return
			}
			mode, err := authStore.GetTenantPermMode(r.Context(), id)
			if err != nil {
				mode = auth.PermModeExplicit // fail-safe
			}
			ctx := contextWith(r.Context(), tenantKey, tenant)
			ctx = contextWith(ctx, permModeKey, mode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RightsCtx loads the authenticated principal's rights into the context.
// It must run after RequireAuth and TenantCtx.
func RightsCtx(authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			// Admins bypass rights checks — no need to load rights.
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			tenant := tenantFromCtx(r)
			if tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			var rights []auth.Right
			var err error
			if p.AuthMethod == auth.AuthMethodAPIKey {
				rights, err = authStore.GetAPIKeyRights(r.Context(), tenant.ID, p.ID)
			} else {
				rights, err = authStore.GetUserRights(r.Context(), tenant.ID, p.ID)
			}
			if err != nil {
				// Fail-safe: no rights loaded means all Can() checks deny.
				rights = nil
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
	authStore *auth.Store,
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
			// Admins bypass all permission checks.
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}

			mode, _ := r.Context().Value(permModeKey).(auth.PermMode)
			if mode == "" {
				mode = auth.PermModeExplicit
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

// ProjectCtx loads the project identified by {projectID} and verifies it
// belongs to the tenant already in context.
func ProjectCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenant := tenantFromCtx(r)
			id := chi.URLParam(r, "projectID")
			project, err := store.GetProject(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if project == nil || project.TenantID != tenant.ID {
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

// ─── Context accessors ────────────────────────────────────────────────────────

func tenantFromCtx(r *http.Request) *repo.Tenant {
	t, _ := r.Context().Value(tenantKey).(*repo.Tenant)
	return t
}

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

func rightsFromCtx(r *http.Request) []auth.Right {
	v, _ := r.Context().Value(rightsKey).([]auth.Right)
	return v
}

func permModeFromCtx(r *http.Request) auth.PermMode {
	v, _ := r.Context().Value(permModeKey).(auth.PermMode)
	if v == "" {
		return auth.PermModeExplicit
	}
	return v
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
