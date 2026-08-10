package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geek1011/BookBrowser/auth"
	_ "github.com/geek1011/BookBrowser/formats/epub"
	_ "github.com/geek1011/BookBrowser/formats/mobi"
	_ "github.com/geek1011/BookBrowser/formats/pdf"
)

func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	bookDir := t.TempDir()
	coverDir := t.TempDir()
	server := NewServer("127.0.0.1:0", bookDir, coverDir, "test", false, false)
	t.Cleanup(func() { server.auth.Close() })
	return server
}

func requestServer(s *Server, method, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	request := httptest.NewRequest(method, target, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	return response
}

func cookieNamed(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestFirstRegistrationCreatesAdmin(t *testing.T) {
	s := newAuthTestServer(t)
	page := requestServer(s, http.MethodGet, "/register", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("register GET status = %d", page.Code)
	}
	csrf := cookieNamed(page, csrfCookieName)
	if csrf == nil {
		t.Fatal("registration page did not set CSRF cookie")
	}
	form := url.Values{
		"csrf_token":       {csrf.Value},
		"name":             {"Admin"},
		"email":            {"admin@example.com"},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"correct horse battery staple"},
	}
	created := requestServer(s, http.MethodPost, "/register", form, csrf)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("registration status=%d body=%s", created.Code, created.Body.String())
	}
	user, err := s.auth.UserByEmail("admin@example.com")
	if err != nil || user == nil || user.Role != auth.RoleAdmin {
		t.Fatalf("bootstrap account = %#v, %v", user, err)
	}
	if cookieNamed(created, sessionCookieName) == nil {
		t.Fatal("registration did not create a session cookie")
	}
}

func TestModernLoginAndConfiguredGoogleButton(t *testing.T) {
	t.Setenv("BOOKBROWSER_GOOGLE_CLIENT_ID", "browser-client.apps.googleusercontent.com")
	s := newAuthTestServer(t)

	login := requestServer(s, http.MethodGet, "/login", nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login GET status = %d", login.Code)
	}
	body := login.Body.String()
	for _, expected := range []string{
		`class="auth-shell"`,
		`aria-label="MicsBook"`,
		`application-name" content="MicsBook"`,
		`id="google-button-host"`,
		`accounts.google.com/gsi/client`,
		`client_id: "browser-client.apps.googleusercontent.com"`,
		`Sign in to your library`,
		`Copyright &copy;`,
		`Micstec`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("modern Google login is missing %q", expected)
		}
	}

	register := requestServer(s, http.MethodGet, "/register", nil)
	if register.Code != http.StatusOK || !strings.Contains(register.Body.String(), `class="auth-shell auth-shell-register"`) {
		t.Fatalf("modern register page status=%d", register.Code)
	}
}

func TestRouteAccessPolicy(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.auth.RegisterEmail("reader@example.com", "Reader", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err := s.auth.NewSession(reader.ID)
	if err != nil {
		t.Fatal(err)
	}
	readerCookie := &http.Cookie{Name: sessionCookieName, Value: readerToken}

	if response := requestServer(s, http.MethodGet, "/books", nil); response.Code != http.StatusSeeOther {
		t.Fatalf("anonymous catalog status=%d", response.Code)
	}
	if response := requestServer(s, http.MethodGet, "/books/missing", nil); response.Code != http.StatusNotFound {
		t.Fatalf("anonymous direct-book status=%d", response.Code)
	}
	if response := requestServer(s, http.MethodGet, "/books", nil, readerCookie); response.Code != http.StatusOK {
		t.Fatalf("reader catalog status=%d", response.Code)
	}
	if response := requestServer(s, http.MethodGet, "/admin", nil, readerCookie); response.Code != http.StatusForbidden {
		t.Fatalf("reader admin status=%d", response.Code)
	}

	adminToken, err := s.auth.NewSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: sessionCookieName, Value: adminToken}
	guide := requestServer(s, http.MethodGet, "/implementation.md", nil, adminCookie)
	if guide.Code != http.StatusOK ||
		!strings.Contains(guide.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(guide.Body.String(), `class="markdown-body"`) ||
		!strings.Contains(guide.Body.String(), "<table>") ||
		!strings.Contains(guide.Body.String(), "BookBrowser authentication") {
		t.Fatalf("admin guide status=%d body=%q", guide.Code, guide.Body.String())
	}

	settings, err := s.auth.Settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.AnonymousBookLinks = false
	if err := s.auth.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if response := requestServer(s, http.MethodGet, "/books/missing", nil); response.Code != http.StatusSeeOther {
		t.Fatalf("disabled anonymous-book status=%d", response.Code)
	}
}

func TestManifestUsesConfiguredPWAName(t *testing.T) {
	s := newAuthTestServer(t)
	settings, err := s.auth.Settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.PWAName = "Pocket MicsBook"
	if err := s.auth.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	response := requestServer(s, http.MethodGet, "/manifest.webmanifest", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "application/manifest+json") {
		t.Fatalf("manifest content type=%q", contentType)
	}
	var manifest webManifest
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != settings.PWAName || manifest.ShortName != settings.PWAName || manifest.StartURL != "/books" {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestReaderContextAboutAndReadingItemLifecycle(t *testing.T) {
	s := newAuthTestServer(t)
	s.SetBuildInfo("abc1234", "2026-08-09T16:20:30Z")
	pdf, err := os.ReadFile(filepath.Join("..", "formats", "pdf", "pdf_test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.BookDir, "notes.pdf"), pdf, 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	book := s.Indexer.BookList()[0]

	about := requestServer(s, http.MethodGet, "/api/about", nil)
	if about.Code != http.StatusOK || !strings.Contains(about.Body.String(), `"build_id":"abc1234"`) ||
		!strings.Contains(about.Body.String(), `"build_time":"2026-08-09T16:20:30Z"`) ||
		!strings.Contains(about.Body.String(), `"build_number":"abc1234-20260809T162030Z"`) {
		t.Fatalf("about response status=%d body=%s", about.Code, about.Body.String())
	}

	anonymous := requestServer(s, http.MethodGet, "/api/reader/context?book_id="+book.ID(), nil)
	if anonymous.Code != http.StatusOK || strings.Contains(anonymous.Body.String(), "csrf_token") || strings.Contains(anonymous.Body.String(), `"authenticated":true`) {
		t.Fatalf("anonymous reader context leaked writable state: %s", anonymous.Body.String())
	}

	user, err := s.auth.RegisterEmail("notes@example.com", "Notes", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.NewSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: sessionCookieName, Value: token}
	context := requestServer(s, http.MethodGet, "/api/reader/context?book_id="+book.ID(), nil, session)
	csrf := cookieNamed(context, csrfCookieName)
	if context.Code != http.StatusOK || csrf == nil || !strings.Contains(context.Body.String(), `"authenticated":true`) {
		t.Fatalf("signed-in context status=%d body=%s", context.Code, context.Body.String())
	}

	created := requestServer(s, http.MethodPost, "/api/reader/items", url.Values{
		"csrf_token": {csrf.Value}, "book_id": {book.ID()}, "kind": {"note"}, "locator": {"page:2"},
		"locator_label": {"Page 2"}, "title": {"Key idea"}, "body": {"Review this section."},
		"excerpt": {"Selected sentence"}, "tags": {"research, revisit"},
	}, session, csrf)
	if created.Code != http.StatusCreated {
		t.Fatalf("create note status=%d body=%s", created.Code, created.Body.String())
	}
	var item auth.ReadingItem
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID == "" || len(item.Tags) != 2 {
		t.Fatalf("created item=%#v", item)
	}

	updated := requestServer(s, http.MethodPost, "/api/reader/items/"+item.ID, url.Values{
		"csrf_token": {csrf.Value}, "title": {"Updated idea"}, "body": {"Edited note."}, "tags": {"edited"},
	}, session, csrf)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "Updated idea") {
		t.Fatalf("update note status=%d body=%s", updated.Code, updated.Body.String())
	}
	management := requestServer(s, http.MethodGet, "/my-library/reading", nil, session, csrf)
	if management.Code != http.StatusOK || !strings.Contains(management.Body.String(), "Updated idea") || !strings.Contains(management.Body.String(), "Open location") {
		t.Fatalf("management status=%d body=%s", management.Code, management.Body.String())
	}

	withoutCSRF := requestServer(s, http.MethodPost, "/api/reader/items/"+item.ID+"/delete", url.Values{}, session)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("delete without CSRF status=%d", withoutCSRF.Code)
	}
	deleted := requestServer(s, http.MethodPost, "/api/reader/items/"+item.ID+"/delete", url.Values{"csrf_token": {csrf.Value}}, session, csrf)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestManagerAndAdminBoundaries(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := s.auth.RegisterEmail("manager@example.com", "Manager", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.auth.UpdateUser(manager.ID, auth.RoleManager, true); err != nil {
		t.Fatal(err)
	}
	managerToken, err := s.auth.NewSession(manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	managerCookie := &http.Cookie{Name: sessionCookieName, Value: managerToken}
	if response := requestServer(s, http.MethodGet, "/admin/library", nil, managerCookie); response.Code != http.StatusOK {
		t.Fatalf("manager library status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestServer(s, http.MethodGet, "/admin/users", nil, managerCookie); response.Code != http.StatusForbidden {
		t.Fatalf("manager users status=%d", response.Code)
	}

	adminToken, err := s.auth.NewSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := &http.Cookie{Name: sessionCookieName, Value: adminToken}
	response := requestServer(s, http.MethodPost, "/admin/users/"+manager.ID, url.Values{
		"role":   {"admin"},
		"active": {"on"},
	}, adminCookie)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing-CSRF user update status=%d", response.Code)
	}
}

func TestPrivateLibraryRoutesAndOwnership(t *testing.T) {
	s := newAuthTestServer(t)
	pdf, err := os.ReadFile(filepath.Join("..", "formats", "pdf", "pdf_test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.BookDir, "reading.pdf"), pdf, 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	if len(s.Indexer.BookList()) != 1 {
		t.Fatalf("indexed books=%d", len(s.Indexer.BookList()))
	}
	book := s.Indexer.BookList()[0]

	owner, err := s.auth.RegisterEmail("owner@example.com", "Owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.auth.RegisterEmail("other@example.com", "Other", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ownerToken, err := s.auth.NewSession(owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherToken, err := s.auth.NewSession(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	ownerCookie := &http.Cookie{Name: sessionCookieName, Value: ownerToken}
	otherCookie := &http.Cookie{Name: sessionCookieName, Value: otherToken}

	page := requestServer(s, http.MethodGet, "/my-library", nil, ownerCookie)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Recently read") {
		t.Fatalf("my library status=%d body=%s", page.Code, page.Body.String())
	}
	csrf := cookieNamed(page, csrfCookieName)
	if csrf == nil {
		t.Fatal("my library did not set CSRF cookie")
	}
	created := requestServer(s, http.MethodPost, "/my-library/lists", url.Values{
		"csrf_token": {csrf.Value},
		"name":       {"Weekend Favorites"},
	}, ownerCookie, csrf)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create list status=%d body=%s", created.Code, created.Body.String())
	}
	lists, err := s.auth.BookListsForUser(owner.ID)
	if err != nil || len(lists) != 1 {
		t.Fatalf("lists=%v err=%v", lists, err)
	}
	list := lists[0]

	missingCSRF := requestServer(s, http.MethodPost, "/books/"+book.ID()+"/tags", url.Values{
		"tag": {"History"},
	}, ownerCookie)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("tag without CSRF status=%d", missingCSRF.Code)
	}
	added := requestServer(s, http.MethodPost, "/my-library/lists/"+list.ID+"/books/"+book.ID(), url.Values{
		"csrf_token": {csrf.Value},
		"next":       {"/books/" + book.ID()},
	}, ownerCookie, csrf)
	if added.Code != http.StatusSeeOther {
		t.Fatalf("add book status=%d body=%s", added.Code, added.Body.String())
	}
	tagged := requestServer(s, http.MethodPost, "/books/"+book.ID()+"/tags", url.Values{
		"csrf_token": {csrf.Value},
		"tag":        {"History"},
	}, ownerCookie, csrf)
	if tagged.Code != http.StatusSeeOther {
		t.Fatalf("tag book status=%d body=%s", tagged.Code, tagged.Body.String())
	}
	read := requestServer(s, http.MethodGet, "/read/"+book.ID(), nil, ownerCookie)
	if read.Code != http.StatusSeeOther || !strings.Contains(read.Header().Get("Location"), "viewer.html") {
		t.Fatalf("read status=%d location=%q", read.Code, read.Header().Get("Location"))
	}

	updated := requestServer(s, http.MethodGet, "/my-library", nil, ownerCookie, csrf)
	for _, expected := range []string{"Weekend Favorites", "History", book.Title} {
		if !strings.Contains(updated.Body.String(), expected) {
			t.Fatalf("updated private library missing %q", expected)
		}
	}
	bookPage := requestServer(s, http.MethodGet, "/books/"+book.ID(), nil, ownerCookie, csrf)
	for _, expected := range []string{"Lists &amp; tags", "Weekend Favorites", "History", "/read/" + book.ID()} {
		if !strings.Contains(bookPage.Body.String(), expected) {
			t.Fatalf("book page missing %q", expected)
		}
	}
	privateList := requestServer(s, http.MethodGet, "/my-library/lists/"+list.ID, nil, otherCookie)
	if privateList.Code != http.StatusNotFound {
		t.Fatalf("other user private list status=%d", privateList.Code)
	}
	otherLibrary := requestServer(s, http.MethodGet, "/my-library", nil, otherCookie)
	if strings.Contains(otherLibrary.Body.String(), "Weekend Favorites") || strings.Contains(otherLibrary.Body.String(), "History") {
		t.Fatal("another user's private metadata leaked")
	}
}

func TestSafeNextAndUploadFilename(t *testing.T) {
	if safeNextValue("https://example.com") != "" || safeNextValue("//example.com") != "" {
		t.Fatal("external redirect was accepted")
	}
	if safeNextValue("/books?sort=title") != "/books?sort=title" {
		t.Fatal("safe local redirect was rejected")
	}
	if got := safeUploadName("../../A strange:book.epub"); got != "A strange_book.epub" {
		t.Fatalf("safe upload name = %q", got)
	}
	if !supportedBookExtension(".epub") || supportedBookExtension(".exe") {
		t.Fatal("book extension validation failed")
	}
}

func TestManagerCanUploadAndRecoverablyRemoveBook(t *testing.T) {
	s := newAuthTestServer(t)
	_, err := s.auth.RegisterEmail("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := s.auth.RegisterEmail("library@example.com", "Library Manager", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.auth.UpdateUser(manager.ID, auth.RoleManager, true); err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.NewSession(manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: sessionCookieName, Value: token}
	page := requestServer(s, http.MethodGet, "/admin/library", nil, session)
	csrf := cookieNamed(page, csrfCookieName)
	if csrf == nil {
		t.Fatal("library page did not set CSRF cookie")
	}
	pdf, err := os.ReadFile("../formats/pdf/pdf_test.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", csrf.Value); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("book", "managed.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pdf); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/library/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.BookDir, "managed.pdf")); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	var uploadedID string
	for _, book := range s.Indexer.BookList() {
		if filepath.Base(book.FilePath) == "managed.pdf" {
			uploadedID = book.ID()
			break
		}
	}
	if uploadedID == "" {
		t.Fatal("uploaded PDF was not indexed")
	}
	removed := requestServer(s, http.MethodPost, "/admin/library/delete/"+uploadedID, url.Values{
		"csrf_token": {csrf.Value},
	}, session, csrf)
	if removed.Code != http.StatusSeeOther {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.BookDir, "managed.pdf")); !os.IsNotExist(err) {
		t.Fatalf("removed book remains in library: %v", err)
	}
	trash, err := filepath.Glob(filepath.Join(s.BookDir, ".bookbrowser", "trash", uploadedID+"-*.deleted"))
	if err != nil || len(trash) != 1 {
		t.Fatalf("recoverable book copies=%v err=%v", trash, err)
	}
	// Wait for the handler's asynchronous re-index before temporary directories close.
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
}
