package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/auth"
	"tinydm/internal/config"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// RegisterRoutes mounts all API v1 routes onto r.
// All routes under /api/v1 (except /auth/login) require authentication.
func RegisterRoutes(r chi.Router, cfg *config.Config, repoStore *repo.Store, authStore *auth.Store, store storage.Store) {
	authHandler := NewAuthHandler(cfg, authStore)
	tenantHandler := NewTenantHandler(repoStore, authStore)
	projectHandler := NewProjectHandler(repoStore, authStore)
	bucketHandler := NewBucketHandler(repoStore, authStore)
	docHandler := NewDocumentHandler(repoStore, authStore, store)

	r.Route("/api/v1", func(r chi.Router) {

		// ── Public ────────────────────────────────────────────────────────────
		r.Post("/auth/login", authHandler.Login)

		// ── Authenticated ─────────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)

			// Auth
			r.Get("/auth/me", authHandler.Me)

			// Tenants — list & create are admin-only; read is open to any authed user
			r.Get("/tenants", tenantHandler.List)
			r.With(auth.RequireAdmin).Post("/tenants", tenantHandler.Create)

			r.Route("/tenants/{tenantID}", func(r chi.Router) {
				r.Use(TenantCtx(repoStore))

				r.Get("/", tenantHandler.Get)
				r.With(auth.RequireAdmin).Put("/", tenantHandler.Update)
				r.With(auth.RequireAdmin).Delete("/", tenantHandler.Delete)

				// Projects
				r.Get("/projects", projectHandler.List)
				r.With(auth.RequireAdmin).Post("/projects", projectHandler.Create)

				r.Route("/projects/{projectID}", func(r chi.Router) {
					r.Use(ProjectCtx(repoStore))

					r.Get("/", projectHandler.Get)
					r.With(auth.RequireAdmin).Put("/", projectHandler.Update)
					r.With(auth.RequireAdmin).Delete("/", projectHandler.Delete)

					// Buckets
					r.Get("/buckets", bucketHandler.List)
					r.With(auth.RequireAdmin).Post("/buckets", bucketHandler.Create)

					r.Route("/buckets/{bucketID}", func(r chi.Router) {
						r.Use(BucketCtx(repoStore))

						r.Get("/", bucketHandler.Get)
						r.With(auth.RequireAdmin).Put("/", bucketHandler.Update)
						r.With(auth.RequireAdmin).Delete("/", bucketHandler.Delete)

						// Documents
						r.Get("/documents", docHandler.List)
						r.Post("/documents", docHandler.Upload)

						r.Route("/documents/{documentID}", func(r chi.Router) {
							r.Use(DocumentCtx(repoStore))

							r.Get("/", docHandler.Get)
							r.Put("/", docHandler.Update)
							r.Delete("/", docHandler.Delete)
							r.Get("/content", docHandler.Download)
							r.Get("/versions", docHandler.ListVersions)
						})
					})
				})
			})
		})
	})

	// Health — no auth
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
}
