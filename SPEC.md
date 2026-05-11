# TinyDM — Project Specification

## Description

TinyDM is a simple document management application similar in concept to Documentum, Alfresco, S3, etc.

## Goals

- Provides basic document management features
- Performant
- Secure
- Small footprint
- Easy to deploy and run
- Supports all main operating systems (macOS, Linux & Windows)
- Single-tenant; all users share one system namespace

## Repository Structure

The logical repository structure is hierarchical:

```
Project
└── Bucket
    └── Document
```

## Initial Features

### Document Support
- Supports common file formats (e.g. MS Office, PDF, multimedia, etc.)
- Documents can be added, edited, deleted & searched
- Documents are automatically versioned on update

### Metadata
Documents can be associated with metadata:
- **System properties** — file size, MIME type, checksum, etc.
- **Extracted metadata** — e.g. EXIF, IMAP, etc.
- **Tags**
- **Custom properties** — definable at runtime

### Audit
All repository events are tracked via audit log.

### REST API
Full REST API for all operations.

### Authorisation

**Principals:**
- Users
- Groups
- API Keys

**User Types:**
- Admin — unrestricted access; bypasses rights checks
- User — access governed by rights grants

**User Rights:**
- Create
- Read
- Update
- Delete

### Web Client
Simple web client for administrators.

### Authentication Methods
- Basic authentication (username + password)
- JWT Bearer token (issued via `POST /api/v1/auth/login`)
- API key (opaque token, scoped to user)

## Tech Stack

| Concern | Choice | Rationale |
|---|---|---|
| Language | Go | Single binary, cross-platform, small footprint, performant |
| HTTP Framework | Chi | Lightweight, idiomatic, minimal overhead |
| Database | SQLite (default) / PostgreSQL (optional) | Zero-dependency default; switchable via storage interface |
| DB Access | sqlc | Type-safe SQL, no ORM magic |
| File Storage | Local filesystem, S3, Azure Blob, GCS (abstracted) | Content-addressed storage; backend selected via `TINYDM_STORAGE_BACKEND` |
| Admin UI | HTMX + Go `html/template` | No build step, ships embedded in binary |
| Auth | bcrypt + JWT + opaque API tokens | Standard, secure, no external dependencies |
| Packaging | Single binary + Docker | Easy deployment; ~20–30MB Docker image |

## Possible Future Features

- Document locking
- Full text indexing
- OAuth
- Associations / relations
