# Form Button Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Disable create/save buttons in inline forms and edit-row forms until all required inputs have values, re-enabling reactively on every keystroke and after HTMX swaps.

**Architecture:** A single 15-line vanilla JS file (`form-enable.js`) uses event delegation — one `input` listener on `document` plus an `htmx:afterSettle` hook — to set `button.disabled` on `.btn-primary` buttons inside `.inline-form` and `.edit-row-form` containers. The existing `.btn:disabled { opacity: 0.5; cursor: not-allowed; }` CSS rule handles the visual state. The script is loaded via a `defer` script tag added to `base.html`. No Go code changes, no template structural changes.

**Tech Stack:** Vanilla JS (ES2015+), HTMX 2 custom events, Go `//go:embed` static file serving.

---

## File Map

| File | Change |
|------|--------|
| `internal/web/static/form-enable.js` | Create (new file, ~15 lines) |
| `internal/web/templates/base.html` | Add `<script>` tag before `</body>` (line 63) |

---

### Task 1: Create `form-enable.js`

**Files:**
- Create: `internal/web/static/form-enable.js`

- [ ] **Step 1: Create the file with this exact content**

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

Write this to `internal/web/static/form-enable.js`.

- [ ] **Step 2: Verify the file is picked up by the Go embed**

The `internal/web/web.go` file uses `//go:embed static` to embed the static directory at compile time. Building the package confirms the new file is embedded without errors:

```bash
go build ./internal/web/...
```

Expected: exits 0 with no output.

- [ ] **Step 3: Run existing web tests to confirm no regression**

```bash
go test ./internal/web/... -v 2>&1 | tail -20
```

Expected: all tests PASS, `ok tinydm/internal/web`.

- [ ] **Step 4: Commit**

```bash
git add internal/web/static/form-enable.js
git commit -m "feat(web): add form-enable.js to gate create/save buttons on required inputs"
```

---

### Task 2: Wire `form-enable.js` into `base.html`

**Files:**
- Modify: `internal/web/templates/base.html:62-63`

- [ ] **Step 1: Add the script tag to `base.html`**

Find this section near the bottom of `base.html` (around line 62):

```html
</div>
</body>
```

Replace with:

```html
</div>
<script src="/app/static/form-enable.js" defer></script>
</body>
```

The full closing section of `base.html` (lines 60–65) should now read:

```html
  </div>

</div>
<script src="/app/static/form-enable.js" defer></script>
</body>
</html>
{{end}}
```

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v 2>&1 | tail -20
```

Expected: all tests PASS. (The Go tests exercise template rendering; the new `<script>` tag has no effect on Go-level assertions.)

- [ ] **Step 3: Run the full test suite**

```bash
go test ./... 2>&1 | tail -10
```

Expected: all packages PASS.

- [ ] **Step 4: Manual smoke test**

Start the server locally and verify:

1. **Projects page** — the "+ Add Project" button is greyed out (opacity 0.5, cursor not-allowed). Type any text in the Name field → button becomes active. Clear the field → button greys out again.

2. **Users page** — the "+ Add User" button is greyed out. Fill in Username, First Name, Last Name, Email, and Password → button becomes active. Clear any one field → button greys out.

3. **Buckets page** — the "+ Add Bucket" button is greyed out. Type a bucket name → button activates. Click Edit on an existing bucket → the Save button starts active (name is pre-filled). Clear the name → Save greys out. Re-type a name → Save activates.

4. **Documents page** — the Save button in an open document-edit row starts active (name pre-filled). Clear the name → Save greys out.

- [ ] **Step 5: Commit**

```bash
git add internal/web/templates/base.html
git commit -m "feat(web): load form-enable.js in base.html to activate button gating"
```
