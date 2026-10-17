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
            nav_copyright_claims: "Copyright claims",
            nav_system_alerts: "System alerts",
            nav_signout: "Sign out",
            nav_signin: "Sign in",
            nav_register: "Register",
            nav_about: "About",
            role_reader: "reader",
            role_manager: "manager",
            role_admin: "admin",
            close: "Close",
            cancel: "Cancel",
            about_dialog_description: "Read, listen to, and organize your ebook library from any device.",
            about_copyright_notice: "Books collected on this site were sourced from materials publicly available for download on the internet. If you believe a title infringes copyright, submit a detailed report. Administrators will review it and remove relevant content when warranted.",
            copyright_report: "Report a copyright concern",
            copyright_dialog_title: "Copyright concern",
            copyright_dialog_intro: "Provide enough information for an administrator to identify the work, understand your rights, and review the requested removal.",
            copyright_field_name: "Your name or organization",
            copyright_field_email: "Contact email",
            copyright_field_book: "Book title or reference",
            copyright_field_details: "Detailed claim",
            copyright_book_placeholder: "Book title, author, ISBN, or URL",
            copyright_details_placeholder: "Identify the copyrighted work, explain your relationship to it, and describe why removal is requested.",
            copyright_authorized: "I confirm this information is accurate and I am authorized to submit this report.",
            copyright_submit: "Send report",
            copyright_submitting: "Sending…",
            copyright_submitted: "Report received. An administrator will review it.",
            copyright_submit_failed: "The report could not be sent. Check the information and try again.",
            admin_copyright_tab: "Copyright claims",
            admin_copyright_pending: "pending",
            admin_copyright_card_help: "Review reports and remove affected books",
            admin_copyright_saved: "The copyright claim was resolved.",
            admin_copyright_reported: "Reported",
            admin_copyright_source: "Page reported from",
            admin_copyright_book_option: "Choose the reported library book…",
            admin_copyright_resolution_placeholder: "Resolution note",
            admin_copyright_remove: "Remove book and resolve",
            admin_copyright_resolve: "Resolve without removal",
            admin_copyright_none_title: "No copyright claims",
            admin_copyright_none_body: "Copyright concerns submitted from the About dialog will appear here.",
            copyright_status_pending: "Pending review",
            copyright_status_removed: "Book removed",
            copyright_status_resolved: "Resolved",
            admin_system_tab: "System alerts",
            admin_system_open: "ongoing",
            admin_system_card_help: "Service failures detected by background checks",
            admin_system_intro: "Background checks post here when a service this site depends on fails. Each problem appears once and updates in place, and it closes automatically when the service recovers.",
            admin_system_saved: "Marked as read.",
            admin_system_read: "Mark as read",
            admin_system_read_all: "Mark all as read",
            admin_system_read_by: "Read by",
            admin_system_first_seen: "First seen",
            admin_system_last_seen: "last seen",
            admin_system_occurrences: "occurrences",
            admin_system_resolved_at: "Recovered",
            admin_system_none_title: "No system alerts",
            admin_system_none_body: "Problems detected by background checks, such as failing text-to-speech, will appear here.",
            system_alert_status_open: "Ongoing",
            system_alert_status_resolved: "Recovered",
            build_number: "Build number",
            build_id: "Build ID",
            built: "Built",
            help_dialog_title: "How to use this app",
            help_loading: "Loading the user guide…",
            help_error: "The user guide could not be loaded. Check your connection and try again.",
            reader_loading: "Loading the book…",
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
            nav_copyright_claims: "版权申诉",
            nav_system_alerts: "系统告警",
            nav_signout: "退出登录",
            nav_signin: "登录",
            nav_register: "注册",
            nav_about: "关于",
            role_reader: "读者",
            role_manager: "图书管理员",
            role_admin: "管理员",
            close: "关闭",
            cancel: "取消",
            about_dialog_description: "随时随地阅读、收听并整理您的电子书库。",
            about_copyright_notice: "本站收集的图书均来源于互联网上可公开下载的资源。如果您认为任何图书侵犯了版权，请提交报告并填写详细信息。管理员收到后会进行审核，并在理由成立时将相关图书下架。",
            copyright_report: "报告版权问题",
            copyright_dialog_title: "版权申诉",
            copyright_dialog_intro: "请提供足够的信息，以便管理员确认相关作品、了解您的权利并审核下架请求。",
            copyright_field_name: "您的姓名或机构名称",
            copyright_field_email: "联系邮箱",
            copyright_field_book: "书名或相关信息",
            copyright_field_details: "申诉详情",
            copyright_book_placeholder: "书名、作者、ISBN 或链接",
            copyright_details_placeholder: "请说明受版权保护的作品、您与该作品的关系，以及申请下架的原因。",
            copyright_authorized: "我确认以上信息准确，并且我有权提交此申诉。",
            copyright_submit: "发送申诉",
            copyright_submitting: "正在发送…",
            copyright_submitted: "申诉已收到，管理员将进行审核。",
            copyright_submit_failed: "申诉发送失败，请检查所填信息后重试。",
            admin_copyright_tab: "版权申诉",
            admin_copyright_pending: "待处理",
            admin_copyright_card_help: "审核申诉并下架相关图书",
            admin_copyright_saved: "版权申诉已处理。",
            admin_copyright_reported: "提交于",
            admin_copyright_source: "报告来源页面",
            admin_copyright_book_option: "选择被报告的馆藏图书…",
            admin_copyright_resolution_placeholder: "处理说明",
            admin_copyright_remove: "下架图书并结案",
            admin_copyright_resolve: "无需下架并结案",
            admin_copyright_none_title: "暂无版权申诉",
            admin_copyright_none_body: "用户从“关于”窗口提交的版权申诉将显示在这里。",
            copyright_status_pending: "待审核",
            copyright_status_removed: "图书已下架",
            copyright_status_resolved: "已处理",
            admin_system_tab: "系统告警",
            admin_system_open: "进行中",
            admin_system_card_help: "后台检查发现的服务故障",
            admin_system_intro: "当本站依赖的服务出现故障时，后台检查会在这里发布告警。每个问题只出现一次并原地更新，服务恢复后自动关闭。",
            admin_system_saved: "已标记为已读。",
            admin_system_read: "标记为已读",
            admin_system_read_all: "全部标记为已读",
            admin_system_read_by: "已读：",
            admin_system_first_seen: "首次发现",
            admin_system_last_seen: "最近发现",
            admin_system_occurrences: "发生次数",
            admin_system_resolved_at: "恢复于",
            admin_system_none_title: "暂无系统告警",
            admin_system_none_body: "后台检查发现的问题（例如语音朗读故障）会显示在这里。",
            system_alert_status_open: "进行中",
            system_alert_status_resolved: "已恢复",
            build_number: "构建编号",
            build_id: "构建 ID",
            built: "构建时间",
            help_dialog_title: "使用说明",
            help_loading: "正在加载使用说明…",
            help_error: "无法加载使用说明，请检查网络连接后重试。",
            reader_loading: "正在加载图书…",
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
        var claimStatuses = document.querySelectorAll("[data-i18n-claim-status]");
        for (i = 0; i < claimStatuses.length; i++) claimStatuses[i].textContent = t("copyright_status_" + claimStatuses[i].getAttribute("data-i18n-claim-status"));
        var alertStatuses = document.querySelectorAll("[data-i18n-alert-status]");
        for (i = 0; i < alertStatuses.length; i++) alertStatuses[i].textContent = t("system_alert_status_" + alertStatuses[i].getAttribute("data-i18n-alert-status"));
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
