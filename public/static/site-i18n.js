(function () {
    "use strict";

    var STRINGS = {
        en: {
            nav_home: "Home",
            nav_books: "Books",
            nav_my_library: "My Library",
            nav_authors: "Authors",
            nav_series: "Series",
            nav_random: "Random",
            nav_search: "Search",
            nav_request: "Request a book",
            nav_my_requests: "My requests",
            nav_admin: "Admin",
            nav_signout: "Sign out",
            nav_signin: "Sign in",
            nav_register: "Register",
            nav_about: "About",
            role_reader: "reader",
            role_manager: "manager",
            role_admin: "admin",
            close: "Close",
            about_dialog_description: "Read, listen to, and organize your ebook library from any device.",
            build_number: "Build number",
            build_id: "Build ID",
            built: "Built",
            help_dialog_title: "How to use this app",
            help_loading: "Loading the user guide…",
            help_error: "The user guide could not be loaded. Check your connection and try again.",
            request_modal_title: "Request a book",
            request_modal_intro: "Can't find a title in the library? Send the details and we will add it, or reply to you if it can't be found.",
            request_field_title: "Title",
            request_field_author: "Author",
            request_field_notes: "Details",
            request_title_placeholder: "Book title",
            request_author_placeholder: "Author (optional)",
            request_notes_placeholder: "Edition, language, ISBN, where you found it… (optional)",
            request_submit: "Submit request",
            request_view_mine: "My requests",
            request_submitting: "Submitting…",
            request_submitted_success: "Request submitted. We will add the book, or reply to you under My requests if it can't be found.",
            request_submit_failed: "The request could not be submitted. Please try again.",
            request_status_pending: "Pending review",
            request_status_added: "Added",
            request_status_unavailable: "Not found",
            requests_heading_eyebrow: "Missing a title?",
            requests_heading: "My book requests",
            requests_request_button: "Request a book",
            requests_saved_notice: "Your request was submitted. A manager will review it, and the outcome will appear here.",
            requests_added_prefix: "The book was added to the library:",
            requests_open_book: "Open the book",
            requests_unavailable_fallback: "The book could not be found.",
            requests_requested_on: "Requested",
            requests_none_title: "No requests yet",
            requests_none_body: "Use the Request a book button in the menu whenever the library is missing a title.",
            admin_requests_resolved_notice: "The request was updated. The requester can now see the outcome on their requests page.",
            admin_requests_search: "Search LibGen",
            admin_requests_added_option: "Choose the added book…",
            admin_requests_added_note: "Optional note to the requester",
            admin_requests_mark_added: "Mark added",
            admin_requests_unavailable_note: "Why it can't be found — shown to the requester",
            admin_requests_not_available: "Not available",
            admin_requests_requested: "requested",
            admin_requests_none_title: "No book requests",
            admin_requests_none_body: "Requests readers send from the Request a book button will appear here.",
            profile_eyebrow: "Your account",
            profile_heading: "Profile",
            profile_email: "Email",
            profile_role: "Role",
            profile_login_method: "Sign-in method",
            profile_google: "Google",
            profile_email_password: "Email password",
            profile_joined: "Joined",
            profile_last_login: "Last sign-in",
            profile_never: "Never",
            profile_display_name: "Display name",
            profile_display_name_placeholder: "How your name appears in the menu",
            profile_location: "Location",
            profile_location_placeholder: "City, country… (optional)",
            profile_bio: "About you",
            profile_bio_placeholder: "A short introduction (optional)",
            profile_save: "Save profile",
            profile_saved: "Your profile was updated."
        },
        zh: {
            nav_home: "首页",
            nav_books: "图书",
            nav_my_library: "我的书库",
            nav_authors: "作者",
            nav_series: "丛书",
            nav_random: "随机",
            nav_search: "搜索",
            nav_request: "求书",
            nav_my_requests: "我的求书",
            nav_admin: "管理",
            nav_signout: "退出登录",
            nav_signin: "登录",
            nav_register: "注册",
            nav_about: "关于",
            role_reader: "读者",
            role_manager: "图书管理员",
            role_admin: "管理员",
            close: "关闭",
            about_dialog_description: "随时随地阅读、收听并整理您的电子书库。",
            build_number: "构建编号",
            build_id: "构建 ID",
            built: "构建时间",
            help_dialog_title: "使用说明",
            help_loading: "正在加载使用说明…",
            help_error: "无法加载使用说明，请检查网络连接后重试。",
            request_modal_title: "求书",
            request_modal_intro: "书库中找不到想要的书？提交详细信息，我们会添加该书；若确实找不到，会在“我的求书”中回复您。",
            request_field_title: "书名",
            request_field_author: "作者",
            request_field_notes: "详细说明",
            request_title_placeholder: "书名",
            request_author_placeholder: "作者（可选）",
            request_notes_placeholder: "版本、语言、ISBN、来源…（可选）",
            request_submit: "提交请求",
            request_view_mine: "我的求书",
            request_submitting: "正在提交…",
            request_submitted_success: "请求已提交。我们会添加该书；若找不到，会在“我的求书”中回复您。",
            request_submit_failed: "请求提交失败，请重试。",
            request_status_pending: "审核中",
            request_status_added: "已添加",
            request_status_unavailable: "未找到",
            requests_heading_eyebrow: "缺少某本书？",
            requests_heading: "我的求书",
            requests_request_button: "求书",
            requests_saved_notice: "请求已提交。图书管理员会审核，结果将显示在这里。",
            requests_added_prefix: "该书已添加到书库：",
            requests_open_book: "打开这本书",
            requests_unavailable_fallback: "未能找到这本书。",
            requests_requested_on: "请求于",
            requests_none_title: "还没有求书请求",
            requests_none_body: "当书库缺少某本书时，使用菜单中的求书按钮提交请求。",
            admin_requests_resolved_notice: "请求已更新。请求者现在可以在其求书页面看到结果。",
            admin_requests_search: "搜索 LibGen",
            admin_requests_added_option: "选择已添加的书…",
            admin_requests_added_note: "给请求者的备注（可选）",
            admin_requests_mark_added: "标记已添加",
            admin_requests_unavailable_note: "无法找到的原因——将显示给请求者",
            admin_requests_not_available: "未找到",
            admin_requests_requested: "请求于",
            admin_requests_none_title: "暂无求书请求",
            admin_requests_none_body: "读者通过求书按钮提交的请求将显示在这里。",
            profile_eyebrow: "您的账户",
            profile_heading: "个人资料",
            profile_email: "邮箱",
            profile_role: "角色",
            profile_login_method: "登录方式",
            profile_google: "Google",
            profile_email_password: "邮箱密码",
            profile_joined: "注册时间",
            profile_last_login: "上次登录",
            profile_never: "从未",
            profile_display_name: "显示名称",
            profile_display_name_placeholder: "菜单中显示的名称",
            profile_location: "所在地",
            profile_location_placeholder: "城市、国家…（可选）",
            profile_bio: "关于您",
            profile_bio_placeholder: "简短介绍（可选）",
            profile_save: "保存资料",
            profile_saved: "个人资料已更新。"
        }
    };

    var LANG_KEY = "micsReader:lang";

    function resolveLang() {
        try {
            var stored = window.localStorage.getItem(LANG_KEY);
            if (stored === "en" || stored === "zh") return stored;
        } catch (_) {}
        var nav = (navigator.language || navigator.userLanguage || "").split("-")[0];
        return nav === "zh" ? "zh" : "en";
    }

    var lang = resolveLang();

    function t(key) {
        var dict = STRINGS[lang];
        if (dict && dict[key] !== undefined) return dict[key];
        if (STRINGS.en[key] !== undefined) return STRINGS.en[key];
        return key;
    }

    function apply() {
        document.documentElement.lang = lang;
        var i;
        var items = document.querySelectorAll("[data-i18n]");
        for (i = 0; i < items.length; i++) items[i].textContent = t(items[i].getAttribute("data-i18n"));
        var titles = document.querySelectorAll("[data-i18n-title]");
        for (i = 0; i < titles.length; i++) titles[i].setAttribute("title", t(titles[i].getAttribute("data-i18n-title")));
        var placeholders = document.querySelectorAll("[data-i18n-placeholder]");
        for (i = 0; i < placeholders.length; i++) placeholders[i].setAttribute("placeholder", t(placeholders[i].getAttribute("data-i18n-placeholder")));
        var aria = document.querySelectorAll("[data-i18n-aria]");
        for (i = 0; i < aria.length; i++) aria[i].setAttribute("aria-label", t(aria[i].getAttribute("data-i18n-aria")));
        var statuses = document.querySelectorAll("[data-i18n-status]");
        for (i = 0; i < statuses.length; i++) statuses[i].textContent = t("request_status_" + statuses[i].getAttribute("data-i18n-status"));
        var roles = document.querySelectorAll("[data-i18n-role]");
        for (i = 0; i < roles.length; i++) roles[i].textContent = t("role_" + roles[i].getAttribute("data-i18n-role"));
        var switches = document.querySelectorAll("[data-lang-switch]");
        for (i = 0; i < switches.length; i++) switches[i].textContent = lang === "zh" ? "English" : "中文";
    }

    function persist(next) {
        try { window.localStorage.setItem(LANG_KEY, next); } catch (_) {}
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (!meta) return;
        var body = new URLSearchParams();
        body.set("csrf_token", meta.getAttribute("content") || "");
        body.set("language", next);
        fetch("/api/reader/language", {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: body.toString()
        }).catch(function () {});
    }

    function setLang(next) {
        if (next !== "en" && next !== "zh") return;
        if (next === lang) return;
        lang = next;
        persist(next);
        apply();
        try { window.dispatchEvent(new CustomEvent("micslangchange", { detail: { lang: lang } })); } catch (_) {}
    }

    document.addEventListener("click", function (event) {
        var target = event.target;
        var button = target && target.closest ? target.closest("[data-lang-switch]") : null;
        if (!button) return;
        setLang(lang === "zh" ? "en" : "zh");
    });

    apply();

    window.SiteI18N = { t: t, lang: function () { return lang; }, setLang: setLang, apply: apply };
})();
