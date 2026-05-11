// Package web provides the HTMX-based admin web UI for TinyDM.
package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/config"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

const sessionCookie = "tdm_session"

// Handler holds the dependencies and pre-parsed templates for the admin UI.
type Handler struct {
	cfg     *config.Config
	repo    *repo.Store
	auth    *auth.Store
	audit   *audit.Store
	storage storage.Store
	tmpls   map[string]*template.Template
}

// New creates a Handler and pre-parses all page templates.
func New(
	cfg *config.Config,
	repoStore *repo.Store,
	authStore *auth.Store,
	auditStore *audit.Store,
	store storage.Store,
) *Handler {
	h := &Handler{
		cfg:     cfg,
		repo:    repoStore,
		auth:    authStore,
		audit:   auditStore,
		storage: store,
		tmpls:   make(map[string]*template.Template),
	}
	h.parseTemplates()
	return h
}

// parseTemplates pre-parses the base layout + each page template.
func (h *Handler) parseTemplates() {
	funcs := template.FuncMap{
		// string converts any value to its string representation.
		"string": func(v interface{}) string {
			return fmt.Sprintf("%v", v)
		},
		// slice returns s[a:b], clamping b to len(s).
		"slice": func(s string, a, b int) string {
			if b > len(s) {
				b = len(s)
			}
			if a > b {
				return ""
			}
			return s[a:b]
		},
		// "not" is intentionally omitted here — the built-in template "not"
		// uses reflection and correctly handles nil/empty slices, maps, bools,
		// and zero-value ints, so there is no need to override it.
	}

	base := template.Must(
		template.New("").Funcs(funcs).ParseFS(templateFS, "templates/base.html"),
	)

	pages := []string{
		"dashboard", "projects", "buckets",
		"documents", "docdetail", "users", "apikeys", "audit",
	}
	for _, page := range pages {
		t := template.Must(base.Clone())
		t = template.Must(t.ParseFS(templateFS, "templates/"+page+".html"))
		h.tmpls[page] = t
	}

	// Login is standalone (no base layout).
	h.tmpls["login"] = template.Must(
		template.New("").Funcs(funcs).ParseFS(templateFS, "templates/login.html"),
	)
}

// RegisterRoutes mounts all app UI routes onto the router.
func RegisterRoutes(r chi.Router, h *Handler) {
	// Serve embedded static assets.
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/app/static/*", http.StripPrefix("/app/static/", http.FileServer(http.FS(staticSub))))

	// Login / logout (no session required).
	r.Get("/app/login", h.loginPage)
	r.Post("/app/login", h.loginSubmit)
	r.Get("/app/logout", h.logout)

	// All other app routes require a valid session.
	r.Group(func(r chi.Router) {
		r.Use(h.requireSession)

		// Dashboard — all logged-in users.
		r.Get("/app/", h.dashboard)
		r.Get("/app", h.dashboard)

		// Projects list — all logged-in users.
		r.Get("/app/projects", h.projects)

		// Buckets list — all logged-in users.
		r.Get("/app/projects/{projectID}/buckets", h.buckets)

		// Documents — all logged-in users (full CRUD).
		r.Get("/app/projects/{projectID}/buckets/{bucketID}/documents", h.documents)
		r.Post("/app/projects/{projectID}/buckets/{bucketID}/documents", h.uploadDocument)
		r.Get("/app/projects/{projectID}/buckets/{bucketID}/documents/rows", h.documentRows)

		// Document detail / edit — all logged-in users.
		r.Get("/app/documents/{documentID}", h.documentDetail)
		r.Put("/app/documents/{documentID}", h.updateDocument)
		r.Get("/app/documents/{documentID}/edit", h.editDocumentForm)
		r.Get("/app/documents/{documentID}/row", h.documentRowPartial)
		r.Delete("/app/documents/{documentID}", h.deleteDocument)
		r.Get("/app/documents/{documentID}/download", h.downloadDocument)

		// Document tags — all logged-in users.
		r.Post("/app/documents/{documentID}/tags", h.addDocumentTag)
		r.Delete("/app/documents/{documentID}/tags/{tag}", h.removeDocumentTag)

		// Document properties — all logged-in users.
		r.Post("/app/documents/{documentID}/properties", h.setDocumentPropertyWeb)
		r.Delete("/app/documents/{documentID}/properties/{key}", h.deleteDocumentPropertyWeb)

		// Document version restore — all logged-in users.
		r.Post("/app/documents/{documentID}/versions/{versionID}/restore", h.restoreDocumentVersionWeb)

		// ── Admin-only routes ──────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(h.requireAdmin)

			// Projects — create / delete.
			r.Post("/app/projects", h.createProject)
			r.Delete("/app/projects/{projectID}", h.deleteProject)

			// Buckets — create / edit / delete.
			r.Post("/app/projects/{projectID}/buckets", h.createBucket)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/edit", h.editBucketForm)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/row", h.bucketRowPartial)
			r.Put("/app/projects/{projectID}/buckets/{bucketID}", h.updateBucket)
			r.Delete("/app/projects/{projectID}/buckets/{bucketID}", h.deleteBucket)

			// Users.
			r.Get("/app/users", h.users)
			r.Post("/app/users", h.createUser)
			r.Post("/app/users/{userID}/activate", h.activateUser)
			r.Post("/app/users/{userID}/deactivate", h.deactivateUser)
			r.Get("/app/users/{userID}/row", h.userRow)
			r.Get("/app/users/{userID}/password-form", h.passwordForm)
			r.Post("/app/users/{userID}/password", h.changeUserPassword)
			r.Delete("/app/users/{userID}", h.deleteUser)

			// API keys.
			r.Get("/app/apikeys", h.apiKeys)
			r.Post("/app/apikeys", h.createAPIKey)
			r.Post("/app/apikeys/{keyID}/revoke", h.revokeAPIKey)

			// Rights management — by principal.
			r.Get("/app/users/{userID}/rights", h.userRightsPanel)
			r.Post("/app/users/{userID}/rights", h.addUserRight)
			r.Delete("/app/users/{userID}/rights", h.removeUserRight)
			r.Get("/app/apikeys/{keyID}/rights", h.apiKeyRightsPanel)
			r.Post("/app/apikeys/{keyID}/rights", h.addAPIKeyRight)
			r.Delete("/app/apikeys/{keyID}/rights", h.removeAPIKeyRight)

			// Rights management — by resource.
			r.Get("/app/projects/{projectID}/rights", h.projectRightsPanel)
			r.Post("/app/projects/{projectID}/rights", h.addProjectRight)
			r.Delete("/app/projects/{projectID}/rights", h.removeProjectRight)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/rights", h.bucketRightsPanel)
			r.Post("/app/projects/{projectID}/buckets/{bucketID}/rights", h.addBucketRight)
			r.Delete("/app/projects/{projectID}/buckets/{bucketID}/rights", h.removeBucketRight)
			r.Get("/app/documents/{documentID}/rights", h.documentRightsPanel)
			r.Post("/app/documents/{documentID}/rights", h.addDocumentRight)
			r.Delete("/app/documents/{documentID}/rights", h.removeDocumentRight)

			// Audit log.
			r.Get("/app/audit", h.auditLog)
			r.Get("/app/audit/events", h.auditEvents)
		})
	})

	// Redirect / → /app/
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
}

// ── Session middleware ─────────────────────────────────────────────────────────

func (h *Handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/app/login", http.StatusFound)
			return
		}
		claims, err := auth.ParseJWT(h.cfg.JWTSecret, cookie.Value)
		if err != nil {
			http.SetCookie(w, clearCookie())
			http.Redirect(w, r, "/app/login", http.StatusFound)
			return
		}
		user, err := h.auth.GetUserByID(r.Context(), claims.Subject)
		if err != nil || user == nil || !user.IsActive {
			http.SetCookie(w, clearCookie())
			http.Redirect(w, r, "/app/login", http.StatusFound)
			return
		}
		ctx := auth.WithPrincipal(r.Context(), auth.Principal{
			ID:       user.ID,
			Username: user.Username,
			UserType: user.UserType,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		if p.UserType != auth.UserTypeAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Page data base type ───────────────────────────────────────────────────────

// basePage contains fields present in every full-page render.
type basePage struct {
	Page      string
	Principal auth.Principal
	Flash     string
	Error     string
}

// ── Template rendering helpers ────────────────────────────────────────────────

// render executes a full-page template with data.
func (h *Handler) render(w http.ResponseWriter, page string, data interface{}) {
	t, ok := h.tmpls[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	name := "base"
	if page == "login" {
		name = "login"
	}
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("template render error", "page", page, "error", err)
	}
}

// renderPartial executes a named sub-template (e.g. a table-row partial).
func (h *Handler) renderPartial(w http.ResponseWriter, pageTmpl, tmplName string, data interface{}) {
	t, ok := h.tmpls[pageTmpl]
	if !ok {
		http.Error(w, "template not found: "+pageTmpl, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, tmplName, data); err != nil {
		slog.Error("partial render error", "tmpl", tmplName, "error", err)
	}
}

// base returns a basePage populated from the request context.
func (h *Handler) base(r *http.Request, page string) basePage {
	p, _ := auth.PrincipalFromContext(r.Context())
	return basePage{Page: page, Principal: p}
}

// clearCookie returns a Set-Cookie that immediately expires the session cookie.
func clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	}
}
