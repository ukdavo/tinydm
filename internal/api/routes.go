package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/cluster"
	"tinydm/internal/config"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// RegisterRoutes mounts all API v1 routes onto r.
// All routes under /api/v1 (except /auth/login) require authentication.
//
// Role matrix:
//
//	superadmin  — create/update/delete tenants; all admin operations; cross-tenant access
//	admin       — full control within their tenant (projects, buckets, users, audit)
//	user        — manage documents, tags, and properties within their tenant
func RegisterRoutes(r chi.Router, cfg *config.Config, repoStore *repo.Store, authStore *auth.Store, store storage.Store, auditStore *audit.Store, locker cluster.Locker) {
	authHandler := NewAuthHandler(cfg, authStore)
	tenantHandler := NewTenantHandler(repoStore, authStore)
	projectHandler := NewProjectHandler(repoStore, authStore)
	bucketHandler := NewBucketHandler(repoStore, authStore)
	docHandler := NewDocumentHandler(repoStore, authStore, store, locker)
	tagHandler := NewTagHandler(repoStore, locker)
	propHandler := NewPropertyHandler(repoStore, locker)
	auditHandler := NewAuditHandler(auditStore)
	userHandler := NewUserHandler(authStore)

	r.Route("/api/v1", func(r chi.Router) {

		// Public
		r.Post("/auth/login", authHandler.Login)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(audit.Middleware(auditStore))

			// Auth
			r.Get("/auth/me", authHandler.Me)

			// Tenants
			// List: any authenticated user (superadmin sees all, others see their own).
			// Create: superadmin only — provisioning a domain is a global operation.
			// Update/Delete: superadmin only (under /tenants/{tenantID}).
			r.Get("/tenants", tenantHandler.List)
			r.With(auth.RequireSuperAdmin).Post("/tenants", tenantHandler.Create)

			r.Route("/tenants/{tenantID}", func(r chi.Router) {
				r.Use(TenantCtx(repoStore))
				r.Use(RequireSameTenant) // enforce tenant isolation; superadmin exempt

				r.Get("/", tenantHandler.Get)
				r.With(auth.RequireSuperAdmin).Put("/", tenantHandler.Update)
				r.With(auth.RequireSuperAdmin).Delete("/", tenantHandler.Delete)

				// Audit log — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/audit", auditHandler.List)

				// Users — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/users", userHandler.ListUsers)

				// API keys — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/apikeys", userHandler.ListAPIKeys)
				r.With(auth.RequireAdmin).Post("/apikeys", userHandler.CreateAPIKey)
				r.With(auth.RequireAdmin).Post("/apikeys/{keyID}/revoke", userHandler.RevokeAPIKey)

				// Projects — list/read: any user; create/update/delete: admin+
				r.Get("/projects", projectHandler.List)
				r.With(auth.RequireAdmin).Post("/projects", projectHandler.Create)

				r.Route("/projects/{projectID}", func(r chi.Router) {
					r.Use(ProjectCtx(repoStore))

					r.Get("/", projectHandler.Get)
					r.With(auth.RequireAdmin).Put("/", projectHandler.Update)
					r.With(auth.RequireAdmin).Delete("/", projectHandler.Delete)

					// Buckets — list/read: any user; create/update/delete: admin+
					r.Get("/buckets", bucketHandler.List)
					r.With(auth.RequireAdmin).Post("/buckets", bucketHandler.Create)

					r.Route("/buckets/{bucketID}", func(r chi.Router) {
						r.Use(BucketCtx(repoStore))

						r.Get("/", bucketHandler.Get)
						r.With(auth.RequireAdmin).Put("/", bucketHandler.Update)
						r.With(auth.RequireAdmin).Delete("/", bucketHandler.Delete)

						// Documents — all authenticated users (role=user and above)
						r.Get("/documents", docHandler.List)
						r.Post("/documents", docHandler.Upload)

						r.Route("/documents/{documentID}", func(r chi.Router) {
							r.Use(DocumentCtx(repoStore))

							r.Get("/", docHandler.Get)
							r.Put("/", docHandler.Update)
							r.Delete("/", docHandler.Delete)
							r.Get("/content", docHandler.Download)

							// Versions
							r.Get("/versions", docHandler.ListVersions)
							r.Route("/versions/{versionID}", func(r chi.Router) {
								r.Use(VersionCtx(repoStore))
								r.Post("/restore", docHandler.RestoreVersion)
							})

							// Tags — all authenticated users
							r.Get("/tags", tagHandler.List)
							r.Put("/tags", tagHandler.Replace)
							r.Post("/tags/{tag}", tagHandler.Add)
							r.Delete("/tags/{tag}", tagHandler.Remove)

							// Custom properties — all authenticated users
							r.Get("/properties", propHandler.List)
							r.Put("/properties", propHandler.Replace)
							r.Put("/properties/{key}", propHandler.Set)
							r.Delete("/properties/{key}", propHandler.Delete)
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

	// API docs — no auth; Swagger UI at /api/docs, raw spec at /api/docs/openapi.yaml
	r.Get("/api/docs", serveSwaggerUI)
	r.Get("/api/docs/openapi.yaml", serveOpenAPISpec)
}
