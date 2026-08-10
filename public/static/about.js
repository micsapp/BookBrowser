(function () {
    'use strict';
    var dialog = document.querySelector('[data-about-dialog]');
    if (!dialog) return;
    function open() {
        if (typeof dialog.showModal === 'function') dialog.showModal();
        else dialog.setAttribute('open', '');
    }
    function close() {
        if (typeof dialog.close === 'function') dialog.close();
        else dialog.removeAttribute('open');
    }
    document.querySelectorAll('[data-about-open]').forEach(function (button) { button.addEventListener('click', open); });
    dialog.querySelectorAll('[data-about-close]').forEach(function (button) { button.addEventListener('click', close); });
    dialog.addEventListener('click', function (event) { if (event.target === dialog) close(); });
}());
