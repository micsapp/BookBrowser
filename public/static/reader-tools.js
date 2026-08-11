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
            save: 'Save',
            save_title: 'Bookmarks and notes',
            about: 'About',
            about_title: 'About MicsBook',
            help: '?',
            help_title: 'User guide',
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
            about_description: 'Read, listen to, and organize your ebook library.',
            build_number: 'Build number',
            build_id: 'Build ID',
            built: 'Built',
            loading: 'loading',
            help_heading: 'How to use this app',
            help_loading: 'Loading the user guide…',
            help_error: 'The user guide could not be loaded. Check your connection and try again.',
            close: 'Close'
        },
        zh: {
            save: '保存',
            save_title: '书签和笔记',
            about: '关于',
            about_title: '关于 MicsBook',
            help: '?',
            help_title: '使用帮助',
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
            about_description: '阅读、收听并整理您的电子书库。',
            build_number: '构建号',
            build_id: '构建 ID',
            built: '构建时间',
            loading: '加载中',
            help_heading: '使用说明',
            help_loading: '正在加载使用说明…',
            help_error: '无法加载使用说明，请检查网络连接后重试。',
            close: '关闭'
        }
    };

    var state = { context: null, items: [], selectedLocator: '', selectedText: '', lang: 'en', panelKind: '' };

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
                }
            }
        } catch (_) {}
    }
    setInterval(hookEpubSelection, 800);

    var tools = document.createElement('div');
    tools.className = 'mics-reader-tools';
    tools.innerHTML = '<button class="mics-reader-button" data-mics-reading hidden></button><button class="mics-reader-button" data-mics-about></button><button class="mics-reader-button" data-mics-lang></button><button class="mics-reader-button" data-mics-help></button>';
    document.body.appendChild(tools);
    var readingButton = tools.querySelector('[data-mics-reading]');
    var aboutButton = tools.querySelector('[data-mics-about]');
    var langButton = tools.querySelector('[data-mics-lang]');
    var helpButton = tools.querySelector('[data-mics-help]');

    function applyLang() {
        readingButton.textContent = t('save');
        readingButton.title = t('save_title');
        aboutButton.textContent = t('about');
        aboutButton.title = t('about_title');
        helpButton.textContent = t('help');
        helpButton.title = t('help_title');
        langButton.textContent = t('lang_button');
        langButton.title = t('language_title');
    }
    applyLang();

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
        if (state.panelKind) openPanel(state.panelKind);
    }
    langButton.onclick = function () { setLanguage(state.lang === 'zh' ? 'en' : 'zh'); };

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
        var titles = { reading: t('bookmarks_title'), about: t('about_heading'), help: t('help_heading') };
        var content = panel(titles[kind]);
        if (kind === 'about') buildAboutPanel(content);
        else if (kind === 'help') buildHelpPanel(content);
        else buildReadingPanel(content);
    }

    aboutButton.onclick = function () { openPanel('about'); };
    helpButton.onclick = function () { openPanel('help'); };
    readingButton.onclick = function () { openPanel('reading'); };

    function buildAboutPanel(content) {
        content.innerHTML = '<p class="mics-about-description"></p><div class="mics-build-list"><div><strong>' + t('build_number') + '</strong><code></code></div><div><strong>' + t('build_id') + '</strong><code></code></div><div><strong>' + t('built') + '</strong><code></code></div></div>';
        var about = state.context && state.context.about ? state.context.about : {};
        var description = about.description || '';
        if (state.lang === 'zh' && (description.indexOf('A private ebook library') === 0 || description === '')) {
            description = t('about_description');
        } else if (!description) {
            description = t('about_description');
        }
        content.querySelector('p').textContent = description;
        var codes = content.querySelectorAll('code');
        codes[0].textContent = about.build_number || t('loading');
        codes[1].textContent = about.build_id || t('loading');
        codes[2].textContent = about.build_time || t('loading');
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
        values.csrf_token = state.context.csrf_token;
        return fetch(url, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8' }, body: params(values) }).then(function (response) {
            return response.json().then(function (data) { if (!response.ok) throw new Error(data.error || 'Request failed'); return data; });
        });
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
            }
        }
        applyLang();
        if (state.panelKind) openPanel(state.panelKind);
    }).catch(function () { fetch('/api/about', { cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (about) { state.context = { about: about }; applyLang(); }); });
}());