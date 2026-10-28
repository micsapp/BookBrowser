(function () {
    'use strict';
    function bookID() {
        var value = location.href.match(/\/download\/([^/?#]+)/);
        if (!value) return '';
        return decodeURIComponent(value[1]).replace(/\.[^.]+$/, '');
    }
    var id = bookID();
    if (!id) return;

    var STRINGS = {
        en: {
            home: 'Home',
            home_title: 'Back to the library',
            offline: 'Offline Books',
            offline_required: 'This feature needs a connection. Reading downloaded books and local read-aloud remain available.',
            signin_required: 'Sign in while connected to save bookmarks and notes.',
            save: 'Save',
            save_title: 'Bookmarks and notes',
            about: 'About',
            about_title: 'About MicsBook',
            help: '?',
            help_title: 'User guide',
            menu_title: 'MicsBook menu',
            move_hint: 'Hold and drag to move',
            language: 'Language',
            language_title: 'Switch language',
            lang_button: '中文',
            bookmarks_title: 'Bookmarks & notes',
            bookmark_here: 'Bookmark here',
            write_note: 'Write note',
            save_note: 'Save note',
            field_title: 'Title',
            field_note: 'Note',
            field_tags: 'Tags',
            go: 'Go',
            edit_save: 'Save',
            edit_delete: 'Delete',
            saved: 'Saved.',
            deleted: 'Deleted.',
            nothing_yet: 'Nothing saved in this book yet.',
            saved_in_book: 'Saved in this book',
            manage: 'Manage all bookmarks & notes',
            bookmark_saved: 'Bookmark saved.',
            note_saved: 'Note saved.',
            kind_bookmark: 'bookmark',
            kind_note: 'note',
            delete_prompt: 'Delete this %s?',
            about_heading: 'About MicsBook',
            about_description: 'Read, listen to, and organize your ebook library from any device.',
            build_number: 'Build number',
            build_id: 'Build ID',
            built: 'Built',
            loading: 'loading',
            help_heading: 'How to use this app',
            help_loading: 'Loading the user guide…',
            help_error: 'The user guide could not be loaded. Check your connection and try again.',
            close: 'Close',
            cancel: 'Cancel',
            copyright_notice: 'Books collected on this site were sourced from materials publicly available for download on the internet. If you believe a title infringes copyright, submit a detailed report. Administrators will review it and remove relevant content when warranted.',
            copyright_report: 'Report a copyright concern',
            copyright_title: 'Copyright concern',
            copyright_intro: 'Provide enough information for an administrator to identify the work, understand your rights, and review the requested removal.',
            copyright_name: 'Your name or organization',
            copyright_email: 'Contact email',
            copyright_book: 'Book title or reference',
            copyright_book_placeholder: 'Book title, author, ISBN, or URL',
            copyright_details: 'Detailed claim',
            copyright_details_placeholder: 'Identify the copyrighted work, explain your relationship to it, and describe why removal is requested.',
            copyright_authorized: 'I confirm this information is accurate and I am authorized to submit this report.',
            copyright_submit: 'Send report',
            copyright_submitting: 'Sending…',
            copyright_submitted: 'Report received. An administrator will review it.',
            copyright_failed: 'The report could not be sent. Check the information and try again.',
            sel_translate: 'Translate',
            sel_define: 'Define',
            translate: 'Translate selection',
            define: 'English dictionary definition',
            translating: 'Translating…',
            translate_failed: 'Translation failed. Please try again.',
            dict_loading: 'Looking up…',
            dict_failed: 'No English definition found.',
            target_zh: 'Translate to Chinese',
            target_en: 'English definition'
        },
        zh: {
            home: '首页',
            home_title: '返回书库',
            offline: '离线书籍',
            offline_required: '此功能需要网络连接。仍可阅读下载的书籍并使用本地语音朗读。',
            signin_required: '请联网登录后保存书签和笔记。',
            save: '保存',
            save_title: '书签和笔记',
            about: '关于',
            about_title: '关于 MicsBook',
            help: '?',
            help_title: '使用帮助',
            menu_title: 'MicsBook 菜单',
            move_hint: '长按并拖动以移动',
            language: '语言',
            language_title: '切换语言',
            lang_button: 'EN',
            bookmarks_title: '书签和笔记',
            bookmark_here: '在此添加书签',
            write_note: '写笔记',
            save_note: '保存笔记',
            field_title: '标题',
            field_note: '笔记',
            field_tags: '标签',
            go: '前往',
            edit_save: '保存',
            edit_delete: '删除',
            saved: '已保存。',
            deleted: '已删除。',
            nothing_yet: '本书还没有保存任何内容。',
            saved_in_book: '本书已保存的内容',
            manage: '管理全部书签与笔记',
            bookmark_saved: '书签已保存。',
            note_saved: '笔记已保存。',
            kind_bookmark: '书签',
            kind_note: '笔记',
            delete_prompt: '确定删除此%s吗？',
            about_heading: '关于 MicsBook',
            about_description: '随时随地阅读、收听并整理您的电子书库。',
            build_number: '构建号',
            build_id: '构建 ID',
            built: '构建时间',
            loading: '加载中',
            help_heading: '使用说明',
            help_loading: '正在加载使用说明…',
            help_error: '无法加载使用说明，请检查网络连接后重试。',
            close: '关闭',
            cancel: '取消',
            copyright_notice: '本站收集的图书均来源于互联网上可公开下载的资源。如果您认为任何图书侵犯了版权，请提交报告并填写详细信息。管理员收到后会进行审核，并在理由成立时将相关图书下架。',
            copyright_report: '报告版权问题',
            copyright_title: '版权申诉',
            copyright_intro: '请提供足够的信息，以便管理员确认相关作品、了解您的权利并审核下架请求。',
            copyright_name: '您的姓名或机构名称',
            copyright_email: '联系邮箱',
            copyright_book: '书名或相关信息',
            copyright_book_placeholder: '书名、作者、ISBN 或链接',
            copyright_details: '申诉详情',
            copyright_details_placeholder: '请说明受版权保护的作品、您与该作品的关系，以及申请下架的原因。',
            copyright_authorized: '我确认以上信息准确，并且我有权提交此申诉。',
            copyright_submit: '发送申诉',
            copyright_submitting: '正在发送…',
            copyright_submitted: '申诉已收到，管理员将进行审核。',
            copyright_failed: '申诉发送失败，请检查所填信息后重试。',
            sel_translate: '翻译',
            sel_define: '词典',
            translate: '翻译所选内容',
            define: '英语词典释义',
            translating: '正在翻译…',
            translate_failed: '翻译失败，请重试。',
            dict_loading: '正在查询…',
            dict_failed: '未找到英语释义。',
            target_zh: '翻译为中文',
            target_en: '英语释义'
        }
    };

    var state = { context: null, items: [], selectedLocator: '', selectedText: '', lang: 'en', panelKind: '' };
    var selTarget = 'zh';

    function t(key) {
        var table = STRINGS[state.lang] || STRINGS.en;
        return table[key] != null ? table[key] : (STRINGS.en[key] != null ? STRINGS.en[key] : key);
    }

    function detectLanguage() {
        var stored = '';
        try { stored = window.localStorage.getItem('micsReader:lang') || ''; } catch (_) {}
        if (stored === 'en' || stored === 'zh') return stored;
        var nav = (window.navigator.language || (window.navigator.languages && window.navigator.languages[0]) || '').toLowerCase();
        return nav.indexOf('zh') === 0 ? 'zh' : 'en';
    }
    state.lang = detectLanguage();

    function epub() { return !!(window.ePubViewer && window.ePubViewer.state && window.ePubViewer.state.rendition); }
    function locator() {
        if (epub()) {
            var location = window.ePubViewer.state.rendition.currentLocation();
            return location && location.start ? location.start.cfi : '';
        }
        var app = window.PDFViewerApplication;
        return app && app.page ? 'page:' + app.page : 'page:1';
    }
    function locatorLabel() {
        if (epub()) {
            var node = document.querySelector('.bar .loc');
            return node && node.textContent.trim() ? node.textContent.trim() : 'EPUB location';
        }
        var app = window.PDFViewerApplication;
        return 'Page ' + (app && app.page ? app.page : 1);
    }
    function selection() {
        if (epub()) {
            try {
                var contents = window.ePubViewer.state.rendition.getContents()[0];
                var selected = contents.window.getSelection();
                var text = selected.toString().trim();
                if (text && selected.rangeCount) {
                    state.selectedLocator = contents.cfiFromRange(selected.getRangeAt(0));
                    state.selectedText = text.slice(0, 4000);
                }
            } catch (_) {}
        } else {
            var text = window.getSelection ? window.getSelection().toString().trim() : '';
            if (text) state.selectedText = text.slice(0, 4000);
        }
        var preview = document.querySelector('.mics-reader-selection');
        if (preview) {
            preview.hidden = !state.selectedText;
            preview.textContent = state.selectedText ? '"' + state.selectedText + '"' : '';
        }
        updateSelectionBar();
        return state.selectedText;
    }
    function go(value) {
        if (epub()) window.ePubViewer.state.rendition.display(value);
        else if (value.indexOf('page:') === 0 && window.PDFViewerApplication) window.PDFViewerApplication.page = parseInt(value.slice(5), 10) || 1;
    }
    document.addEventListener('selectionchange', selection);
    // EPUB text lives inside epub.js's own iframe, so selection changes there
    // fire on the content document, not on this page. Capture them directly so
    // a selection survives until the panel button is clicked (that click clears
    // the iframe selection, so reading it lazily loses the text).
    function hookEpubSelection() {
        if (!epub()) return;
        try {
            var rendition = window.ePubViewer.state.rendition;
            if (!rendition._micsRelocationHook) {
                rendition._micsRelocationHook = true;
                rendition.on('relocated', function () {
                    state.selectedLocator = '';
                    state.selectedText = '';
                });
            }
            var contents = rendition.getContents();
            for (var i = 0; i < contents.length; i++) {
                var doc = contents[i].document;
                if (doc && !doc._micsSelectionHook) {
                    doc._micsSelectionHook = true;
                    doc.addEventListener('selectionchange', selection);
                    blockContextMenu(doc);
                }
            }
        } catch (_) {}
    }
    setInterval(hookEpubSelection, 800);

    // When text is highlighted, suppress the browser's default context menu
    // (bookmark / note-taking / translate) and offer our own free translation
    // and dictionary instead.
    function hasSelectionIn(doc) {
        try {
            var sel = doc.getSelection ? doc.getSelection() : null;
            return !!(sel && !sel.isCollapsed && sel.toString().trim().length > 0);
        } catch (_) { return false; }
    }
    function blockContextMenu(doc) {
        if (!doc || doc._micsMenuBlocked) return;
        doc._micsMenuBlocked = true;
        doc.addEventListener('contextmenu', function (event) {
            if (hasSelectionIn(doc)) {
                event.preventDefault();
                event.stopPropagation();
                updateSelectionBar(true);
            }
        }, true);
    }
    blockContextMenu(document);

    var selectionBar = null;
    var selectionPop = null;
    var selBarTimer = null;

    function selectionRect() {
        if (epub()) {
            try {
                var contents = window.ePubViewer.state.rendition.getContents();
                for (var i = 0; i < contents.length; i++) {
                    var win = contents[i].window;
                    var sel = win.getSelection();
                    if (sel && !sel.isCollapsed && sel.rangeCount > 0) {
                        var rect = sel.getRangeAt(0).getBoundingClientRect();
                        if (rect && rect.width > 0) {
                            var frame = contents[i].element.getBoundingClientRect();
                            return { left: frame.left + rect.left, top: frame.top + rect.top, width: rect.width, height: rect.height };
                        }
                    }
                }
            } catch (_) {}
            return null;
        }
        var sel = window.getSelection();
        if (sel && !sel.isCollapsed && sel.rangeCount > 0) {
            var rect = sel.getRangeAt(0).getBoundingClientRect();
            if (rect && rect.width > 0) return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
        }
        return null;
    }

    function buildSelectionUI() {
        selectionBar = document.createElement('div');
        selectionBar.className = 'mics-selbar';
        selectionBar.hidden = true;
        selectionBar.innerHTML = '<button data-mics-translate></button><button data-mics-define></button>';
        document.body.appendChild(selectionBar);
        selectionPop = document.createElement('div');
        selectionPop.className = 'mics-selpop';
        selectionPop.hidden = true;
        selectionPop.innerHTML = '<header><span class="mics-selpop-source"></span><div class="mics-selpop-target"><button type="button" data-mics-target="zh"></button><button type="button" data-mics-target="en"></button></div><button class="mics-reader-close" aria-label="' + t('close') + '">×</button></header><div class="mics-selpop-content"></div>';
        selectionPop.querySelector('.mics-reader-close').onclick = function () {
            selectionPop.hidden = true;
            selectionBar.hidden = true;
        };
        document.body.appendChild(selectionPop);
        selectionBar.querySelector('[data-mics-translate]').onclick = doTranslate;
        selectionBar.querySelector('[data-mics-define]').onclick = doDefine;
        selectionPop.querySelectorAll('[data-mics-target]').forEach(function (btn) {
            btn.onclick = function () {
                selTarget = btn.getAttribute('data-mics-target');
                doTranslate();
            };
        });
        document.addEventListener('click', function (event) {
            if (selectionBar.contains(event.target) || selectionPop.contains(event.target)) return;
            selectionPop.hidden = true;
        });
        applySelectionLang();
    }
    function applySelectionLang() {
        if (!selectionBar) return;
        selectionBar.querySelector('[data-mics-translate]').textContent = t('sel_translate');
        selectionBar.querySelector('[data-mics-translate]').title = t('translate');
        selectionBar.querySelector('[data-mics-define]').textContent = t('sel_define');
        selectionBar.querySelector('[data-mics-define]').title = t('define');
        showTargetLabels();
    }

    function updateSelectionBar(immediate) {
        if (!selectionBar) return;
        if (selBarTimer) { window.clearTimeout(selBarTimer); selBarTimer = null; }
        selBarTimer = window.setTimeout(function () {
            var rect = selectionRect();
            if (!state.selectedText || !rect) {
                selectionBar.hidden = true;
                return;
            }
            var barW = selectionBar.offsetWidth || 140;
            var left = rect.left + rect.width / 2 - barW / 2;
            left = Math.max(8, Math.min(left, window.innerWidth - barW - 8));
            var top = rect.top - selectionBar.offsetHeight - 8;
            if (top < 8) top = rect.bottom + 8;
            selectionBar.style.left = left + 'px';
            selectionBar.style.top = top + 'px';
            selectionBar.hidden = false;
        }, immediate ? 0 : 200);
    }

    function placeSelectionPop() {
        var popW = selectionPop.offsetWidth || 320;
        var left = selectionBar.offsetLeft + selectionBar.offsetWidth / 2 - popW / 2;
        left = Math.max(8, Math.min(left, window.innerWidth - popW - 8));
        selectionPop.style.left = left + 'px';
        selectionPop.style.top = (selectionBar.offsetTop + selectionBar.offsetHeight + 8) + 'px';
        selectionPop.hidden = false;
    }

    function translateFree(text, target) {
        return fetch('https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=' + encodeURIComponent(target) + '&dt=t&q=' + encodeURIComponent(text)).then(function (response) {
            if (!response.ok) throw new Error('translate failed');
            return response.json();
        }).then(function (data) {
            var out = '';
            var lines = data && data[0];
            if (lines) for (var i = 0; i < lines.length; i++) out += (lines[i][0] || '');
            return out.trim();
        });
    }
    function defineFree(word) {
        return fetch('https://api.dictionaryapi.dev/api/v2/entries/en/' + encodeURIComponent(word)).then(function (response) {
            if (!response.ok) throw new Error('not in dictionary');
            return response.json();
        });
    }
    function showTargetLabels() {
        if (!selectionPop) return;
        selectionPop.querySelectorAll('[data-mics-target]').forEach(function (btn) {
            var tgt = btn.getAttribute('data-mics-target');
            btn.textContent = tgt === 'zh' ? t('target_zh') : t('target_en');
            btn.classList.toggle('active', tgt === selTarget);
        });
    }
    function doTranslate() {
        var text = state.selectedText;
        if (!text || !selectionPop) return;
        selectionPop.querySelector('.mics-selpop-source').textContent = text.slice(0, 200);
        var content = selectionPop.querySelector('.mics-selpop-content');
        showTargetLabels();
        placeSelectionPop();
        if (selTarget === 'en') {
            doDefine();
            return;
        }
        content.innerHTML = '<p class="mics-selpop-status">' + t('translating') + '</p>';
        translateFree(text, 'zh-CN').then(function (result) {
            if (result) {
                content.innerHTML = '';
                var line = document.createElement('p');
                line.textContent = result;
                content.appendChild(line);
            } else {
                content.innerHTML = '<p class="mics-selpop-status error">' + t('translate_failed') + '</p>';
            }
        }).catch(function () {
            content.innerHTML = '<p class="mics-selpop-status error">' + t(navigator.onLine === false ? 'offline_required' : 'translate_failed') + '</p>';
        });
    }
    function doDefine() {
        var text = state.selectedText;
        if (!text || !selectionPop) return;
        selTarget = 'en';
        selectionPop.querySelector('.mics-selpop-source').textContent = text.slice(0, 200);
        var content = selectionPop.querySelector('.mics-selpop-content');
        showTargetLabels();
        var word = (text.trim().split(/\s+/, 1)[0] || '').replace(/[^A-Za-z'-]/g, '');
        if (!word) {
            content.innerHTML = '<p class="mics-selpop-status error">' + t('dict_failed') + '</p>';
            return;
        }
        content.innerHTML = '<p class="mics-selpop-status">' + t('dict_loading') + '</p>';
        placeSelectionPop();
        defineFree(word).then(function (entries) {
            content.innerHTML = '';
            (entries || []).slice(0, 2).forEach(function (entry) {
                (entry.meanings || []).forEach(function (meaning) {
                    var pos = document.createElement('h4');
                    pos.textContent = meaning.partOfSpeech || '';
                    content.appendChild(pos);
                    (meaning.definitions || []).slice(0, 3).forEach(function (defn) {
                        var p = document.createElement('p');
                        p.textContent = defn.definition || '';
                        content.appendChild(p);
                    });
                });
            });
            if (!content.childNodes.length) content.innerHTML = '<p class="mics-selpop-status error">' + t('dict_failed') + '</p>';
        }).catch(function () {
            content.innerHTML = '<p class="mics-selpop-status error">' + t(navigator.onLine === false ? 'offline_required' : 'dict_failed') + '</p>';
        });
    }

    var tools = document.createElement('div');
    tools.className = 'mics-reader-tools';
    tools.innerHTML = '<button type="button" class="mics-reader-menu-trigger" data-mics-menu aria-haspopup="menu" aria-controls="mics-reader-menu" aria-expanded="false"><svg class="mics-reader-brand-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H11a3 3 0 0 1 3 3v13a3 3 0 0 0-3-3H4V5.5Zm16 0A2.5 2.5 0 0 0 17.5 3H14v16a3 3 0 0 1 3-3h3V5.5Z"/></svg><span class="mics-reader-brand-label">MicsBook</span><span class="mics-reader-menu-chevron" aria-hidden="true"></span></button><div class="mics-reader-menu" id="mics-reader-menu" role="menu" hidden><button type="button" role="menuitem" data-mics-home></button><button type="button" role="menuitem" data-mics-reading hidden></button><div class="mics-reader-menu-divider" role="separator"></div><button type="button" role="menuitem" data-mics-lang></button><button type="button" role="menuitem" data-mics-help></button><button type="button" role="menuitem" data-mics-about></button></div>';
    var epubHeader = document.querySelector('.app > .bar:first-of-type');
    var headerLeft = epubHeader && epubHeader.querySelector('.left');
    var pdfHeaderLeft = document.querySelector('#toolbarViewerLeft');
    if (headerLeft) {
        epubHeader.classList.add('mics-reader-header');
        tools.classList.add('mics-reader-tools--header');
        headerLeft.appendChild(tools);
    } else if (pdfHeaderLeft) {
        tools.classList.add('mics-reader-tools--pdf-header');
        pdfHeaderLeft.insertBefore(tools, pdfHeaderLeft.firstChild);
    } else {
        document.body.appendChild(tools);
    }
    var menuButton = tools.querySelector('[data-mics-menu]');
    var toolsMenu = tools.querySelector('.mics-reader-menu');
    var homeButton = tools.querySelector('[data-mics-home]');
    var readingButton = tools.querySelector('[data-mics-reading]');
    var offlineButton = document.createElement('button');
    offlineButton.type = 'button'; offlineButton.setAttribute('role', 'menuitem');
    offlineButton.setAttribute('data-mics-offline', '');
    toolsMenu.insertBefore(offlineButton, readingButton);
    offlineButton.onclick = function () { window.location.href = '/offline'; };
    var aboutButton = tools.querySelector('[data-mics-about]');
    var langButton = tools.querySelector('[data-mics-lang]');
    var helpButton = tools.querySelector('[data-mics-help]');
    var suppressMenuClick = false;

    function placeToolsMenu() {
        toolsMenu.style.left = '0';
        toolsMenu.style.right = 'auto';
        toolsMenu.style.top = 'calc(100% + 5px)';
        toolsMenu.style.bottom = 'auto';
        var rect = toolsMenu.getBoundingClientRect();
        if (rect.right > window.innerWidth - 8) {
            toolsMenu.style.left = 'auto';
            toolsMenu.style.right = '0';
        }
        rect = toolsMenu.getBoundingClientRect();
        if (rect.bottom > window.innerHeight - 8) {
            toolsMenu.style.top = 'auto';
            toolsMenu.style.bottom = 'calc(100% + 5px)';
        }
    }

    function setupToolsDrag() {
        var savedKey = 'micsReader:tools-position';
        var holdDelay = 320;
        var drag = null;
        var holdTimer = null;

        function applyPosition(left, top) {
            var maxLeft = Math.max(0, window.innerWidth - tools.offsetWidth);
            var maxTop = Math.max(0, window.innerHeight - tools.offsetHeight);
            tools.style.left = Math.max(0, Math.min(left, maxLeft)) + 'px';
            tools.style.top = Math.max(0, Math.min(top, maxTop)) + 'px';
        }
        function makeFloating(left, top) {
            tools.classList.add('mics-reader-tools--floating');
            applyPosition(left, top);
        }
        function savePosition() {
            try {
                var rect = tools.getBoundingClientRect();
                window.localStorage.setItem(savedKey, JSON.stringify({ left: rect.left, top: rect.top }));
            } catch (_) {}
        }
        try {
            var saved = JSON.parse(window.localStorage.getItem(savedKey) || 'null');
            if (saved && typeof saved.left === 'number' && typeof saved.top === 'number') {
                makeFloating(saved.left, saved.top);
            }
        } catch (_) {}

        menuButton.addEventListener('pointerdown', function (event) {
            if (event.pointerType === 'mouse' && event.button !== 0) return;
            var rect = tools.getBoundingClientRect();
            drag = {
                pointerId: event.pointerId,
                startX: event.clientX,
                startY: event.clientY,
                startLeft: rect.left,
                startTop: rect.top,
                active: false
            };
            try { menuButton.setPointerCapture(event.pointerId); } catch (_) {}
            holdTimer = window.setTimeout(function () {
                if (!drag) return;
                drag.active = true;
                closeToolsMenu(false);
                makeFloating(drag.startLeft, drag.startTop);
                tools.classList.add('dragging');
            }, holdDelay);
        });
        menuButton.addEventListener('pointermove', function (event) {
            if (!drag || event.pointerId !== drag.pointerId) return;
            var dx = event.clientX - drag.startX;
            var dy = event.clientY - drag.startY;
            if (!drag.active) {
                if (Math.abs(dx) + Math.abs(dy) > 9) {
                    window.clearTimeout(holdTimer);
                    suppressMenuClick = true;
                    window.setTimeout(function () { suppressMenuClick = false; }, 400);
                    try { menuButton.releasePointerCapture(event.pointerId); } catch (_) {}
                    drag = null;
                }
                return;
            }
            event.preventDefault();
            applyPosition(drag.startLeft + dx, drag.startTop + dy);
        });
        function finishDrag(event) {
            if (!drag || event.pointerId !== drag.pointerId) return;
            window.clearTimeout(holdTimer);
            if (drag.active) {
                suppressMenuClick = true;
                tools.classList.remove('dragging');
                savePosition();
            }
            try { menuButton.releasePointerCapture(event.pointerId); } catch (_) {}
            drag = null;
        }
        menuButton.addEventListener('pointerup', finishDrag);
        menuButton.addEventListener('pointercancel', finishDrag);
        window.addEventListener('resize', function () {
            if (!tools.classList.contains('mics-reader-tools--floating')) return;
            var rect = tools.getBoundingClientRect();
            applyPosition(rect.left, rect.top);
            savePosition();
        });
    }

    function closeToolsMenu(restoreFocus) {
        if (toolsMenu.hidden) return;
        toolsMenu.hidden = true;
        menuButton.setAttribute('aria-expanded', 'false');
        tools.classList.remove('open');
        if (restoreFocus) menuButton.focus();
    }
    function openToolsMenu() {
        toolsMenu.hidden = false;
        menuButton.setAttribute('aria-expanded', 'true');
        tools.classList.add('open');
        placeToolsMenu();
        var first = toolsMenu.querySelector('[role="menuitem"]:not([hidden])');
        if (first) first.focus();
    }
    menuButton.onclick = function () {
        if (suppressMenuClick) {
            suppressMenuClick = false;
            return;
        }
        if (toolsMenu.hidden) openToolsMenu();
        else closeToolsMenu(false);
    };
    setupToolsDrag();
    document.addEventListener('click', function (event) {
        if (!toolsMenu.hidden && !tools.contains(event.target)) closeToolsMenu(false);
    });
    tools.addEventListener('keydown', function (event) {
        if (event.key === 'Escape' && !toolsMenu.hidden) {
            event.preventDefault();
            closeToolsMenu(true);
            return;
        }
        if (toolsMenu.hidden || event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
        event.preventDefault();
        var items = Array.prototype.slice.call(toolsMenu.querySelectorAll('[role="menuitem"]:not([hidden])'));
        var current = items.indexOf(document.activeElement);
        var next = event.key === 'ArrowDown' ? current + 1 : current - 1;
        if (next < 0) next = items.length - 1;
        if (next >= items.length) next = 0;
        if (items[next]) items[next].focus();
    });

    function applyLang() {
        menuButton.title = t('menu_title') + ' — ' + t('move_hint');
        menuButton.setAttribute('aria-label', t('menu_title'));
        toolsMenu.setAttribute('aria-label', t('menu_title'));
        homeButton.textContent = t('home');
        homeButton.title = t('home_title');
        offlineButton.textContent = t('offline');
        readingButton.textContent = t('save');
        readingButton.title = t('save_title');
        aboutButton.textContent = t('about');
        aboutButton.title = t('about_title');
        helpButton.textContent = t('help');
        helpButton.title = t('help_title');
        langButton.textContent = t('language') + ' · ' + t('lang_button');
        langButton.title = t('language_title');
        applyCopyrightDialogLang();
        renderSettingsAbout();
        applySelectionLang();
    }
    applyLang();
    buildSelectionUI();

    function setLanguage(lang) {
        if (lang !== 'en' && lang !== 'zh' || lang === state.lang) return;
        state.lang = lang;
        try { window.localStorage.setItem('micsReader:lang', lang); } catch (_) {}
        if (state.context && state.context.authenticated && state.context.csrf_token) {
            request('/api/reader/language', { language: lang }).then(function () {
                if (state.context) state.context.language = lang;
            }).catch(function () {});
        }
        applyLang();
        try { window.dispatchEvent(new CustomEvent('micslangchange', { detail: { lang: lang } })); } catch (_) {}
        if (state.panelKind) openPanel(state.panelKind);
    }
    langButton.onclick = function () { closeToolsMenu(false); setLanguage(state.lang === 'zh' ? 'en' : 'zh'); };

    function panel(title) {
        var node = document.createElement('section');
        node.className = 'mics-reader-panel';
        node.innerHTML = '<header><h2></h2><button class="mics-reader-close" aria-label="' + t('close') + '">×</button></header><div class="mics-reader-content"></div>';
        node.querySelector('h2').textContent = title;
        node.querySelector('.mics-reader-close').onclick = function () { node.remove(); state.panelKind = ''; };
        document.body.appendChild(node);
        return node.querySelector('.mics-reader-content');
    }
    function closePanels() { document.querySelectorAll('.mics-reader-panel').forEach(function (node) { node.remove(); }); }

    function openPanel(kind) {
        closePanels();
        state.panelKind = kind;
        var about = state.context && state.context.about ? state.context.about : {};
        var aboutTitle = state.lang === 'zh' ? '关于 ' + (about.name || 'MicsBook') : 'About ' + (about.name || 'MicsBook');
        var titles = { reading: t('bookmarks_title'), about: aboutTitle, help: t('help_heading') };
        var content = panel(titles[kind]);
        if (kind === 'about') buildAboutPanel(content, false);
        else if (kind === 'help') buildHelpPanel(content);
        else buildReadingPanel(content);
    }

    aboutButton.onclick = function () { closeToolsMenu(false); openPanel('about'); };
    helpButton.onclick = function () { closeToolsMenu(false); openPanel('help'); };
    readingButton.onclick = function () { closeToolsMenu(false); openPanel('reading'); };
    homeButton.onclick = function () { window.location.href = '/'; };

    var copyrightDialog = null;
    var copyrightContext = {};

    function showCopyrightToast(message) {
        var toast = document.querySelector('.mics-copyright-toast');
        if (!toast) {
            toast = document.createElement('div');
            toast.className = 'mics-copyright-toast';
            toast.setAttribute('role', 'status');
            toast.setAttribute('aria-live', 'polite');
            document.body.appendChild(toast);
        }
        toast.textContent = message;
        toast.classList.add('visible');
        window.clearTimeout(toast._hideTimer);
        toast._hideTimer = window.setTimeout(function () { toast.classList.remove('visible'); }, 5000);
    }

    function applyCopyrightDialogLang() {
        if (!copyrightDialog) return;
        copyrightDialog.querySelector('[data-copy-title]').textContent = t('copyright_title');
        copyrightDialog.querySelector('[data-copy-intro]').textContent = t('copyright_intro');
        copyrightDialog.querySelector('[data-copy-name-label]').textContent = t('copyright_name');
        copyrightDialog.querySelector('[data-copy-email-label]').textContent = t('copyright_email');
        copyrightDialog.querySelector('[data-copy-book-label]').textContent = t('copyright_book');
        copyrightDialog.querySelector('[name="book_title"]').placeholder = t('copyright_book_placeholder');
        copyrightDialog.querySelector('[data-copy-details-label]').textContent = t('copyright_details');
        copyrightDialog.querySelector('[name="details"]').placeholder = t('copyright_details_placeholder');
        copyrightDialog.querySelector('[data-copy-authorized]').textContent = t('copyright_authorized');
        copyrightDialog.querySelector('[data-copy-submit]').textContent = t('copyright_submit');
        copyrightDialog.querySelector('[data-copy-cancel]').textContent = t('cancel');
        copyrightDialog.querySelector('[data-copy-close]').setAttribute('aria-label', t('close'));
    }

    function currentBookTitle() {
        var node = document.querySelector('.book-title');
        var title = node && node.textContent ? node.textContent.trim() : '';
        if (!title && document.title && document.title !== 'Ebook Reader') title = document.title.trim();
        return title || id;
    }

    function prepareCopyrightForm(resetBook) {
        if (!copyrightDialog) return;
        var form = copyrightDialog.querySelector('form');
        form.elements.book_id.value = id;
        form.elements.source_url.value = window.location.href;
        if (copyrightContext.csrf_token) form.elements.csrf_token.value = copyrightContext.csrf_token;
        if (resetBook || !form.elements.book_title.value.trim()) form.elements.book_title.value = currentBookTitle();
        if (!form.elements.claimant_name.value && copyrightContext.name) form.elements.claimant_name.value = copyrightContext.name;
        if (!form.elements.claimant_email.value && copyrightContext.email) form.elements.claimant_email.value = copyrightContext.email;
    }

    function ensureCopyrightDialog() {
        if (copyrightDialog) return copyrightDialog;
        copyrightDialog = document.createElement('dialog');
        copyrightDialog.className = 'mics-copyright-dialog';
        copyrightDialog.innerHTML = '<button type="button" class="mics-reader-close" data-copy-close>×</button><form autocomplete="off"><h2 data-copy-title></h2><p class="mics-copyright-intro" data-copy-intro></p><input type="hidden" name="csrf_token"><input type="hidden" name="book_id"><input type="hidden" name="source_url"><label><span data-copy-name-label></span><input type="text" name="claimant_name" maxlength="200" required></label><label><span data-copy-email-label></span><input type="email" name="claimant_email" maxlength="254" required></label><label><span data-copy-book-label></span><input type="text" name="book_title" maxlength="500" required></label><label><span data-copy-details-label></span><textarea name="details" rows="6" minlength="20" maxlength="5000" required></textarea></label><label class="mics-copyright-confirm"><input type="checkbox" name="authorized" required><span data-copy-authorized></span></label><label class="mics-copyright-honeypot" aria-hidden="true">Website<input type="text" name="company_website" tabindex="-1" autocomplete="off"></label><div class="mics-copyright-status" role="status" aria-live="polite"></div><div class="mics-copyright-actions"><button type="submit" class="mics-reader-action danger" data-copy-submit></button><button type="button" class="mics-reader-action secondary" data-copy-cancel></button></div></form>';
        document.body.appendChild(copyrightDialog);
        applyCopyrightDialogLang();
        var form = copyrightDialog.querySelector('form');
        var status = copyrightDialog.querySelector('.mics-copyright-status');
        var submit = copyrightDialog.querySelector('[data-copy-submit]');
        function closeCopyright() {
            if (copyrightDialog.close) copyrightDialog.close();
            else copyrightDialog.removeAttribute('open');
        }
        copyrightDialog.querySelector('[data-copy-close]').onclick = closeCopyright;
        copyrightDialog.querySelector('[data-copy-cancel]').onclick = closeCopyright;
        copyrightDialog.addEventListener('click', function (event) { if (event.target === copyrightDialog) closeCopyright(); });
        form.addEventListener('submit', function (event) {
            event.preventDefault();
            submit.disabled = true;
            status.className = 'mics-copyright-status';
            status.textContent = t('copyright_submitting');
            fetch('/api/copyright-claims', {
                method: 'POST', credentials: 'same-origin',
                headers: { 'X-Requested-With': 'fetch', 'Accept': 'application/json' },
                body: new FormData(form)
            }).then(function (response) {
                return response.json().then(function (data) { return { ok: response.ok, data: data }; });
            }).then(function (result) {
                submit.disabled = false;
                if (!result.ok) {
                    status.className = 'mics-copyright-status error';
                    status.textContent = t('copyright_failed');
                    return;
                }
                form.reset();
                prepareCopyrightForm(true);
                if (copyrightDialog.close) copyrightDialog.close();
                else copyrightDialog.removeAttribute('open');
                showCopyrightToast(t('copyright_submitted'));
            }).catch(function () {
                submit.disabled = false;
                status.className = 'mics-copyright-status error';
                status.textContent = t('copyright_failed');
            });
        });
        return copyrightDialog;
    }

    function openCopyrightDialog() {
        var dialog = ensureCopyrightDialog();
        var status = dialog.querySelector('.mics-copyright-status');
        status.textContent = '';
        status.className = 'mics-copyright-status';
        prepareCopyrightForm(false);
        var submit = dialog.querySelector('[data-copy-submit]');
        submit.disabled = !dialog.querySelector('[name="csrf_token"]').value;
        if (dialog.showModal) dialog.showModal();
        else dialog.setAttribute('open', '');
        fetch('/api/copyright/context', { credentials: 'same-origin', cache: 'no-store' }).then(function (response) {
            return response.json();
        }).then(function (context) {
            copyrightContext = context || {};
            dialog.querySelector('[name="csrf_token"]').value = copyrightContext.csrf_token || '';
            prepareCopyrightForm(false);
            submit.disabled = !copyrightContext.csrf_token;
        }).catch(function () {
            submit.disabled = true;
            status.className = 'mics-copyright-status error';
            status.textContent = t('copyright_failed');
        });
    }

    function buildAboutPanel(content, showBrand) {
        var about = state.context && state.context.about ? state.context.about : {};
        content.innerHTML = (showBrand ? '<div class="mics-about-brand"><span aria-hidden="true">M</span><strong></strong></div>' : '') + '<p class="mics-about-description"></p><div class="mics-build-list"><div><strong>' + t('build_number') + '</strong><code></code></div><div><strong>' + t('build_id') + '</strong><code></code></div><div><strong>' + t('built') + '</strong><code></code></div></div>';
        if (showBrand) {
            content.querySelector('.mics-about-brand strong').textContent = about.name || 'MicsBook';
        }
        content.querySelector('.mics-about-description').textContent = t('about_description');
        var codes = content.querySelectorAll('code');
        codes[0].textContent = about.build_number || t('loading');
        codes[1].textContent = about.build_id || t('loading');
        codes[2].textContent = about.build_time || t('loading');
        var notice = document.createElement('div');
        notice.className = 'mics-copyright-notice';
        var noticeText = document.createElement('p');
        noticeText.textContent = t('copyright_notice');
        var report = document.createElement('button');
        report.type = 'button';
        report.className = 'mics-reader-action danger';
        report.textContent = t('copyright_report');
        report.onclick = openCopyrightDialog;
        notice.appendChild(noticeText);
        notice.appendChild(report);
        content.appendChild(notice);
    }

    function renderSettingsAbout() {
        document.querySelectorAll('[data-mics-reader-about]').forEach(function (content) {
            buildAboutPanel(content, true);
        });
    }

    function buildHelpPanel(content) {
        content.innerHTML = '<p class="mics-help-loading">' + t('help_loading') + '</p>';
        fetch('/api/help?lang=' + encodeURIComponent(state.lang), { cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (help) {
            if (help.document) content.innerHTML = help.document;
            else throw new Error('empty guide');
        }).catch(function () {
            content.innerHTML = '<p class="mics-reader-status">' + t('help_error') + '</p>';
        });
    }

    function params(values) { var body = new URLSearchParams(); Object.keys(values).forEach(function (key) { body.set(key, values[key] || ''); }); return body; }
    function request(url, values) {
        if (navigator.onLine === false) return Promise.reject(new Error(t('offline_required')));
        if (!state.context || !state.context.csrf_token) return Promise.reject(new Error(t('signin_required')));
        values.csrf_token = state.context.csrf_token;
        return fetch(url, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8' }, body: params(values) }).then(function (response) {
            return response.json().then(function (data) { if (!response.ok) throw new Error(data.error || 'Request failed'); return data; });
        }).catch(function (error) { if (error instanceof TypeError) throw new Error(t('offline_required')); throw error; });
    }
    function field(parent, name, label, value, textarea) {
        var wrapper = document.createElement('label'); wrapper.textContent = label;
        var input = document.createElement(textarea ? 'textarea' : 'input'); input.name = name; input.value = value || ''; if (textarea) input.rows = 3;
        wrapper.appendChild(input); parent.appendChild(wrapper); return input;
    }
    function status(content, message, error) { var node = content.querySelector('.mics-reader-status'); node.textContent = message; node.style.color = error ? '#a3333f' : ''; }
    function kindLabel(kind) { return kind === 'note' ? t('kind_note') : t('kind_bookmark'); }
    function renderItems(content) {
        var list = content.querySelector('.mics-reader-list'); list.textContent = '';
        if (!state.items.length) { var empty = document.createElement('p'); empty.textContent = t('nothing_yet'); empty.className = 'mics-reader-status'; list.appendChild(empty); return; }
        state.items.forEach(function (item) {
            var card = document.createElement('article'); card.className = 'mics-reader-item';
            var head = document.createElement('div'); head.className = 'mics-reader-item-head'; head.innerHTML = '<span class="mics-reader-item-kind"></span><small></small>';
            head.querySelector('span').textContent = kindLabel(item.kind); head.querySelector('span').classList.add(item.kind); head.querySelector('small').textContent = item.locator_label;
            card.appendChild(head);
            var title = field(card, 'title', t('field_title'), item.title, false);
            var body = item.kind === 'note' ? field(card, 'body', t('field_note'), item.body, true) : null;
            var tags = field(card, 'tags', t('field_tags'), (item.tags || []).join(', '), false);
            var buttons = document.createElement('div'); buttons.className = 'mics-reader-item-buttons';
            buttons.innerHTML = '<button class="mics-reader-mini">' + t('go') + '</button><button class="mics-reader-mini">' + t('edit_save') + '</button><button class="mics-reader-mini delete">' + t('edit_delete') + '</button>';
            buttons.children[0].onclick = function () { go(item.locator); closePanels(); state.panelKind = ''; };
            buttons.children[1].onclick = function () { request('/api/reader/items/' + encodeURIComponent(item.id), { title: title.value, body: body ? body.value : '', tags: tags.value }).then(function (updated) { Object.assign(item, updated); status(content, t('saved')); }).catch(function (err) { status(content, err.message, true); }); };
            buttons.children[2].onclick = function () {
                if (!confirm(t('delete_prompt').replace('%s', kindLabel(item.kind)))) return;
                request('/api/reader/items/' + encodeURIComponent(item.id) + '/delete', {}).then(function () { state.items = state.items.filter(function (other) { return other.id !== item.id; }); renderItems(content); status(content, t('deleted')); }).catch(function (err) { status(content, err.message, true); });
            };
            card.appendChild(buttons); list.appendChild(card);
        });
    }
    function create(content, kind, title, body, tags) {
        selection();
        return request('/api/reader/items', { book_id: id, kind: kind, locator: state.selectedLocator || locator(), locator_label: locatorLabel(), title: title, body: body, excerpt: selection(), tags: tags }).then(function (item) {
            state.items.unshift(item); state.selectedLocator = ''; state.selectedText = ''; renderItems(content); status(content, kind === 'note' ? t('note_saved') : t('bookmark_saved')); return item;
        }).catch(function (err) { status(content, err.message, true); return null; });
    }
    function buildReadingPanel(content) {
        content.innerHTML = '<div class="mics-reader-actions"><button class="mics-reader-action">' + t('bookmark_here') + '</button><button class="mics-reader-action secondary">' + t('write_note') + '</button></div><div class="mics-note-editor" hidden></div><div class="mics-reader-selection" hidden></div><p class="mics-reader-status"></p><h3>' + t('saved_in_book') + '</h3><div class="mics-reader-list"></div><a class="mics-reader-manage" href="/my-library/reading">' + t('manage') + '</a>';
        var editor = content.querySelector('.mics-note-editor');
        content.querySelectorAll('.mics-reader-action')[0].onclick = function () { create(content, 'bookmark', '', '', ''); };
        content.querySelectorAll('.mics-reader-action')[1].onclick = function () {
            editor.hidden = !editor.hidden;
            if (editor.childNodes.length) return;
            var title = field(editor, 'title', t('field_title'), '', false); var body = field(editor, 'body', t('field_note'), '', true); var tags = field(editor, 'tags', t('field_tags'), '', false);
            var save = document.createElement('button'); save.className = 'mics-reader-action'; save.textContent = t('save_note'); save.style.marginTop = '9px'; save.onclick = function () { create(content, 'note', title.value, body.value, tags.value).then(function (item) { if (!item) return; title.value = ''; body.value = ''; tags.value = ''; editor.hidden = true; }); }; editor.appendChild(save);
        };
        selection();
        renderItems(content);
    }

    fetch('/api/reader/context?book_id=' + encodeURIComponent(id), { credentials: 'same-origin', cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (context) {
        state.context = context; state.items = context.items || [];
        if (context.authenticated) {
            readingButton.hidden = false;
            if (context.language === 'en' || context.language === 'zh') {
                state.lang = context.language;
                try { window.localStorage.setItem('micsReader:lang', context.language); } catch (_) {}
                try { window.dispatchEvent(new CustomEvent('micslangchange', { detail: { lang: context.language } })); } catch (_) {}
            }
        }
        applyLang();
        if (state.panelKind) openPanel(state.panelKind);
    }).catch(function () {
        return fetch('/api/about', { cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (about) { state.context = { about: about }; applyLang(); }).catch(function () {
            state.context = {};
            applyLang();
        });
    });
}());
