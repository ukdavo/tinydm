package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/repo"
)

// ── Pagination helpers ────────────────────────────────────────────────────────

// WebPagination carries everything a template needs to render a pagination bar.
type WebPagination struct {
	Page       int    // current page (1-indexed)
	Limit      int    // items per page
	Total      int    // total matching rows
	TotalPages int    // ceil(Total/Limit)
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
	ExtraQuery string // "&q=foo&tag=bar" — preserves active filters in pager links
}

// parsePage reads ?page= from the request (1-indexed, default 1).
// Also reads ?limit= clamped to [1, 200], default 50.
func parsePage(r *http.Request) (page, limit int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return page, limit
}

// newWebPagination builds a WebPagination from total item count, current page,
// page size, and any extra query-string pairs (e.g. "&q=foo") for filter links.
func newWebPagination(total, page, limit int, extraQuery string) WebPagination {
	if limit <= 0 {
		limit = 50
	}
	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	return WebPagination{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		ExtraQuery: extraQuery,
	}
}

// pageOffset converts a 1-indexed page + limit into a SQL OFFSET value.
func pageOffset(page, limit int) int {
	if page < 1 {
		page = 1
	}
	return (page - 1) * limit
}

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
	tenants, _, err := h.repo.ListTenants(ctx, repo.PageOpts{Limit: 1})
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
		SameSite: http.SameSiteLaxMode, // CSRF protection
		Secure:   h.cfg.SecureCookies,  // set true behind HTTPS
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

	recent, _, _ := h.audit.List(r.Context(), audit.Filter{
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
	Pager   WebPagination
}

func (h *Handler) tenants(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if !p.IsSuperAdmin() {
		tenant, _ := h.repo.GetTenant(r.Context(), p.TenantID)
		var tenants []*repo.Tenant
		if tenant != nil {
			tenants = append(tenants, tenant)
		}
		h.render(w, "tenants", tenantsData{
			basePage: h.base(r, "tenants"),
			Tenants:  tenants,
			Pager:    newWebPagination(len(tenants), 1, len(tenants)+1, ""),
		})
		return
	}
	page, limit := parsePage(r)
	tenants, total, _ := h.repo.ListTenants(r.Context(), repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})
	h.render(w, "tenants", tenantsData{
		basePage: h.base(r, "tenants"),
		Tenants:  tenants,
		Pager:    newWebPagination(total, page, limit, ""),
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
	adminUser, adminPass, err := h.auth.CreateDomainAdmin(r.Context(), t.ID)
	if err != nil {
		http.Error(w, "tenant created but failed to provision domain admin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl, ok := h.tmpls["tenants"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.ExecuteTemplate(w, "tenant-row", t)
	_ = tmpl.ExecuteTemplate(w, "tenant-created-notice", struct{ Username, Password string }{adminUser.Username, adminPass})
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

type tenantStats struct {
	Projects  int
	Buckets   int
	Documents int
	Users     int
	APIKeys   int
}

type projectsData struct {
	basePage
	Tenant   *repo.Tenant
	Stats    tenantStats
	Projects []*repo.Project
	Pager    WebPagination
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	page, limit := parsePage(r)
	projects, total, _ := h.repo.ListProjects(r.Context(), tenantID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})

	var stats tenantStats
	stats.Projects, _ = h.repo.CountProjects(r.Context(), tenantID)
	stats.Buckets, _ = h.repo.CountBuckets(r.Context(), tenantID)
	stats.Documents, _ = h.repo.CountDocuments(r.Context(), tenantID)
	stats.Users, _ = h.auth.CountUsersByTenant(r.Context(), tenantID)
	stats.APIKeys, _ = h.auth.CountAPIKeysByTenant(r.Context(), tenantID)

	h.render(w, "projects", projectsData{
		basePage: h.base(r, "projects"),
		Tenant:   tenant,
		Stats:    stats,
		Projects: projects,
		Pager:    newWebPagination(total, page, limit, ""),
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

// bucketRow wraps a Bucket with the parent TenantID and document count,
// needed by the bucket-row template to build URLs and show stats.
type bucketRow struct {
	*repo.Bucket
	TenantID string
	DocCount int
}

type projectStats struct {
	Buckets   int
	Documents int
}

type bucketsData struct {
	basePage
	Tenant  *repo.Tenant
	Project *repo.Project
	Stats   projectStats
	Buckets []bucketRow
	Pager   WebPagination
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

	page, limit := parsePage(r)
	raw, total, _ := h.repo.ListBuckets(r.Context(), projectID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})
	var rows []bucketRow
	for _, b := range raw {
		n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
		rows = append(rows, bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
	}

	var stats projectStats
	stats.Buckets, _ = h.repo.CountBucketsInProject(r.Context(), projectID)
	stats.Documents, _ = h.repo.CountDocumentsInProject(r.Context(), projectID)

	h.render(w, "buckets", bucketsData{
		basePage: h.base(r, "buckets"),
		Tenant:   tenant,
		Project:  project,
		Stats:    stats,
		Buckets:  rows,
		Pager:    newWebPagination(total, page, limit, ""),
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
	n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
	h.renderPartial(w, "buckets", "bucket-row", bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
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

type bucketSummaryStats struct {
	Documents int
	TotalSize string
}

type documentsData struct {
	basePage
	Tenant    *repo.Tenant
	Project   *repo.Project
	Bucket    *repo.Bucket
	Stats     bucketSummaryStats
	Documents []*repo.Document
	Pager     WebPagination
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

	page, limit := parsePage(r)
	docs, total, _ := h.repo.ListDocuments(r.Context(), bucketID, repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)})

	docCount, _ := h.repo.CountDocumentsInBucket(r.Context(), bucketID)
	sizeBytes, _ := h.repo.SumDocumentSizeInBucket(r.Context(), bucketID)

	h.render(w, "documents", documentsData{
		basePage: h.base(r, "documents"),
		Tenant:   tenant,
		Project:  project,
		Bucket:   bucket,
		Stats: bucketSummaryStats{
			Documents: docCount,
			TotalSize: formatSize(sizeBytes),
		},
		Documents: docs,
		Pager:     newWebPagination(total, page, limit, ""),
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
	Tenant        *repo.Tenant
	Users         []*auth.User
	Pager         WebPagination
	PermModeTenant struct {
		ID       string
		PermMode string
	}
}

// tenantUsers renders GET /admin/tenants/{tenantID}/users
func (h *Handler) tenantUsers(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	page, limit := parsePage(r)
	users, total, _ := h.auth.ListUsers(r.Context(), tenantID, limit, pageOffset(page, limit))
	mode, _ := h.auth.GetTenantPermMode(r.Context(), tenantID)
	data := usersData{
		basePage: h.base(r, "users"),
		Tenant:   tenant,
		Users:    users,
		Pager:    newWebPagination(total, page, limit, ""),
	}
	data.PermModeTenant.ID = tenantID
	data.PermModeTenant.PermMode = string(mode)
	h.render(w, "users", data)
}

// createTenantUser handles POST /admin/tenants/{tenantID}/users
func (h *Handler) createTenantUser(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	username := r.FormValue("username")
	email := r.FormValue("email")
	firstName := strings.TrimSpace(r.FormValue("first_name"))
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	password := r.FormValue("password")
	role := r.FormValue("role")

	if username == "" || email == "" || password == "" || firstName == "" || lastName == "" {
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

	user, err := h.auth.CreateUser(r.Context(), tenantID, username, email, firstName, lastName, hash, userType)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPartial(w, "users", "user-row", user)
}

// userRow handles GET /admin/users/{userID}/row
//
// Returns the normal user-row partial. Used by the Cancel button in the
// inline password-change form to restore the row to its display state.
func (h *Handler) userRow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		http.NotFound(w, r)
		return
	}
	h.renderPartial(w, "users", "user-row", user)
}

// passwordForm handles GET /admin/users/{userID}/password-form
//
// Returns the inline password-change row partial so HTMX can swap the normal
// user row with an editable form.
func (h *Handler) passwordForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		http.NotFound(w, r)
		return
	}
	h.renderPartial(w, "users", "user-change-password-row", user)
}

// changeUserPassword handles POST /admin/users/{userID}/password
//
// Reads the new password from the form body, validates a minimum length of 8,
// hashes it, updates the user, then returns the refreshed user-row partial so
// HTMX can swap the form row back to the normal display row.
func (h *Handler) changeUserPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	password := r.FormValue("password")
	if len(password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.auth.ChangePassword(r.Context(), id, hash); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusForbidden)
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
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── API Keys ──────────────────────────────────────────────────────────────────

type apiKeysData struct {
	basePage
	Tenant *repo.Tenant
	Keys   []*auth.APIKey
	NewKey string // only set immediately after creation
	Pager  WebPagination
}

func (h *Handler) tenantAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	page, limit := parsePage(r)
	keys, total, _ := h.auth.ListAPIKeys(r.Context(), tenantID, limit, pageOffset(page, limit))
	h.render(w, "apikeys", apiKeysData{
		basePage: h.base(r, "apikeys"),
		Tenant:   tenant,
		Keys:     keys,
		Pager:    newWebPagination(total, page, limit, ""),
	})
}

func (h *Handler) createTenantAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
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
	if _, err = h.auth.CreateAPIKey(r.Context(), tenantID, &uid, name, hash, prefix, nil); err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-render the full page so the new key is displayed once.
	keys, total, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 50, 0)
	h.render(w, "apikeys", apiKeysData{
		basePage: h.base(r, "apikeys"),
		Tenant:   tenant,
		Keys:     keys,
		NewKey:   plaintext,
		Pager:    newWebPagination(total, 1, 50, ""),
	})
}

func (h *Handler) revokeTenantAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	id := chi.URLParam(r, "keyID")

	if err := h.auth.RevokeAPIKey(r.Context(), tenantID, id); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	// Return the updated row.
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
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
	TenantID string
	Events   []*audit.Event
	Pager    WebPagination
}

func (h *Handler) auditLog(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	tenantID := chi.URLParam(r, "tenantID")
	if p.UserType != auth.UserTypeSuperAdmin && tenantID != p.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	page, limit := parsePage(r)
	events, total, _ := h.audit.List(r.Context(), audit.Filter{
		TenantID: tenantID,
		Limit:    limit,
		Offset:   pageOffset(page, limit),
	})
	h.render(w, "audit", auditData{
		basePage: h.base(r, "audit"),
		TenantID: tenantID,
		Events:   events,
		Pager:    newWebPagination(total, page, limit, ""),
	})
}

// ── Phase 7: Bucket edit / update ────────────────────────────────────────────

func (h *Handler) editBucketForm(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	b, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || b == nil {
		http.NotFound(w, r)
		return
	}
	n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
	h.renderPartial(w, "buckets", "bucket-edit-row", bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
}

func (h *Handler) bucketRowPartial(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	b, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || b == nil {
		http.NotFound(w, r)
		return
	}
	n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
	h.renderPartial(w, "buckets", "bucket-row", bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
}

func (h *Handler) updateBucket(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	name := r.FormValue("name")
	desc := r.FormValue("description")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	b, err := h.repo.UpdateBucket(r.Context(), bucketID, name, desc)
	if err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	n, _ := h.repo.CountDocumentsInBucket(r.Context(), b.ID)
	h.renderPartial(w, "buckets", "bucket-row", bucketRow{Bucket: b, TenantID: tenantID, DocCount: n})
}

// ── Phase 7: Document rows (HTMX search / tag-filter partial) ─────────────────

func (h *Handler) documentRows(w http.ResponseWriter, r *http.Request) {
	bucketID := chi.URLParam(r, "bucketID")
	q := r.URL.Query().Get("q")
	tag := r.URL.Query().Get("tag")
	page, limit := parsePage(r)
	opts := repo.PageOpts{Limit: limit, Offset: pageOffset(page, limit)}

	var docs []*repo.Document
	var total int
	if tag != "" {
		docs, total, _ = h.repo.ListDocumentsByTag(r.Context(), bucketID, tag, opts)
		if q != "" {
			// Apply in-memory name filter on top of tag filter.
			ql := strings.ToLower(q)
			var filtered []*repo.Document
			for _, d := range docs {
				if strings.Contains(strings.ToLower(d.Name), ql) {
					filtered = append(filtered, d)
				}
			}
			docs = filtered
			total = len(filtered) // approximate — tag-filtered page only
		}
	} else if q != "" {
		docs, total, _ = h.repo.SearchDocuments(r.Context(), bucketID, q, opts)
	} else {
		docs, total, _ = h.repo.ListDocuments(r.Context(), bucketID, opts)
	}

	t, ok := h.tmpls["documents"]
	if !ok {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	// Build extra query params for pagination links.
	extra := ""
	if q != "" {
		extra += "&q=" + url.QueryEscape(q)
	}
	if tag != "" {
		extra += "&tag=" + url.QueryEscape(tag)
	}
	pager := newWebPagination(total, page, limit, extra)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Emit the rows.
	if len(docs) == 0 {
		fmt.Fprintf(w, `<tr><td colspan="6"><div class="empty-state"><p>No documents found.</p></div></td></tr>`)
	} else {
		for _, d := range docs {
			if err := t.ExecuteTemplate(w, "document-row", d); err != nil {
				slog.Error("document-row render error", "error", err)
			}
		}
	}

	// OOB swap: update the pagination bar without a full page reload.
	if err := t.ExecuteTemplate(w, "docs-pager-oob", pager); err != nil {
		slog.Error("docs-pager-oob render error", "error", err)
	}
}

// ── Phase 7: Document inline-edit partials ────────────────────────────────────

func (h *Handler) editDocumentForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	h.renderPartial(w, "documents", "document-edit-row", doc)
}

func (h *Handler) documentRowPartial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	h.renderPartial(w, "documents", "document-row", doc)
}

func (h *Handler) updateDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		_ = r.ParseForm()
	}

	name := r.FormValue("name")
	if name == "" {
		name = doc.Name
	}

	file, fh, fileErr := r.FormFile("file")
	if fileErr == nil {
		// Full content replacement — creates a version snapshot.
		defer file.Close()
		p, _ := auth.PrincipalFromContext(r.Context())
		key, size, checksum, err := h.storage.Put(r.Context(), file)
		if err != nil {
			http.Error(w, "storage error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ct := fh.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		doc, err = h.repo.UpdateDocument(r.Context(), id, name, ct, size, checksum, key, p.Username)
		if err != nil {
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Name-only rename — no snapshot.
		doc, err = h.repo.RenameDocument(r.Context(), id, name)
		if err != nil {
			http.Error(w, "rename failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	h.renderPartial(w, "documents", "document-row", doc)
}

// ── Phase 7: Document detail page ─────────────────────────────────────────────

// docProperty is a key/value pair for template rendering of document properties.
type docProperty struct {
	DocID string
	Key   string
	Value string
}

// docTagsData holds a document ID and its tag list for the doc-tags-inner partial.
type docTagsData struct {
	DocID string
	Tags  []string
}

// documentDetailData holds everything needed to render the document detail page.
type documentDetailData struct {
	basePage
	Doc       *repo.Document
	Tenant    *repo.Tenant
	Project   *repo.Project
	Bucket    *repo.Bucket
	TagsData  docTagsData
	UserProps []docProperty
	SysProps  []docProperty
	Versions  []*repo.DocumentVersion
	Rights    []ResourceRight
	Users     []*auth.User
	APIKeys   []*auth.APIKey
}

// resolveDocumentContext walks bucket → project → tenant for a given document.
func (h *Handler) resolveDocumentContext(ctx context.Context, doc *repo.Document) (*repo.Tenant, *repo.Project, *repo.Bucket, error) {
	bucket, err := h.repo.GetBucket(ctx, doc.BucketID)
	if err != nil || bucket == nil {
		return nil, nil, nil, fmt.Errorf("bucket not found")
	}
	project, err := h.repo.GetProject(ctx, bucket.ProjectID)
	if err != nil || project == nil {
		return nil, nil, nil, fmt.Errorf("project not found")
	}
	tenant, err := h.repo.GetTenant(ctx, project.TenantID)
	if err != nil || tenant == nil {
		return nil, nil, nil, fmt.Errorf("tenant not found")
	}
	return tenant, project, bucket, nil
}

// buildDocDetailData assembles the full documentDetailData for a document.
func (h *Handler) buildDocDetailData(r *http.Request, doc *repo.Document) (documentDetailData, error) {
	tenant, project, bucket, err := h.resolveDocumentContext(r.Context(), doc)
	if err != nil {
		return documentDetailData{}, err
	}

	tags, _ := h.repo.ListDocumentTags(r.Context(), doc.ID)

	rawProps, _ := h.repo.GetDocumentProperties(r.Context(), doc.ID)
	keys := make([]string, 0, len(rawProps))
	for k := range rawProps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var userProps, sysProps []docProperty
	for _, k := range keys {
		dp := docProperty{DocID: doc.ID, Key: k, Value: rawProps[k]}
		if strings.HasPrefix(k, "sys.") {
			sysProps = append(sysProps, dp)
		} else {
			userProps = append(userProps, dp)
		}
	}

	versions, _, _ := h.repo.ListDocumentVersions(r.Context(), doc.ID, repo.PageOpts{Limit: 500})

	tenantID := project.TenantID
	rawRights, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "document", doc.ID)
	allDocUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	docUsers := filterNonAdminUsers(allDocUsers)
	docKeys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)

	data := documentDetailData{
		basePage:  h.base(r, "documents"),
		Doc:       doc,
		Tenant:    tenant,
		Project:   project,
		Bucket:    bucket,
		TagsData:  docTagsData{DocID: doc.ID, Tags: tags},
		UserProps: userProps,
		SysProps:  sysProps,
		Versions:  versions,
		Rights:    h.resolveRightNames(r.Context(), rawRights),
		Users:     docUsers,
		APIKeys:   docKeys,
	}
	return data, nil
}

func (h *Handler) documentDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), id)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	data, err := h.buildDocDetailData(r, doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, "docdetail", data)
}

// ── Phase 7: Document tags ─────────────────────────────────────────────────────

func (h *Handler) addDocumentTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	tag := strings.TrimSpace(r.FormValue("tag"))
	if tag == "" {
		http.Error(w, "tag required", http.StatusBadRequest)
		return
	}
	if err := h.repo.AddDocumentTag(r.Context(), id, tag); err != nil {
		http.Error(w, "add tag failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tags, _ := h.repo.ListDocumentTags(r.Context(), id)
	h.renderPartial(w, "docdetail", "doc-tags-inner", docTagsData{DocID: id, Tags: tags})
}

func (h *Handler) removeDocumentTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	tag := chi.URLParam(r, "tag")
	if err := h.repo.RemoveDocumentTag(r.Context(), id, tag); err != nil {
		http.Error(w, "remove tag failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tags, _ := h.repo.ListDocumentTags(r.Context(), id)
	h.renderPartial(w, "docdetail", "doc-tags-inner", docTagsData{DocID: id, Tags: tags})
}

// ── Phase 7: Document properties ──────────────────────────────────────────────

func (h *Handler) setDocumentPropertyWeb(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	key := strings.TrimSpace(r.FormValue("key"))
	value := r.FormValue("value")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := h.repo.SetDocumentProperty(r.Context(), id, key, value); err != nil {
		http.Error(w, "set property failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rawProps, _ := h.repo.GetDocumentProperties(r.Context(), id)
	var keys []string
	for k := range rawProps {
		if !strings.HasPrefix(k, "sys.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	t, ok := h.tmpls["docdetail"]
	if !ok {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, k := range keys {
		dp := docProperty{DocID: id, Key: k, Value: rawProps[k]}
		if err := t.ExecuteTemplate(w, "prop-row", dp); err != nil {
			slog.Error("prop-row render error", "error", err)
		}
	}
}

func (h *Handler) deleteDocumentPropertyWeb(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	key := chi.URLParam(r, "key")
	if err := h.repo.DeleteDocumentProperty(r.Context(), id, key); err != nil {
		http.Error(w, "delete property failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Return empty — hx-swap="outerHTML" with empty body removes the target element.
	w.WriteHeader(http.StatusOK)
}

// ── Phase 7: Document version restore ─────────────────────────────────────────

func (h *Handler) restoreDocumentVersionWeb(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "documentID")
	versionID := chi.URLParam(r, "versionID")
	p, _ := auth.PrincipalFromContext(r.Context())
	if _, err := h.repo.RestoreDocumentVersion(r.Context(), docID, versionID, p.Username); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// auditEvents handles the HTMX partial for filtered audit rows.
func (h *Handler) auditEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	tenantID := chi.URLParam(r, "tenantID")
	if p.UserType != auth.UserTypeSuperAdmin && tenantID != p.TenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	page, limit := parsePage(r)

	from := q.Get("from")
	to := q.Get("to")
	// datetime-local inputs produce "2006-01-02T15:04"; SQLite wants a space.
	if from != "" {
		from = strings.ReplaceAll(from, "T", " ")
	}
	if to != "" {
		to = strings.ReplaceAll(to, "T", " ")
	}

	action := q.Get("action")
	principal := q.Get("principal")

	events, total, _ := h.audit.List(r.Context(), audit.Filter{
		TenantID:  tenantID,
		Action:    action,
		Principal: principal,
		From:      from,
		To:        to,
		Limit:     limit,
		Offset:    pageOffset(page, limit),
	})

	t, ok := h.tmpls["audit"]
	if !ok {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	// Build extra query string for pagination links (preserving filter state).
	extra := ""
	if action != "" {
		extra += "&action=" + url.QueryEscape(action)
	}
	if principal != "" {
		extra += "&principal=" + url.QueryEscape(principal)
	}
	if from != "" {
		extra += "&from=" + url.QueryEscape(from)
	}
	if to != "" {
		extra += "&to=" + url.QueryEscape(to)
	}
	pager := newWebPagination(total, page, limit, extra)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Emit the rows.
	if len(events) == 0 {
		fmt.Fprintf(w, `<tr><td colspan="5"><div class="empty-state"><p>No events found.</p></div></td></tr>`)
	} else {
		for _, ev := range events {
			if err := t.ExecuteTemplate(w, "audit-row", ev); err != nil {
				slog.Error("audit-row render error", "error", err)
			}
		}
	}

	// OOB swap: update the pagination bar without a full page reload.
	if err := t.ExecuteTemplate(w, "audit-pager-oob", pager); err != nil {
		slog.Error("audit-pager-oob render error", "error", err)
	}
}

// formatSize converts bytes to a human-readable string (e.g. "1.4 GB", "840 KB").
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ── Rights page data ──────────────────────────────────────────────────────────

type userRightsPage struct {
	basePage
	Tenant *repo.Tenant
	User   *auth.User
	Rights []auth.Right
}

type apiKeyRightsPage struct {
	basePage
	Tenant *repo.Tenant
	Key    *auth.APIKey
	Rights []auth.Right
}

// ResourceRight is a Right with a resolved display name for the principal.
type ResourceRight struct {
	auth.Right
	PrincipalName string
}

type resourceRightsPage struct {
	basePage
	Tenant       *repo.Tenant
	ResourceType string
	ResourceID   string
	ResourceName string
	ParentID     string
	Rights       []ResourceRight
	Users        []*auth.User
	APIKeys      []*auth.APIKey
}

// ── User rights handlers ──────────────────────────────────────────────────────

func (h *Handler) userRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	user, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil || user == nil || user.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	rights, err := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	if err != nil {
		rights = nil
	}

	data := userRightsPage{
		basePage: h.base(r, "users"),
		Tenant:   tenant,
		User:     user,
		Rights:   rights,
	}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

func (h *Handler) addUserRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	user, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil || user == nil || user.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	cc, cr, cu, cd := permLevelToFlags(r.FormValue("perm_level"))
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: "user",
		PrincipalID:   userID,
		ResourceType:  r.FormValue("resource_type"),
		ResourceID:    r.FormValue("resource_id"),
		CanCreate:     cc,
		CanRead:       cr,
		CanUpdate:     cu,
		CanDelete:     cd,
	}
	if params.ResourceID == "" {
		params.ResourceID = "*"
	}

	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, "failed to add right", http.StatusInternalServerError)
		return
	}

	// Re-render the panel partial.
	rights, _ := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	data := userRightsPage{basePage: h.base(r, "users"), Tenant: tenant, User: user, Rights: rights}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

func (h *Handler) removeUserRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	user, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil || user == nil || user.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	resourceType := r.FormValue("resource_type")
	resourceID := r.FormValue("resource_id")

	_ = h.auth.DeleteRight(r.Context(), tenantID, "user", userID, resourceType, resourceID)

	rights, _ := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	data := userRightsPage{basePage: h.base(r, "users"), Tenant: tenant, User: user, Rights: rights}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

// ── API key rights handlers ───────────────────────────────────────────────────

func (h *Handler) apiKeyRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	keys, _, err := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	if key == nil {
		http.NotFound(w, r)
		return
	}
	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

func (h *Handler) addAPIKeyRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	cc, cr, cu, cd := permLevelToFlags(r.FormValue("perm_level"))
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: "apikey",
		PrincipalID:   keyID,
		ResourceType:  r.FormValue("resource_type"),
		ResourceID:    r.FormValue("resource_id"),
		CanCreate:     cc,
		CanRead:       cr,
		CanUpdate:     cu,
		CanDelete:     cd,
	}
	if params.ResourceID == "" {
		params.ResourceID = "*"
	}
	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, "failed to add right", http.StatusInternalServerError)
		return
	}

	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	if key == nil {
		http.NotFound(w, r)
		return
	}
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

func (h *Handler) removeAPIKeyRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_ = h.auth.DeleteRight(r.Context(), tenantID, "apikey", keyID,
		r.FormValue("resource_type"), r.FormValue("resource_id"))

	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	if key == nil {
		http.NotFound(w, r)
		return
	}
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

// ── Per-resource rights helpers ───────────────────────────────────────────────

// resolveRightNames enriches raw rights with principal display names.
func (h *Handler) resolveRightNames(ctx context.Context, rights []auth.Right) []ResourceRight {
	out := make([]ResourceRight, 0, len(rights))
	for _, r := range rights {
		rr := ResourceRight{Right: r}
		if r.PrincipalType == "user" {
			if u, _ := h.auth.GetUserByID(ctx, r.PrincipalID); u != nil {
				rr.PrincipalName = u.Username
			}
		} else if r.PrincipalType == "apikey" {
			if k, _ := h.auth.GetAPIKeyByID(ctx, r.PrincipalID); k != nil {
				rr.PrincipalName = k.Name + " (" + k.KeyPrefix + "…)"
			}
		}
		out = append(out, rr)
	}
	return out
}

// parsePrincipal splits "user:uuid" or "apikey:uuid" form values.
func parsePrincipal(v string) (principalType, principalID string, ok bool) {
	i := strings.IndexByte(v, ':')
	if i < 1 {
		return "", "", false
	}
	return v[:i], v[i+1:], true
}

// cascadeProjectRight copies a project right down to all its buckets and documents.
func (h *Handler) cascadeProjectRight(ctx context.Context, base auth.UpsertRightParams) {
	buckets, _, _ := h.repo.ListBuckets(ctx, base.ResourceID, repo.PageOpts{Limit: repo.MaxPageLimit})
	for _, b := range buckets {
		p := base
		p.ResourceType = "bucket"
		p.ResourceID = b.ID
		_ = h.auth.UpsertRight(ctx, p)
		docs, _, _ := h.repo.ListDocuments(ctx, b.ID, repo.PageOpts{Limit: repo.MaxPageLimit})
		for _, d := range docs {
			pd := base
			pd.ResourceType = "document"
			pd.ResourceID = d.ID
			_ = h.auth.UpsertRight(ctx, pd)
		}
	}
}

// cascadeBucketRight copies a bucket right down to all its documents.
func (h *Handler) cascadeBucketRight(ctx context.Context, base auth.UpsertRightParams) {
	docs, _, _ := h.repo.ListDocuments(ctx, base.ResourceID, repo.PageOpts{Limit: repo.MaxPageLimit})
	for _, d := range docs {
		p := base
		p.ResourceType = "document"
		p.ResourceID = d.ID
		_ = h.auth.UpsertRight(ctx, p)
	}
}

// permLevelToFlags converts a hierarchical perm level to individual CRUD booleans.
// Each level implies all lower levels: delete→update→create→read.
func permLevelToFlags(level string) (canCreate, canRead, canUpdate, canDelete bool) {
	switch level {
	case "delete":
		return true, true, true, true
	case "update":
		return true, true, true, false
	case "create":
		return true, true, false, false
	case "read":
		return false, true, false, false
	default:
		return false, false, false, false
	}
}

// filterNonAdminUsers returns only users with UserType == "user" (excludes admins/superadmins).
func filterNonAdminUsers(users []*auth.User) []*auth.User {
	out := users[:0:0]
	for _, u := range users {
		if u.UserType == auth.UserTypeUser {
			out = append(out, u)
		}
	}
	return out
}

// PermLevel returns the effective permission level label for display.
func (rr ResourceRight) PermLevel() string {
	switch {
	case rr.CanDelete:
		return "Delete"
	case rr.CanUpdate:
		return "Update"
	case rr.CanCreate:
		return "Create"
	case rr.CanRead:
		return "Read"
	default:
		return "None"
	}
}

// ── Perm mode handler ─────────────────────────────────────────────────────────

func (h *Handler) setPermMode(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := auth.PermMode(r.FormValue("perm_mode"))
	if mode != auth.PermModeExplicit && mode != auth.PermModeOpen && mode != auth.PermModeInherit {
		http.Error(w, "invalid perm_mode", http.StatusBadRequest)
		return
	}
	if err := h.auth.SetTenantPermMode(r.Context(), tenantID, mode); err != nil {
		http.Error(w, "failed to update permission mode", http.StatusInternalServerError)
		return
	}
	// Return a small success partial that replaces just the select element label.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span class="badge badge-green">Saved</span>`)
}

// ── Per-resource rights handlers ──────────────────────────────────────────────

// ── Project rights ────────────────────────────────────────────────────────────

func (h *Handler) projectRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "project", projectID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "projects", "project-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "projects"),
		Tenant:       tenant,
		ResourceType: "project",
		ResourceID:   projectID,
		ResourceName: project.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) addProjectRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pt, pid, ok := parsePrincipal(r.FormValue("principal"))
	if !ok {
		http.Error(w, "invalid principal", http.StatusBadRequest)
		return
	}
	cc, cr, cu, cd := permLevelToFlags(r.FormValue("perm_level"))
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: pt,
		PrincipalID:   pid,
		ResourceType:  "project",
		ResourceID:    projectID,
		CanCreate:     cc,
		CanRead:       cr,
		CanUpdate:     cu,
		CanDelete:     cd,
	}
	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.FormValue("cascade") == "on" {
		h.cascadeProjectRight(r.Context(), params)
	}
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "project", projectID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "projects", "project-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "projects"),
		Tenant:       tenant,
		ResourceType: "project",
		ResourceID:   projectID,
		ResourceName: project.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) removeProjectRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	projectID := chi.URLParam(r, "projectID")
	project, err := h.repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	pt, pid, _ := parsePrincipal(r.FormValue("principal"))
	_ = h.auth.DeleteRight(r.Context(), tenantID, pt, pid, "project", projectID)
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "project", projectID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "projects", "project-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "projects"),
		Tenant:       tenant,
		ResourceType: "project",
		ResourceID:   projectID,
		ResourceName: project.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

// ── Bucket rights ─────────────────────────────────────────────────────────────

func (h *Handler) bucketRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	bucket, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || bucket == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), bucket.ProjectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "bucket", bucketID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "buckets", "bucket-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "buckets"),
		Tenant:       tenant,
		ResourceType: "bucket",
		ResourceID:   bucketID,
		ResourceName: bucket.Name,
		ParentID:     project.ID,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) addBucketRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	bucket, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || bucket == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), bucket.ProjectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pt, pid, ok := parsePrincipal(r.FormValue("principal"))
	if !ok {
		http.Error(w, "invalid principal", http.StatusBadRequest)
		return
	}
	cc, cr, cu, cd := permLevelToFlags(r.FormValue("perm_level"))
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: pt,
		PrincipalID:   pid,
		ResourceType:  "bucket",
		ResourceID:    bucketID,
		CanCreate:     cc,
		CanRead:       cr,
		CanUpdate:     cu,
		CanDelete:     cd,
	}
	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.FormValue("cascade") == "on" {
		h.cascadeBucketRight(r.Context(), params)
	}
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "bucket", bucketID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "buckets", "bucket-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "buckets"),
		Tenant:       tenant,
		ResourceType: "bucket",
		ResourceID:   bucketID,
		ResourceName: bucket.Name,
		ParentID:     project.ID,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) removeBucketRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	bucketID := chi.URLParam(r, "bucketID")
	bucket, err := h.repo.GetBucket(r.Context(), bucketID)
	if err != nil || bucket == nil {
		http.NotFound(w, r)
		return
	}
	project, err := h.repo.GetProject(r.Context(), bucket.ProjectID)
	if err != nil || project == nil || project.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	pt, pid, _ := parsePrincipal(r.FormValue("principal"))
	_ = h.auth.DeleteRight(r.Context(), tenantID, pt, pid, "bucket", bucketID)
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "bucket", bucketID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	h.renderPartial(w, "buckets", "bucket-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "buckets"),
		Tenant:       tenant,
		ResourceType: "bucket",
		ResourceID:   bucketID,
		ResourceName: bucket.Name,
		ParentID:     project.ID,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

// ── Document rights ───────────────────────────────────────────────────────────

func (h *Handler) documentRightsPanel(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), documentID)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	tenant, project, _, err := h.resolveDocumentContext(r.Context(), doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tenantID := project.TenantID
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "document", documentID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	h.renderPartial(w, "docdetail", "document-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "docdetail"),
		Tenant:       tenant,
		ResourceType: "document",
		ResourceID:   documentID,
		ResourceName: doc.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) addDocumentRight(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), documentID)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	tenant, project, _, err := h.resolveDocumentContext(r.Context(), doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tenantID := project.TenantID
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	pt, pid, ok := parsePrincipal(r.FormValue("principal"))
	if !ok {
		http.Error(w, "invalid principal", http.StatusBadRequest)
		return
	}
	cc, cr, cu, cd := permLevelToFlags(r.FormValue("perm_level"))
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: pt,
		PrincipalID:   pid,
		ResourceType:  "document",
		ResourceID:    documentID,
		CanCreate:     cc,
		CanRead:       cr,
		CanUpdate:     cu,
		CanDelete:     cd,
	}
	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "document", documentID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	h.renderPartial(w, "docdetail", "document-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "docdetail"),
		Tenant:       tenant,
		ResourceType: "document",
		ResourceID:   documentID,
		ResourceName: doc.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}

func (h *Handler) removeDocumentRight(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "documentID")
	doc, err := h.repo.GetDocument(r.Context(), documentID)
	if err != nil || doc == nil {
		http.NotFound(w, r)
		return
	}
	tenant, project, _, err := h.resolveDocumentContext(r.Context(), doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tenantID := project.TenantID
	pt, pid, _ := parsePrincipal(r.FormValue("principal"))
	_ = h.auth.DeleteRight(r.Context(), tenantID, pt, pid, "document", documentID)
	raw, _ := h.auth.ListRightsByResource(r.Context(), tenantID, "document", documentID)
	allUsers, _, _ := h.auth.ListUsers(r.Context(), tenantID, 500, 0)
	users := filterNonAdminUsers(allUsers)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	h.renderPartial(w, "docdetail", "document-rights-panel", resourceRightsPage{
		basePage:     h.base(r, "docdetail"),
		Tenant:       tenant,
		ResourceType: "document",
		ResourceID:   documentID,
		ResourceName: doc.Name,
		Rights:       h.resolveRightNames(r.Context(), raw),
		Users:        users,
		APIKeys:      keys,
	})
}
