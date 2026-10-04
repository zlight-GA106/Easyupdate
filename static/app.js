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
  });
});
