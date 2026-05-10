# Design: User First/Last Name & Admin Password Change

**Date:** 2026-05-10  
**Status:** Approved

## Summary

Add `first_name` and `last_name` (required) to the user entity, and allow admins to change any user's password from the admin UI via a modal dialog.

## Scope

- Database migration adding two new columns to `users`
- Auth store: struct, queries, and a new `ChangePassword` method
- REST API: updated user fields and a new password-change endpoint
- Web handlers: updated create-user form handling, new password-change handler
- Web UI: updated user table, create form, and a modal dialog for password change

Out of scope: self-service password change (non-admin users do not access the admin UI).

---

## 1. Database — Migration 005

A new migration file in both `internal/db/migrations/` and `internal/db/migrations_pg/` recreates the `users` table using the established pattern from migration 004 (create new table, copy rows, drop old, rename).

New columns:

| Column | Type | Constraint |
|---|---|---|
| `first_name` | TEXT | NOT NULL DEFAULT '' |
| `last_name` | TEXT | NOT NULL DEFAULT '' |

Existing rows receive empty strings on migration. The DEFAULT '' is a migration-time convenience; the application layer enforces non-empty values at the boundary.

---

## 2. Auth Store (`internal/auth/store.go`)

### User struct

```go
type User struct {
    ID           string
    TenantID     string
    Username     string
    Email        string
    FirstName    string
    LastName     string
    PasswordHash string
    UserType     UserType
    IsActive     bool
}
```

### Updated methods

- `GetUserByUsername` — SELECT adds `first_name`, `last_name`; `scanUser` scans them
- `GetUserByID` — same
- `ListUsers` — SELECT and scan updated
- `CreateUser` — signature gains `firstName, lastName string`; INSERT includes them
- `scanUser` — updated to scan two new fields

### New method

```go
func (s *Store) ChangePassword(ctx context.Context, userID, newHash string) error
```

Simple `UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`. Returns an error if no row was affected (user not found).

---

## 3. API Layer (`internal/api/users.go`)

### ListUsers response

`safeUser` gains `first_name` and `last_name` JSON fields.

### CreateUser (if/when exposed via API)

Not currently a REST endpoint — user creation goes through the web UI only. No change needed here beyond the store signature update.

### New endpoint: change password

```
PATCH /api/v1/tenants/{tenantID}/users/{userID}/password
```

- Requires admin or superadmin principal (enforced by existing RBAC middleware)
- Request body: `{"password": "..."}`
- Validates password non-empty and minimum length (8 chars)
- Hashes with `auth.HashPassword`, calls `store.ChangePassword`
- Returns 204 No Content on success

---

## 4. Web Handlers (`internal/web/handlers.go`)

### createTenantUser

Reads `first_name` and `last_name` from the POST form. Both are required — returns 400 if either is empty. Passes them through to `store.CreateUser`.

### changeUserPassword (new)

```
POST /admin/users/{userID}/password
```

- Reads `password` from form body
- Validates non-empty, minimum 8 characters
- Calls `auth.HashPassword` then `store.ChangePassword`
- On success: returns HTTP 200 with an empty body — HTMX closes the modal client-side
- On error: returns the error message so HTMX can display it inside the modal

### Route registration (web.go)

```go
r.Post("/admin/users/{userID}/password", h.changeUserPassword)
```

---

## 5. Web UI (`templates/users.html`)

### User table columns

Add **First Name** and **Last Name** columns between Username and Email, or combine as a single **Name** column showing "FirstName LastName".

### Create user form

Add two new required inputs: `first_name` and `last_name`. Included in the `hx-include` list on the submit button.

### User row

- Shows first/last name
- Adds a **Change Password** button for admin/superadmin rows (not shown for superadmin rows the caller cannot act on, consistent with existing deactivate/delete logic)

### Password change modal

Uses the native `<dialog>` HTML element — no JS library needed.

```html
<dialog id="pwd-modal-{{.ID}}">
  <form method="dialog">
    <h3>Change Password — {{.Username}}</h3>
    <input type="password" name="password" id="pwd-{{.ID}}"
           placeholder="New password (8+ chars)" required minlength="8">
    <div class="flex gap-8">
      <button type="button" class="btn btn-primary"
        hx-post="/admin/users/{{.ID}}/password"
        hx-include="#pwd-{{.ID}}"
        hx-on::after-request="document.getElementById('pwd-modal-{{.ID}}').close()">
        Save
      </button>
      <button class="btn btn-ghost" value="cancel">Cancel</button>
    </div>
  </form>
</dialog>
```

The **Change Password** button in the user row calls `document.getElementById('pwd-modal-{{.ID}}').showModal()` via an `onclick` attribute. No framework dependency.

---

## Error handling

- Missing first/last name on create: HTTP 400 "first name and last name are required"
- Missing/short password on change: HTTP 400 "password must be at least 8 characters"
- Store errors: HTTP 500 "internal error" (consistent with existing handlers)
- Superadmin accounts: password change is permitted (admin manages all accounts)

## Testing

- `auth/store_test.go`: add cases for `ChangePassword` (success, user not found)
- `auth/store_test.go`: update `CreateUser` calls to pass first/last name
- `api/auth_test.go` / `api/tenants_test.go`: update any user fixtures to include first/last name
