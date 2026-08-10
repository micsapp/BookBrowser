"use strict";

window.onerror = function (msg, url, line, column, err) {
    if (msg.indexOf("Permission denied") > -1) return;
    if (msg.indexOf("Object expected") > -1 && url.indexOf("epub") > -1) return;
    document.querySelector(".app .error").classList.remove("hidden");
    document.querySelector(".app .error .error-title").innerHTML = "Error";
    document.querySelector(".app .error .error-description").innerHTML = "Please try reloading the page or using a different browser (Chrome or Firefox), and if the error still persists, <a href=\"https://github.com/geek1011/ePubViewer/issues\">report an issue</a>.";
    document.querySelector(".app .error .error-info").innerHTML = msg;
    document.querySelector(".app .error .error-dump").innerHTML = JSON.stringify({
        error: err.toString(),
        stack: err.stack,
        msg: msg,
        url: url,
        line: line,
        column: column,
    });
    try {
        Raven.captureException(err);
    } catch (err) {}
};

let App = function (el) {
    this.ael = el;
    this.state = {};
    this.doReset();

    document.body.addEventListener("keyup", this.onKeyUp.bind(this));

    this.qsa(".tab-list .item").forEach(el => el.addEventListener("click", this.onTabClick.bind(this, el.dataset.tab)));
    this.qs(".sidebar .search-bar .search-box").addEventListener("keydown", event => {
        if (event.keyCode == 13) this.qs(".sidebar .search-bar .search-button").click();
    });
    this.qs(".sidebar .search-bar .search-button").addEventListener("click", this.onSearchClick.bind(this));
    this.qs(".sidebar-wrapper").addEventListener("click", event => {
        try {
            if (event.target.classList.contains("sidebar-wrapper")) event.target.classList.add("out");
        } catch (err) {
            this.fatal("error hiding sidebar", err);
        }
    });
    this.qsa(".chips[data-chips]").forEach(el => {
        Array.from(el.querySelectorAll(".chip[data-value]")).forEach(cel => cel.addEventListener("click", event => {
            this.setChipActive(el.dataset.chips, cel.dataset.value);
        }));
    });
    this.qs("button.prev").addEventListener("click", () => this.state.rendition.prev());
    this.qs("button.next").addEventListener("click", () => this.state.rendition.next());
    this.qs("button.open").addEventListener("click", () => this.doOpenBook());
    this.qs("#tts-fab").addEventListener("click", () => this.onTTSClick());
    this.qs("#tts-options-button").addEventListener("click", () => this.toggleTTSOptions());
    this.qs("#tts-options-close").addEventListener("click", () => this.toggleTTSOptions(false));
    this.qs("#tts-stop-after").addEventListener("change", () => this.saveTTSPreferences());
    this.qs("#tts-duration-minutes").addEventListener("change", () => this.saveTTSPreferences());
    this.qs("#tts-keep-screen-on").addEventListener("change", () => this.saveTTSPreferences());
    document.addEventListener("visibilitychange", this.onTTSVisibilityChange.bind(this));
    window.addEventListener("pagehide", () => this.releaseTTSWakeLock());
    this.state.ttsSpeaking = false;

    try {
        this.qs(".bar .loc").style.cursor = "pointer";
        this.qs(".bar .loc").addEventListener("click", event => {
            try {
                let answer = prompt(`Location to go to (up to ${this.state.book.locations.length()})?`, this.state.rendition.currentLocation().start.location);
                if (!answer) return;
                answer = answer.trim();
                if (answer == "") return;

                let parsed = parseInt(answer, 10);
                if (isNaN(parsed) || parsed < 0) throw new Error("Invalid location: not a positive integer");
                if (parsed > this.state.book.locations.length()) throw new Error("Invalid location");

                let cfi = this.state.book.locations.cfiFromLocation(parsed);
                if (cfi === -1) throw new Error("Invalid location");

                this.state.rendition.display(cfi);
            } catch (err) {
                alert(err.toString());
            }
        });
    } catch (err) {
        this.fatal("error attaching event handlers for location go to", err);
        throw err;
    }

    this.doTab("toc");

    try {
        this.loadSettingsFromStorage();
        this.loadTTSPreferences();
        this.setupTTSMediaSession();
        this.setupTTSStatusDrag();
    } catch (err) {
        this.fatal("error loading settings", err);
        throw err;
    }
    this.applyTheme();
};

App.prototype.doBook = function (url, opts) {
    this.qs(".book").innerHTML = "Loading";

    opts = opts || {
        encoding: "epub"
    };
    console.log("doBook", url, opts);
    this.doReset();

    try {
        this.state.book = ePub(url, opts);
        this.qs(".book").innerHTML = "";
        this.state.rendition = this.state.book.renderTo(this.qs(".book"), {});
    } catch (err) {
        this.fatal("error loading book", err);
        throw err;
    }

    this.state.book.ready.then(this.onBookReady.bind(this)).catch(this.fatal.bind(this, "error loading book"));

    this.state.book.loaded.navigation.then(this.onNavigationLoaded.bind(this)).catch(this.fatal.bind(this, "error loading toc"));
    this.state.book.loaded.metadata.then(this.onBookMetadataLoaded.bind(this)).catch(this.fatal.bind(this, "error loading metadata"));
    this.state.book.loaded.cover.then(this.onBookCoverLoaded.bind(this)).catch(this.fatal.bind(this, "error loading cover"));

    this.state.rendition.hooks.content.register(this.applyTheme.bind(this));
    this.state.rendition.hooks.content.register(this.loadFonts.bind(this));

    this.state.rendition.on("relocated", this.onRenditionRelocated.bind(this));
    this.state.rendition.on("relocated", () => {
        clearTimeout(this.state.ttsAdvanceTimer);
        this.state.ttsAdvanceTimer = null;
        if (this.state.ttsSpeaking && !this.state.ttsAbort) {
            // A display() promise may settle before epub.js emits relocated.
            // Do not use the navigation lock to decide whether to rebuild TTS:
            // every relocation during long-track playback must preserve the
            // persistent audio element and resync its current paragraph. A
            // chapter transition deliberately disables track mode first.
            if (this.state.ttsTrackMode) {
                this.state.ttsAutoNavigating = false;
                this.state.ttsAutoNavigationCFI = null;
                requestAnimationFrame(() => this.updateTTSTrackParagraph(true));
                return;
            }
            this.readPageTTS();
        }
    });
    this.state.rendition.on("click", this.onRenditionClick.bind(this));
    this.state.rendition.on("keyup", this.onKeyUp.bind(this));
    this.state.rendition.on("displayed", this.onRenditionDisplayedTouchSwipe.bind(this));
    this.state.rendition.on("relocated", this.onRenditionRelocatedUpdateIndicators.bind(this));
    this.state.rendition.on("relocated", this.onRenditionRelocatedSavePos.bind(this));
    this.state.rendition.on("started", this.onRenditionStartedRestorePos.bind(this));
    this.state.rendition.on("displayError", this.fatal.bind(this, "error rendering book"));

    this.state.rendition.display();

    if (this.state.dictInterval) window.clearInterval(this.state.dictInterval);
    this.state.dictInterval = window.setInterval(this.checkDictionary.bind(this), 50);
    this.doDictionary(null);
};

App.prototype.loadSettingsFromStorage = function () {
    ["theme", "font", "font-size", "line-spacing", "margin"].forEach(container => this.restoreChipActive(container));
};

App.prototype.restoreChipActive = function (container) {
    let v = localStorage.getItem(`ePubViewer:${container}`);
    if (v) return this.setChipActive(container, v);
    this.setDefaultChipActive(container);
};

App.prototype.setDefaultChipActive = function (container) {
    let el = this.qs(`.chips[data-chips='${container}']`).querySelector(".chip[data-default]");
    this.setChipActive(container, el.dataset.value);
    return el.dataset.value;
};

App.prototype.setChipActive = function (container, value) {
    Array.from(this.qs(`.chips[data-chips='${container}']`).querySelectorAll(".chip[data-value]")).forEach(el => {
        el.classList[el.dataset.value == value ? "add" : "remove"]("active");
    });
    localStorage.setItem(`ePubViewer:${container}`, value);
    this.applyTheme();
    return value;
};

App.prototype.getChipActive = function (container) {
    let el = this.qs(`.chips[data-chips='${container}']`).querySelector(".chip.active[data-value]");
    if (!el) return this.qs(`.chips[data-chips='${container}']`).querySelector(".chip[data-default]");
    return el.dataset.value;
};

App.prototype.doOpenBook = function () {
    var fi = document.createElement("input");
    fi.setAttribute("accept", "application/epub+zip");
    fi.style.display = "none";
    fi.type = "file";
    fi.onchange = event => {
        var reader = new FileReader();
        reader.addEventListener("load", () => {
            var arr = (new Uint8Array(reader.result)).subarray(0, 2);
            var header = "";
            for (var i = 0; i < arr.length; i++) {
                header += arr[i].toString(16);
            }
            if (header == "504b") {
                this.doBook(reader.result, {
                    encoding: "binary"
                });
            } else {
                this.fatal("invalid file", "not an epub book");
            }
        }, false);
        if (fi.files[0]) {
            reader.readAsArrayBuffer(fi.files[0]);
        }
    };
    document.body.appendChild(fi);
    fi.click();
};

App.prototype.fatal = function (msg, err, usersFault) {
    console.error(msg, err);
    document.querySelector(".app .error").classList.remove("hidden");
    document.querySelector(".app .error .error-title").innerHTML = "Error";
    document.querySelector(".app .error .error-description").innerHTML = usersFault ? "" : "Please try reloading the page or using a different browser, and if the error still persists, <a href=\"https://github.com/geek1011/ePubViewer/issues\">report an issue</a>.";
    document.querySelector(".app .error .error-info").innerHTML = msg + ": " + err.toString();
    document.querySelector(".app .error .error-dump").innerHTML = JSON.stringify({
        error: err.toString(),
        stack: err.stack
    });
    try {
        if (!usersFault) Raven.captureException(err);
    } catch (err) {}
};

App.prototype.doReset = function () {
    if (this.state.ttsSpeaking) this.stopTTS();
    else this.releaseTTSWakeLock();
    if (this.state.dictInterval) window.clearInterval(this.state.dictInterval);
    if (this.state.rendition) this.state.rendition.destroy();
    if (this.state.book) this.state.book.destroy();
    this.state = {
        book: null,
        rendition: null,
        ttsSpeaking: false,
        ttsAbort: true,
        ttsRunId: 0,
        ttsBlobPromises: {}
    };
    let ttsButton = this.qs("#tts-fab");
    if (ttsButton) {
        ttsButton.classList.remove("playing");
        ttsButton.setAttribute("aria-pressed", "false");
        ttsButton.setAttribute("aria-label", "Start reading aloud");
    }
    let ttsStatus = this.qs(".tts-status");
    if (ttsStatus) ttsStatus.classList.add("hidden");
    this.qs(".sidebar-wrapper").classList.add("out");
    this.qs(".bar .book-title").innerHTML = "";
    this.qs(".bar .book-author").innerHTML = "";
    this.qs(".bar .loc").innerHTML = "";
    this.qs(".search-results").innerHTML = "";
    this.qs(".search-box").value = "";
    this.qs(".toc-list").innerHTML = "";
    this.qs(".info .cover").src = "";
    this.qs(".info .title").innerHTML = "";
    this.qs(".info .series-info").classList.remove("hidden");
    this.qs(".info .series-name").innerHTML = "";
    this.qs(".info .series-index").innerHTML = "";
    this.qs(".info .author").innerHTML = "";
    this.qs(".info .description").innerHTML = "";
    this.qs(".book").innerHTML = '<div class="empty-wrapper"><div class="empty"><div class="app-name">ePubViewer</div><div class="message"><a href="javascript:ePubViewer.doOpenBook();" class="big-button">Open a Book</a></div></div></div>';
    this.qs(".sidebar-button").classList.add("hidden");
    this.qs(".bar button.prev").classList.add("hidden");
    this.qs(".bar button.next").classList.add("hidden");
    this.doDictionary(null);
};

App.prototype.qs = function (q) {
    return this.ael.querySelector(q);
};

App.prototype.qsa = function (q) {
    return Array.from(this.ael.querySelectorAll(q));
};

App.prototype.el = function (t, c) {
    let e = document.createElement(t);
    if (c) e.classList.add(c);
    return e;
};

App.prototype.onBookReady = function (event) {
    this.qs(".sidebar-button").classList.remove("hidden");
    this.qs(".bar button.prev").classList.remove("hidden");
    this.qs(".bar button.next").classList.remove("hidden");

    console.log("bookKey", this.state.book.key());

    let chars = 1650;
    let key = `${this.state.book.key()}:locations-${chars}`;
    let stored = localStorage.getItem(key);
    console.log("storedLocations", typeof stored == "string" ? stored.substr(0, 40) + "..." : stored);

    if (stored) return this.state.book.locations.load(stored);
    console.log("generating locations");
    return this.state.book.locations.generate(chars).then(() => {
        localStorage.setItem(key, this.state.book.locations.save());
        console.log("locations generated", this.state.book.locations);
    }).catch(err => console.error("error generating locations", err));
};

App.prototype.onTocItemClick = function (href, event) {
    console.log("tocClick", href);
    this.state.rendition.display(href).catch(err => console.warn("error displaying page", err));
    event.stopPropagation();
    event.preventDefault();
};

App.prototype.onNavigationLoaded = function (nav) {
    console.log("navigation", nav);
    let toc = this.qs(".toc-list");
    toc.innerHTML = "";
    let handleItems = (items, indent) => {
        items.forEach(item => {
            let a = toc.appendChild(this.el("a", "item"));
            a.href = item.href;
            a.dataset.href = item.href;
            a.innerHTML = `${"&nbsp;".repeat(indent*4)}${item.label.trim()}`;
            a.addEventListener("click", this.onTocItemClick.bind(this, item.href));
            handleItems(item.subitems, indent + 1);
        });
    };
    handleItems(nav.toc, 0);
};

App.prototype.onRenditionRelocated = function (event) {
    try {this.doDictionary(null);} catch (err) {}
    try {
        let navItem = (function flatten(arr) {
            return [].concat(...arr.map(v => [v, ...flatten(v.subitems)]));
        })(this.state.book.navigation.toc).filter(
            item => this.state.book.canonical(item.href) == this.state.book.canonical(event.start.href)
        )[0] || null;

        this.qsa(".toc-list .item").forEach(el => el.classList[(navItem && el.dataset.href == navItem.href) ? "add" : "remove"]("active"));
    } catch (err) {
        this.fatal("error updating toc", err);
    }
};

App.prototype.onBookMetadataLoaded = function (metadata) {
    console.log("metadata", metadata);
    this.state.bookMetadata = metadata;
    this.qs(".bar .book-title").innerText = metadata.title.trim();
    this.qs(".bar .book-author").innerText = metadata.creator.trim();
    this.qs(".info .title").innerText = metadata.title.trim();
    this.qs(".info .author").innerText = metadata.creator.trim();
    if (!metadata.series || metadata.series.trim() == "") this.qs(".info .series-info").classList.add("hidden");
    this.qs(".info .series-name").innerText = metadata.series.trim();
    this.qs(".info .series-index").innerText = metadata.seriesIndex.trim();
    this.qs(".info .description").innerText = metadata.description;
    if (sanitizeHtml) this.qs(".info .description").innerHTML = sanitizeHtml(metadata.description);
    this.updateTTSMediaMetadata();
};

App.prototype.onBookCoverLoaded = function (url) {
    if (!this.state.book.archived) {
        this.qs(".cover").src = url;
        return;
    }
    this.state.book.archive.createUrl(url).then(url => {
        this.qs(".cover").src = url;
    }).catch(this.fatal.bind(this, "error loading cover"));
};

App.prototype.onKeyUp = function (event) {
    let kc = event.keyCode || event.which;
    let b = null;
    if (kc == 37) {
        this.state.rendition.prev();
        b = this.qs(".app .bar button.prev");
    } else if (kc == 39) {
        this.state.rendition.next();
        b = this.qs(".app .bar button.next");
    }
    if (b) {
        b.style.transform = "scale(1.15)";
        window.setTimeout(() => b.style.transform = "", 150);
    }
};

App.prototype.onRenditionClick = function (event) {
    try {
        if (event.target.tagName.toLowerCase() == "a" && event.target.href) return;
        if (event.target.parentNode.tagName.toLowerCase() == "a" && event.target.parentNode.href) return;
        if (window.getSelection().toString().length !== 0) return;
        if (this.state.rendition.manager.getContents()[0].window.getSelection().toString().length !== 0) return;
    } catch (err) {}

    let wrapper = this.state.rendition.manager.container;
    let third = wrapper.clientWidth / 3;
    let x = event.pageX - wrapper.scrollLeft;
    let b = null;
    if (x > wrapper.clientWidth - 20) {
        event.preventDefault();
        this.doSidebar();
    } else if (x < third) {
        event.preventDefault();
        this.state.rendition.prev();
        b = this.qs(".bar button.prev");
    } else if (x > (third * 2)) {
        event.preventDefault();
        this.state.rendition.next();
        b = this.qs(".bar button.next");
    }
    if (b) {
        b.style.transform = "scale(1.15)";
        window.setTimeout(() => b.style.transform = "", 150);
    }
};

App.prototype.onRenditionDisplayedTouchSwipe = function (event) {
    let start = null
    let end = null;
    const el = event.document.documentElement;

    el.addEventListener('touchstart', event => {
        start = event.changedTouches[0];
    });
    el.addEventListener('touchend', event => {
        end = event.changedTouches[0];

        let hr = (end.screenX - start.screenX) / el.getBoundingClientRect().width;
        let vr = (end.screenY - start.screenY) / el.getBoundingClientRect().height;

        if (hr > vr && hr > 0.25) return this.state.rendition.prev();
        if (hr < vr && hr < -0.25) return this.state.rendition.next();
        if (vr > hr && vr > 0.25) return;
        if (vr < hr && vr < -0.25) return;
    });
};

App.prototype.applyTheme = function () {
    let theme = {
        bg: this.getChipActive("theme").split(";")[0],
        fg: this.getChipActive("theme").split(";")[1],
        l: "#1e83d2",
        ff: this.getChipActive("font"),
        fs: this.getChipActive("font-size"),
        lh: this.getChipActive("line-spacing"),
        ta: "justify",
        m: this.getChipActive("margin")
    };

    let rules = {
        "body": {
            "background": theme.bg,
            "color": theme.fg,
            "font-family": theme.ff != "" ? `${theme.ff} !important` : "!invalid-hack",
            "font-size": theme.fs != "" ? `${theme.fs} !important` : "!invalid-hack",
            "line-height": `${theme.lh} !important`,
            "text-align": `${theme.ta} !important`,
            "padding-top": theme.m,
            "padding-bottom": theme.m
        },
        "p": {
            "font-family": theme.ff != "" ? `${theme.ff} !important` : "!invalid-hack",
            "font-size": theme.fs != "" ? `${theme.fs} !important` : "!invalid-hack",
        },
        "a": {
            "color": "inherit !important",
            "text-decoration": "none !important",
            "-webkit-text-fill-color": "inherit !important"
        },
        "a:link": {
            "color": `${theme.l} !important`,
            "text-decoration": "none !important",
            "-webkit-text-fill-color": `${theme.l} !important`
        },
        "a:link:hover": {
            "background": "rgba(0, 0, 0, 0.1) !important"
        },
        "img": {
            "max-width": "100% !important"
        },
    };

    try {
        this.ael.style.background = theme.bg;
        this.ael.style.fontFamily = theme.ff;
        this.ael.style.color = theme.fg;
        if(this.state.rendition) this.state.rendition.getContents().forEach(c => c.addStylesheetRules(rules));
    } catch (err) {
        console.error("error applying theme", err);
    }
};

App.prototype.loadFonts = function() {
    this.state.rendition.getContents().forEach(c => {
        [
            "https://fonts.googleapis.com/css?family=Arbutus+Slab",
            "https://fonts.googleapis.com/css?family=Lato:400,400i,700,700i"
        ].forEach(url => {
            let el = c.document.body.appendChild(c.document.createElement("link"));
            el.setAttribute("rel", "stylesheet");
            el.setAttribute("href", url);
        });
    });
};

App.prototype.onRenditionRelocatedUpdateIndicators = function (event) {
    try {
        let stxt = (event.start.location > 0) ? `Loc ${event.start.location}/${this.state.book.locations.length()}` : ((event.start.percentage > 0 && event.start.percentage < 1) ? `${Math.round(event.start.percentage * 100)}%` : ``);
        this.qs(".bar .loc").innerHTML = stxt;
    } catch (err) {
        console.error("error updating indicators");
    }
};

App.prototype.onRenditionRelocatedSavePos = function (event) {
    localStorage.setItem(`${this.state.book.key()}:pos`, event.start.cfi);
};

App.prototype.onRenditionStartedRestorePos = function (event) {
    try {
        if (this.pendingLocator) {
            let requested = this.pendingLocator;
            this.pendingLocator = null;
            this.state.rendition.display(requested);
            return;
        }
        let stored = localStorage.getItem(`${this.state.book.key()}:pos`);
        console.log("storedPos", stored);
        if (stored) this.state.rendition.display(stored);
    } catch (err) {
        this.fatal("error restoring position", err);
    }
};

App.prototype.checkDictionary = function () {
    try {
        let selection = this.state.rendition.manager ? this.state.rendition.manager.getContents()[0].window.getSelection().toString().trim() : "";
        if (selection.length < 2 || selection.indexOf(" ") > -1) {
            if (this.state.showDictTimeout) window.clearTimeout(this.state.showDictTimeout);
            this.doDictionary(null);
            return;
        }
        this.state.showDictTimeout = window.setTimeout(() => {
            try {
                let newSelection = this.state.rendition.manager.getContents()[0].window.getSelection().toString().trim();
                if (newSelection == selection) this.doDictionary(newSelection);
            } catch (err) {console.error(`showDictTimeout: ${err.toString()}`)}
        }, 300);
    } catch (err) {console.error(`checkDictionary: ${err.toString()}`)}
};

App.prototype.doDictionary = function (word) {
    if (this.state.lastWord) if (this.state.lastWord == word) return;
    this.state.lastWord = word;

    if (!this.qs(".dictionary-wrapper").classList.contains("hidden")) console.log("hide dictionary");
    this.qs(".dictionary-wrapper").classList.add("hidden");
    this.qs(".dictionary").innerHTML = "";
    if (!word) return;

    console.log(`define ${word}`);
    this.qs(".dictionary-wrapper").classList.remove("hidden");
    this.qs(".dictionary").innerHTML = "";

    let definitionEl = this.qs(".dictionary").appendChild(document.createElement("div"));
    definitionEl.classList.add("definition");

    let wordEl = definitionEl.appendChild(document.createElement("div"));
    wordEl.classList.add("word");
    wordEl.innerText = word;

    let meaningsEl = definitionEl.appendChild(document.createElement("div"));
    meaningsEl.classList.add("meanings");
    meaningsEl.innerHTML = "Loading";

    fetch(`https://dict.geek1011.net/word/${encodeURIComponent(word)}`).then(resp => {
        if (resp.status >= 500) throw new Error(`Dictionary not available`);
        return resp.json();
    }).then(obj => {
        if (obj.status == "error") throw new Error(`ApiError: ${obj.result}`);
        return obj.result;
    }).then(word => {
        console.log("dictLookup", word);
        meaningsEl.innerHTML = "";
        wordEl.innerText = [word.word].concat(word.alternates || []).join(", ").toLowerCase();
        
        if (word.info && word.info.trim() != "") {
            let infoEl = meaningsEl.appendChild(document.createElement("div"));
            infoEl.classList.add("info");
            infoEl.innerText = word.info;
        }
        
        (word.meanings || []).map((meaning, i) => {
            let meaningEl = meaningsEl.appendChild(document.createElement("div"));
            meaningEl.classList.add("meaning");

            let meaningTextEl = meaningEl.appendChild(document.createElement("div"));
            meaningTextEl.classList.add("text");
            meaningTextEl.innerText = `${i + 1}. ${meaning.text}`;

            if (meaning.example && meaning.example.trim() != "") {
                let meaningExampleEl = meaningEl.appendChild(document.createElement("div"));
                meaningExampleEl.classList.add("example");
                meaningExampleEl.innerText = meaning.example;
            }
        });
        
        if (word.credit && word.credit.trim() != "") {
            let creditEl = meaningsEl.appendChild(document.createElement("div"));
            creditEl.classList.add("credit");
            creditEl.innerText = word.credit;
        }
    }).catch(err => {
        try {
            console.error("dictLookup", err);
            if (err.toString().toLowerCase().indexOf("not in dictionary") > -1) {
                meaningsEl.innerHTML = "Word not in dictionary.";
                return;
            }
            if (err.toString().toLowerCase().indexOf("not available") > -1 || err.toString().indexOf("networkerror") > -1 || err.toString().indexOf("failed to fetch") > -1) {
                meaningsEl.innerHTML = "Dictionary not available.";
                return;
            }
            meaningsEl.innerHTML = `Dictionary not available: ${err.toString()}`;
        } catch (err) {}
    });
};

App.prototype.doFullscreen = () => {
    document.fullscreenEnabled = document.fullscreenEnabled || document.mozFullScreenEnabled || document.documentElement.webkitRequestFullScreen;

    let requestFullscreen = element => {
        if (element.requestFullscreen) {
            element.requestFullscreen();
        } else if (element.mozRequestFullScreen) {
            element.mozRequestFullScreen();
        } else if (element.webkitRequestFullScreen) {
            element.webkitRequestFullScreen(Element.ALLOW_KEYBOARD_INPUT);
        }
    };

    if (document.fullscreenEnabled) {
        requestFullscreen(document.documentElement);
    }
};

App.prototype.doSearch = function (q) {
    return Promise.all(this.state.book.spine.spineItems.map(item => {
        return item.load(this.state.book.load.bind(this.state.book)).then(doc => {
            let results = item.find(q);
            item.unload();
            return Promise.resolve(results);
        });
    })).then(results => Promise.resolve([].concat.apply([], results)));
};

App.prototype.onResultClick = function (href, event) {
    console.log("tocClick", href);
    this.state.rendition.display(href);
    event.stopPropagation();
    event.preventDefault();
};

App.prototype.doTab = function (tab) {
    try {
        this.qsa(".tab-list .item").forEach(el => el.classList[(el.dataset.tab == tab) ? "add" : "remove"]("active"));
        this.qsa(".tab-container .tab").forEach(el => el.classList[(el.dataset.tab != tab) ? "add" : "remove"]("hidden"));
        try {
            this.qs(".tab-container").scrollTop = 0;
        } catch (err) {}
    } catch (err) {
        this.fatal("error showing tab", err);
    }
};

App.prototype.onTabClick = function (tab, event) {
    console.log("tabClick", tab);
    this.doTab(tab);
    event.stopPropagation();
    event.preventDefault();
};

App.prototype.onSearchClick = function (event) {
    this.doSearch(this.qs(".sidebar .search-bar .search-box").value.trim()).then(results => {
        this.qs(".sidebar .search-results").innerHTML = "";
        let resultsEl = document.createDocumentFragment();
        results.slice(0, 200).forEach(result => {
            let resultEl = resultsEl.appendChild(this.el("a", "item"));
            resultEl.href = result.cfi;
            resultEl.addEventListener("click", this.onResultClick.bind(this, result.cfi));

            let textEl = resultEl.appendChild(this.el("div", "text"));
            textEl.innerText = result.excerpt.trim();
        });
        this.qs(".app .sidebar .search-results").appendChild(resultsEl);
    }).catch(err => this.fatal("error searching book", err));
};

App.prototype.doSidebar = function () {
    this.qs(".sidebar-wrapper").classList.toggle('out');
};

App.prototype.loadTTSPreferences = function () {
    let stopAfter = localStorage.getItem("ePubViewer:tts-stop-after") === "true";
    this.qs("#tts-stop-after").checked = stopAfter;

    let duration = parseInt(localStorage.getItem("ePubViewer:tts-duration-minutes") || "30", 10);
    if (isNaN(duration)) duration = 30;
    duration = Math.max(1, Math.min(480, duration));
    this.qs("#tts-duration-minutes").value = duration;

    let keepScreenOn = localStorage.getItem("ePubViewer:tts-keep-screen-on") === "true";
    let wakeCheckbox = this.qs("#tts-keep-screen-on");
    wakeCheckbox.checked = keepScreenOn;
    wakeCheckbox.disabled = !("wakeLock" in navigator);
    this.syncTTSOptionsUI();
    this.updateTTSWakeStatus();
};

App.prototype.saveTTSPreferences = function () {
    let options = this.getTTSOptions();
    localStorage.setItem("ePubViewer:tts-stop-after", options.stopAfter ? "true" : "false");
    localStorage.setItem("ePubViewer:tts-duration-minutes", options.durationMinutes.toString());
    localStorage.setItem("ePubViewer:tts-keep-screen-on", options.keepScreenOn ? "true" : "false");
    this.qs("#tts-duration-minutes").value = options.durationMinutes;
    this.syncTTSOptionsUI();
    if (this.state.ttsSpeaking) {
        if (this.state.ttsStopAfter !== options.stopAfter || this.state.ttsDurationMinutes !== options.durationMinutes) {
            this.pauseTTSTimer();
            this.state.ttsStopAfter = options.stopAfter;
            this.state.ttsDurationMinutes = options.durationMinutes;
            this.state.ttsRemainingMs = options.stopAfter ? options.durationMinutes * 60 * 1000 : 0;
            if (!this.state.ttsPaused) this.startTTSTimer();
            this.renderTTSStatus();
        }
        if (!this.state.ttsPaused && options.keepScreenOn) this.requestTTSWakeLock();
        else this.releaseTTSWakeLock();
    }
};

App.prototype.getTTSOptions = function () {
    let durationMinutes = parseInt(this.qs("#tts-duration-minutes").value || "30", 10);
    if (isNaN(durationMinutes)) durationMinutes = 30;
    durationMinutes = Math.max(1, Math.min(480, durationMinutes));
    return {
        stopAfter: this.qs("#tts-stop-after").checked,
        durationMinutes: durationMinutes,
        keepScreenOn: this.qs("#tts-keep-screen-on").checked
    };
};

App.prototype.syncTTSOptionsUI = function () {
    this.qs("#tts-duration-minutes").disabled = !this.qs("#tts-stop-after").checked;
};

App.prototype.toggleTTSOptions = function (open) {
    let panel = this.qs("#tts-options-panel");
    let button = this.qs("#tts-options-button");
    if (typeof open !== "boolean") open = panel.classList.contains("hidden");
    panel.classList.toggle("hidden", !open);
    button.setAttribute("aria-expanded", open ? "true" : "false");
    button.setAttribute("aria-label", open ? "Close read-aloud options" : "Open read-aloud options");
    if (open) this.qs("#tts-stop-after").focus();
};

App.prototype.updateTTSWakeStatus = function (message) {
    let status = this.qs("#tts-wake-status");
    if (!status) return;
    status.classList.remove("active", "unsupported", "error");
    if (!("wakeLock" in navigator)) {
        status.textContent = "Keep screen on is not supported by this browser.";
        status.classList.add("unsupported");
    } else if (message) {
        status.textContent = message;
    } else if (this.state.ttsWakeLock && !this.state.ttsWakeLock.released) {
        status.textContent = "Screen will stay on while TTS is playing.";
        status.classList.add("active");
    } else if (this.qs("#tts-keep-screen-on").checked) {
        status.textContent = "Screen-on mode will activate when TTS starts.";
    } else {
        status.textContent = "Screen-on mode is off.";
    }
};

App.prototype.requestTTSWakeLock = function () {
    if (!("wakeLock" in navigator) || !this.state.ttsSpeaking || this.state.ttsPaused ||
        !this.qs("#tts-keep-screen-on").checked || document.visibilityState !== "visible") {
        this.updateTTSWakeStatus();
        return Promise.resolve(null);
    }
    if (this.state.ttsWakeLock && !this.state.ttsWakeLock.released) return Promise.resolve(this.state.ttsWakeLock);
    let that = this;
    return navigator.wakeLock.request("screen").then(lock => {
        if (!that.state.ttsSpeaking || that.state.ttsPaused ||
            !that.qs("#tts-keep-screen-on").checked || document.visibilityState !== "visible") {
            return lock.release().catch(() => {}).then(() => {
                that.updateTTSWakeStatus();
                return null;
            });
        }
        that.state.ttsWakeLock = lock;
        that.updateTTSWakeStatus();
        lock.addEventListener("release", () => {
            if (that.state.ttsWakeLock === lock) that.state.ttsWakeLock = null;
            that.updateTTSWakeStatus();
        });
        return lock;
    }).catch(err => {
        console.warn("screen wake lock", err);
        that.updateTTSWakeStatus("The device did not allow the screen to stay on.");
        that.qs("#tts-wake-status").classList.add("error");
        return null;
    });
};

App.prototype.releaseTTSWakeLock = function () {
    let lock = this.state && this.state.ttsWakeLock;
    if (!lock) {
        if (this.ael) this.updateTTSWakeStatus();
        return Promise.resolve();
    }
    this.state.ttsWakeLock = null;
    let released = lock.released ? Promise.resolve() : lock.release().catch(() => {});
    return released.then(() => this.updateTTSWakeStatus());
};

App.prototype.onTTSVisibilityChange = function () {
    if (document.visibilityState === "visible" && this.state.ttsSpeaking && !this.state.ttsPaused) {
        if (this.qs("#tts-keep-screen-on").checked) this.requestTTSWakeLock();
        if (this.state.ttsTrackMode) this.updateTTSTrackParagraph(true);
        let audio = this.state.ttsAudio;
        if (audio && audio.src && audio.paused && !audio.ended && !this.state.ttsTrackLoading) {
            audio.play().catch(err => console.warn("foreground TTS recovery", err));
        }
    } else if (document.visibilityState !== "visible") {
        this.releaseTTSWakeLock();
    }
};

App.prototype.setupTTSMediaSession = function () {
    if (!("mediaSession" in navigator)) return;
    let handlers = {
        play: () => this.state.ttsSpeaking ? this.resumeTTS() : this.startTTS(),
        pause: () => this.pauseTTS(),
        stop: () => this.stopTTS()
    };
    Object.keys(handlers).forEach(action => {
        try { navigator.mediaSession.setActionHandler(action, handlers[action]); } catch (err) {}
    });
    this.updateTTSMediaMetadata();
};

App.prototype.updateTTSMediaMetadata = function () {
    if (!("mediaSession" in navigator)) return;
    let metadata = this.state.bookMetadata || {};
    let title = (metadata.title || this.qs(".bar .book-title").textContent || "Ebook").trim();
    let artist = (metadata.creator || this.qs(".bar .book-author").textContent || "").trim();
    try {
        if (typeof MediaMetadata !== "undefined") {
            navigator.mediaSession.metadata = new MediaMetadata({
                title: title,
                artist: artist,
                album: "MicsBook",
                artwork: [
                    { src: "/static/icons/icon-192.png", sizes: "192x192", type: "image/png" },
                    { src: "/static/icons/icon-512.png", sizes: "512x512", type: "image/png" }
                ]
            });
        }
    } catch (err) {}
};

App.prototype.setTTSMediaPlaybackState = function (state) {
    if (!("mediaSession" in navigator)) return;
    try { navigator.mediaSession.playbackState = state; } catch (err) {}
};

App.prototype.updateTTSMediaPositionState = function (audio) {
    if (!("mediaSession" in navigator) || !("setPositionState" in navigator.mediaSession) || !audio) return;
    let duration = audio.duration;
    if (!duration || !isFinite(duration) || duration <= 0) return;
    try {
        navigator.mediaSession.setPositionState({
            duration: duration,
            playbackRate: audio.playbackRate || 1,
            position: Math.min(Math.max(audio.currentTime || 0, 0), duration)
        });
    } catch (err) {}
};

App.prototype.clearTTSMediaPositionState = function () {
    if (!("mediaSession" in navigator) || !("setPositionState" in navigator.mediaSession)) return;
    try { navigator.mediaSession.setPositionState(); } catch (err) {}
};

App.prototype.startTTSTimer = function () {
    clearTimeout(this.state.ttsStopTimer);
    clearInterval(this.state.ttsCountdownTimer);
    this.state.ttsStopTimer = null;
    this.state.ttsCountdownTimer = null;
    if (!this.state.ttsStopAfter) return;
    if (!this.state.ttsRemainingMs || this.state.ttsRemainingMs <= 0) {
        this.finishTimedTTS();
        return;
    }
    this.state.ttsDeadline = Date.now() + this.state.ttsRemainingMs;
    this.state.ttsStopTimer = setTimeout(() => this.finishTimedTTS(), this.state.ttsRemainingMs);
    this.state.ttsCountdownTimer = setInterval(() => {
        if (this.hasTTSTimeExpired()) this.finishTimedTTS();
        else this.renderTTSStatus();
    }, 1000);
};

App.prototype.pauseTTSTimer = function () {
    if (this.state.ttsDeadline) {
        this.state.ttsRemainingMs = Math.max(0, this.state.ttsDeadline - Date.now());
        this.state.ttsDeadline = null;
    }
    clearTimeout(this.state.ttsStopTimer);
    clearInterval(this.state.ttsCountdownTimer);
    this.state.ttsStopTimer = null;
    this.state.ttsCountdownTimer = null;
};

App.prototype.hasTTSTimeExpired = function () {
    if (!this.state.ttsStopAfter) return false;
    if (this.state.ttsDeadline) return Date.now() >= this.state.ttsDeadline;
    return this.state.ttsRemainingMs <= 0;
};

App.prototype.finishTimedTTS = function () {
    if (!this.state.ttsSpeaking) return;
    this.state.ttsRemainingMs = 0;
    this.stopTTS("Play time finished");
};

App.prototype.pauseTTS = function () {
    if (!this.state.ttsSpeaking || this.state.ttsPaused) return;
    this.state.ttsPaused = true;
    this.state.ttsStatusBeforePause = this.state.ttsStatusBase;
    this.pauseTTSTimer();
    let audio = this.state.ttsAudio;
    if (audio) {
        try { audio.pause(); } catch (err) {}
    }
    if (window.speechSynthesis) {
        try { window.speechSynthesis.pause(); } catch (err) {}
    }
    this.releaseTTSWakeLock();
    this.setTTSMediaPlaybackState("paused");
    this.state.ttsStatusBase = "Paused";
    this.renderTTSStatus();
};

App.prototype.resumeTTS = function () {
    if (!this.state.ttsSpeaking || !this.state.ttsPaused) return;
    this.state.ttsPaused = false;
    this.startTTSTimer();
    if (window.speechSynthesis && this.state.ttsUtterance) {
        try { window.speechSynthesis.resume(); } catch (err) {}
    } else if (this.state.ttsAudio && this.state.ttsAudio.src) {
        this.state.ttsAudio.play().catch(err => {
            if (this.state.ttsAudioReject) this.state.ttsAudioReject(err);
            else console.warn("resume TTS", err);
        });
    } else if (this.state.ttsTrackMode) {
        if (!this.state.ttsTrackLoading) this.playTTSTrack(this.state.ttsTrackIndex, this.state.ttsRunId);
    } else {
        this.playNextChunk(this.state.ttsRunId);
    }
    this.requestTTSWakeLock();
    this.setTTSMediaPlaybackState("playing");
    this.state.ttsStatusBase = this.state.ttsStatusBeforePause || "Reading current page...";
    this.state.ttsStatusBeforePause = null;
    this.renderTTSStatus();
};

App.prototype.onTTSClick = function () {
    if (!this.state.ttsSpeaking) {
        this.startTTS();
        return;
    }
    if (this.state.ttsPaused) {
        this.resumeTTS();
    } else {
        this.pauseTTS();
    }
};

App.prototype.setTTSPlaying = function (playing) {
    this.state.ttsSpeaking = playing;
    clearTimeout(this.state.ttsNoticeTimer);
    let button = this.qs("#tts-fab");
    button.classList.toggle("playing", playing);
    button.setAttribute("aria-pressed", playing ? "true" : "false");
    button.setAttribute("aria-label", playing ? "Stop reading aloud" : "Start reading aloud");
    button.setAttribute("title", playing ? "Stop reading aloud" : "Read aloud");
    let label = button.querySelector(".tts-sr-label");
    if (label) label.textContent = playing ? "Stop reading aloud" : "Start reading aloud";
    let status = this.qs(".tts-status");
    if (playing) {
        status.classList.remove("finished");
        status.classList.remove("hidden");
        this.updateTTSStatus("Reading current page...");
    } else {
        status.classList.add("hidden");
    }
};

App.prototype.updateTTSStatus = function (message) {
    this.state.ttsStatusBase = message;
    this.renderTTSStatus();
};

App.prototype.renderTTSStatus = function () {
    let text = this.qs(".tts-status-text");
    if (!text) return;
    let message = this.state.ttsStatusBase || "Reading current page...";
    if (this.state.ttsStopAfter && this.state.ttsSpeaking) {
        let remaining = this.state.ttsDeadline ? Math.max(0, this.state.ttsDeadline - Date.now()) : Math.max(0, this.state.ttsRemainingMs || 0);
        let seconds = Math.ceil(remaining / 1000);
        let minutes = Math.floor(seconds / 60);
        let secondValue = seconds % 60;
        let secondPart = (secondValue < 10 ? "0" : "") + secondValue;
        message += " • " + minutes + ":" + secondPart + " left";
    }
    text.textContent = message;
};

App.prototype.showTTSNotice = function (message) {
    let status = this.qs(".tts-status");
    status.classList.add("finished");
    status.classList.remove("hidden");
    this.qs(".tts-status-text").textContent = message;
    clearTimeout(this.state.ttsNoticeTimer);
    this.state.ttsNoticeTimer = setTimeout(() => status.classList.add("hidden"), 4000);
};

App.prototype.setupTTSStatusDrag = function () {
    let status = this.qs(".tts-status");
    if (!status) return;
    let savedKey = "ePubViewer:tts-status-pos";
    let dragging = null;
    let moveHandler = null;
    let upHandler = null;

    let applyPosition = (x, y) => {
        status.style.right = "auto";
        status.style.bottom = "auto";
        status.style.left = x + "px";
        status.style.top = y + "px";
    };

    let savePosition = () => {
        try {
            let rect = status.getBoundingClientRect();
            window.localStorage.setItem(savedKey, JSON.stringify({
                left: rect.left,
                top: rect.top
            }));
        } catch (err) {
            /* ignore */
        }
    };

    try {
        let saved = window.localStorage.getItem(savedKey);
        if (saved) {
            let pos = JSON.parse(saved);
            applyPosition(pos.left, pos.top);
        }
    } catch (err) {
        /* ignore */
    }

    status.addEventListener("pointerdown", (e) => {
        if (e.button !== 0 && e.pointerType === "mouse") return;
        if (status.classList.contains("hidden")) return;
        e.preventDefault();
        status.classList.add("dragging");
        dragging = {
            pointerId: e.pointerId,
            startX: e.clientX,
            startY: e.clientY,
            startLeft: status.getBoundingClientRect().left,
            startTop: status.getBoundingClientRect().top,
            moved: false
        };
        status.setPointerCapture(e.pointerId);

        moveHandler = (ev) => {
            if (!dragging || ev.pointerId !== dragging.pointerId) return;
            ev.preventDefault();
            let dx = ev.clientX - dragging.startX;
            let dy = ev.clientY - dragging.startY;
            if (!dragging.moved && Math.abs(dx) + Math.abs(dy) < 4) return;
            dragging.moved = true;
            let x = dragging.startLeft + dx;
            let y = dragging.startTop + dy;
            x = Math.max(0, Math.min(x, window.innerWidth - status.offsetWidth));
            y = Math.max(0, Math.min(y, window.innerHeight - status.offsetHeight));
            applyPosition(x, y);
        };

        upHandler = (ev) => {
            if (!dragging || ev.pointerId !== dragging.pointerId) return;
            status.classList.remove("dragging");
            if (dragging.moved) savePosition();
            dragging = null;
            status.releasePointerCapture(ev.pointerId);
            window.removeEventListener("pointermove", moveHandler);
            window.removeEventListener("pointerup", upHandler);
            window.removeEventListener("pointercancel", upHandler);
        };

        window.addEventListener("pointermove", moveHandler);
        window.addEventListener("pointerup", upHandler);
        window.addEventListener("pointercancel", upHandler);
    });
};

App.prototype.startTTS = function () {
    if (!this.state.rendition) {
        console.log("TTS unavailable");
        return;
    }
    try {
        if (this.state.ttsSpeaking) {
            this.stopTTS();
            return;
        }

        this.saveTTSPreferences();
        let options = this.getTTSOptions();
        this.state.ttsAbort = false;
        this.state.ttsPaused = false;
        this.state.ttsIndex = 0;
        this.state.ttsStopAfter = options.stopAfter;
        this.state.ttsDurationMinutes = options.durationMinutes;
        this.state.ttsRemainingMs = options.stopAfter ? options.durationMinutes * 60 * 1000 : 0;
        this.state.ttsDeadline = null;
        this.state.ttsBlobPromises = {};
        this.state.ttsTrackMode = false;
        this.state.ttsTracks = null;
        this.state.ttsTrackIndex = 0;
        this.state.ttsTrackParagraphIndex = 0;
        this.toggleTTSOptions(false);
        this.setTTSPlaying(true);
        this.startTTSTimer();
        this.requestTTSWakeLock();
        this.updateTTSMediaMetadata();
        this.setTTSMediaPlaybackState("playing");
        this.readPageTTS();
    } catch (err) {
        console.error("startTTS", err);
        this.setTTSPlaying(false);
    }
};

App.prototype.stopTTS = function (reason) {
    this.state.ttsAbort = true;
    this.state.ttsPaused = false;
    this.state.ttsRunId = (this.state.ttsRunId || 0) + 1;
    clearTimeout(this.state.ttsTimeout);
    clearTimeout(this.state.ttsAdvanceTimer);
    clearTimeout(this.state.ttsStopTimer);
    clearInterval(this.state.ttsCountdownTimer);
    this.state.ttsTimeout = null;
    this.state.ttsAdvanceTimer = null;
    this.state.ttsStopTimer = null;
    this.state.ttsCountdownTimer = null;
    this.state.ttsDeadline = null;
    this.cancelTTSOutput();
    this.state.ttsChunks = null;
    this.state.ttsParagraphs = null;
    this.state.ttsTracks = null;
    this.state.ttsTrackMode = false;
    this.state.ttsTrackLoading = false;
    this.state.ttsAutoNavigating = false;
    this.state.ttsAutoNavigationCFI = null;
    this.clearTTSHighlights();
    this.releaseTTSWakeLock();
    this.clearTTSMediaPositionState();
    this.setTTSMediaPlaybackState("none");
    this.setTTSPlaying(false);
    if (reason) this.showTTSNotice(reason);
};

App.prototype.cancelTTSOutput = function () {
    let rejectAudio = this.state.ttsAudioReject;
    this.state.ttsAudioReject = null;
    let audio = this.state.ttsAudio;
    if (audio) {
        try {
            audio.ontimeupdate = null;
            audio.onloadedmetadata = null;
            audio.onplaying = null;
            audio.onpause = null;
            audio.onended = null;
            audio.onerror = null;
            audio.pause();
            audio.removeAttribute("src");
            audio.load();
        } catch (err) {}
    }
    if (this.state.ttsAudioUrl) {
        try { URL.revokeObjectURL(this.state.ttsAudioUrl); } catch (err) {}
    }
    this.state.ttsAudio = null;
    this.state.ttsAudioUrl = null;
    this.state.ttsBlobPromises = {};
    if (rejectAudio) {
        try { rejectAudio(new Error("TTS cancelled")); } catch (err) {}
    }
    if (window.speechSynthesis) {
        try { window.speechSynthesis.cancel(); } catch (err) {}
    }
    this.state.ttsUtterance = null;
};

// Build long, fully downloaded TTS tracks from the rest of the current EPUB
// spine document. This follows the same background-safe pattern as a music
// player: the active and next tracks are blobs before the PWA is hidden, and
// paragraph timing metadata drives highlighting inside each long track.
App.prototype.readPageTTS = function () {
    if (this.state.ttsAbort || !this.state.rendition) return;
    this.cancelTTSOutput();
    this.clearTTSHighlights();
    clearTimeout(this.state.ttsTimeout);
    let runId = (this.state.ttsRunId || 0) + 1;
    this.state.ttsRunId = runId;
    this.state.ttsTrackMode = true;
    this.updateTTSStatus("Preparing background audio...");
    let that = this;
    this.getTTSChapterParagraphs().then(result => {
        if (that.state.ttsAbort || that.state.ttsRunId !== runId) return;
        let paragraphs = result && result.paragraphs || [];
        let tracks = that.buildTTSTracks(paragraphs);
        if (!tracks.length) {
            that.state.ttsNextChapterHref = result && result.nextHref;
            that.advanceTTSChapter();
            return;
        }
        that.state.ttsTracks = tracks;
        that.state.ttsTrackIndex = 0;
        that.state.ttsTrackParagraphIndex = 0;
        that.state.ttsNextChapterHref = result.nextHref;
        that.state.ttsBlobPromises = {};
        that.prefetchTTSTracks(runId, 0);
        // Do not depend on network work after the user immediately backgrounds
        // the PWA. Buffer several long tracks first, then keep reading ahead.
        let ready = tracks.slice(0, Math.min(3, tracks.length)).map((track, index) => that.fetchTTSTrack(track, runId, index));
        Promise.all(ready).then(() => {
            if (!that.state.ttsAbort && that.state.ttsRunId === runId) that.playTTSTrack(0, runId);
        }).catch(err => that.fallbackToLegacyTTS(err));
    }).catch(err => {
        console.warn("long-track TTS unavailable; using page fallback", err);
        if (!that.state.ttsAbort && that.state.ttsRunId === runId) that.readPageTTSLegacy();
    });
};

App.prototype.getTTSChapterParagraphs = function () {
    let that = this;
    return this.getCurrentPageText().then(current => {
        let contents = that.state.rendition.getContents && that.state.rendition.getContents()[0];
        let doc = contents && contents.document;
        if (!doc || !doc.body || !doc.createTreeWalker) {
            return { paragraphs: current && current.paragraphs || [], nextHref: null };
        }
        let walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
        let entries = [];
        let node;
        while ((node = walker.nextNode())) {
            let parent = node.parentElement;
            if (parent && /^(SCRIPT|STYLE|NOSCRIPT|SVG)$/i.test(parent.tagName)) continue;
            let value = (node.nodeValue || "").replace(/\s+/g, " ").trim();
            if (!value) continue;
            entries.push({
                text: value,
                element: that.getTTSParagraphElement(node, doc),
                doc: doc
            });
        }
        let all = that.makeTTSCollection(entries, 0).paragraphs;
        let firstVisible = current && current.paragraphs && current.paragraphs[0];
        let start = firstVisible ? all.findIndex(paragraph => paragraph.element === firstVisible.element) : 0;
        if (start < 0) start = 0;
        all = all.slice(start);
        all.forEach((paragraph, index) => {
            paragraph.sequence = index;
            paragraph.total = all.length;
            try { paragraph.cfi = contents.cfiFromNode(paragraph.element); } catch (err) { paragraph.cfi = ""; }
        });

        let nextHref = null;
        try {
            let location = that.state.rendition.currentLocation();
            let href = location && location.start && location.start.href;
            let section = href && that.state.book.spine.get(href);
            let next = section && section.next && section.next();
            nextHref = next && next.href || null;
        } catch (err) {}
        return { paragraphs: all, nextHref: nextHref };
    });
};

App.prototype.buildTTSTracks = function (paragraphs) {
    let pieces = [];
    paragraphs.forEach(paragraph => {
        let remaining = paragraph.text.trim();
        while (remaining.length > 2400) {
            let cut = remaining.lastIndexOf(" ", 2400);
            if (cut < 1200) cut = 2400;
            pieces.push(Object.assign({}, paragraph, { text: remaining.slice(0, cut).trim() }));
            remaining = remaining.slice(cut).trim();
        }
        if (remaining) pieces.push(Object.assign({}, paragraph, { text: remaining }));
    });

    let tracks = [];
    let track = { paragraphs: [], characters: 0 };
    pieces.forEach(paragraph => {
        let added = paragraph.text.length + (track.paragraphs.length ? 2 : 0);
        if (track.paragraphs.length && track.characters + added > 6000) {
            tracks.push(track);
            track = { paragraphs: [], characters: 0 };
            added = paragraph.text.length;
        }
        track.paragraphs.push(paragraph);
        track.characters += added;
    });
    if (track.paragraphs.length) tracks.push(track);
    return tracks;
};

App.prototype.decodeTTSTimingHeader = function (value, count) {
    if (!value) throw new Error("TTS track timing metadata is missing");
    let padded = value + "=".repeat((4 - value.length % 4) % 4);
    let offsets = JSON.parse(atob(padded.replace(/-/g, "+").replace(/_/g, "/")));
    if (!Array.isArray(offsets) || offsets.length !== count) throw new Error("invalid TTS track timing metadata");
    return offsets.map(value => Math.max(0, Number(value) || 0));
};

App.prototype.fetchTTSTrack = function (track, runId, index) {
    let key = "track:" + runId + ":" + index;
    if (!this.state.ttsBlobPromises) this.state.ttsBlobPromises = {};
    if (this.state.ttsBlobPromises[key]) return this.state.ttsBlobPromises[key];
    let payload = {
        paragraphs: track.paragraphs.map(paragraph => paragraph.text),
        rate: "+0%"
    };
    if (this.state.ttsVoice) payload.voice = this.state.ttsVoice;
    let promise = fetch("/tts/track", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
    }).then(resp => {
        if (!resp.ok) throw new Error("TTS track HTTP " + resp.status);
        let offsets = this.decodeTTSTimingHeader(resp.headers.get("X-TTS-Paragraph-Offsets"), track.paragraphs.length);
        return resp.blob().then(blob => ({ blob: blob, offsets: offsets }));
    });
    this.state.ttsBlobPromises[key] = promise;
    promise.catch(() => {
        if (this.state.ttsBlobPromises && this.state.ttsBlobPromises[key] === promise) {
            delete this.state.ttsBlobPromises[key];
        }
    });
    return promise;
};

App.prototype.prefetchTTSTracks = function (runId, index) {
    if (!this.state.ttsTracks || this.state.ttsRunId !== runId) return;
    for (let i = index; i < Math.min(this.state.ttsTracks.length, index + 5); i++) {
        this.fetchTTSTrack(this.state.ttsTracks[i], runId, i).catch(() => {});
    }
};

App.prototype.playTTSTrack = function (index, runId) {
    if (this.state.ttsAbort || this.state.ttsPaused || this.state.ttsRunId !== runId) return;
    if (!this.state.ttsTracks || index >= this.state.ttsTracks.length) {
        this.advanceTTSChapter();
        return;
    }
    if (this.hasTTSTimeExpired()) {
        this.finishTimedTTS();
        return;
    }
    if (this.state.ttsTrackLoading && this.state.ttsTrackIndex === index) return;

    let track = this.state.ttsTracks[index];
    this.state.ttsTrackIndex = index;
    this.state.ttsTrackParagraphIndex = 0;
    this.state.ttsTrackLoading = true;
    this.updateTTSStatus("Preparing reading track " + (index + 1) + " of " + this.state.ttsTracks.length + "...");
    this.prefetchTTSTracks(runId, index);
    let that = this;
    this.fetchTTSTrack(track, runId, index).then(result => {
        if (that.state.ttsAbort || that.state.ttsRunId !== runId || that.state.ttsTrackIndex !== index) return;
        that.state.ttsTrackLoading = false;
        let audio = that.qs("#tts-audio");
        let url = URL.createObjectURL(result.blob);
        audio.src = url;
        audio.preload = "auto";
        that.state.ttsAudio = audio;
        that.state.ttsAudioUrl = url;
        that.state.ttsTrackOffsets = result.offsets;
        that.state.ttsLastMediaSecond = -1;
        let settled = false;
        let cleanup = () => {
            if (settled) return;
            settled = true;
            audio.ontimeupdate = null;
            audio.onloadedmetadata = null;
            audio.onplaying = null;
            audio.onpause = null;
            audio.onended = null;
            audio.onerror = null;
            if (that.state.ttsAudioUrl === url) that.state.ttsAudioUrl = null;
            if (that.state.ttsAudioReject === rejectAudio) that.state.ttsAudioReject = null;
            URL.revokeObjectURL(url);
        };
        let rejectAudio = err => {
            cleanup();
            if (!that.state.ttsAbort && that.state.ttsRunId === runId) that.fallbackToLegacyTTS(err);
        };
        that.state.ttsAudioReject = rejectAudio;
        audio.onloadedmetadata = () => that.updateTTSMediaPositionState(audio);
        audio.onplaying = () => that.setTTSMediaPlaybackState("playing");
        audio.onpause = () => {
            if (that.state.ttsPaused) that.setTTSMediaPlaybackState("paused");
        };
        audio.ontimeupdate = () => {
            if (that.hasTTSTimeExpired()) {
                that.finishTimedTTS();
                return;
            }
            that.updateTTSTrackParagraph(false);
            let second = Math.floor(audio.currentTime || 0);
            if (second !== that.state.ttsLastMediaSecond) {
                that.state.ttsLastMediaSecond = second;
                that.updateTTSMediaPositionState(audio);
            }
        };
        audio.onended = () => {
            cleanup();
            if (that.state.ttsAbort || that.state.ttsRunId !== runId) return;
            that.state.ttsTrackIndex = index + 1;
            that.playTTSTrack(index + 1, runId);
        };
        audio.onerror = () => {
            cleanup();
            that.fallbackToLegacyTTS(new Error("TTS track audio error"));
        };
        audio.load();
        that.updateTTSTrackParagraph(true);
        if (!that.state.ttsPaused) {
            audio.play().catch(err => {
                cleanup();
                that.fallbackToLegacyTTS(err);
            });
        }
    }).catch(err => {
        that.state.ttsTrackLoading = false;
        if (!that.state.ttsAbort && that.state.ttsRunId === runId) that.fallbackToLegacyTTS(err);
    });
};

App.prototype.updateTTSTrackParagraph = function (forceNavigate) {
    if (!this.state.ttsTrackMode || !this.state.ttsTracks) return;
    let track = this.state.ttsTracks[this.state.ttsTrackIndex];
    let audio = this.state.ttsAudio;
    if (!track || !audio) return;
    let offsets = this.state.ttsTrackOffsets || [];
    let elapsed = (audio.currentTime || 0) * 1000;
    let index = 0;
    while (index + 1 < offsets.length && elapsed >= offsets[index + 1]) index++;
    if (!forceNavigate && index === this.state.ttsTrackParagraphIndex) return;
    this.state.ttsTrackParagraphIndex = index;
    let paragraph = track.paragraphs[index];
    if (!paragraph) return;
    this.refreshTTSParagraphElement(paragraph);
    this.highlightTTSParagraph(paragraph);
    this.updateTTSStatus("Reading paragraph " + (paragraph.sequence + 1) + " of " + paragraph.total);
    if (document.visibilityState !== "visible" || !paragraph.cfi) return;
    // Never navigate when the current paragraph is already on screen. The
    // initial forced refresh used to call display() for the visible paragraph;
    // epub.js can resolve that without emitting relocated, leaving the
    // navigation lock set forever and disabling all later page following.
    if (this.isTTSParagraphVisible(paragraph)) return;
    if (this.state.ttsAutoNavigating) return;
    this.state.ttsAutoNavigating = true;
    this.state.ttsAutoNavigationCFI = paragraph.cfi;
    let targetCFI = paragraph.cfi;
    this.state.rendition.display(targetCFI).then(() => {
        // display() is allowed to complete without a relocated event. Always
        // release our lock from the settled promise as well as the event path.
        if (this.state.ttsAutoNavigationCFI === targetCFI) {
            this.state.ttsAutoNavigating = false;
            this.state.ttsAutoNavigationCFI = null;
        }
        requestAnimationFrame(() => {
            this.refreshTTSParagraphElement(paragraph);
            this.highlightTTSParagraph(paragraph);
        });
    }).catch(err => {
        if (this.state.ttsAutoNavigationCFI === targetCFI) {
            this.state.ttsAutoNavigating = false;
            this.state.ttsAutoNavigationCFI = null;
        }
        console.warn("TTS paragraph navigation", err);
    });
};

// A rendition display can replace or repaginate its iframe. Resolve the saved
// CFI back into the current live document before checking visibility or adding
// the highlight class; stored DOM nodes may belong to the previous page view.
App.prototype.refreshTTSParagraphElement = function (paragraph) {
    if (!paragraph || !paragraph.cfi || !this.state.rendition) return paragraph;
    try {
        let range = this.state.rendition.getRange(paragraph.cfi);
        let node = range && range.startContainer;
        let doc = node && (node.ownerDocument || (node.nodeType === 9 ? node : null));
        if (!node || !doc) return paragraph;
        paragraph.element = this.getTTSParagraphElement(node, doc);
        paragraph.doc = doc;
    } catch (err) {}
    return paragraph;
};

App.prototype.isTTSParagraphVisible = function (paragraph) {
    try {
        let rects = paragraph.element.getClientRects();
        let view = paragraph.doc.defaultView;
        let location = this.state.rendition.currentLocation();
        let startPage = location && location.start && location.start.displayed && location.start.displayed.page || 1;
        let endPage = location && location.end && location.end.displayed && location.end.displayed.page || startPage;
        let layout = this.state.rendition.manager && this.state.rendition.manager.layout;
        let pageWidth = layout && layout.pageWidth ||
            (this.state.rendition.manager.container && this.state.rendition.manager.container.clientWidth) ||
            view.innerWidth;
        // epub.js lays every page of a spine document side by side in one very
        // wide iframe. window.innerWidth is therefore the full chapter width,
        // not the visible reader page. Compare against the current page band.
        let visibleLeft = Math.max(0, (startPage - 1) * pageWidth);
        let visibleRight = Math.max(visibleLeft + pageWidth, endPage * pageWidth);
        for (let i = 0; i < rects.length; i++) {
            let rect = rects[i];
            if (rect.bottom > 0 && rect.top < view.innerHeight && rect.right > visibleLeft && rect.left < visibleRight) return true;
        }
    } catch (err) {}
    return false;
};

App.prototype.advanceTTSChapter = function () {
    if (this.state.ttsAbort || !this.state.rendition) return;
    if (this.hasTTSTimeExpired()) {
        this.finishTimedTTS();
        return;
    }
    let nextHref = this.state.ttsNextChapterHref;
    if (!nextHref) {
        this.stopTTS();
        return;
    }
    this.state.ttsTrackMode = false;
    this.state.ttsAutoNavigating = false;
    this.state.ttsAutoNavigationCFI = null;
    clearTimeout(this.state.ttsAdvanceTimer);
    this.state.ttsAdvanceTimer = setTimeout(() => {
        if (!this.state.ttsAbort) this.stopTTS();
    }, 1800);
    this.state.rendition.display(nextHref).catch(() => this.stopTTS());
};

App.prototype.fallbackToLegacyTTS = function (err) {
    if (this.state.ttsAbort || !this.state.ttsSpeaking) return;
    console.warn("TTS long-track fallback", err);
    this.state.ttsTrackMode = false;
    this.state.ttsTrackLoading = false;
    this.readPageTTSLegacy();
};

// Compatibility fallback for browsers or older TTS backends which do not
// support long tracks and paragraph timing headers.
App.prototype.readPageTTSLegacy = function () {
    if (this.state.ttsAbort || !this.state.rendition) return;
    this.cancelTTSOutput();
    this.clearTTSHighlights();
    clearTimeout(this.state.ttsTimeout);
    let runId = (this.state.ttsRunId || 0) + 1;
    this.state.ttsRunId = runId;
    let that = this;
    this.getCurrentPageText().then(collect => {
        if (that.state.ttsAbort || that.state.ttsRunId !== runId) return;
        if (!collect || !collect.text || !collect.text.trim()) {
            that.advancePageTTS(); // empty page -> try next
            return;
        }
        that.state.ttsParagraphs = collect.paragraphs || [];
        that.state.ttsChunks = that.splitTextIntoChunks(collect.text, that.state.ttsParagraphs);
        that.state.ttsIndex = 0;
        if (!that.state.ttsChunks.length) {
            that.advancePageTTS();
            return;
        }
        that.state.ttsStatus = "Page " + (collect.page || 0) + " - reading " + that.state.ttsChunks.length + " parts";
        that.updateTTSStatus(that.state.ttsStatus);
        that.playNextChunk(runId);
    }).catch(err => {
        console.error("readPageTTS", err);
        if (!that.state.ttsAbort && that.state.ttsRunId === runId) that.stopTTS();
    });
};

// Build a CFI range string from start/end CFIs and resolve it to a DOM Range
App.prototype.buildRangeCfi = function (startCfi, endCfi) {
    try {
        let s = startCfi.indexOf("epubcfi(") === 0 ? startCfi.slice(8, -1) : startCfi;
        let e = endCfi.indexOf("epubcfi(") === 0 ? endCfi.slice(8, -1) : endCfi;
        let sParts = s.split("!");
        let eParts = e.split("!");
        let base = sParts[0];
        let sSeg = (sParts[1] || "").split("/");
        let eSeg = (eParts[1] || "").split("/");
        let common = [];
        let i = 0;
        while (i < sSeg.length && i < eSeg.length && sSeg[i] === eSeg[i]) {
            common.push(sSeg[i]);
            i++;
        }
        let sRest = "/" + sSeg.slice(i).join("/");
        let eRest = "/" + eSeg.slice(i).join("/");
        return "epubcfi(" + base + "!" + common.join("/") + "," + sRest + "," + eRest + ")";
    } catch (err) {
        return null;
    }
};

// Resolve current page to {text, nodes, page} using the rendition's page mapping
App.prototype.getCurrentPageText = function () {
    return new Promise(resolve => {
        try {
            let loc = this.state.rendition.currentLocation();
            let startCfi = loc && loc.start && loc.start.cfi;
            let endCfi = loc && loc.end && loc.end.cfi;
            if (!startCfi) {
                resolve(this.pageTextFallback());
                return;
            }
            let rangeCfi = this.buildRangeCfi(startCfi, endCfi || startCfi);
            if (!rangeCfi) {
                resolve(this.pageTextFallback());
                return;
            }
            // Rendition#getRange resolves the CFI against the visible iframe.
            // Book#getRange resolves against a separate document, so styling
            // those nodes never appears in the reader.
            let range = this.state.rendition.getRange(rangeCfi);
            if (!range) {
                resolve(this.pageTextFallback());
                return;
            }
            resolve(this.collectRangeText(range, loc));
        } catch (err) {
            console.warn("visible page range failed", err);
            resolve(this.pageTextFallback());
        }
    });
};

App.prototype.getTTSParagraphElement = function (node, doc) {
    let element = node && (node.nodeType === 1 ? node : node.parentElement);
    let fallback = element;
    let blockTags = /^(ADDRESS|ARTICLE|ASIDE|BLOCKQUOTE|DD|DIV|DT|FIGCAPTION|FOOTER|H[1-6]|HEADER|LI|MAIN|P|PRE|SECTION|TD|TH)$/;
    while (element && element !== doc.body) {
        if (blockTags.test(element.tagName)) return element;
        element = element.parentElement;
    }
    return fallback || doc.body;
};

App.prototype.makeTTSCollection = function (entries, page) {
    let paragraphs = [];
    entries.forEach(entry => {
        let paragraph = paragraphs.length ? paragraphs[paragraphs.length - 1] : null;
        if (!paragraph || paragraph.element !== entry.element) {
            paragraph = {
                text: "",
                element: entry.element,
                doc: entry.doc
            };
            paragraphs.push(paragraph);
        }
        paragraph.text += (paragraph.text ? " " : "") + entry.text;
    });
    paragraphs = paragraphs.filter(paragraph => paragraph.text.trim().length > 0);
    return {
        text: paragraphs.map(paragraph => paragraph.text).join("\n"),
        paragraphs: paragraphs,
        page: page || 0
    };
};

// Collect live paragraphs within a DOM Range, in document order.
App.prototype.collectRangeText = function (range, loc) {
    let doc = range.startContainer && range.startContainer.ownerDocument || (range.commonAncestorContainer && range.commonAncestorContainer.ownerDocument);
    if (!doc || !doc.body) return { text: "", paragraphs: [] };
    let walker = doc.createTreeWalker ? doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT) : null;
    if (!walker) return { text: "", paragraphs: [] };
    let node;
    let entries = [];
    while ((node = walker.nextNode())) {
        try {
            if (typeof range.intersectsNode === "function" && !range.intersectsNode(node)) continue;
        } catch (err) { continue; }
        let start = node === range.startContainer ? range.startOffset : 0;
        let end = node === range.endContainer ? range.endOffset : (node.nodeValue || "").length;
        let value = (node.nodeValue || "").slice(start, end).replace(/\s+/g, " ").trim();
        if (!value) continue;
        entries.push({
            text: value,
            element: this.getTTSParagraphElement(node, doc),
            doc: doc
        });
    }
    let page = 0;
    try {
        let display = loc && loc.start && loc.start.displayed;
        if (display) page = display.page;
    } catch (err) {}
    return this.makeTTSCollection(entries, page);
};

App.prototype.playNextChunk = function (runId) {
    if (this.state.ttsAbort || !this.state.ttsChunks || this.state.ttsRunId !== runId) return;
    if (this.state.ttsPaused) return;
    if (this.hasTTSTimeExpired()) {
        this.finishTimedTTS();
        return;
    }
    let i = this.state.ttsIndex;
    if (i >= this.state.ttsChunks.length) {
        this.advancePageTTS();
        return;
    }

    let chunk = this.state.ttsChunks[i];
    this.highlightTTSParagraph(chunk.paragraph);
    let paragraphCount = this.state.ttsChunks.paragraphCount || this.state.ttsChunks.length;
    this.state.ttsStatus = "Reading paragraph " + (chunk.paragraphIndex + 1) + " of " + paragraphCount;
    this.updateTTSStatus(this.state.ttsStatus);
    this.prefetchTTSChunks(runId, i);

    this.speakChunk(chunk.text, runId, i).then(() => {
        this.advanceTTSChunk(i, runId);
    }).catch(err => {
        console.error("tts chunk", err);
        if (this.state.ttsAbort || this.state.ttsRunId !== runId) return;
        if (this.state.ttsPaused) return;
        // fallback to browser speech for this chunk
        if (window.speechSynthesis) {
            try {
                let u = new SpeechSynthesisUtterance(chunk.text);
                u.lang = this.state.ttsVoice && this.state.ttsVoice.indexOf("zh") === 0 ? "zh-CN" : "en-US";
                u.rate = 0.9;
                this.state.ttsUtterance = u;
                let finishFallback = () => {
                    if (this.state.ttsAbort || this.state.ttsRunId !== runId) return;
                    this.state.ttsUtterance = null;
                    this.advanceTTSChunk(i, runId);
                };
                u.onend = finishFallback;
                u.onerror = finishFallback;
                window.speechSynthesis.speak(u);
            } catch (err2) {
                this.advanceTTSChunk(i, runId);
            }
        } else {
            this.advanceTTSChunk(i, runId);
        }
    });
};

App.prototype.advanceTTSChunk = function (index, runId) {
    if (this.state.ttsAbort || this.state.ttsRunId !== runId) return;
    this.state.ttsIndex = index + 1;
    if (!this.state.ttsPaused) this.playNextChunk(runId);
};

App.prototype.prefetchTTSChunks = function (runId, index) {
    if (!this.state.ttsChunks || this.state.ttsRunId !== runId) return;
    for (let i = index; i < Math.min(this.state.ttsChunks.length, index + 3); i++) {
        this.fetchTTSBlob(this.state.ttsChunks[i].text, runId, i).catch(() => {});
    }
};

// When a page's chunks are done, auto-advance to the next page. The relocated
// event continues reading on the new page.
App.prototype.advancePageTTS = function () {
    if (this.state.ttsAbort || !this.state.rendition) {
        this.stopTTS();
        return;
    }
    if (this.hasTTSTimeExpired()) {
        this.finishTimedTTS();
        return;
    }
    // The rendition.next() may not relocate if there is no next page; guard with a timer
    clearTimeout(this.state.ttsAdvanceTimer);
    this.state.ttsAdvanceTimer = setTimeout(() => {
        if (!this.state.ttsAbort) this.stopTTS();
    }, 900);
    try {
        this.state.rendition.next();
    } catch (err) {
        this.stopTTS();
    }
};

App.prototype.fetchTTSBlob = function (text, runId, index) {
    let key = runId + ":" + index;
    if (!this.state.ttsBlobPromises) this.state.ttsBlobPromises = {};
    if (this.state.ttsBlobPromises[key]) return this.state.ttsBlobPromises[key];

    let params = new URLSearchParams();
    params.set("text", text);
    if (this.state.ttsVoice) params.set("voice", this.state.ttsVoice);
    params.set("rate", "+0%");

    let promise = fetch("/tts/tts", { method: "POST", body: params }).then(resp => {
        if (!resp.ok) throw new Error("TTS HTTP " + resp.status);
        return resp.blob();
    });
    this.state.ttsBlobPromises[key] = promise;
    promise.catch(() => {
        if (this.state.ttsBlobPromises && this.state.ttsBlobPromises[key] === promise) {
            delete this.state.ttsBlobPromises[key];
        }
    });
    return promise;
};

App.prototype.speakChunk = function (text, runId, index) {
    let that = this;
    return new Promise((resolve, reject) => {
        that.fetchTTSBlob(text, runId, index)
            .then(blob => {
                if (that.state.ttsAbort || that.state.ttsRunId !== runId) {
                    reject(new Error("TTS cancelled"));
                    return;
                }
                let url = URL.createObjectURL(blob);
                let audio = that.qs("#tts-audio");
                audio.src = url;
                that.state.ttsAudio = audio;
                that.state.ttsAudioUrl = url;
                let settled = false;
                let cleanup = () => {
                    if (settled) return;
                    settled = true;
                    audio.ontimeupdate = null;
                    audio.onloadedmetadata = null;
                    audio.onplaying = null;
                    audio.onpause = null;
                    audio.onended = null;
                    audio.onerror = null;
                    if (that.state.ttsAudio === audio) that.state.ttsAudio = null;
                    if (that.state.ttsAudioUrl === url) that.state.ttsAudioUrl = null;
                    if (that.state.ttsAudioReject === fail) that.state.ttsAudioReject = null;
                    URL.revokeObjectURL(url);
                };
                let fail = err => {
                    cleanup();
                    reject(err);
                };
                that.state.ttsAudioReject = fail;
                audio.onloadedmetadata = () => that.updateTTSMediaPositionState(audio);
                audio.onplaying = () => that.setTTSMediaPlaybackState("playing");
                audio.onpause = () => {
                    if (that.state.ttsPaused) that.setTTSMediaPlaybackState("paused");
                };
                audio.ontimeupdate = () => {
                    if (that.hasTTSTimeExpired()) that.finishTimedTTS();
                    else that.updateTTSMediaPositionState(audio);
                };
                audio.onended = () => {
                    cleanup();
                    resolve();
                };
                audio.onerror = () => {
                    fail(new Error("audio error"));
                };
                audio.load();
                if (that.state.ttsPaused) return;
                audio.play().catch(err => {
                    fail(err);
                });
            })
            .catch(reject);
    });
};

App.prototype.highlightTTSParagraph = function (paragraph) {
    this.clearTTSHighlights();
    if (!paragraph || !paragraph.element || !paragraph.doc) return;
    try {
        let style = paragraph.doc.getElementById("tts-reader-highlight-style");
        if (!style) {
            style = paragraph.doc.createElement("style");
            style.id = "tts-reader-highlight-style";
            style.textContent = ".tts-reading-paragraph{" +
                "background:rgba(255,193,7,.22)!important;" +
                "box-shadow:inset 3px 0 0 #f5a000,0 0 0 4px rgba(255,193,7,.12)!important;" +
                "border-radius:4px!important;" +
                "transition:background-color .18s ease,box-shadow .18s ease!important;" +
                "}";
            (paragraph.doc.head || paragraph.doc.documentElement).appendChild(style);
        }
        paragraph.element.classList.add("tts-reading-paragraph");
        this.state.ttsHighlight = [paragraph.element];
    } catch (err) {
        console.warn("TTS paragraph highlight failed", err);
    }
};

App.prototype.clearTTSHighlights = function () {
    if (!this.state.ttsHighlight) return;
    for (let element of this.state.ttsHighlight) {
        try { element.classList.remove("tts-reading-paragraph"); } catch (err) {}
    }
    this.state.ttsHighlight = null;
};

App.prototype.detectLang = function (text) {
    // Basic heuristics: CJK chars -> zh, else en (overridable by adding more later)
    if (/[\u4e00-\u9fff\u3400-\u4dbf]/.test(text)) return "zh";
    return "en";
};

// Keep speech requests manageable while retaining the paragraph association
// used by the visible reading highlight.
App.prototype.splitTextIntoChunks = function (text, paragraphs) {
    let chunks = [];
    let sources = paragraphs && paragraphs.length ? paragraphs : text.split(/\n+/).map(value => ({ text: value.trim() }));
    let maxLength = 320;
    let appendChunk = (value, paragraph, paragraphIndex) => {
        value = value.trim();
        if (value) chunks.push({ text: value, paragraph: paragraph, paragraphIndex: paragraphIndex });
    };
    sources.forEach((paragraph, paragraphIndex) => {
        let para = (paragraph.text || "").trim();
        if (!para) return;
        let sentences = para.match(/[^.!?。！？；;]+[.!?。！？；;]*/g) || [para];
        let current = "";
        for (let s of sentences) {
            let trimmed = s.trim();
            if (!trimmed) continue;
            if (current && current.length + trimmed.length + 1 > maxLength) {
                appendChunk(current, paragraph, paragraphIndex);
                current = "";
            }
            while (trimmed.length > maxLength) {
                let splitAt = trimmed.lastIndexOf(" ", maxLength);
                if (splitAt < maxLength / 2) splitAt = maxLength;
                appendChunk(trimmed.slice(0, splitAt), paragraph, paragraphIndex);
                trimmed = trimmed.slice(splitAt).trim();
            }
            current += (current ? " " : "") + trimmed;
        }
        appendChunk(current, paragraph, paragraphIndex);
    });
    chunks.paragraphCount = sources.filter(paragraph => (paragraph.text || "").trim()).length;
    return chunks;
};

// Fallback for when CFI range resolution fails (band-based, works for scrolled layouts)
App.prototype.pageTextFallback = function () {
    return this.getVisibleText();
};

App.prototype.getVisibleText = function () {
    let entries = [];
    try {
        let scroller = this.state.rendition.manager.container;
        if (!scroller) return { text: "", paragraphs: [] };

        let sb = scroller.getBoundingClientRect();
        let bandTop = sb.top;
        let bandBottom = sb.bottom;

        let views = this.state.rendition.views() || [];
        views.forEach(view => {
            try {
                let content = view.contents;
                let iframe = view.iframe;
                if (!content || !iframe) return;
                let doc = content.document;
                let ir = iframe.getBoundingClientRect();

                let walker = typeof doc.createTreeWalker !== "undefined" ? doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT) : null;
                if (!walker) return;

                let node;
                let processed = new Set();
                while ((node = walker.nextNode())) {
                    if (processed.has(node)) continue;
                    processed.add(node);
                    let value = (node.nodeValue || "").replace(/\s+/g, " ").trim();
                    if (!value) continue;

                    let range = doc.createRange();
                    range.selectNodeContents(node);
                    let rects = range.getClientRects();
                    range.detach && range.detach();

                    let visible = false;
                    for (let i = 0; i < rects.length; i++) {
                        let r = rects[i];
                        if (r.width === 0 && r.height === 0) continue;
                        let top = ir.top + r.top;
                        let bottom = ir.top + r.bottom;
                        if (bottom > bandTop && top < bandBottom) {
                            visible = true;
                            break;
                        }
                    }
                    if (!visible) continue;
                    entries.push({
                        text: value,
                        element: this.getTTSParagraphElement(node, doc),
                        doc: doc
                    });
                }
            } catch (err) {}
        });
    } catch (err) {
        console.error("getVisibleText", err);
    }
    return this.makeTTSCollection(entries, 0);
};

let ePubViewer = null;

try {
    ePubViewer = new App(document.querySelector(".app"));
    window.ePubViewer = ePubViewer;
    let requestedLocator = null;
    try { requestedLocator = new URLSearchParams(location.search).get("locator"); } catch (err) {}
    let ufn = location.hash.replace("#", "");
    if (!ufn && !requestedLocator) ufn = location.search.replace("?", "");
    if (ufn.startsWith("!")) {
        ufn = ufn.replace("!", "");
        document.querySelector(".app button.open").style = "display: none !important";
    }
    if (ufn) {
        ePubViewer.pendingLocator = requestedLocator;
        fetch(ufn).then(resp => {
            if (resp.status != 200) throw new Error("response status: " + resp.status.toString() + " " + resp.statusText);
        }).catch(err => {
            ePubViewer.fatal("error loading book", err, true);
        });
        ePubViewer.doBook(ufn);
    }
} catch (err) {
    document.querySelector(".app .error").classList.remove("hidden");
    document.querySelector(".app .error .error-title").innerHTML = "Error";
    document.querySelector(".app .error .error-description").innerHTML = "Please try reloading the page or using a different browser (Chrome or Firefox), and if the error still persists, <a href=\"https://github.com/geek1011/ePubViewer/issues\">report an issue</a>.";
    document.querySelector(".app .error .error-dump").innerHTML = JSON.stringify({
        error: err.toString(),
        stack: err.stack
    });
    try {
        Raven.captureException(err);
    } catch (err) {}
}
