(function () {
    'use strict';
    var aboutDialog = document.querySelector('[data-about-dialog]');
    var copyrightDialog = document.querySelector('[data-copyright-dialog]');

    function show(dialog) {
        if (!dialog) return;
        if (typeof dialog.showModal === 'function') dialog.showModal();
        else dialog.setAttribute('open', '');
    }
    function close(dialog) {
        if (!dialog) return;
        if (typeof dialog.close === 'function') dialog.close();
        else dialog.removeAttribute('open');
    }
    function t(key, fallback) {
        if (window.SiteI18N && window.SiteI18N.t) {
            var value = window.SiteI18N.t(key);
            if (value !== key) return value;
        }
        return fallback;
    }
    function showToast(message) {
        var toast = document.querySelector('[data-copyright-toast]');
        if (!toast) {
            toast = document.createElement('div');
            toast.className = 'copyright-toast';
            toast.setAttribute('data-copyright-toast', '');
            toast.setAttribute('role', 'status');
            toast.setAttribute('aria-live', 'polite');
            document.body.appendChild(toast);
        }
        toast.textContent = message;
        toast.classList.add('visible');
        window.clearTimeout(toast._hideTimer);
        toast._hideTimer = window.setTimeout(function () { toast.classList.remove('visible'); }, 5000);
    }

    if (aboutDialog) {
        document.querySelectorAll('[data-about-open]').forEach(function (button) {
            button.addEventListener('click', function () { show(aboutDialog); });
        });
        aboutDialog.querySelectorAll('[data-about-close]').forEach(function (button) {
            button.addEventListener('click', function () { close(aboutDialog); });
        });
        aboutDialog.addEventListener('click', function (event) {
            if (event.target === aboutDialog) close(aboutDialog);
        });
    }

    if (!copyrightDialog) return;
    var form = copyrightDialog.querySelector('[data-copyright-form]');
    var status = copyrightDialog.querySelector('[data-copyright-status]');
    var submit = form.querySelector('[type="submit"]');
    var submitting = false;

    function setStatus(kind, message) {
        status.className = 'copyright-form-status copyright-form-status-' + kind;
        status.textContent = message;
    }
    function openCopyrightDialog() {
        close(aboutDialog);
        form.elements.source_url.value = window.location.href;
        setStatus('', '');
        show(copyrightDialog);
        if (window.SiteI18N && window.SiteI18N.apply) window.SiteI18N.apply();
        var first = form.querySelector('input[name="claimant_name"]');
        if (first) first.focus();
    }

    document.querySelectorAll('[data-copyright-open]').forEach(function (button) {
        button.addEventListener('click', openCopyrightDialog);
    });
    copyrightDialog.querySelectorAll('[data-copyright-close]').forEach(function (button) {
        button.addEventListener('click', function () { if (!submitting) close(copyrightDialog); });
    });
    copyrightDialog.addEventListener('click', function (event) {
        if (event.target === copyrightDialog && !submitting) close(copyrightDialog);
    });

    form.addEventListener('submit', function (event) {
        event.preventDefault();
        if (submitting) return;
        submitting = true;
        submit.disabled = true;
        setStatus('loading', t('copyright_submitting', 'Sending…'));
        fetch('/api/copyright-claims', {
            method: 'POST',
            headers: { 'X-Requested-With': 'fetch', 'Accept': 'application/json' },
            body: new FormData(form)
        }).then(function (response) {
            return response.json().then(function (data) { return { ok: response.ok, data: data }; });
        }).then(function (result) {
            submitting = false;
            submit.disabled = false;
            if (!result.ok) {
                setStatus('error', t('copyright_submit_failed', 'The report could not be sent. Check the information and try again.'));
                return;
            }
            form.reset();
            form.elements.source_url.value = window.location.href;
            close(copyrightDialog);
            showToast(t('copyright_submitted', 'Report received. An administrator will review it.'));
        }).catch(function () {
            submitting = false;
            submit.disabled = false;
            setStatus('error', t('copyright_submit_failed', 'The report could not be sent. Check the information and try again.'));
        });
    });
}());
