(function () {
    "use strict";
    var key = "bookbrowser.theme";
    var root = document.documentElement;
    var system = window.matchMedia("(prefers-color-scheme: dark)");
    var preference = readPreference();

    function readPreference() {
        try {
            var saved = localStorage.getItem(key);
            return saved === "dark" || saved === "light" ? saved : null;
        } catch (_) { return null; }
    }

    function updateControls() {
        var dark = root.getAttribute("data-theme") === "dark";
        var chinese = root.lang.indexOf("zh") === 0;
        var label = chinese ? (dark ? "浅色模式" : "深色模式") : (dark ? "Light mode" : "Dark mode");
        var action = chinese ? (dark ? "切换到浅色主题" : "切换到深色主题") : (dark ? "Switch to light theme" : "Switch to dark theme");
        document.querySelectorAll("[data-theme-toggle]").forEach(function (button) {
            button.setAttribute("aria-label", action);
            button.setAttribute("title", action);
            var text = button.querySelector("[data-theme-label]");
            if (text) text.textContent = label;
        });
    }

    function apply(theme) {
        root.setAttribute("data-theme", theme);
        root.style.colorScheme = theme;
        root.style.backgroundColor = theme === "dark" ? "#101722" : "#f6f8fb";
        var meta = document.querySelector('meta[name="theme-color"]');
        if (meta) meta.content = theme === "dark" ? "#101722" : "#f6f8fb";
        updateControls();
    }

    function resolvedTheme() { return preference || (system.matches ? "dark" : "light"); }
    // This script runs in the head so the saved theme applies before first paint.
    apply(resolvedTheme());

    document.addEventListener("click", function (event) {
        if (!event.target.closest("[data-theme-toggle]")) return;
        preference = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
        try { localStorage.setItem(key, preference); } catch (_) {}
        apply(preference);
    });
    document.addEventListener("DOMContentLoaded", updateControls);
    new MutationObserver(updateControls).observe(root, { attributes: true, attributeFilter: ["lang"] });
    window.addEventListener("storage", function (event) {
        if (event.key !== key && event.key !== null) return;
        preference = readPreference();
        apply(resolvedTheme());
    });
    function systemChanged() { if (!preference) apply(resolvedTheme()); }
    if (system.addEventListener) system.addEventListener("change", systemChanged);
    else system.addListener(systemChanged);
})();
