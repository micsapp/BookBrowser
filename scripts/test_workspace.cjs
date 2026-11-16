// Run against a disposable local server with EPUBs, Node 22+, and headless Chrome.
// BOOKBROWSER_CDP_URL=http://127.0.0.1:19347 node scripts/test_workspace.cjs http://127.0.0.1:18197 /tmp/ui-previews
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

(async () => {
    const base = process.argv[2] || "http://127.0.0.1:18197";
    assert.ok(["127.0.0.1", "localhost"].includes(new URL(base).hostname), "Use a disposable server: this test creates an account.");
    const output = process.argv[3];
    if (output) await fs.mkdir(output, { recursive: true });
    const cdp = process.env.BOOKBROWSER_CDP_URL || "http://127.0.0.1:19347";
    const target = await fetch(cdp + "/json/new?about:blank", { method: "PUT" }).then(r => r.json());
    const socket = new WebSocket(target.webSocketDebuggerUrl);
    const pending = new Map(), errors = [];
    let id = 0;
    socket.onmessage = event => {
        const message = JSON.parse(event.data);
        if (message.method === "Runtime.exceptionThrown") errors.push(message.params.exceptionDetails);
        if (!message.id) return;
        const task = pending.get(message.id);
        if (!task) return;
        pending.delete(message.id); clearTimeout(task.timer);
        message.error ? task.reject(new Error(JSON.stringify(message.error))) : task.resolve(message.result);
    };
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    const call = (method, params = {}) => new Promise((resolve, reject) => {
        const request = ++id;
        const timer = setTimeout(() => { pending.delete(request); reject(new Error("CDP timeout: " + method)); }, 30000);
        pending.set(request, { resolve, reject, timer });
        socket.send(JSON.stringify({ id: request, method, params }));
    });
    const evaluate = async expression => {
        const result = await call("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
        if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
        return result.result.value;
    };
    const wait = async expression => {
        for (let attempt = 0; attempt < 150; attempt++) {
            try { if (await evaluate(expression)) return; } catch (_) {}
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        throw new Error("Timed out: " + expression + " " + await evaluate("location.href"));
    };
    const navigate = async pathname => {
        await call("Page.navigate", { url: base + pathname });
        await wait(`location.pathname === ${JSON.stringify(pathname.split("?")[0])} && document.readyState === 'complete'`);
    };
    const viewport = async width => {
        await call("Emulation.setDeviceMetricsOverride", { width, height: width > 1600 ? 1080 : 950, deviceScaleFactor: 1, mobile: false });
        await new Promise(resolve => setTimeout(resolve, 250));
    };
    const screenshot = async name => {
        if (!output) return;
        const result = await call("Page.captureScreenshot", { format: "png" });
        await fs.writeFile(path.join(output, name + ".png"), Buffer.from(result.data, "base64"));
    };
    const noOverflow = async label => {
        const size = await evaluate("({viewport:innerWidth,document:document.documentElement.scrollWidth})");
        assert.ok(size.document <= size.viewport + 1, label + ": " + JSON.stringify(size));
    };
    try {
        await call("Page.enable"); await call("Runtime.enable"); await call("Network.enable");
        await call("Network.setBlockedURLs", { urls: ["*://sp.micstec.com/*", "*://cdn.ravenjs.com/*", "*://fonts.googleapis.com/*", "*://cdnjs.cloudflare.com/*"] });
        await call("Network.clearBrowserCookies");
        await viewport(1440);
        await navigate("/login");
        let signedIn = await evaluate(`(async () => {
            const body = new URLSearchParams({csrf_token:document.querySelector('meta[name="csrf-token"]').content,email:'workspace-admin@example.com',password:'workspace test password'});
            const r = await fetch('/login',{method:'POST',body}); return !new URL(r.url).pathname.startsWith('/login');
        })()`);
        if (!signedIn) {
            await navigate("/register");
            signedIn = await evaluate(`(async () => {
                const body = new URLSearchParams({csrf_token:document.querySelector('meta[name="csrf-token"]').content,name:'Alex Morgan',email:'workspace-admin@example.com',password:'workspace test password',password_confirm:'workspace test password'});
                const r = await fetch('/register',{method:'POST',body}); return new URL(r.url).pathname !== '/register';
            })()`);
        }
        assert.ok(signedIn, "test account sign-in");
        await navigate("/books");
        await wait('document.querySelectorAll(".books .book").length > 2 && !!window.SiteI18N');
        await evaluate('SiteI18N.setLang("en"); localStorage.removeItem("bookbrowser.catalog-view")');
        await navigate("/books");
        for (const width of [1440, 1920, 1024, 768, 390, 320]) {
            await viewport(width);
            await noOverflow("catalog " + width);
            const layout = await evaluate(`({wrapper:document.querySelector('.body-wrapper').getBoundingClientRect().width,available:document.documentElement.clientWidth,columns:getComputedStyle(document.querySelector('.books')).gridTemplateColumns.split(' ').length})`);
            assert.ok(layout.wrapper >= layout.available - 1, "catalog should use the available width: " + JSON.stringify(layout));
            if (width >= 1440) assert.ok(layout.columns >= 5, "desktop should show at least five columns");
            if (width <= 390) assert.equal(layout.columns, 2, "phone grid should have two columns");
            if ([1440, 1920, 390].includes(width)) await screenshot("library-" + width);
            console.log("PASS responsive catalog at " + width + "px (" + layout.columns + " columns)");
        }
        await viewport(390);
        assert.equal(await evaluate('document.querySelector(".sidebar").inert'), true);
        await evaluate('document.querySelector("[data-menu-toggle]").click()');
        assert.equal(await evaluate('document.querySelector("[data-menu-toggle]").getAttribute("aria-expanded")'), "true");
        assert.equal(await evaluate('document.getElementById("main-content").inert'), true);
        await screenshot("mobile-navigation");
        await call("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 });
        assert.equal(await evaluate('document.querySelector("[data-menu-toggle]").getAttribute("aria-expanded")'), "false");
        assert.equal(await evaluate('document.activeElement.hasAttribute("data-menu-toggle")'), true);
        await evaluate('document.querySelector("[data-menu-toggle]").click(); document.querySelector("[data-menu-close]").click()');
        assert.equal(await evaluate('document.body.classList.contains("menu-open")'), false);
        await evaluate('document.querySelector("[data-menu-toggle]").click(); document.querySelector("[data-menu-dismiss]").click()');
        assert.equal(await evaluate('document.body.classList.contains("menu-open")'), false);
        console.log("PASS mobile navigation, backdrop, Escape, and focus restoration");

        await viewport(1440);
        await evaluate('document.querySelector(".view-list").click()');
        await navigate("/books");
        assert.equal(await evaluate('document.querySelector(".current-view").classList.contains("list")'), true);
        assert.equal(await evaluate('document.querySelector(".view-list").getAttribute("aria-pressed")'), "true");
        await screenshot("library-list");
        await evaluate('document.querySelector(".view-cards").click(); const s=document.querySelector("[data-catalog-sort]"); s.value="title-asc"; s.dispatchEvent(new Event("change"))');
        await wait('location.search.includes("title-asc") && document.readyState === "complete" && document.querySelector("[data-catalog-sort]").value === "title-asc"');
        const titles = await evaluate('Array.from(document.querySelectorAll(".books .book .title")).map(a=>a.textContent)');
        assert.deepEqual(titles, titles.slice().sort(), "title sorting should reach the server");
        console.log("PASS persistent grid/list controls and title sorting");

        await evaluate('const q=document.querySelector(".bar input[name=q]"); q.value="AI for People"; q.form.requestSubmit()');
        await wait('location.pathname === "/search" && !!document.querySelector(".search-summary")');
        assert.equal(await evaluate('document.querySelector("input[name=q]").value'), "AI for People");
        assert.ok(await evaluate('document.querySelectorAll(".books .book").length > 0'));
        console.log("PASS search submission and query retention");

        for (const pathname of ["/authors", "/series", "/my-library", "/admin", "/admin/library", "/admin/users", "/profile", "/books/7bac3f1616"]) {
            await viewport(1440); await navigate(pathname); await noOverflow(pathname + " desktop");
            if (pathname === "/admin") await screenshot("admin-desktop");
            if (pathname.includes("7bac")) await screenshot("book-desktop");
            await viewport(390); await noOverflow(pathname + " mobile");
            if (pathname.includes("7bac")) await screenshot("book-mobile");
        }
        console.log("PASS author, series, personal library, admin, profile, and book layouts");
        await viewport(1440); await navigate("/books");
        await evaluate('SiteI18N.setLang("zh")');
        assert.equal(await evaluate('document.querySelector(".catalog-header h1").textContent'), "发现好书");
        await noOverflow("Chinese desktop"); await screenshot("library-chinese");
        await evaluate('SiteI18N.setLang("en"); document.querySelector("[data-about-open]").click()');
        assert.equal(await evaluate('document.querySelector("[data-about-dialog]").open'), true);
        await evaluate('document.querySelector("[data-about-close]").click(); document.querySelector(".book-request-open-button").click()');
        await wait('!!document.querySelector("#book-request-form") && !!document.querySelector("#book-request-form").getClientRects().length');
        await call("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 });
        await wait('!document.querySelector("#book-request-form").getClientRects().length');
        console.log("PASS language switching and existing About/request dialogs");
        assert.deepEqual(errors, [], "browser JavaScript exceptions");
    } finally {
        socket.close();
        await fetch(cdp + "/json/close/" + target.id).catch(() => {});
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
