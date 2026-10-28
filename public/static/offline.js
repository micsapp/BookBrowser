(function () {
    "use strict";
    var store = window.BookBrowserOffline;
    if (!store) return;
    var page = document.querySelector("[data-offline-library]"), buttons = document.querySelectorAll("[data-offline-save]"), controllers = new Map();
    var strings = {
        en: { library: "Library", device: "On this device", heading: "Offline Books", intro: "Read downloaded books with or without a connection. Read aloud prefers online voices and falls back to an installed local voice.", storage: "Storage", limit: "Book storage limit (MB)", apply: "Apply", storage_help: "Book files count toward this limit. Readers use a little extra space shared by all books. No narration files are saved. Downloads are kept until you remove them, sign out, or clear site data.", keep: "Request persistent storage", clear: "Remove all downloads", empty: "No downloaded books yet. Connect, open a book in the library, and select Save offline.", save: "Save offline", remove: "Remove download", read: "Read", cancel: "Cancel download", preparing: "Preparing the offline reader…", ready: "Available offline", incomplete: "Incomplete or active download. Remove it to retry.", cancelled: "Download cancelled. No partial book was kept.", saved: "Book saved for offline reading.", unsupported: "Offline storage is unavailable. Use HTTPS and allow this site to store data.", confirm_clear: "Remove all downloaded books from this device? Server copies and reading positions will be kept.", confirm_remove: "Remove this downloaded copy? The server copy and reading position will be kept.", persisted: "Persistent storage is enabled. Clearing site data or signing out still removes downloads.", managed: "Storage is managed by the browser and may be cleared when space is low. You can request persistent storage.", not_granted: "The browser did not grant persistent storage. Downloads still work, but may be cleared when space is low.", used: "Book files: {used} / {limit}. Site data including readers: approximately {site}.", missing: "The browser removed this download. Remove its entry and download it again while connected.", signin: "Sign in before saving books offline.", failed: "The download failed. Reconnect and try again.", interrupted: "Download interrupted. Remove this entry to retry.", need_worker: "The offline reader is not ready. Reload while connected and try again." },
        zh: { library: "书库", device: "此设备", heading: "离线书籍", intro: "下载后可随时阅读。朗读优先使用在线语音，无法连接时自动使用已安装的本地语音。", storage: "存储空间", limit: "书籍存储上限（MB）", apply: "应用", storage_help: "上限适用于书籍文件。阅读器使用少量共享空间，不保存朗读音频。移除下载、退出登录或清除网站数据时会删除本地书籍。", keep: "请求持久存储", clear: "移除全部下载", empty: "尚未下载书籍。连接网络后，在书籍详情页点击“离线保存”。", save: "离线保存", remove: "移除下载", read: "阅读", cancel: "取消下载", preparing: "正在准备离线阅读器…", ready: "可离线阅读", incomplete: "下载未完成或仍在进行。移除后可重试。", cancelled: "已取消下载，未保留不完整的书籍。", saved: "书籍已保存，可离线阅读。", unsupported: "无法使用离线存储。请使用 HTTPS 并允许此网站存储数据。", confirm_clear: "移除此设备上的全部下载？服务器书籍和阅读位置会保留。", confirm_remove: "移除此下载？服务器书籍和阅读位置会保留。", persisted: "已启用持久存储。退出登录或清除网站数据仍会移除下载。", managed: "浏览器管理存储空间，空间不足时可能会清理。可请求持久存储。", not_granted: "浏览器未授予持久存储。仍可离线阅读，但空间不足时下载可能被清理。", used: "书籍文件：{used} / {limit}。包含阅读器的网站数据约为 {site}。", missing: "浏览器已移除此下载。请移除记录并联网重新下载。", signin: "请先登录，再保存离线书籍。", failed: "下载失败，请联网重试。", interrupted: "下载中断。移除此记录后可重试。", need_worker: "离线阅读器尚未就绪，请联网刷新后重试。" }
    };
    function lang() { try { return localStorage.getItem("micsReader:lang") || ((navigator.language || "").indexOf("zh") === 0 ? "zh" : "en"); } catch (_) { return "en"; } }
    function t(key) { return (strings[lang()] || strings.en)[key] || strings.en[key] || key; }
    function size(n) { return n >= 1024 * 1024 ? (n / (1024 * 1024)).toFixed(1) + " MB" : Math.ceil(n / 1024) + " KB"; }
    function message(text, error) { var node = document.querySelector("[data-offline-message]"); if (node) { node.textContent = text; node.dataset.error = String(!!error); } }
    function status(button, text) { var node = button.parentNode.querySelector("[data-offline-status]"); if (node) node.textContent = text; }
    function friendly(error) { return error.name === "AbortError" ? t("cancelled") : error.message || t("failed"); }
    async function identity(checkServer) {
        var meta = document.querySelector('meta[name="bookbrowser-user"]');
        if (!checkServer && meta && meta.content) return store.selectOwner(meta.content);
        if (!page && !meta && !location.pathname.startsWith("/static/reader/")) return;
        var controller = new AbortController(), timer = setTimeout(function () { controller.abort(); }, 5000);
        try {
            var response = await fetch("/api/offline/context", { credentials: "same-origin", cache: "no-store", signal: controller.signal });
            if (response.ok) { var data = await response.json(); await store.selectOwner(data.user_id); }
        } catch (_) { /* Offline or an expired session does not erase downloaded books. */ }
        finally { clearTimeout(timer); }
    }
    var initialized = store.supported() ? identity() : Promise.resolve();
    window.BookBrowserOfflineReady = initialized;
    async function prepare(format, signal) {
        var worker = await Promise.race([navigator.serviceWorker.ready, new Promise(function (_, reject) { setTimeout(function () { reject(new Error(t("need_worker"))); }, 15000); })]);
        if (!worker.active) throw new Error(t("need_worker"));
        if (!navigator.serviceWorker.controller) {
            await new Promise(function (resolve, reject) {
                var timer = setTimeout(function () { navigator.serviceWorker.removeEventListener("controllerchange", ready); reject(new Error(t("need_worker"))); }, 5000);
                function ready() { clearTimeout(timer); navigator.serviceWorker.removeEventListener("controllerchange", ready); resolve(); }
                navigator.serviceWorker.addEventListener("controllerchange", ready);
            });
        }
        var assets = BookBrowserOfflineAssets[format];
        var cache = await caches.open(BookBrowserOfflineAssets.cacheName);
        for (var index = 0; index < assets.length; index += 6) {
            await Promise.all(assets.slice(index, index + 6).map(async function (url) {
                if (await cache.match(url)) return;
                var response = await fetch(url, { signal: signal, cache: "reload", redirect: "error" });
                if (!response.ok) throw new Error(t("need_worker"));
                await cache.put(url, response);
            }));
        }
    }
    async function persistent() {
        if (!navigator.storage || !navigator.storage.persist) return false;
        return navigator.storage.persist().catch(function () { return false; });
    }
    buttons.forEach(function (button) {
        button.addEventListener("click", async function () {
            var id = button.dataset.bookId;
            if (controllers.has(id)) { controllers.get(id).abort(); return; }
            button.disabled = true;
            try {
                var persistence = persistent();
                await initialized;
                var existing = (await store.list()).find(function (book) { return book.id === id; });
                if (existing) {
                    if (confirm(t("confirm_remove"))) { await store.remove(id); status(button, ""); }
                    return;
                }
                var controller = new AbortController(); controllers.set(id, controller);
                button.disabled = false; button.textContent = t("cancel"); status(button, t("preparing"));
                await persistence;
                await store.save({ id: id, title: button.dataset.bookTitle, author: button.dataset.bookAuthor, format: button.dataset.bookFormat, bytes: Number(button.dataset.bookBytes), cover: button.dataset.bookCover === "true" }, {
                    signal: controller.signal, prepare: prepare,
                    onProgress: function (received, total) { status(button, Math.floor(received / total * 100) + "% · " + size(received) + " / " + size(total)); }
                });
                status(button, t("saved"));
            } catch (error) { status(button, friendly(error)); }
            finally { controllers.delete(id); button.disabled = false; await refresh().catch(function () {}); }
        });
    });
    function element(tag, text, className) { var node = document.createElement(tag); if (text) node.textContent = text; if (className) node.className = className; return node; }
    async function refresh() {
        if (!store.supported()) {
            message(t("unsupported"), true);
            buttons.forEach(function (button) { button.disabled = true; status(button, t("unsupported")); });
            return;
        }
        var books = await store.list();
        buttons.forEach(function (button) {
            if (controllers.has(button.dataset.bookId)) return;
            var book = books.find(function (item) { return item.id === button.dataset.bookId; });
            button.textContent = t(book ? "remove" : "save");
            if (book) status(button, t(book.status === "ready" ? "ready" : "incomplete"));
        });
        if (!page) return;
        page.querySelectorAll("[data-offline-text]").forEach(function (node) { node.textContent = t(node.dataset.offlineText); });
        document.documentElement.lang = lang();
        var language = page.querySelector("[data-offline-language]"); language.textContent = lang() === "zh" ? "English" : "中文";
        var usage = await store.usage(), estimate = navigator.storage && navigator.storage.estimate ? await navigator.storage.estimate() : null;
        page.querySelector("[data-offline-usage]").textContent = t("used").replace("{used}", size(usage.bytes)).replace("{limit}", size(usage.limit)).replace("{site}", estimate ? size(estimate.usage) : "—");
        if (document.activeElement !== page.querySelector("[data-offline-limit]")) page.querySelector("[data-offline-limit]").value = String(usage.limit / (1024 * 1024));
        var persisted = navigator.storage && navigator.storage.persisted ? await navigator.storage.persisted() : false;
        page.querySelector("[data-offline-persistence]").textContent = t(persisted ? "persisted" : "managed");
        var list = page.querySelector("[data-offline-books]"); list.replaceChildren();
        page.querySelector("[data-offline-empty]").hidden = books.length > 0;
        for (var book of books.sort(function (a, b) { return b.savedAt - a.savedAt; })) {
            var card = element("article", "", "offline-book"), img = element("img");
            img.alt = ""; img.src = book.status === "ready" ? "/offline-cover/" + book.id : "/static/nocover.jpg"; card.appendChild(img);
            var info = element("div", "", "offline-book-info"); info.appendChild(element("h2", book.title)); info.appendChild(element("p", book.author));
            info.appendChild(element("p", book.format.toUpperCase() + " · " + size(book.bytes), "offline-muted"));
            var actions = element("div", "", "offline-book-actions");
            if (book.status === "ready") {
                var read = element("a", t("read"), "offline-read"); read.href = store.readerURL(book); actions.appendChild(read);
                read.addEventListener("click", (function (item) { return async function (event) {
                    event.preventDefault();
                    if (await store.savedResponse(item.id, item.format)) location.href = store.readerURL(item);
                    else message(t("missing"), true);
                }; })(book));
            } else info.appendChild(element("p", t("interrupted"), "offline-muted"));
            var remove = element("button", t("remove")); remove.type = "button"; actions.appendChild(remove);
            remove.addEventListener("click", (function (item) { return async function () {
                if (!confirm(t("confirm_remove"))) return;
                try { await store.remove(item.id); await refresh(); } catch (error) { message(friendly(error), true); }
            }; })(book));
            info.appendChild(actions); card.appendChild(info); list.appendChild(card);
        }
    }
    if (page) {
        page.querySelector("[data-offline-limit-save]").onclick = async function () { try { await store.setLimit(Number(page.querySelector("[data-offline-limit]").value) * 1024 * 1024); await refresh(); message(""); } catch (error) { message(friendly(error), true); } };
        page.querySelector("[data-offline-clear]").onclick = async function () { if (!confirm(t("confirm_clear"))) return; try { await store.clear(false); await refresh(); message(""); } catch (error) { message(friendly(error), true); } };
        page.querySelector("[data-offline-persist]").onclick = async function () { var granted = await persistent(); await refresh(); if (!granted) message(t("not_granted")); };
        page.querySelector("[data-offline-language]").onclick = function () { try { localStorage.setItem("micsReader:lang", lang() === "zh" ? "en" : "zh"); } catch (_) {} refresh().catch(function (error) { message(friendly(error), true); }); };
    }
    window.addEventListener("bookbrowserofflinechange", function () { refresh().catch(function (error) { message(friendly(error), true); }); });
    window.addEventListener("micslangchange", function () { refresh().catch(function () {}); });
    window.addEventListener("storage", function (event) { if (event.key === "bookbrowserOffline:changed") refresh().catch(function () {}); });
    window.addEventListener("online", function () { identity(true).then(refresh).catch(function () {}); });
    initialized.then(refresh).catch(function (error) { message(friendly(error), true); buttons.forEach(function (button) { button.disabled = true; status(button, friendly(error)); }); });
}());
