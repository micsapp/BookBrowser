var CACHE_PREFIX = "bookbrowser-pwa-";
var CACHE_NAME = CACHE_PREFIX + "v11";
var APP_SHELL = [
    "/manifest.webmanifest",
    "/static/offline.html",
    "/static/normalize.css",
    "/static/style.css",
    "/static/about.js",
    "/static/help.js",
    "/static/reader-tools.css",
    "/static/reader-tools.js",
    "/static/pwa.js",
    "/static/icons/icon-192.png",
    "/static/icons/icon-512.png",
    "/static/icons/apple-touch-icon.png",
    "/static/icons/favicon-32.png"
];

self.addEventListener("install", function (event) {
    event.waitUntil(
        caches.open(CACHE_NAME).then(function (cache) {
            return cache.addAll(APP_SHELL);
        }).then(function () {
            return self.skipWaiting();
        })
    );
});

self.addEventListener("activate", function (event) {
    event.waitUntil(
        caches.keys().then(function (keys) {
            return Promise.all(keys.map(function (key) {
                if (key.indexOf(CACHE_PREFIX) === 0 && key !== CACHE_NAME) {
                    return caches.delete(key);
                }
                return Promise.resolve(false);
            }));
        }).then(function () {
            return self.clients.claim();
        })
    );
});

function shouldBypass(url) {
    return url.pathname.indexOf("/tts/") === 0 ||
        url.pathname.indexOf("/download") === 0 ||
        url.pathname.indexOf("/covers/") === 0 ||
        url.pathname.indexOf("/api/") === 0;
}

function cacheStaticAsset(request) {
    return caches.match(request).then(function (cached) {
        var network = fetch(request).then(function (response) {
            if (response && response.ok) {
                var copy = response.clone();
                caches.open(CACHE_NAME).then(function (cache) {
                    cache.put(request, copy);
                });
            }
            return response;
        }).catch(function (error) {
            if (cached) return cached;
            throw error;
        });

        return cached || network;
    });
}

function networkFirst(request) {
    return fetch(request).then(function (response) {
        if (response && response.ok) {
            var copy = response.clone();
            caches.open(CACHE_NAME).then(function (cache) {
                cache.put(request, copy);
            });
        }
        return response;
    }).catch(function (error) {
        return caches.match(request).then(function (cached) {
            if (cached) return cached;
            throw error;
        });
    });
}

self.addEventListener("fetch", function (event) {
    var request = event.request;
    if (request.method !== "GET") return;

    var url = new URL(request.url);
    if (url.origin !== self.location.origin || shouldBypass(url)) return;

    if (request.mode === "navigate") {
        event.respondWith(
            fetch(request).catch(function () {
                return caches.match("/static/offline.html");
            })
        );
        return;
    }

    if (url.pathname === "/manifest.webmanifest") {
        event.respondWith(networkFirst(request));
        return;
    }

    if (url.pathname.indexOf("/static/") === 0) {
        event.respondWith(cacheStaticAsset(request));
    }
});
