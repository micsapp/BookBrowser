(function () {
    'use strict';
    var LANG_KEY = 'micsReader:lang';
    var dialog = document.querySelector('[data-help-dialog]');
    if (!dialog) return;
    var body = dialog.querySelector('[data-help-body]');
    var loaded = false;

    function t(key, fallback) {
        if (window.SiteI18N && window.SiteI18N.t) {
            var value = window.SiteI18N.t(key);
            if (value !== key) return value;
        }
        return fallback;
    }

    function currentLang() {
        try {
            var stored = window.localStorage.getItem(LANG_KEY);
            if (stored === 'en' || stored === 'zh') return stored;
        } catch (_) {}
        var nav = (navigator.language || navigator.userLanguage || '').split('-')[0];
        return nav === 'zh' ? 'zh' : 'en';
    }

    function load() {
        loaded = true;
        body.innerHTML = '<p>' + t('help_loading', 'Loading the user guide…') + '</p>';
        fetch('/api/help?lang=' + encodeURIComponent(currentLang()), { headers: { 'Accept': 'application/json' } })
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
                body.innerHTML = '<p>' + t('help_error', 'The user guide could not be loaded. Check your connection and try again.') + '</p>';
            });
    }

    function open() {
        if (typeof dialog.showModal === 'function') dialog.showModal();
        else dialog.setAttribute('open', '');
        if (!loaded && body) load();
    }
    function close() {
        if (typeof dialog.close === 'function') dialog.close();
        else dialog.removeAttribute('open');
    }
    document.querySelectorAll('[data-help-open]').forEach(function (button) { button.addEventListener('click', open); });
    dialog.querySelectorAll('[data-help-close]').forEach(function (button) { button.addEventListener('click', close); });
    dialog.addEventListener('click', function (event) { if (event.target === dialog) close(); });
    dialog.addEventListener('cancel', function (event) { event.preventDefault(); close(); });
    window.addEventListener('micslangchange', function () {
        if (loaded && body) load();
    });
}());
