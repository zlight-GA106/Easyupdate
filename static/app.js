document.querySelectorAll('form').forEach(form => {
  form.addEventListener('submit', event => {
    if (!form.checkValidity()) return;
    const button = event.submitter;
    if (button) {
      button.classList.add('busy');
      // Keep the submitter enabled so its name/value reaches the server.
      button.setAttribute('aria-busy', 'true');
    }
    const status = form.querySelector('.upload-status');
    if (status) status.textContent = '正在上传…';
    const progress = form.querySelector('[data-upload-progress]');
    if (progress) progress.hidden = false;
  });
});

window.addEventListener('pageshow', () => {
  document.querySelectorAll('button.busy').forEach(button => {
    button.classList.remove('busy');
    button.removeAttribute('aria-busy');
  });
  document.querySelectorAll('[data-upload-progress]').forEach(progress => {
    progress.hidden = true;
  });
  document.querySelectorAll('.upload-status').forEach(status => {
    status.textContent = '';
  });
});
