// Run against a DISPOSABLE local server, with fixtures created using:
// python3 scripts/create_offline_fixtures.py /tmp/offline-test/books
// Requires Node 22+ and headless Chrome with CDP (see test_epub_covers.cjs).
// node scripts/test_offline.cjs http://127.0.0.1:18193
const assert = require("node:assert/strict");

(async () => {
    const base = process.argv[2] || "http://127.0.0.1:18193";
    assert.ok(["127.0.0.1", "localhost"].includes(new URL(base).hostname), "Use a disposable local server; this test creates an account and clears downloads.");
    const cdp = process.env.BOOKBROWSER_CDP_URL || "http://127.0.0.1:19341";
    const target = await fetch(cdp + "/json/new?about:blank", { method: "PUT" }).then(r => r.json());
    const socket = new WebSocket(target.webSocketDebuggerUrl);
    const pending = new Map();
    const errors = [];
    let nextID = 0;
    socket.onmessage = event => {
        const data = JSON.parse(event.data);
        if (!data.id) {
            if (data.method === "Target.detachedFromTarget") {
                for (const [id, task] of pending) if (task.sessionId === data.params.sessionId) {
                    clearTimeout(task.timer); pending.delete(id); task.reject(new Error("Worker target detached"));
                }
            }
            if (data.method === "Runtime.exceptionThrown") errors.push(data.params.exceptionDetails);
            if (data.method === "Log.entryAdded" && data.params.entry.level === "error") errors.push(data.params.entry);
            return;
        }
        const task = pending.get(data.id); pending.delete(data.id);
        if (!task) return;
        clearTimeout(task.timer);
        data.error ? task.reject(new Error(JSON.stringify(data.error))) : task.resolve(data.result);
    };
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    const call = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
        const id = ++nextID;
        const timer = setTimeout(() => { pending.delete(id); reject(new Error("CDP timeout: " + method)); }, method === "Runtime.evaluate" ? 65000 : 10000);
        pending.set(id, { resolve, reject, timer, sessionId }); socket.send(JSON.stringify({ id, method, params, sessionId }));
    });
    const evaluate = async expression => {
        const result = await call("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true, timeout: 60000 });
        if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
        return result.result.value;
    };
    const wait = async (expression, label) => {
        for (let i = 0; i < 250; i++) {
            try { if (await evaluate(expression)) return; } catch (_) {}
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        const state = await evaluate('({url:location.href,online:navigator.onLine,body:document.body.innerText.slice(0,1600)})').catch(() => ({}));
        throw new Error("Timed out: " + label + ": " + JSON.stringify(state) + " errors=" + JSON.stringify(errors.slice(-8)));
    };
    const navigate = async path => { await call("Page.navigate", { url: base + path }); };
    const workerSessions = new Map();
    const offline = async value => {
        // Service workers run in their own target; disabling only the page's
        // connection would leave navigation fetches in the worker online.
        const targets = await call("Target.getTargets");
        for (const [id] of workerSessions) if (!targets.targetInfos.some(t => t.targetId === id)) workerSessions.delete(id);
        for (const item of targets.targetInfos.filter(t => t.type === "service_worker" && t.url.startsWith(base))) {
            if (value && !workerSessions.has(item.targetId)) {
                const attached = await call("Target.attachToTarget", { targetId: item.targetId, flatten: true });
                workerSessions.set(item.targetId, attached.sessionId);
                await call("Network.enable", {}, attached.sessionId);
            }
        }
        for (const session of [undefined, ...workerSessions.values()]) {
            await call("Network.emulateNetworkConditionsByRule", { emulateOfflineServiceWorker: value, matchedNetworkConditions: [{ urlPattern: "", offline: value, latency: 0, downloadThroughput: -1, uploadThroughput: -1 }] }, session);
            await call("Network.overrideNetworkState", { offline: value, latency: 0, downloadThroughput: -1, uploadThroughput: -1 }, session).catch(() => {});
        }
    };
    const pass = text => console.log("PASS " + text);
    try {
        await call("Page.enable"); await call("Network.enable"); await call("Runtime.enable"); await call("Log.enable");
        await call("Network.clearBrowserCookies");
        await call("Storage.clearDataForOrigin", { origin: base, storageTypes: "all" });
        // Keep optional analytics/CDN dependencies from delaying test navigation.
        await call("Network.setBlockedURLs", { urls: ["*://sp.micstec.com/*", "*://cdn.ravenjs.com/*", "*://www.googletagmanager.com/*", "*://fonts.googleapis.com/*", "*://cdnjs.cloudflare.com/*"] });
        await navigate("/register");
        await wait('!!document.querySelector("#register-password")', "registration page");
        const registered = await evaluate(`(async () => {
            const form = new URLSearchParams({csrf_token:document.querySelector('meta[name="csrf-token"]').content,name:'Offline Tester',email:'offline-'+Date.now()+'@example.com',password:'offline testing password',password_confirm:'offline testing password'});
            const r = await fetch('/register',{method:'POST',body:form}); return r.url;
        })()`);
        assert.ok(!registered.endsWith("/register"), registered);
        await navigate("/books");
        await wait('document.querySelectorAll(".books .book .title").length >= 2', "indexed test books");
        const links = await evaluate(`Array.from(document.querySelectorAll('.books .book .title')).map(a=>({title:a.textContent,path:a.getAttribute('href')}))`);
        const epub = links.find(item => item.title === "Offline Test");
        assert.ok(epub, JSON.stringify(links));
        await navigate(epub.path);
        await wait('!!window.BookBrowserOffline && !!document.querySelector("[data-offline-save]")', "offline controls");
        await evaluate("BookBrowserOfflineReady");
        await evaluate('document.querySelector("[data-offline-save]").click()');
        await wait('(async()=> (await BookBrowserOffline.list()).some(b=>b.status==="ready"&&b.format==="epub"))()', "EPUB download");
        const savedEPUB = await evaluate('(async()=> (await BookBrowserOffline.list()).find(b=>b.format==="epub"))()');
        assert.equal(savedEPUB.title, "Offline Test"); pass("complete EPUB download through Save offline");

        // Reservation limits also apply to simultaneous requests, before any bytes are kept.
        const storage = await evaluate(`(async()=>{
            await BookBrowserOffline.setLimit(1024*1024);
            const book={id:'eeeeeeeeee',format:'epub',bytes:2*1024*1024,title:'Too large'};
            let error='';try{await BookBrowserOffline.save(book)}catch(e){error=e.message}
            await BookBrowserOffline.setLimit(500*1024*1024);
            return {error,books:await BookBrowserOffline.list()};
        })()`);
        assert.match(storage.error, /limit/); assert.equal(storage.books.length, 1); pass("storage limit rejects downloads without deleting saved books");

        const concurrent = await evaluate(`(async()=>{
            await BookBrowserOffline.setLimit(1024*1024);
            const controller=new AbortController();let entered=false;
            const first=BookBrowserOffline.save({id:'bbbbbbbbbb',format:'epub',bytes:700000,title:'Reservation'}, {signal:controller.signal,prepare:(_format,signal)=>{entered=true;return new Promise((_,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('Cancelled','AbortError'))))}}).catch(e=>e.name);
            while(!entered)await new Promise(r=>setTimeout(r,10));
            let second='';try{await BookBrowserOffline.save({id:'aaaaaaaaaa',format:'epub',bytes:700000,title:'Second reservation'})}catch(e){second=e.message}
            controller.abort();const cancelled=await first;
            await BookBrowserOffline.setLimit(500*1024*1024);
            return {second,cancelled,books:await BookBrowserOffline.list()};
        })()`);
        assert.match(concurrent.second, /limit/); assert.equal(concurrent.cancelled, "AbortError"); assert.equal(concurrent.books.length, 1); pass("concurrent downloads reserve storage and cancellation releases the reservation");

        const failed = await evaluate(`(async()=>{
            const original=window.fetch; window.fetch=async()=>new Response('PKbroken');
            let error='';try{await BookBrowserOffline.save({id:'dddddddddd',format:'epub',bytes:100,title:'Incomplete'})}catch(e){error=e.message}finally{window.fetch=original}
            return {error,books:await BookBrowserOffline.list()};
        })()`);
        assert.match(failed.error, /incomplete/); assert.equal(failed.books.length, 1); pass("truncated download is removed and never marked ready");

        const race = await evaluate(`(async()=>{
            let release;
            const saving=BookBrowserOffline.save({id:'cccccccccc',format:'epub',bytes:100,title:'Cancelled'}, {prepare:()=>new Promise(r=>release=r)}).catch(e=>e.message);
            while(!release) await new Promise(r=>setTimeout(r,10));
            await BookBrowserOffline.remove('cccccccccc');
            const original=window.fetch;window.fetch=async()=>new Response('PK'+'x'.repeat(98));release();
            let error;try{error=await saving}finally{window.fetch=original}
            return {error,books:await BookBrowserOffline.list()};
        })()`);
        assert.match(race.error, /cancelled/); assert.equal(race.books.length, 1); pass("removing an in-flight download prevents its later commit");

        const pdfLink = links.find(item => item.path !== epub.path);
        await navigate(pdfLink.path);
        await wait('!!window.BookBrowserOffline && !!document.querySelector("[data-offline-save]")', "PDF controls");
        await evaluate("BookBrowserOfflineReady"); await evaluate('document.querySelector("[data-offline-save]").click()');
        await wait('(async()=> (await BookBrowserOffline.list()).some(b=>b.status==="ready"&&b.format==="pdf"))()', "PDF download");
        const savedPDF = await evaluate('(async()=> (await BookBrowserOffline.list()).find(b=>b.format==="pdf"))()');
        pass("complete PDF download with worker, CMaps, images, and localization");

        const range = await evaluate(`(async()=>{
            const r=await fetch('/download/${savedPDF.id}.pdf',{headers:{Range:'bytes=0-4'}});
            const bad=await fetch('/download/${savedPDF.id}.pdf',{headers:{Range:'bytes=999999999-'}});
            const suffix=await fetch('/download/${savedPDF.id}.pdf',{headers:{Range:'bytes=-5'}});
            return {status:r.status,text:await r.text(),range:r.headers.get('Content-Range'),bad:bad.status,suffix:suffix.status,suffixLength:(await suffix.arrayBuffer()).byteLength};
        })()`);
        assert.equal(range.status, 206); assert.equal(range.text, "%PDF-"); assert.match(range.range, /^bytes 0-4\//); assert.equal(range.bad, 416); assert.equal(range.suffixLength, 5); pass("PDF byte ranges, suffix ranges, and invalid range handling");

        await offline(true); await navigate("/");
        await wait('document.querySelectorAll(".offline-book").length===2', "offline cold navigation");
        pass("offline app launch lists downloaded books without server HTML");
        await navigate("/read/" + savedEPUB.id);
        await wait('!!window.ePubViewer && ePubViewer.state.readerRevealed', "offline EPUB open");
        assert.ok(await evaluate('ePubViewer.state.rendition.getContents().some(c=>c.document.body.textContent.includes("Offline chapter one"))'));
        const firstPosition = await evaluate('localStorage.getItem(ePubViewer.state.book.key()+":pos")');
        await evaluate('ePubViewer.state.rendition.next()');
        await wait('localStorage.getItem(ePubViewer.state.book.key()+":pos") !== ' + JSON.stringify(firstPosition), "saved reading position after page turn");
        const position = await evaluate('localStorage.getItem(ePubViewer.state.book.key()+":pos")');
        const timeOrigin = await evaluate('performance.timeOrigin');
        await call("Page.reload", { ignoreCache: true });
        await wait('performance.timeOrigin !== ' + timeOrigin + ' && !!window.ePubViewer && ePubViewer.state.readerRevealed', "offline EPUB reload");
        assert.equal(await evaluate('localStorage.getItem(ePubViewer.state.book.key()+":pos")'), position);
        pass("offline EPUB rendering, server Read link fallback, and resume after reload");

        const tts = await evaluate(`(async()=>{
            const viewer=ePubViewer, app=Object.create(Object.getPrototypeOf(viewer));
            app.state={ttsAbort:false,ttsRunId:77,ttsSpeed:1,ttsBlobPromises:{}};
            const originalFetch=window.fetch, speechDescriptor=Object.getOwnPropertyDescriptor(window,'speechSynthesis'), originalUtterance=window.SpeechSynthesisUtterance;
            let requests=0, selected='', advanced=0;
            const fakeSpeech={getVoices:()=>[{name:'Remote',lang:'en-US',localService:false},{name:'Local',lang:'en-US',localService:true}],speak:u=>{selected=u.voice.name;setTimeout(()=>u.onend(),0)},pause:()=>{}};
            Object.defineProperty(window,'speechSynthesis',{configurable:true,value:fakeSpeech});
            window.SpeechSynthesisUtterance=function(text){this.text=text};
            try{
                window.fetch=async()=>{requests++;return new Response('audio')};
                await app.fetchTTSBlob('Hello',77,0);
                const onlineRequests=requests;
                app.state.ttsBlobPromises={};
                window.fetch=async()=>{requests++;throw new TypeError('Failed to fetch')};
                app.state.ttsChunks=[{text:'Hello offline',paragraph:null,paragraphIndex:0}];app.state.ttsIndex=0;
                app.highlightTTSParagraph=()=>{};app.updateTTSStatus=()=>{};app.hasTTSTimeExpired=()=>false;
                app.advanceTTSChunk=()=>advanced++;app.stopTTS=reason=>{throw new Error(reason)};
                app.playNextChunk(77);
                for(let i=0;i<100&&!advanced;i++)await new Promise(r=>setTimeout(r,10));
                const backedOff=app.state.ttsRetryAt>Date.now();
                app.state.ttsRetryAt=Date.now()-1;window.fetch=async()=>{requests++;return new Response('recovered audio')};
                await app.fetchTTSBlob('Hello again',77,3);
                const recovered=app.state.ttsRetryAt===0;
                window.fetch=(_url,opts)=>new Promise((_,reject)=>opts.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError'))));
                let timeout=false;try{await app.fetchTTSRequest('/tts/test',{},r=>r.blob(),20)}catch(e){timeout=e.name==='AbortError'}
                fakeSpeech.getVoices=()=>[{name:'Remote only',lang:'en-US',localService:false}];
                let noLocal='';try{await app.speakLocalTTSChunk('Hello',77)}catch(e){noLocal=e.message}
                viewer.state.ttsRetryAt=Date.now()+30000;window.dispatchEvent(new Event('online'));
                return {onlineRequests,selected,advanced,backedOff,recovered,timeout,noLocal,onlineEvent:viewer.state.ttsRetryAt===0};
            }finally{window.fetch=originalFetch;window.SpeechSynthesisUtterance=originalUtterance;Object.defineProperty(window,'speechSynthesis',speechDescriptor)}
        })()`);
        assert.equal(tts.onlineRequests, 1); assert.equal(tts.selected, "Local"); assert.equal(tts.advanced, 1); assert.ok(tts.backedOff && tts.recovered && tts.timeout && tts.onlineEvent, JSON.stringify(tts)); assert.match(tts.noLocal, /no local voice/);
        pass("downloaded-book TTS prefers online, falls back locally, bounds requests, and retries after recovery");

        await navigate("/static/reader/pdf/web/viewer.html?file=/download/" + savedPDF.id + ".pdf");
        await wait('!!window.PDFViewerApplication && PDFViewerApplication.pagesCount>0', "offline PDF open");
        await wait('document.querySelectorAll(".page[data-loaded]").length>0', "offline PDF rendered page");
        pass("offline PDF rendering after fresh navigation");

        await offline(false); await navigate("/offline");
        await wait('!!window.BookBrowserOffline && !!document.querySelector("[data-offline-library]")', "online offline library");
        await evaluate("BookBrowserOfflineReady");
        const upgrade = await evaluate(`(async()=>{
            const count=(await BookBrowserOffline.list()).length;
            await caches.open('bookbrowser-pwa-obsolete');
            const registration=await navigator.serviceWorker.ready;
            await registration.unregister();
            await navigator.serviceWorker.register('/sw.js?offline-regression-update=1',{scope:'/'});
            for(let i=0;i<200&&(await caches.keys()).includes('bookbrowser-pwa-obsolete');i++)await new Promise(r=>setTimeout(r,50));
            return {count,after:(await BookBrowserOffline.list()).length,oldRemoved:!(await caches.keys()).includes('bookbrowser-pwa-obsolete')};
        })()`);
        assert.ok(upgrade.oldRemoved); assert.equal(upgrade.after, upgrade.count); pass("service worker upgrade removes old app caches and preserves downloaded books");

        // A successful logout goes through the service worker even without a form handler.
        await navigate("/books"); await wait('!!document.querySelector("meta[name=csrf-token]") && !!window.BookBrowserOffline', "logout source");
        const invalidLogout = await evaluate(`(async()=>{const r=await fetch('/logout',{method:'POST',body:new URLSearchParams({csrf_token:'invalid'})});return {status:r.status,books:(await BookBrowserOffline.list()).length}})()`);
        assert.equal(invalidLogout.status, 403); assert.equal(invalidLogout.books, 2); pass("invalid logout request preserves downloaded books");
        const logout = await evaluate(`(async()=>{
            await fetch('/logout',{method:'POST',body:new URLSearchParams({csrf_token:document.querySelector('meta[name="csrf-token"]').content})});
            return {books:await BookBrowserOffline.list(),active:await BookBrowserOffline.active()};
        })()`);
        assert.equal(logout.books.length, 0); assert.equal(logout.active.owner, ""); pass("explicit sign-out clears downloads and invalidates the offline account");
    } finally {
        await offline(false).catch(() => {});
        socket.close();
        await fetch(cdp + "/json/close/" + target.id);
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
