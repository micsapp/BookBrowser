(function () {
    "use strict";

    if (!("serviceWorker" in navigator)) return;

    window.addEventListener("load", function () {
        navigator.serviceWorker.getRegistrations().then(function (registrations) {
            registrations.forEach(function (registration) {
                var worker = registration.active || registration.waiting || registration.installing;
                if (worker && worker.scriptURL.indexOf("/static/reader/epub/sw.js") !== -1) {
                    registration.unregister();
                }
            });
        }).catch(function () {
            // A stale reader-specific registration is harmless if it cannot be inspected.
        });

        navigator.serviceWorker.register("/sw.js", { scope: "/" }).then(function (registration) {
            registration.update();
        }).catch(function (error) {
            if (window.console && console.warn) {
                console.warn("BookBrowser PWA registration failed:", error);
            }
        });
    });
}());
