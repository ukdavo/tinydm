# Form Button Gating Design Spec

**Date:** 2026-05-11
**Status:** Approved
**Scope:** Disable inline-form and edit-row-form submit buttons until all required inputs have values.

---

## Background

Create and Save buttons in TinyDM's web UI are always enabled, even when required fields are empty. Clicking them with empty fields submits to the server and returns a validation error. Gating the buttons client-side improves UX by making the ready/not-ready state visible at a glance.

---

## Goals

- Disable `.btn-primary` buttons inside `.inline-form` and `.edit-row-form` containers when any `input[required]` in the same container is empty.
- Re-enable when all required inputs have non-empty (trimmed) values.
- Handle both static DOM (page load) and HTMX-injected content (edit rows appear after HTMX swap).

## Non-Goals

- No changes to server-side validation.
- No gating of Delete, Cancel, or ghost buttons.
- No gating of the file upload button or document property/tag forms.
- No changes to form templates beyond adding a `<script>` tag to `base.html`.

---

## Affected Forms

| Page | Container class | Button gated | Required inputs |
|------|----------------|-------------|----------------|
| Projects | `.inline-form` | + Add Project | `name` |
| Buckets | `.inline-form` | + Add Bucket | `name` |
| Buckets | `.edit-row-form` | Save (bucket edit) | `name` |
| Documents | `.edit-row-form` | Save (document edit) | `name` |
| Users | `.inline-form` | + Add User | `username`, `first_name`, `last_name`, `email`, `password` |
| API Keys | `.inline-form` | + Add API Key | `name` |

---

## Implementation

### New file: `internal/web/static/form-enable.js`

```js
function checkForm(container) {
  const btn = container.querySelector('button.btn-primary');
  if (!btn) return;
  const required = [...container.querySelectorAll('input[required]')];
  if (required.length === 0) return;
  btn.disabled = required.some(i => i.value.trim() === '');
}

function initForms(root = document) {
  root.querySelectorAll('.inline-form, .edit-row-form').forEach(checkForm);
}

document.addEventListener('DOMContentLoaded', initForms);
document.addEventListener('input', e => {
  if (!e.target.matches('input[required]')) return;
  const container = e.target.closest('.inline-form, .edit-row-form');
  if (container) checkForm(container);
});
document.addEventListener('htmx:afterSettle', e => initForms(e.detail.elt));
```

**Behaviour:**

- `DOMContentLoaded`: disables all create buttons (inputs are empty on load); edit-row Save buttons start enabled because their inputs have pre-filled `value` attributes.
- `input` event delegation: re-checks on every keystroke without attaching per-element listeners.
- `htmx:afterSettle`: re-checks newly injected elements (e.g., bucket-edit-row swapped in via HTMX) scoped to `e.detail.elt` to avoid re-scanning the full document on every swap.
- `value.trim()`: a value of only whitespace is treated as empty.

### Template change: `internal/web/templates/base.html`

Add before `</body>`:

```html
<script src="/app/static/form-enable.js" defer></script>
```

### CSS

No changes needed. The existing rule handles the visual state:

```css
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
```

---

## Testing

- `go test ./internal/web/...` must pass (no Go-level changes beyond serving the static file).
- Manual smoke test:
  - Open Projects page → "+ Add Project" button is greyed out.
  - Type in Name field → button becomes active.
  - Clear Name field → button greys out again.
  - Open Buckets page, click Edit on a bucket → Save button starts active (name pre-filled); clear name → Save greys out.
  - Open Users page → "+ Add User" button greyed out; fill all 5 fields → button activates; clear any one → greys out again.

---

## Affected Files

| File | Change |
|------|--------|
| `internal/web/static/form-enable.js` | Create (new file, ~15 lines) |
| `internal/web/templates/base.html` | Add `<script>` tag before `</body>` |
