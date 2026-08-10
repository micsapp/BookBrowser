(function () {
    'use strict';
    var dialog = document.querySelector('[data-help-dialog]');
    if (!dialog) return;
    var body = dialog.querySelector('[data-help-body]');
    var loaded = false;
    function open() {
        if (typeof dialog.showModal === 'function') dialog.showModal();
        else dialog.setAttribute('open', '');
        if (!loaded && body) {
            loaded = true;
            body.innerHTML = '<p>Loading the user guide…</p>';
            fetch('/api/help', { headers: { 'Accept': 'application/json' } })
                .then(function (resp) {
                    if (!resp.ok) throw new Error('help request failed');
                    return resp.json();
                })
                .then(function (data) {
                    var titleEl = dialog.querySelector('[data-help-title]');
                    if (titleEl && data.title) titleEl.textContent = data.title;
                    if (data.document) body.innerHTML = data.document;
                })
                .catch(function () {
                    body.innerHTML = '<p>The user guide could not be loaded. Check your connection and try again.</p>';
                });
        }
    }
    function close() {
        if (typeof dialog.close === 'function') dialog.close();
        else dialog.removeAttribute('open');
    }
    document.querySelectorAll('[data-help-open]').forEach(function (button) { button.addEventListener('click', open); });
    dialog.querySelectorAll('[data-help-close]').forEach(function (button) { button.addEventListener('click', close); });
    dialog.addEventListener('click', function (event) { if (event.target === dialog) close(); });
    dialog.addEventListener('cancel', function (event) { event.preventDefault(); close(); });
}());