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
				r.Use(TenantCtx(repoStore, authStore))
				r.Use(RequireSameTenant) // enforce tenant isolation; superadmin exempt
				r.Use(RightsCtx(authStore))

				r.Get("/", tenantHandler.Get)
				r.With(auth.RequireSuperAdmin).Put("/", tenantHandler.Update)
				r.With(auth.RequireSuperAdmin).Delete("/", tenantHandler.Delete)

				// Audit log — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/audit", auditHandler.List)

				// Users — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/users", userHandler.ListUsers)
				r.With(auth.RequireAdmin).Patch("/users/{userID}/password", userHandler.ChangePassword)

				// API keys — domain admin or superadmin
				r.With(auth.RequireAdmin).Get("/apikeys", userHandler.ListAPIKeys)
				r.With(auth.RequireAdmin).Post("/apikeys", userHandler.CreateAPIKey)
				r.With(auth.RequireAdmin).Post("/apikeys/{keyID}/revoke", userHandler.RevokeAPIKey)

				// Projects
				r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceProject,
					nil, nil)).Get("/projects", projectHandler.List)
				r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceProject,
					nil, nil)).Post("/projects", projectHandler.Create)

				r.Route("/projects/{projectID}", func(r chi.Router) {
					r.Use(ProjectCtx(repoStore))

					r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceProject,
						func(r *http.Request) string { return chi.URLParam(r, "projectID") },
						nil)).Get("/", projectHandler.Get)
					r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceProject,
						func(r *http.Request) string { return chi.URLParam(r, "projectID") },
						nil)).Put("/", projectHandler.Update)
					r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceProject,
						func(r *http.Request) string { return chi.URLParam(r, "projectID") },
						nil)).Delete("/", projectHandler.Delete)

					// Buckets
					r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceBucket,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Get("/buckets", bucketHandler.List)
					r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceBucket,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Post("/buckets", bucketHandler.Create)

					r.Route("/buckets/{bucketID}", func(r chi.Router) {
						r.Use(BucketCtx(repoStore))

						r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceBucket,
							func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
							})).Get("/", bucketHandler.Get)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceBucket,
							func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
							})).Put("/", bucketHandler.Update)
						r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceBucket,
							func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
							})).Delete("/", bucketHandler.Delete)

						// Documents
						r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
							nil,
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/documents", docHandler.List)
						r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceDocument,
							nil,
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Post("/documents", docHandler.Upload)

						r.Route("/documents/{documentID}", func(r chi.Router) {
							r.Use(DocumentCtx(repoStore))

							r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Get("/", docHandler.Get)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Put("/", docHandler.Update)
							r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Delete("/", docHandler.Delete)
							r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Get("/content", docHandler.Download)

							// Versions
							r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Get("/versions", docHandler.ListVersions)
							r.Route("/versions/{versionID}", func(r chi.Router) {
								r.Use(VersionCtx(repoStore))
								r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
									func(r *http.Request) string { return chi.URLParam(r, "documentID") },
									func(r *http.Request) []auth.ResourceAncestor {
										return []auth.ResourceAncestor{
											{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
											{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
										}
									})).Post("/restore", docHandler.RestoreVersion)
							})

							// Tags and properties — require document read at minimum
							r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Get("/tags", tagHandler.List)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Put("/tags", tagHandler.Replace)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Post("/tags/{tag}", tagHandler.Add)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Delete("/tags/{tag}", tagHandler.Remove)

							r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Get("/properties", propHandler.List)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Put("/properties", propHandler.Replace)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Put("/properties/{key}", propHandler.Set)
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Delete("/properties/{key}", propHandler.Delete)
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
