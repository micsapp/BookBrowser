importScripts("/static/offline-store.js", "/static/offline-assets.js");
var CACHE_PREFIX = "bookbrowser-pwa-";
var CACHE_NAME = BookBrowserOfflineAssets.cacheName;
var APP_SHELL = [
    "/manifest.webmanifest",
    "/static/offline.html",
    "/static/offline-library.html",
    "/static/offline.css",
    "/static/offline.js",
    "/static/offline-store.js",
    "/static/offline-assets.js",
    "/static/nocover.jpg",
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
            // An update must prepare readers for saved books before deleting the old shell.
            return BookBrowserOffline.list().catch(function () { return []; }).then(function (books) {
                var assets = APP_SHELL.slice();
                books.forEach(function (book) {
                    // Include in-flight downloads: they may finish during activation.
                    assets = assets.concat(BookBrowserOfflineAssets[book.format] || []);
                });
                return cache.addAll(Array.from(new Set(assets)));
            });
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

async function savedBookResponse(request, id, format) {
    var response = await BookBrowserOffline.savedResponse(id, format);
    if (!response) return fetch(request);
    var range = request.headers.get("Range");
    if (!range) return response;
    var blob = await response.blob(), match = /^bytes=(\d*)-(\d*)$/.exec(range);
    var start = match && match[1] ? Number(match[1]) : 0;
    var end = match && match[2] ? Number(match[2]) : blob.size - 1;
    if (match && !match[1] && match[2]) { start = Math.max(0, blob.size - Number(match[2])); end = blob.size - 1; }
    if (!match || (!match[1] && !match[2]) || !Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start > end || start >= blob.size) {
        return new Response(null, { status: 416, headers: { "Content-Range": "bytes */" + blob.size } });
    }
    end = Math.min(end, blob.size - 1);
    var headers = new Headers(response.headers);
    headers.set("Content-Range", "bytes " + start + "-" + end + "/" + blob.size);
    headers.set("Content-Length", String(end - start + 1));
    return new Response(blob.slice(start, end + 1), { status: 206, headers: headers });
}

async function offlineNavigation(url) {
    var read = /^\/read\/([a-f0-9]{10})\/?$/.exec(url.pathname);
    if (read) {
        var books = await BookBrowserOffline.list().catch(function () { return []; });
        var book = books.find(function (item) { return item.id === read[1] && item.status === "ready"; });
        if (book) return Response.redirect(new URL(BookBrowserOffline.readerURL(book), self.location.origin).href);
    }
    if (url.pathname.indexOf("/static/reader/") === 0) {
        var path = url.pathname === "/static/reader/epub" ? "/static/reader/epub/" : url.pathname;
        var reader = await caches.match(path);
        if (reader) return reader;
    }
    return await caches.match("/static/offline-library.html") || await caches.match("/static/offline.html");
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
    var url = new URL(request.url);
    if (url.origin === self.location.origin && url.pathname === "/logout" && request.method === "POST") {
        event.respondWith(fetch(request).then(async function (response) {
            // An invalid CSRF submission must not erase local downloads.
            if (response.ok || response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400)) await BookBrowserOffline.clear(true);
            return response;
        }));
        return;
    }
    if (request.method !== "GET" || url.origin !== self.location.origin) return;
    var book = /^\/download\/([a-f0-9]{10})\.(epub|pdf)$/.exec(url.pathname);
    if (book && !request.headers.has("X-BookBrowser-Download")) {
        event.respondWith(savedBookResponse(request, book[1], book[2]));
        return;
    }
    var cover = /^\/offline-cover\/([a-f0-9]{10})$/.exec(url.pathname);
    if (cover) {
        event.respondWith(BookBrowserOffline.savedResponse(cover[1], null, true).then(function (response) { return response || caches.match("/static/nocover.jpg"); }));
        return;
    }
    if (url.pathname === "/offline") {
        event.respondWith(caches.match("/static/offline-library.html").then(function (response) { return response || fetch(request); }));
        return;
    }
    if (url.origin !== self.location.origin || shouldBypass(url)) return;

    if (request.mode === "navigate") {
        if (url.pathname.indexOf("/static/reader/") === 0) {
            event.respondWith(caches.match(url.pathname).then(function (reader) {
                return reader || fetch(request).catch(function () { return offlineNavigation(url); });
            }));
            return;
        }
        event.respondWith(
            fetch(request).catch(function () {
                return offlineNavigation(url);
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
