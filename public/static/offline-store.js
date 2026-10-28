/* Shared by the page and service worker. Downloads never use the disposable PWA cache. */
(function (root) {
    "use strict";
    var DB_NAME = "bookbrowser-offline-v1";
    var CACHE_PREFIX = "bookbrowser-downloads-v1-";
    var DEFAULT_LIMIT = 500 * 1024 * 1024;
    var opening;

    function supported() { return !!(root.indexedDB && root.caches); }
    function request(req) {
        return new Promise(function (resolve, reject) {
            req.onsuccess = function () { resolve(req.result); };
            req.onerror = function () { reject(req.error); };
        });
    }
    function open() {
        if (!supported()) return Promise.reject(new Error("Offline storage is unavailable. Use HTTPS and allow this site to store data."));
        if (!opening) opening = new Promise(function (resolve, reject) {
            var req = indexedDB.open(DB_NAME, 1);
            req.onupgradeneeded = function () {
                req.result.createObjectStore("books", { keyPath: "key" });
                req.result.createObjectStore("settings", { keyPath: "key" });
            };
            req.onsuccess = function () {
                req.result.onversionchange = function () { req.result.close(); opening = null; };
                resolve(req.result);
            };
            req.onerror = function () { opening = null; reject(req.error); };
        });
        return opening;
    }
    async function transaction(stores, mode, run) {
        var db = await open();
        var tx = db.transaction(stores, mode);
        var done = new Promise(function (resolve, reject) {
            tx.oncomplete = resolve;
            tx.onabort = tx.onerror = function () { reject(tx.error || new Error("Storage operation failed.")); };
        });
        try {
            var value = await run(tx);
            await done;
            return value;
        } catch (error) {
            try { tx.abort(); } catch (_) {}
            await done.catch(function () {});
            throw error;
        }
    }
    function token() { return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2); }
    function setting(key) {
        return transaction(["settings"], "readonly", function (tx) { return request(tx.objectStore("settings").get(key)); });
    }
    async function active() { return await setting("active") || { owner: "", epoch: "" }; }
    function changed() {
        if (root.dispatchEvent && root.CustomEvent) root.dispatchEvent(new CustomEvent("bookbrowserofflinechange"));
        try { if (root.localStorage) root.localStorage.setItem("bookbrowserOffline:changed", token()); } catch (_) {}
    }
    async function reset(owner, onlyIfDifferent) {
        var previous = await transaction(["books", "settings"], "readwrite", async function (tx) {
            var store = tx.objectStore("settings");
            var old = await request(store.get("active"));
            if (onlyIfDifferent && old && old.owner === owner) return null;
            store.put({ key: "active", owner: owner, epoch: token() });
            tx.objectStore("books").clear();
            return old;
        });
        // Invalidate metadata first: a download still running in another tab cannot commit.
        if (previous && previous.epoch) await caches.delete(CACHE_PREFIX + previous.epoch);
        changed();
    }
    async function selectOwner(owner) {
        if (!owner) throw new Error("Sign in before saving books offline.");
        await reset(owner, true);
    }
    async function clear(signOut) { var state = await active(); await reset(signOut ? "" : state.owner, false); }
    async function list() {
        return transaction(["books", "settings"], "readonly", async function (tx) {
            var values = await Promise.all([request(tx.objectStore("settings").get("active")), request(tx.objectStore("books").getAll())]);
            var state = values[0];
            return state ? values[1].filter(function (book) { return book.owner === state.owner && book.epoch === state.epoch; }) : [];
        });
    }
    async function usage() {
        var values = await Promise.all([list(), setting("limit")]);
        return { bytes: values[0].reduce(function (total, book) { return total + book.bytes; }, 0), limit: values[1] ? values[1].value : DEFAULT_LIMIT };
    }
    async function setLimit(bytes) {
        if (!Number.isSafeInteger(bytes) || bytes < 1024 * 1024) throw new Error("Choose a storage limit of at least 1 MB.");
        await transaction(["books", "settings"], "readwrite", async function (tx) {
            var books = await request(tx.objectStore("books").getAll());
            if (books.reduce(function (n, book) { return n + book.bytes; }, 0) > bytes) throw new Error("Remove some downloads before lowering the storage limit.");
            tx.objectStore("settings").put({ key: "limit", value: bytes });
        });
        changed();
    }
    async function reserve(book) {
        if (!/^[a-f0-9]{10}$/.test(book.id) || !/^(epub|pdf)$/.test(book.format) || !Number.isSafeInteger(book.bytes) || book.bytes <= 0) throw new Error("This book cannot be saved offline.");
        return transaction(["books", "settings"], "readwrite", async function (tx) {
            var store = tx.objectStore("books"), settings = tx.objectStore("settings");
            var values = await Promise.all([request(settings.get("active")), request(settings.get("limit")), request(store.getAll())]);
            var state = values[0];
            if (!state || !state.owner) throw new Error("Sign in before saving books offline.");
            var key = state.owner + ":" + book.id;
            if (values[2].some(function (item) { return item.key === key; })) throw new Error("This book is already saved or downloading. Remove the local copy to download it again.");
            var limit = values[1] ? values[1].value : DEFAULT_LIMIT;
            if (values[2].reduce(function (n, item) { return n + item.bytes; }, 0) + book.bytes > limit) throw new Error("The offline storage limit would be exceeded. Remove a download or increase the limit in Offline Books.");
            var item = Object.assign({}, book, { key: key, owner: state.owner, epoch: state.epoch, status: "downloading", token: token(), savedAt: Date.now() });
            item.cacheKey = "/.bookbrowser-offline/" + item.token + "/" + book.id + "." + book.format;
            store.put(item);
            return item;
        });
    }
    async function discard(item) {
        await transaction(["books"], "readwrite", async function (tx) {
            var store = tx.objectStore("books"), current = await request(store.get(item.key));
            if (current && current.token === item.token) store.delete(item.key);
        });
        var cache = await caches.open(CACHE_PREFIX + item.epoch);
        await Promise.all([cache.delete(item.cacheKey), cache.delete(item.cacheKey + "/cover")]);
        changed();
    }
    async function remove(id) {
        var books = await list(), book = books.find(function (item) { return item.id === id; });
        if (book) await discard(book);
    }
    async function savedResponse(id, format, cover) {
        var state = await active();
        if (!state.owner) return null;
        var item = await transaction(["books"], "readonly", function (tx) { return request(tx.objectStore("books").get(state.owner + ":" + id)); });
        if (!item || item.epoch !== state.epoch || item.status !== "ready" || (format && item.format !== format)) return null;
        var cache = await caches.open(CACHE_PREFIX + state.epoch);
        var response = await cache.match(item.cacheKey + (cover ? "/cover" : ""));
        if ((await active()).epoch !== state.epoch) return null;
        return response || null;
    }
    async function save(book, options) {
        options = options || {};
        var item, controller = new AbortController(), timer;
        function abort() { controller.abort(); }
        function touch() { clearTimeout(timer); timer = setTimeout(abort, 60000); }
        if (options.signal) {
            if (options.signal.aborted) abort();
            options.signal.addEventListener("abort", abort);
        }
        try {
            var estimate = root.navigator.storage && root.navigator.storage.estimate ? await root.navigator.storage.estimate() : null;
            if (estimate && estimate.quota && estimate.quota - estimate.usage < book.bytes + 12 * 1024 * 1024) throw new Error("There is not enough browser storage for this book and its reader.");
            item = await reserve(book);
            changed();
            touch();
            if (options.prepare) await options.prepare(book.format, controller.signal);
            touch();
            var response = await fetch("/download/" + book.id + "." + book.format, { credentials: "same-origin", cache: "no-store", redirect: "error", signal: controller.signal, headers: { "X-BookBrowser-Download": "1" } });
            if (response.status !== 200 || /text\/html/i.test(response.headers.get("Content-Type") || "")) throw new Error("The complete book could not be downloaded. Reconnect or sign in and try again.");
            var cache = await caches.open(CACHE_PREFIX + item.epoch);
            var headers = { "Content-Type": book.format === "pdf" ? "application/pdf" : "application/epub+zip", "Content-Length": String(book.bytes), "Cache-Control": "no-store", "Accept-Ranges": "bytes" };
            headers["Content-Disposition"] = response.headers.get("Content-Disposition") || 'attachment; filename="' + book.id + '.' + book.format + '"';
            function validateMagic(magic) {
                if ((book.format === "epub" && (magic[0] !== 80 || magic[1] !== 75)) || (book.format === "pdf" && String.fromCharCode.apply(null, magic) !== "%PDF-")) throw new Error("The downloaded file is not a valid book.");
            }
            if (response.body && response.body.getReader && root.ReadableStream) {
                // Stream into the cache rather than retaining a large PDF in RAM.
                var reader = response.body.getReader(), received = 0, prefix = [], downloadError;
                var stream = new ReadableStream({
                    async pull(output) {
                        try {
                            touch();
                            var result = await reader.read();
                            if (controller.signal.aborted) throw new DOMException("Download cancelled.", "AbortError");
                            if (result.done) {
                                if (received !== book.bytes) throw new Error("The download was incomplete or the book changed. Please try again.");
                                validateMagic(prefix);
                                output.close();
                                return;
                            }
                            received += result.value.byteLength;
                            if (received > book.bytes) throw new Error("The book has changed on the server. Reload its details before downloading again.");
                            for (var n = 0; n < result.value.length && prefix.length < 5; n++) prefix.push(result.value[n]);
                            if (prefix.length === 5) validateMagic(prefix);
                            if (options.onProgress) options.onProgress(received, book.bytes);
                            output.enqueue(result.value);
                        } catch (error) {
                            downloadError = error;
                            output.error(error);
                            await reader.cancel().catch(function () {});
                        }
                    },
                    cancel: function () { return reader.cancel(); }
                });
                try { await cache.put(item.cacheKey, new Response(stream, { headers: headers })); }
                catch (error) { throw downloadError || error; }
            } else {
                var blob = await response.blob();
                if (blob.size !== book.bytes) throw new Error("The download was incomplete or the book changed. Please try again.");
                validateMagic(new Uint8Array(await blob.slice(0, 5).arrayBuffer()));
                await cache.put(item.cacheKey, new Response(blob, { headers: headers }));
            }
            if (book.cover) {
                try {
                    touch();
                    var cover = await fetch("/covers/" + book.id + "_thumb.jpg", { signal: controller.signal, cache: "no-store" });
                    if (cover.ok && /^image\//i.test(cover.headers.get("Content-Type") || "")) await cache.put(item.cacheKey + "/cover", cover);
                } catch (_) { /* A missing thumbnail does not make the book unreadable. */ }
            }
            if (controller.signal.aborted) throw new DOMException("Download cancelled.", "AbortError");
            await transaction(["books", "settings"], "readwrite", async function (tx) {
                var store = tx.objectStore("books");
                var values = await Promise.all([request(tx.objectStore("settings").get("active")), request(store.get(item.key))]);
                if (!values[0] || values[0].epoch !== item.epoch || !values[1] || values[1].token !== item.token) throw new Error("Download cancelled because the account or offline library changed.");
                item.status = "ready";
                store.put(item);
            });
            changed();
            return item;
        } catch (error) {
            if (item) await discard(item).catch(function () {});
            if (error.name === "QuotaExceededError") throw new Error("Browser storage is full. Remove some downloads and try again.");
            throw error;
        } finally {
            clearTimeout(timer);
            if (options.signal) options.signal.removeEventListener("abort", abort);
        }
    }
    function readerURL(book) {
        return book.format === "pdf" ? "/static/reader/pdf/web/viewer.html?file=/download/" + book.id + ".pdf" : "/static/reader/epub/#!/download/" + book.id + ".epub";
    }
    root.BookBrowserOffline = { supported: supported, active: active, selectOwner: selectOwner, list: list, usage: usage, setLimit: setLimit, save: save, remove: remove, clear: clear, savedResponse: savedResponse, readerURL: readerURL, cachePrefix: CACHE_PREFIX };
}(typeof self !== "undefined" ? self : window));
