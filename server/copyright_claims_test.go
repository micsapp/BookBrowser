package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/geek1011/BookBrowser/auth"
)

func TestCopyrightClaimSubmissionAdminReviewAndRemoval(t *testing.T) {
	s := newAuthTestServer(t)
	bookPath := writeTestEpub(t, s.BookDir, "reported.epub", "Reported Book")
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	if len(s.Indexer.BookList()) != 1 {
		t.Fatalf("indexed books=%d", len(s.Indexer.BookList()))
	}
	bookID := s.Indexer.BookList()[0].ID()

	login := requestServer(s, http.MethodGet, "/login", nil)
	loginBody := login.Body.String()
	for _, expected := range []string{
		`data-i18n="about_copyright_notice"`, `data-copyright-dialog`,
		`data-copyright-form`, `name="authorized"`, `/static/about.js`,
	} {
		if !strings.Contains(loginBody, expected) {
			t.Errorf("About copyright flow is missing %q", expected)
		}
	}
	context := requestServer(s, http.MethodGet, "/api/copyright/context", nil)
	csrf := cookieNamed(context, csrfCookieName)
	if context.Code != http.StatusOK || csrf == nil || !strings.Contains(context.Body.String(), `"csrf_token"`) {
		t.Fatalf("copyright context status=%d cookies=%v body=%s", context.Code, context.Result().Cookies(), context.Body.String())
	}

	claimForm := url.Values{
		"csrf_token":     {csrf.Value},
		"claimant_name":  {"Rights Holder"},
		"claimant_email": {"rights@example.com"},
		"book_id":        {bookID},
		"book_title":     {"Reported Book"},
		"source_url":     {"https://example.test/read/reported"},
		"details":        {"I control the electronic publication rights and did not authorize this copy."},
		"authorized":     {"on"},
	}
	created := requestServer(s, http.MethodPost, "/api/copyright-claims", claimForm, csrf)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"ok":true`) {
		t.Fatalf("create copyright claim status=%d body=%s", created.Code, created.Body.String())
	}
	claims, err := s.auth.CopyrightClaimsAll()
	if err != nil || len(claims) != 1 || claims[0].BookID != bookID {
		t.Fatalf("claims=%#v err=%v", claims, err)
	}

	badCSRF := requestServer(s, http.MethodPost, "/api/copyright-claims", claimForm)
	if badCSRF.Code != http.StatusForbidden {
		t.Fatalf("claim without CSRF status=%d", badCSRF.Code)
	}

	_, _, adminSession := registerTestUser(t, s, "admin-claims@example.com", "Claims Admin")
	reader, _, readerSession := registerTestUser(t, s, "reader-claims@example.com", "Claims Reader")
	manager, _, managerSession := registerTestUser(t, s, "manager-claims@example.com", "Claims Manager")
	if _, err := s.auth.UpdateUser(manager.ID, auth.RoleManager, true); err != nil {
		t.Fatal(err)
	}
	if forbidden := requestServer(s, http.MethodGet, "/admin/copyright", nil, readerSession); forbidden.Code != http.StatusForbidden {
		t.Fatalf("reader copyright admin status=%d", forbidden.Code)
	}
	if reader.Role != auth.RoleReader {
		t.Fatalf("second user role=%q", reader.Role)
	}
	managerDashboard := requestServer(s, http.MethodGet, "/admin", nil, managerSession)
	if managerDashboard.Code != http.StatusOK {
		t.Fatalf("manager dashboard status=%d", managerDashboard.Code)
	}
	if strings.Contains(managerDashboard.Body.String(), "/admin/copyright") || strings.Contains(managerDashboard.Body.String(), "nav-copyright-claims") {
		t.Fatal("manager dashboard exposed the copyright claim navigation")
	}
	if forbidden := requestServer(s, http.MethodGet, "/admin/copyright", nil, managerSession); forbidden.Code != http.StatusForbidden {
		t.Fatalf("manager copyright admin status=%d", forbidden.Code)
	}
	if forbidden := requestServer(s, http.MethodPost, "/admin/copyright/"+claims[0].ID, nil, managerSession); forbidden.Code != http.StatusForbidden {
		t.Fatalf("manager copyright resolve status=%d", forbidden.Code)
	}

	adminPage := requestServer(s, http.MethodGet, "/admin/copyright", nil, adminSession)
	if adminPage.Code != http.StatusOK || !strings.Contains(adminPage.Body.String(), "Reported Book") || !strings.Contains(adminPage.Body.String(), "Remove book and resolve") || !strings.Contains(adminPage.Body.String(), `class="nav-copyright-claims"`) {
		t.Fatalf("admin copyright page status=%d body=%s", adminPage.Code, adminPage.Body.String())
	}
	adminCSRF := cookieNamed(adminPage, csrfCookieName)
	if adminCSRF == nil {
		t.Fatal("admin copyright page did not issue a CSRF cookie")
	}

	missingClaim := url.Values{
		"csrf_token": {adminCSRF.Value}, "action": {"remove"}, "book_id": {bookID},
	}
	requestServer(s, http.MethodPost, "/admin/copyright/missing", missingClaim, adminCSRF, adminSession)
	if _, err := os.Stat(bookPath); err != nil {
		t.Fatalf("unknown claim removed a book: %v", err)
	}

	remove := url.Values{
		"csrf_token": {adminCSRF.Value}, "action": {"remove"}, "book_id": {bookID},
		"resolution": {"Rights confirmed; book moved to recoverable storage."},
	}
	resolved := requestServer(s, http.MethodPost, "/admin/copyright/"+claims[0].ID, remove, adminCSRF, adminSession)
	if resolved.Code != http.StatusSeeOther || resolved.Header().Get("Location") != "/admin/copyright?saved=1" {
		t.Fatalf("remove claim status=%d location=%q body=%s", resolved.Code, resolved.Header().Get("Location"), resolved.Body.String())
	}
	if _, err := os.Stat(bookPath); !os.IsNotExist(err) {
		t.Fatalf("reported book still exists after removal: %v", err)
	}
	stored, err := s.auth.CopyrightClaimByID(claims[0].ID)
	if err != nil || stored.Status != auth.CopyrightClaimRemoved {
		t.Fatalf("resolved claim=%#v err=%v", stored, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && s.findBook(bookID) != nil {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCopyrightClaimI18NAndReaderSurface(t *testing.T) {
	s := newAuthTestServer(t)
	read := func(path string) string {
		t.Helper()
		response := requestServer(s, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, response.Code)
		}
		return response.Body.String()
	}
	translations := read("/static/site-i18n.js")
	for _, expected := range []string{"本站收集的图书", "报告版权问题", "版权申诉", "下架图书并结案", "copyright_status_removed"} {
		if !strings.Contains(translations, expected) {
			t.Errorf("site copyright i18n is missing %q", expected)
		}
	}
	readerTools := read("/static/reader-tools.js")
	for _, expected := range []string{"copyright_notice", "openCopyrightDialog", "showCopyrightToast", "/api/copyright/context", "/api/copyright-claims", "管理员收到后会进行审核"} {
		if !strings.Contains(readerTools, expected) {
			t.Errorf("reader copyright flow is missing %q", expected)
		}
	}
	aboutScript := read("/static/about.js")
	for _, expected := range []string{"close(copyrightDialog)", "showToast", "copyright_submitted"} {
		if !strings.Contains(aboutScript, expected) {
			t.Errorf("site copyright success flow is missing %q", expected)
		}
	}
	claimStyles := read("/static/style.css")
	for _, expected := range []string{
		".copyright-claim-card .admin-request-meta",
		"overflow-wrap: anywhere",
		"grid-template-columns: minmax(0, 1fr)",
		".copyright-claim-card .request-card-head { flex-direction: column",
		".admin-request-actions.copyright-claim-actions",
		".admin-request-resolve.copyright-remove-form",
		".admin-request-resolve.copyright-resolve-form",
	} {
		if !strings.Contains(claimStyles, expected) {
			t.Errorf("mobile copyright claim styles are missing %q", expected)
		}
	}
	genericForm := strings.LastIndex(claimStyles, ".admin-request-resolve {")
	claimForm := strings.LastIndex(claimStyles, ".admin-request-resolve.copyright-remove-form")
	if genericForm < 0 || claimForm < genericForm {
		t.Error("copyright claim form layout must follow the generic admin form rules in the CSS cascade")
	}
}
