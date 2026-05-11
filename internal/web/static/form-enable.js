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
