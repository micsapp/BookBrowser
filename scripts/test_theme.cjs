// Disposable local server with the workspace test account and EPUB fixtures.
// Run test_workspace.cjs first, then: node scripts/test_theme.cjs http://127.0.0.1:18197 /tmp/theme-previews
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

(async () => {
    const base = process.argv[2] || "http://127.0.0.1:18197";
    assert.ok(["127.0.0.1", "localhost"].includes(new URL(base).hostname), "Use a disposable local server.");
    const output = process.argv[3];
    if (output) await fs.mkdir(output, { recursive: true });
    const cdp = process.env.BOOKBROWSER_CDP_URL || "http://127.0.0.1:19347";
    const errors = [], pages = [];
    async function newPage(url = "about:blank") {
        const target = await fetch(cdp + "/json/new?" + encodeURIComponent(url), { method: "PUT" }).then(r => r.json());
        const socket = new WebSocket(target.webSocketDebuggerUrl);
        const pending = new Map(); let id = 0;
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
            const timer = setTimeout(() => { pending.delete(request); reject(new Error(method + " timeout")); }, 30000);
            pending.set(request, { resolve, reject, timer }); socket.send(JSON.stringify({ id: request, method, params }));
        });
        const evaluate = async expression => {
            const result = await call("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
            if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
            return result.result.value;
        };
        const wait = async expression => {
            for (let i = 0; i < 150; i++) {
                try { if (await evaluate(expression)) return; } catch (_) {}
                await new Promise(resolve => setTimeout(resolve, 100));
            }
            throw new Error("Timed out: " + expression + ": " + await evaluate("location.href"));
        };
        const navigate = async pathname => {
            await call("Page.navigate", { url: base + pathname });
            await wait(`location.pathname === ${JSON.stringify(pathname)} && document.readyState === 'complete' && !!document.querySelector('[data-theme-toggle]')`);
        };
        const page = { call, evaluate, wait, navigate, async close() { socket.close(); await fetch(cdp + "/json/close/" + target.id).catch(() => {}); } };
        pages.push(page);
        await call("Page.enable"); await call("Runtime.enable"); await call("Network.enable");
        await call("Network.setBlockedURLs", { urls: ["*://sp.micstec.com/*", "*://cdn.ravenjs.com/*", "*://fonts.googleapis.com/*", "*://cdnjs.cloudflare.com/*"] });
        return page;
    }
    try {
        const page = await newPage();
        const { call, evaluate, wait, navigate } = page;
        const viewport = async width => { await call("Emulation.setDeviceMetricsOverride", { width, height: 950, deviceScaleFactor: 1, mobile: false }); await new Promise(r => setTimeout(r, 150)); };
        const media = value => call("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value }] });
        const toggle = () => evaluate('Array.from(document.querySelectorAll("[data-theme-toggle]")).find(b=>b.getClientRects().length).click()');
        const theme = () => evaluate('document.documentElement.getAttribute("data-theme")');
        const screenshot = async name => {
            if (!output) return;
            const data = await call("Page.captureScreenshot", { format: "png" });
            await fs.writeFile(path.join(output, name + ".png"), Buffer.from(data.data, "base64"));
        };
        const noOverflow = async label => {
            const metrics = await evaluate('({width:document.documentElement.clientWidth,scroll:document.documentElement.scrollWidth})');
            assert.ok(metrics.scroll <= metrics.width + 1, label + " " + JSON.stringify(metrics));
        };
        const darkSurface = async selector => {
            const colors = await evaluate(`Array.from(document.querySelectorAll(${JSON.stringify(selector)})).filter(e=>e.getClientRects().length).map(e=>({selector:${JSON.stringify(selector)},background:getComputedStyle(e).backgroundColor,color:getComputedStyle(e).color}))`);
            for (const colorset of colors) {
                const mean = value => value.match(/[\d.]+/g).slice(0, 3).reduce((sum, number) => sum + Number(number), 0) / 3;
                assert.ok(mean(colorset.background) < 100, "surface should be dark: " + JSON.stringify(colorset));
                assert.ok(mean(colorset.color) > 120, "text should remain readable: " + JSON.stringify(colorset));
            }
        };

        await call("Network.clearBrowserCookies");
        await viewport(1440); await media("light"); await navigate("/login");
        await evaluate('localStorage.removeItem("bookbrowser.theme")');
        await navigate("/login"); assert.equal(await theme(), "light");
        await media("dark"); await wait('document.documentElement.dataset.theme === "dark"');
        await darkSurface(".auth-panel"); await screenshot("login-dark-desktop");
        await viewport(390); await noOverflow("dark login mobile"); await screenshot("login-dark-mobile");
        await toggle(); assert.equal(await theme(), "light");
        assert.equal(await evaluate('localStorage.getItem("bookbrowser.theme")'), "light");
        await media("light"); await media("dark"); assert.equal(await theme(), "light");
        await navigate("/register"); assert.equal(await theme(), "light");
        await toggle(); await navigate("/login"); assert.equal(await theme(), "dark");
        console.log("PASS system preference, explicit override, persistence, and anonymous theme controls");

        const other = await newPage(); await other.navigate("/login");
        await toggle(); await other.wait('document.documentElement.dataset.theme === "light"');
        await other.evaluate('document.querySelector("[data-theme-toggle]").click()');
        await wait('document.documentElement.dataset.theme === "dark"');
        await other.close();
        console.log("PASS theme changes synchronize between open tabs");

        await evaluate('localStorage.removeItem("bookbrowser.theme")');
        const blocked = await call("Page.addScriptToEvaluateOnNewDocument", { source: '(function(){var get=Storage.prototype.getItem,set=Storage.prototype.setItem;Storage.prototype.getItem=function(key){if(key==="bookbrowser.theme")throw new DOMException("Theme storage blocked","SecurityError");return get.call(this,key)};Storage.prototype.setItem=function(key,value){if(key==="bookbrowser.theme")throw new DOMException("Theme storage blocked","QuotaExceededError");return set.call(this,key,value)}})()' });
        await navigate("/login"); assert.equal(await theme(), "dark");
        await toggle(); assert.equal(await theme(), "light");
        await call("Page.removeScriptToEvaluateOnNewDocument", { identifier: blocked.identifier });
        await navigate("/login");
        await evaluate('localStorage.setItem("bookbrowser.theme", "invalid")');
        await navigate("/login"); assert.equal(await theme(), "dark");
        console.log("PASS unavailable preference storage and invalid saved themes remain usable");

        const signedIn = await evaluate(`(async()=>{
            const body=new URLSearchParams({csrf_token:document.querySelector('meta[name="csrf-token"]').content,email:'workspace-admin@example.com',password:'workspace test password'});
            const r=await fetch('/login',{method:'POST',body});return new URL(r.url).pathname !== '/login';
        })()`);
        assert.ok(signedIn, "Run test_workspace.cjs first to create the isolated test account");
        await viewport(1440); await navigate("/books");
        if (await theme() !== "dark") await toggle();
        await evaluate('SiteI18N.setLang("en")');
        for (const width of [1440, 390, 320]) {
            await viewport(width); await noOverflow("dark catalog " + width);
            await darkSurface(".books.cards .book");
            assert.equal(await evaluate('Array.from(document.querySelectorAll("[data-theme-toggle]")).some(b=>b.getClientRects().length && b.getAttribute("aria-label")==="Switch to light theme")'), true);
            await screenshot("library-dark-" + width);
        }
        assert.ok(await evaluate('Array.from(document.querySelectorAll(".books .cover img")).every(img=>!/(invert|brightness)/.test(getComputedStyle(img).filter))'));
        console.log("PASS dark catalog, mobile access, and unchanged cover colors");
        await viewport(1440); await toggle(); await screenshot("library-light-desktop");
        assert.equal(await theme(), "light"); await toggle();
        const screens = [
            ["/authors", ".items .item"], ["/series", ".items .item"],
            ["/my-library", ".personal-library-section, .personal-empty-state"],
            ["/my-library/reading", ".personal-empty-state"],
            ["/admin", ".admin-card"], ["/admin/library", ".table-scroll"],
            ["/admin/users", ".table-scroll"], ["/admin/requests", ".personal-empty-state"],
            ["/profile", ".profile-info-card, .profile-form"],
            ["/books/7bac3f1616", ".personal-book-tools, .share-book"],
            ["/offline", ".offline-storage"]
        ];
        for (const [pathname, selector] of screens) {
            await viewport(1440); await navigate(pathname); assert.equal(await theme(), "dark");
            await darkSurface(selector); await noOverflow(pathname + " dark desktop");
            if (["/profile", "/admin", "/books/7bac3f1616", "/offline"].includes(pathname)) await screenshot("dark-" + pathname.replaceAll("/", "-").slice(1));
            await viewport(390); await noOverflow(pathname + " dark mobile");
        }
        console.log("PASS dark book, personal library, profile, admin, and offline surfaces");
        await viewport(1440); await navigate("/books");
        await evaluate('SiteI18N.setLang("zh")');
        await wait('document.querySelector(".sidebar [data-theme-toggle]").getAttribute("aria-label") === "切换到浅色主题"');
        await evaluate('SiteI18N.setLang("en");document.querySelector("[data-about-open]").click()');
        await darkSurface(".about-dialog"); await screenshot("about-dark");
        await evaluate('document.querySelector("[data-about-close]").click();document.querySelector(".book-request-open-button").click()');
        await wait('!!document.querySelector("#book-request-form") && !!document.querySelector("#book-request-form").getClientRects().length');
        await darkSurface(".book-request-modal"); await screenshot("request-dark");
        await call("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 });
        await wait('!document.querySelector("#book-request-form").getClientRects().length');
        console.log("PASS translated theme labels and dark dialogs");
        assert.deepEqual(errors, [], "browser JavaScript exceptions");
    } finally {
        for (const page of pages) await page.close().catch(() => {});
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
