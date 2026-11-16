(function () {
    "use strict";
    var path = location.pathname.replace(/\/$/, "") || "/";
    var matches = Array.from(document.querySelectorAll("[data-nav-link]")).filter(function (link) {
        var href = link.getAttribute("href");
        return path === href || path.indexOf(href + "/") === 0;
    });
    matches.sort(function (a, b) { return b.getAttribute("href").length - a.getAttribute("href").length; });
    if (matches[0]) matches[0].setAttribute("aria-current", "page");
    var sort = document.querySelector("[data-catalog-sort]");
    if (sort) {
        var requested = new URLSearchParams(location.search).get("sort") || "modified-desc";
        if (Array.from(sort.options).some(function (option) { return option.value === requested; })) sort.value = requested;
        sort.addEventListener("change", function () {
            var url = new URL(location.href);
            url.searchParams.set("sort", sort.value);
            location.assign(url.pathname + url.search);
        });
    }
    var toggle = document.querySelector("[data-menu-toggle]");
    var sidebar = document.querySelector(".sidebar");
    var backdrop = document.querySelector("[data-menu-close]");
    var content = document.getElementById("main-content");
    if (!toggle || !sidebar || !backdrop) return;
    var mobile = window.matchMedia("(max-width: 900px)");
    var open = false;
    function setOpen(next, restoreFocus) {
        open = mobile.matches && next;
        document.body.classList.toggle("menu-open", open);
        toggle.setAttribute("aria-expanded", String(open));
        backdrop.hidden = !open;
        sidebar.inert = mobile.matches && !open;
        if (mobile.matches && !open) sidebar.setAttribute("aria-hidden", "true");
        else sidebar.removeAttribute("aria-hidden");
        if (content) content.inert = open;
        if (open) sidebar.querySelector("a, button").focus();
        else if (restoreFocus) toggle.focus();
    }
    toggle.addEventListener("click", function () { setOpen(!open, true); });
    backdrop.addEventListener("click", function () { setOpen(false, true); });
    sidebar.querySelector("[data-menu-dismiss]").addEventListener("click", function () { setOpen(false, true); });
    sidebar.addEventListener("click", function (event) {
        if (event.target.closest("a, [data-help-open], [data-about-open], .book-request-open-button")) setOpen(false, false);
    });
    document.addEventListener("keydown", function (event) {
        if (!open) return;
        if (event.key === "Escape") { event.preventDefault(); setOpen(false, true); }
        if (event.key !== "Tab") return;
        var focusable = Array.from(sidebar.querySelectorAll("a[href], button:not([disabled])")).filter(function (item) { return item.getClientRects().length; });
        var first = focusable[0], last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });
    mobile.addEventListener("change", function () { setOpen(false, false); });
    setOpen(false, false);
})();
