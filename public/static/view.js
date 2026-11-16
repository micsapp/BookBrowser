(function () {
    "use strict";
    var view = document.querySelector(".current-view");
    var list = document.querySelector(".view-list");
    var cards = document.querySelector(".view-cards");
    if (!view || !list || !cards) return;
    var key = "bookbrowser.catalog-view";
    function setView(value) {
        value = value === "list" ? "list" : "cards";
        view.classList.toggle("list", value === "list");
        view.classList.toggle("cards", value === "cards");
        cards.classList.toggle("active", value === "cards");
        list.classList.toggle("active", value === "list");
        cards.setAttribute("aria-pressed", String(value === "cards"));
        list.setAttribute("aria-pressed", String(value === "list"));
        try { localStorage.setItem(key, value); } catch (_) {}
    }
    list.addEventListener("click", function () { setView("list"); });
    cards.addEventListener("click", function () { setView("cards"); });
    var saved;
    try { saved = localStorage.getItem(key); } catch (_) {}
    setView(saved);
})();
