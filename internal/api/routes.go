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
//	admin — full control (projects, buckets, users, audit)
//	user  — manage documents, tags, and properties within their rights
func RegisterRoutes(r chi.Router, cfg *config.Config, repoStore *repo.Store, authStore *auth.Store, store storage.Store, auditStore *audit.Store, locker cluster.Locker) {
	mode := auth.PermMode(cfg.PermMode)

	authHandler := NewAuthHandler(cfg, authStore)
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

			// Audit log — admin only
			r.With(auth.RequireAdmin).Get("/audit", auditHandler.List)

			// Users — admin only
			r.With(auth.RequireAdmin).Get("/users", userHandler.ListUsers)
			r.With(auth.RequireAdmin).Patch("/users/{userID}/password", userHandler.ChangePassword)

			// API keys — admin only
			r.With(auth.RequireAdmin).Get("/apikeys", userHandler.ListAPIKeys)
			r.With(auth.RequireAdmin).Post("/apikeys", userHandler.CreateAPIKey)
			r.With(auth.RequireAdmin).Post("/apikeys/{keyID}/revoke", userHandler.RevokeAPIKey)

			// Projects, buckets, and documents — RBAC-gated via RightsCtx
			r.Group(func(r chi.Router) {
				r.Use(RightsCtx(authStore))

				// Projects
				r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceProject,
					nil, nil)).Get("/projects", projectHandler.List)
				r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceProject,
					nil, nil)).Post("/projects", projectHandler.Create)

			r.Route("/projects/{projectID}", func(r chi.Router) {
				r.Use(ProjectCtx(repoStore))

				r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Get("/", projectHandler.Get)
				r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Put("/", projectHandler.Update)
				r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Delete("/", projectHandler.Delete)

				// Buckets
				r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Get("/buckets", bucketHandler.List)
				r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Post("/buckets", bucketHandler.Create)

				r.Route("/buckets/{bucketID}", func(r chi.Router) {
					r.Use(BucketCtx(repoStore))

					r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Get("/", bucketHandler.Get)
					r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Put("/", bucketHandler.Update)
					r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Delete("/", bucketHandler.Delete)

					// Documents
					r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Get("/documents", docHandler.List)
					r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Post("/documents", docHandler.Upload)

					r.Route("/documents/{documentID}", func(r chi.Router) {
						r.Use(DocumentCtx(repoStore))

						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/", docHandler.Get)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/", docHandler.Update)
						r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/", docHandler.Delete)
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/content", docHandler.Download)

						// Versions
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/versions", docHandler.ListVersions)
						r.Route("/versions/{versionID}", func(r chi.Router) {
							r.Use(VersionCtx(repoStore))
							r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Post("/restore", docHandler.RestoreVersion)
						})

						// Tags and properties
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/tags", tagHandler.List)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/tags", tagHandler.Replace)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Post("/tags/{tag}", tagHandler.Add)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/tags/{tag}", tagHandler.Remove)

						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/properties", propHandler.List)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties", propHandler.Replace)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties/{key}", propHandler.Set)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/properties/{key}", propHandler.Delete)
					})
				})
			}) // end r.Route("/projects/{projectID}")
			}) // end r.Group (RightsCtx)
		})
	})

	// Health — no auth
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// API docs
	r.Get("/api/docs", serveSwaggerUI)
	r.Get("/api/docs/openapi.yaml", serveOpenAPISpec)
}
