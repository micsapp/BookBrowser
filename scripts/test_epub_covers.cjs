// Browser regression tests; requires Node 22+ and Chrome with remote debugging:
// google-chrome --headless=new --no-sandbox --remote-debugging-port=19338 \
//   --user-data-dir=/tmp/bookbrowser-cover-tests
// node scripts/test_epub_covers.cjs http://127.0.0.1:8092
// Set BOOKBROWSER_CDP_URL to use another Chrome debugging endpoint.
const assert = require("node:assert/strict");

async function checkCovers() {
    const viewer = window.ePubViewer;
    const cases = [
        { name: "EPUB 2 legacy cover", version: "2.0", legacy: true, expected: "cover.gif" },
        { name: "EPUB 3 legacy cover", legacy: true, expected: "cover.gif" },
        { name: "EPUB 3 cover property", properties: "cover-image", expected: "cover.gif" },
        { name: "EPUB 3 property tokens", properties: "svg cover-image", expected: "cover.gif" },
        { name: "EPUB 3 modern cover takes precedence", legacy: true, modern: true, expected: "modern.gif" },
        { name: "EPUB 2 without a cover", version: "2.0", expected: "" },
        { name: "EPUB 3 without a cover", expected: "" },
        { name: "EPUB 3 missing cover file", legacy: true, missing: true, expected: "cover.gif" },
    ];
    const results = [];
    for (const [index, spec] of cases.entries()) {
        const zip = new JSZip();
        zip.file("mimetype", "application/epub+zip");
        zip.file("META-INF/container.xml", '<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="EPUB/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>');
        zip.file("EPUB/content.opf", `
            <package xmlns="http://www.idpf.org/2007/opf" version="${spec.version || "3.0"}" unique-identifier="bookid">
                <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
                    <dc:title>Cover regression ${index}</dc:title><dc:creator>Test Author</dc:creator>
                    <dc:identifier id="bookid">cover-regression-${index}</dc:identifier><dc:language>en</dc:language>
                    ${spec.legacy ? '<meta name="cover" content="legacy-cover"/>' : ""}
                </metadata>
                <manifest>
                    <item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/>
                    <item id="legacy-cover" href="cover.gif" media-type="image/gif" properties="${spec.properties || ""}"/>
                    ${spec.modern ? '<item id="modern-cover" href="modern.gif" media-type="image/gif" properties="cover-image"/>' : ""}
                </manifest>
                <spine><itemref idref="chapter"/></spine>
            </package>`);
        zip.file("EPUB/chapter.xhtml", '<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><p>Readable EPUB fixture.</p></body></html>');
        const image = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
        if (!spec.missing) zip.file("EPUB/cover.gif", image, { base64: true });
        if (spec.modern) zip.file("EPUB/modern.gif", image, { base64: true });
        viewer.doBook(await zip.generateAsync({ type: "arraybuffer" }), {});
        const hasChapter = () => viewer.state.readerRevealed && viewer.state.rendition.getContents().some(content => content.document.body.textContent.includes("Readable EPUB fixture."));
        const deadline = Date.now() + 10000;
        while ((!hasChapter() || !viewer.state.book.locations.length()) && Date.now() < deadline) {
            await new Promise(resolve => setTimeout(resolve, 50));
        }
        // Allow the cover promise and image load/error event to settle.
        await new Promise(resolve => setTimeout(resolve, 100));
        const error = document.querySelector(".app .error");
        const cover = viewer.qs(".cover");
        const result = {
            name: spec.name,
            coverPath: viewer.state.book.package.coverPath || "",
            expected: spec.expected,
            readable: hasChapter(),
            bookReady: viewer.state.bookReady,
            pageReady: viewer.state.pageReady,
            text: viewer.state.rendition.getContents().map(content => content.document.body.textContent),
            error: error.classList.contains("hidden") ? "" : error.textContent,
            imageLoaded: cover.complete && cover.naturalWidth > 0,
        };
        results.push(result);
        if (result.error || !result.readable) break;
    }
    viewer.doReset();
    return results;
}

(async () => {
    const base = process.argv[2] || "http://127.0.0.1:8092";
    const cdp = process.env.BOOKBROWSER_CDP_URL || "http://127.0.0.1:19338";
    const target = await fetch(cdp + "/json/new?about:blank", { method: "PUT" }).then(response => response.json());
    const socket = new WebSocket(target.webSocketDebuggerUrl);
    let id = 0;
    const pending = new Map();
    socket.onmessage = event => {
        const message = JSON.parse(event.data);
        if (!message.id) return;
        const task = pending.get(message.id);
        pending.delete(message.id);
        if (message.error) task.reject(new Error(JSON.stringify(message.error)));
        else task.resolve(message.result);
    };
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    const call = (method, params = {}) => new Promise((resolve, reject) => {
        pending.set(++id, { resolve, reject });
        socket.send(JSON.stringify({ id, method, params }));
    });
    try {
        await call("Page.enable");
        await call("Page.navigate", { url: base + "/static/reader/epub/" });
        let ready = false;
        for (let attempt = 0; attempt < 100; attempt++) {
            const result = await call("Runtime.evaluate", { expression: "!!window.ePubViewer", returnByValue: true });
            if (result.result.value) { ready = true; break; }
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        assert.ok(ready, "reader did not initialize");
        const result = await call("Runtime.evaluate", {
            expression: `(${checkCovers.toString()})()`, awaitPromise: true, returnByValue: true, timeout: 90000,
        });
        assert.ok(!result.exceptionDetails, JSON.stringify(result.exceptionDetails));
        const checks = result.result.value;
        for (const check of checks) {
            assert.equal(check.error, "", check.name);
            assert.ok(check.readable, check.name + " should display the chapter: " + JSON.stringify(check));
            assert.equal(check.coverPath, check.expected, check.name);
            if (check.expected && !check.name.includes("missing")) assert.ok(check.imageLoaded, check.name + " should display the cover");
            console.log("PASS " + check.name);
        }
        assert.equal(checks.length, 8, "all cover fixtures should be tested");
    } finally {
        socket.close();
        await fetch(cdp + "/json/close/" + target.id);
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
