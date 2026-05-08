package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

// ── Login / Logout ─────────────────────────────────────────────────────────────

type loginData struct {
	Error             string
	TenantName        string
	Username          string
	DefaultTenantName string // shown as a hint on the login page
}

// defaultTenantName returns the display name of the first tenant in the DB,
// falling back to the bootstrap tenant name from config. Used to pre-populate
// the login hint so users know what to type.
func (h *Handler) defaultTenantName(ctx context.Context) string {
	tenants, err := h.repo.ListTenants(ctx)
	if err == nil && len(tenants) > 0 {
		return tenants[0].Name
	}
	return h.cfg.BootstrapTenantName
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", loginData{
		DefaultTenantName: h.defaultTenantName(r.Context()),
	})
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	tenantName := r.FormValue("tenant_name")
	username := r.FormValue("username")
	password := r.FormValue("password")

	defaultName := h.defaultTenantName(r.Context())

	if tenantName == "" || username == "" || password == "" {
		h.render(w, "login", loginData{
			Error:             "All fields are required.",
			TenantName:        tenantName,
			Username:          username,
			DefaultTenantName: defaultName,
		})
		return
	}

	// Resolve the public tenant name to an internal ID.
	tenant, err := h.repo.GetTenantByName(r.Context(), tenantName)
	if err != nil || tenant == nil {
		h.render(w, "login", loginData{
			Error:             "Invalid credentials.",
			TenantName:        tenantName,
			Username:          username,
			DefaultTenantName: defaultName,
		})
		return
	}

	user, err := h.auth.GetUserByUsername(r.Context(), tenant.ID, username)
	if err != nil || user == nil || !user.IsActive {
		h.render(w, "login", loginData{
			Error:             "Invalid credentials.",
			TenantName:        tenantName,
			Username:          username,
			DefaultTenantName: defaultName,
		})
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		h.render(w, "login", loginData{
			Error:             "Invalid credentials.",
			TenantName:        tenantName,
			Username:          username,
			DefaultTenantName: defaultName,
		})
		return
	}

	token, err := auth.NewJWT(
		h.cfg.JWTSecret,
		h.cfg.JWTExpiryMinutes,
		user.ID,
		user.TenantID,
		user.Username,
		user.UserType,
	)
	if err != nil {
		h.render(w, "login", loginData{Error: "Could not create session."})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   h.cfg.JWTExpiryMinutes * 60,
	})
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, clearCookie())
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

type dashboardStats struct {
	Tenants   int
	Users     int
	Projects  int
	Buckets   int
	Documents int
}

type dashboardData struct {
	basePage
	Stats       dashboardStats
	RecentAudit []*audit.Event
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	bp := h.base(r, "dashboard")
	tid := bp.Principal.TenantID

	var stats dashboardStats
	stats.Tenants, _ = h.repo.CountTenants(r.Context())
	stats.Users, _ = h.auth.CountUsers(r.Context())
	stats.Projects, _ = h.repo.CountProjects(r.Context(), tid)
	stats.Buckets, _ = h.repo.CountBuckets(r.Context(), tid)
	stats.Documents, _ = h.repo.CountDocuments(r.Context(), tid)

	recent, _ := h.audit.List(r.Context(), audit.Filter{
		TenantID: tid,
		Limit:    10,
	})

	h.render(w, "dashboard", dashboardData{
		basePage:    bp,
		Stats:       stats,
		RecentAudit: recent,
	})
}

// ── Tenants ───────────────────────────────────────────────────────────────────

type tenantsData struct {
	basePage
	Tenants []*repo.Tenant
}

func (h *Handler) tenants(w http.ResponseWriter, r *http.Request) {
	tenants, _ := h.repo.ListTenants(r.Context())
	h.render(w, "tenants", tenantsData{
		basePage: h.base(r, "tenants"),
		Tenants:  tenants,
	})
}

func (h *Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	desc := r.FormValue("description")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	t, err := h.repo.CreateTenant(r.Context(), name, desc)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPartial(w, "tenants", "tenant-row", t)
}

func (h *Handler) deleteTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tenantID")
	if err := h.repo.DeleteTenant(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Projects ──────────────────────────────────────────────────────────────────

type projectsData struct {
	basePage
	Tenant   *repo.Tenant
	Projects []*repo.Project
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	projects, _ := h.repo.ListProjects(r.Context(), tenantID)
	h.render(w, "projects", projectsData{
		basePage: h.base(r, "projects"),
		Tenant:   tenant,
		Projects: projects,
	})
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	name := r.FormValue("name")
	desc := r.FormValue("description")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	p, err := h.repo.CreateProject(r.Context(), tenantID, name, desc)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPartial(w, "projects", "project-row", p)
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectID")
	if err := h.repo.DeleteProject(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Buckets ───────────────────────────────────────────────────────────────────

// bucketRow wraps a Bucket with the parent TenantID, needed by the bucket-row
// template to build breadcrumb and delete URLs.
type bucketRow struct {
	*repo.Bucket
	TenantID string
}

type bucketsData struct {
	basePage
	Tenant  *repo.Tenant
	Project *repo.Project
	Buckets []bucketRow
}

func (h *Handler) buckets(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.NotFound(w, r)
		return
	}

	raw, _ := h.repo.ListBuckets(r.Context(), projectID)
	var rows []bucketRow
	for _, b := range raw {
		rows = append(rows, bucketRow{Bucket: b, TenantID: tenantID})
	}

	h.render(w, "buckets", bucketsData{
		basePage: h.base(r, "buckets"),
		Tenant:   tenant,
		Project:  project,
		Buckets:  rows,
	})
}

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	name := r.FormValue("name")
	desc := r.FormValue("description")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	b, err := h.repo.CreateBucket(r.Context(), projectID, name, desc)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPartial(w, "buckets", "bucket-row", bucketRow{Bucket: b, TenantID: tenantID})
}

func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bucketID")
	if err := h.repo.DeleteBucket(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Documents ─────────────────────────────────────────────────────────────────

type documentsData struct {
	basePage
	Tenant    *repo.Tenant
	Project   *repo.Project
	Bucket    *repo.Bucket
	Documents []*repo.Document
}

func (h *Handler) documents(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	bucketID := chi.URLParam(r, "bucketID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.NotFound(w, r)
		return
	}
	bucket, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || bucket == nil {
		http.NotFound(w, r)
		return
	}

	docs, _ := h.repo.ListDocuments(r.Context(), bucketID)
	h.render(w, "documents", documentsData{
		basePage:  h.base(r, "documents"),
		Tenant:    tenant,
		Project:   project,
		Bucket:    bucket,
		Documents: docs,
	})
}

func (h *Handler) uploadDocument(w http.ResponseWriter, r *http.Request) {
	bucketID := chi.URLParam(r, "bucketID")

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "bad multipart form", http.StatusBadRequest)
		return
	}

	file, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	if name == "" {
		name = fh.Filename
	}

	key, size, checksum, err := h.storage.Put(r.Context(), file)
	if err != nil {
		http.Error(w, "storage error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	contentType := fh.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	p, _ := auth.PrincipalFromContext(r.Context())
	doc, err := h.repo.CreateDocument(r.Context(), bucketID, name, contentType, size, checksum, key, p.Username)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPartial(w, "documents", "document-row", doc)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	if err := h.repo.DeleteDocument(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	// Best-effort storage cleanup.
	if err := h.storage.Delete(r.Context(), doc.StorageKey); err != nil {
		slog.Warn("storage delete failed", "key", doc.StorageKey, "error", err)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) downloadDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	rc, err := h.storage.Get(r.Context(), doc.StorageKey)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, url.PathEscape(doc.Name)))
	w.Header().Set("Content-Length", strconv.FormatInt(doc.Size, 10))
	if _, err := io.Copy(w, rc); err != nil {
		slog.Warn("download copy error", "id", id, "error", err)
	}
}

// ── Users ─────────────────────────────────────────────────────────────────────

type usersData struct {
	basePage
	Users []*auth.User
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	users, _ := h.auth.ListUsers(r.Context(), p.TenantID)
	h.render(w, "users", usersData{
		basePage: h.base(r, "users"),
		Users:    users,
	})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")
	role := r.FormValue("role")

	if username == "" || email == "" || password == "" {
		http.Error(w, "all fields required", http.StatusBadRequest)
		return
	}

	userType := auth.UserTypeUser
	if role == "admin" {
		userType = auth.UserTypeAdmin
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.auth.CreateUser(r.Context(), p.TenantID, username, email, hash, userType)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPartial(w, "users", "user-row", user)
}

func (h *Handler) activateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if err := h.auth.SetUserActive(r.Context(), id, true); err != nil {
		http.Error(w, "activate failed", http.StatusInternalServerError)
		return
	}
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	h.renderPartial(w, "users", "user-row", user)
}

func (h *Handler) deactivateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if err := h.auth.SetUserActive(r.Context(), id, false); err != nil {
		http.Error(w, "deactivate failed", http.StatusInternalServerError)
		return
	}
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	h.renderPartial(w, "users", "user-row", user)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if err := h.auth.DeleteUser(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── API Keys ──────────────────────────────────────────────────────────────────

type apiKeysData struct {
	basePage
	Keys   []*auth.APIKey
	NewKey string // only set immediately after creation
}

func (h *Handler) apiKeys(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	keys, _ := h.auth.ListAPIKeys(r.Context(), p.TenantID)
	h.render(w, "apikeys", apiKeysData{
		basePage: h.base(r, "apikeys"),
		Keys:     keys,
	})
}

func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	name := r.FormValue("name")
	if name == "" {
		name = "api-key-" + time.Now().Format("20060102")
	}

	plaintext, hash, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		http.Error(w, "key generation failed", http.StatusInternalServerError)
		return
	}

	uid := p.ID
	if _, err = h.auth.CreateAPIKey(r.Context(), p.TenantID, &uid, name, hash, prefix, nil); err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the full page so the new key can be displayed once.
	keys, _ := h.auth.ListAPIKeys(r.Context(), p.TenantID)
	h.render(w, "apikeys", apiKeysData{
		basePage: h.base(r, "apikeys"),
		Keys:     keys,
		NewKey:   plaintext,
	})
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "keyID")
	p, _ := auth.PrincipalFromContext(r.Context())

	if err := h.auth.RevokeAPIKey(r.Context(), id); err != nil {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}

	// Find and return the updated row.
	keys, _ := h.auth.ListAPIKeys(r.Context(), p.TenantID)
	for _, k := range keys {
		if k.ID == id {
			h.renderPartial(w, "apikeys", "apikey-row", k)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// ── Audit Log ─────────────────────────────────────────────────────────────────

type auditData struct {
	basePage
	Events []*audit.Event
}

func (h *Handler) auditLog(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	events, _ := h.audit.List(r.Context(), audit.Filter{
		TenantID: p.TenantID,
		Limit:    50,
	})
	h.render(w, "audit", auditData{
		basePage: h.base(r, "audit"),
		Events:   events,
	})
}

// auditEvents handles the HTMX partial for filtered audit rows.
func (h *Handler) auditEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	from := q.Get("from")
	to := q.Get("to")
	// datetime-local inputs produce "2006-01-02T15:04"; SQLite wants a space.
	if from != "" {
		from = strings.ReplaceAll(from, "T", " ")
	}
	if to != "" {
		to = strings.ReplaceAll(to, "T", " ")
	}

	events, _ := h.audit.List(r.Context(), audit.Filter{
		TenantID:  p.TenantID,
		Action:    q.Get("action"),
		Principal: q.Get("principal"),
		From:      from,
		To:        to,
		Limit:     limit,
	})

	t, ok := h.tmpls["audit"]
	if !ok {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(events) == 0 {
		fmt.Fprintf(w, `<tr><td colspan="5"><div class="empty-state"><p>No events found.</p></div></td></tr>`)
		return
	}
	for _, ev := range events {
		if err := t.ExecuteTemplate(w, "audit-row", ev); err != nil {
			slog.Error("audit-row render error", "error", err)
		}
	}
}
