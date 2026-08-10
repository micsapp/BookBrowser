(function () {
    'use strict';
    function bookID() {
        var value = location.href.match(/\/download\/([^/?#]+)/);
        if (!value) return '';
        return decodeURIComponent(value[1]).replace(/\.[^.]+$/, '');
    }
    var id = bookID();
    if (!id) return;
    var state = { context: null, items: [], selectedLocator: '', selectedText: '' };

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
        return state.selectedText;
    }
    function go(value) {
        if (epub()) window.ePubViewer.state.rendition.display(value);
        else if (value.indexOf('page:') === 0 && window.PDFViewerApplication) window.PDFViewerApplication.page = parseInt(value.slice(5), 10) || 1;
    }
    document.addEventListener('selectionchange', selection);

    var tools = document.createElement('div');
    tools.className = 'mics-reader-tools';
    tools.innerHTML = '<button class="mics-reader-button" data-mics-reading hidden title="Bookmarks and notes">Save</button><button class="mics-reader-button" data-mics-about title="About MicsBook">About</button>';
    document.body.appendChild(tools);
    var readingButton = tools.querySelector('[data-mics-reading]');

    function panel(title) {
        var node = document.createElement('section');
        node.className = 'mics-reader-panel';
        node.innerHTML = '<header><h2></h2><button class="mics-reader-close" aria-label="Close">×</button></header><div class="mics-reader-content"></div>';
        node.querySelector('h2').textContent = title;
        node.querySelector('.mics-reader-close').onclick = function () { node.remove(); };
        document.body.appendChild(node);
        return node.querySelector('.mics-reader-content');
    }
    function closePanels() { document.querySelectorAll('.mics-reader-panel').forEach(function (node) { node.remove(); }); }
    tools.querySelector('[data-mics-about]').onclick = function () {
        closePanels();
        var content = panel(state.context ? state.context.about.name : 'MicsBook');
        var about = state.context ? state.context.about : {};
        content.innerHTML = '<p class="mics-about-description"></p><div class="mics-build-list"><div><strong>Build number</strong><code></code></div><div><strong>Build ID</strong><code></code></div><div><strong>Built</strong><code></code></div></div>';
        content.querySelector('p').textContent = about.description || 'Read, listen to, and organize your ebook library.';
        var codes = content.querySelectorAll('code');
        codes[0].textContent = about.build_number || 'loading'; codes[1].textContent = about.build_id || 'loading'; codes[2].textContent = about.build_time || 'loading';
    };

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
    function renderItems(content) {
        var list = content.querySelector('.mics-reader-list'); list.textContent = '';
        if (!state.items.length) { var empty = document.createElement('p'); empty.textContent = 'Nothing saved in this book yet.'; empty.className = 'mics-reader-status'; list.appendChild(empty); return; }
        state.items.forEach(function (item) {
            var card = document.createElement('article'); card.className = 'mics-reader-item';
            var head = document.createElement('div'); head.className = 'mics-reader-item-head'; head.innerHTML = '<span class="mics-reader-item-kind"></span><small></small>';
            head.querySelector('span').textContent = item.kind; head.querySelector('span').classList.add(item.kind); head.querySelector('small').textContent = item.locator_label;
            card.appendChild(head);
            var title = field(card, 'title', 'Title', item.title, false);
            var body = item.kind === 'note' ? field(card, 'body', 'Note', item.body, true) : null;
            var tags = field(card, 'tags', 'Tags', (item.tags || []).join(', '), false);
            var buttons = document.createElement('div'); buttons.className = 'mics-reader-item-buttons'; buttons.innerHTML = '<button class="mics-reader-mini">Go</button><button class="mics-reader-mini">Save</button><button class="mics-reader-mini delete">Delete</button>';
            buttons.children[0].onclick = function () { go(item.locator); closePanels(); };
            buttons.children[1].onclick = function () { request('/api/reader/items/' + encodeURIComponent(item.id), { title: title.value, body: body ? body.value : '', tags: tags.value }).then(function (updated) { Object.assign(item, updated); status(content, 'Saved.'); }).catch(function (err) { status(content, err.message, true); }); };
            buttons.children[2].onclick = function () { if (!confirm('Delete this ' + item.kind + '?')) return; request('/api/reader/items/' + encodeURIComponent(item.id) + '/delete', {}).then(function () { state.items = state.items.filter(function (other) { return other.id !== item.id; }); renderItems(content); status(content, 'Deleted.'); }).catch(function (err) { status(content, err.message, true); }); };
            card.appendChild(buttons); list.appendChild(card);
        });
    }
    function create(content, kind, title, body, tags) {
        selection();
        return request('/api/reader/items', { book_id: id, kind: kind, locator: state.selectedLocator || locator(), locator_label: locatorLabel(), title: title, body: body, excerpt: selection(), tags: tags }).then(function (item) {
            state.items.unshift(item); state.selectedLocator = ''; state.selectedText = ''; renderItems(content); status(content, kind === 'note' ? 'Note saved.' : 'Bookmark saved.'); return item;
        }).catch(function (err) { status(content, err.message, true); return null; });
    }
    readingButton.onclick = function () {
        closePanels(); selection();
        var content = panel('Bookmarks & notes');
        content.innerHTML = '<div class="mics-reader-actions"><button class="mics-reader-action">Bookmark here</button><button class="mics-reader-action secondary">Write note</button></div><div class="mics-note-editor" hidden></div><p class="mics-reader-status"></p><h3>Saved in this book</h3><div class="mics-reader-list"></div><a class="mics-reader-manage" href="/my-library/reading">Manage all bookmarks & notes</a>';
        var editor = content.querySelector('.mics-note-editor');
        content.querySelectorAll('.mics-reader-action')[0].onclick = function () { create(content, 'bookmark', '', '', ''); };
        content.querySelectorAll('.mics-reader-action')[1].onclick = function () {
            editor.hidden = !editor.hidden;
            if (editor.childNodes.length) return;
            var title = field(editor, 'title', 'Title', '', false); var body = field(editor, 'body', 'Note', '', true); var tags = field(editor, 'tags', 'Tags', '', false);
            var save = document.createElement('button'); save.className = 'mics-reader-action'; save.textContent = 'Save note'; save.style.marginTop = '9px'; save.onclick = function () { create(content, 'note', title.value, body.value, tags.value).then(function (item) { if (!item) return; title.value = ''; body.value = ''; tags.value = ''; editor.hidden = true; }); }; editor.appendChild(save);
        };
        renderItems(content);
    };

    fetch('/api/reader/context?book_id=' + encodeURIComponent(id), { credentials: 'same-origin', cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (context) {
        state.context = context; state.items = context.items || []; if (context.authenticated) readingButton.hidden = false;
    }).catch(function () { fetch('/api/about', { cache: 'no-store' }).then(function (response) { return response.json(); }).then(function (about) { state.context = { about: about }; }); });
}());
