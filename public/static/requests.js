(function () {
    "use strict";

    var template = document.getElementById("book-request-template");
    var openButtons = document.querySelectorAll(".book-request-open-button");
    if (!template || !openButtons.length || !window.picoModal) return;

    var fragment = template.content.cloneNode(true);
    var form = fragment.querySelector("#book-request-form");
    var status = fragment.querySelector("#book-request-status");
    if (!form || !status) return;

    var modal = window.picoModal({
        content: fragment,
        modalClass: "book-request-modal",
        closeButton: true,
        overlayClose: true,
        escCloses: true,
        modalStyles: {
            overflow: "auto",
            backgroundColor: "white",
            padding: "12px 14px",
            borderRadius: "12px"
        }
    });

    var submitting = false;

    var t = function (key, fallback) {
        if (window.SiteI18N && window.SiteI18N.t) {
            var value = window.SiteI18N.t(key);
            if (value !== key) return value;
        }
        return fallback;
    };

    var setStatus = function (kind, message) {
        status.className = "book-request-status book-request-status-" + kind;
        status.textContent = message;
    };

    var openModal = function () {
        if (!submitting) form.reset();
        setStatus("", "");
        modal.show();
        // picoModal builds its DOM on the first show(), so translate the
        // freshly inserted form now instead of at script load.
        if (window.SiteI18N && window.SiteI18N.apply) window.SiteI18N.apply();
        var first = form.querySelector("input[name='title']");
        if (first) first.focus();
    };

    for (var i = 0; i < openButtons.length; i++) {
        openButtons[i].addEventListener("click", openModal);
    }

    form.addEventListener("submit", function (event) {
        event.preventDefault();
        if (submitting) return;
        submitting = true;
        setStatus("loading", t("request_submitting", "Submitting…"));
        fetch("/requests", {
            method: "POST",
            headers: { "X-Requested-With": "fetch" },
            body: new FormData(form)
        }).then(function (response) {
            return response.json().then(function (data) {
                return { ok: response.ok, data: data };
            });
        }).then(function (result) {
            submitting = false;
            if (result.ok) {
                setStatus("success", t("request_submitted_success", "Request submitted. We will add the book, or reply to you under My requests if it can't be found."));
                updateBadge(1);
                form.reset();
            } else {
                setStatus("error", (result.data && result.data.error) || t("request_submit_failed", "The request could not be submitted. Please try again."));
            }
        }).catch(function () {
            submitting = false;
            setStatus("error", t("request_submit_failed", "The request could not be submitted. Please try again."));
        });
    });

    function updateBadge(delta) {
        var navButton = document.querySelector(".book-request-nav-button");
        if (!navButton) return;
        var badge = navButton.querySelector(".nav-badge");
        if (!badge) {
            badge = document.createElement("span");
            badge.className = "nav-badge";
            navButton.appendChild(badge);
        }
        var count = parseInt(badge.textContent, 10) || 0;
        count = Math.max(0, count + delta);
        badge.textContent = String(count);
        badge.setAttribute("aria-label", count + " pending request(s)");
    }
})();
