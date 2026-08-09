package server

import (
	"bytes"
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
	if guide.Code != http.StatusOK || !strings.Contains(guide.Body.String(), "BookBrowser authentication") {
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
