package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geek1011/BookBrowser/auth"
)

func registerTestUser(t *testing.T, s *Server, email, name string) (*auth.User, *http.Cookie, *http.Cookie) {
	t.Helper()
	page := requestServer(s, http.MethodGet, "/register", nil)
	csrf := cookieNamed(page, csrfCookieName)
	if csrf == nil {
		t.Fatal("registration page did not set CSRF cookie")
	}
	form := url.Values{
		"csrf_token":       {csrf.Value},
		"name":             {name},
		"email":            {email},
		"password":         {"correct horse battery staple"},
		"password_confirm": {"correct horse battery staple"},
	}
	created := requestServer(s, http.MethodPost, "/register", form, csrf)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("registration status=%d body=%s", created.Code, created.Body.String())
	}
	user, err := s.auth.UserByEmail(email)
	if err != nil || user == nil {
		t.Fatalf("registered user: %v", err)
	}
	return user, csrf, cookieNamed(created, sessionCookieName)
}

func writeTestEpub(t *testing.T, dir, filename, title string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mimetype, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mimetype.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	container, err := zw.Create("META-INF/container.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Write([]byte(`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)); err != nil {
		t.Fatal(err)
	}
	opf, err := zw.Create("OEBPS/content.opf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opf.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="uid"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>%s</dc:title><dc:creator>Test Author</dc:creator><dc:identifier id="uid">test-id</dc:identifier><dc:language>en</dc:language></metadata><manifest><item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="ch1"/></spine></package>`, title))); err != nil {
		t.Fatal(err)
	}
	chapter, err := zw.Create("OEBPS/ch1.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chapter.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title></head><body><p>Chapter one.</p></body></html>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBookRequestSubmitListAndFetchResponse(t *testing.T) {
	s := newAuthTestServer(t)
	reader, csrf, session := registerTestUser(t, s, "reader@example.com", "Reader")

	form := url.Values{
		"csrf_token": {csrf.Value},
		"title":      {"The Martian"},
		"author":     {"Andy Weir"},
		"notes":      {"English edition"},
	}
	created := requestServer(s, http.MethodPost, "/requests", form, csrf, session)
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/requests?saved=1" {
		t.Fatalf("create request status=%d location=%q body=%s", created.Code, created.Header().Get("Location"), created.Body.String())
	}

	list := requestServer(s, http.MethodGet, "/requests", nil, session)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "The Martian") {
		t.Fatalf("requests page status=%d body=%s", list.Code, list.Body.String())
	}

	fetchForm := url.Values{
		"csrf_token": {csrf.Value},
		"title":      {"Dune"},
		"author":     {""},
		"notes":      {""},
	}
	request := httptest.NewRequest(http.MethodPost, "/requests", strings.NewReader(fetchForm.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Requested-With", "fetch")
	request.AddCookie(csrf)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("fetch request status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || !payload.OK || payload.ID == "" {
		t.Fatalf("fetch payload=%s err=%v", response.Body.String(), err)
	}

	bad := url.Values{"csrf_token": {csrf.Value}, "title": {"   "}}
	badRequest := httptest.NewRequest(http.MethodPost, "/requests", strings.NewReader(bad.Encode()))
	badRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRequest.Header.Set("X-Requested-With", "fetch")
	badRequest.AddCookie(csrf)
	badRequest.AddCookie(session)
	badResponse := httptest.NewRecorder()
	s.router.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest || !strings.Contains(badResponse.Body.String(), "title") {
		t.Fatalf("bad request status=%d body=%s", badResponse.Code, badResponse.Body.String())
	}

	anonymous := requestServer(s, http.MethodPost, "/requests", form, csrf)
	if anonymous.Code != http.StatusSeeOther || !strings.HasPrefix(anonymous.Header().Get("Location"), "/login") {
		t.Fatalf("anonymous request status=%d location=%q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	requests, err := s.auth.BookRequestsForUser(reader.ID)
	if err != nil || len(requests) != 2 {
		t.Fatalf("stored requests=%d err=%v", len(requests), err)
	}
}

func TestAdminBookRequestResolution(t *testing.T) {
	s := newAuthTestServer(t)
	admin, adminCSRF, adminSession := registerTestUser(t, s, "admin@example.com", "Admin")
	reader, readerCSRF, readerSession := registerTestUser(t, s, "reader2@example.com", "Reader Two")
	manager, _, managerSession := registerTestUser(t, s, "manager@example.com", "Manager")
	if _, err := s.auth.UpdateUser(manager.ID, auth.RoleManager, true); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"csrf_token": {readerCSRF.Value}, "title": {"Dune"}, "author": {"Frank Herbert"}}
	if created := requestServer(s, http.MethodPost, "/requests", form, readerCSRF, readerSession); created.Code != http.StatusSeeOther {
		t.Fatalf("create request status=%d", created.Code)
	}
	requests, err := s.auth.BookRequestsForUser(reader.ID)
	if err != nil || len(requests) != 1 {
		t.Fatalf("stored requests=%d err=%v", len(requests), err)
	}
	requestID := requests[0].ID

	forbidden := requestServer(s, http.MethodGet, "/admin/requests", nil, readerSession)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("reader admin requests status=%d", forbidden.Code)
	}

	managerPage := requestServer(s, http.MethodGet, "/admin/requests", nil, managerSession)
	if managerPage.Code != http.StatusOK || !strings.Contains(managerPage.Body.String(), "Dune") {
		t.Fatalf("manager requests page status=%d body=%s", managerPage.Code, managerPage.Body.String())
	}
	managerCSRF := cookieNamed(managerPage, csrfCookieName)
	if managerCSRF == nil {
		t.Fatal("manager requests page did not set CSRF cookie")
	}

	badBook := url.Values{"csrf_token": {managerCSRF.Value}, "action": {"added"}, "book_id": {"nope"}}
	if resolved := requestServer(s, http.MethodPost, "/admin/requests/"+requestID, badBook, managerCSRF, managerSession); resolved.Code != http.StatusSeeOther {
		t.Fatalf("resolve with missing book status=%d", resolved.Code)
	}
	if stored, _ := s.auth.BookRequestsForUser(reader.ID); stored[0].Status != auth.BookRequestPending {
		t.Fatal("resolve with unknown book should not change the request")
	}

	writeTestEpub(t, s.BookDir, "dune.epub", "Dune")
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatalf("refresh index: %v", err)
	}
	if len(s.Indexer.BookList()) != 1 {
		t.Fatalf("indexed books=%d", len(s.Indexer.BookList()))
	}
	bookID := s.Indexer.BookList()[0].ID()

	added := url.Values{"csrf_token": {managerCSRF.Value}, "action": {"added"}, "book_id": {bookID}, "message": {"Enjoy!"}}
	if resolved := requestServer(s, http.MethodPost, "/admin/requests/"+requestID, added, managerCSRF, managerSession); resolved.Code != http.StatusSeeOther {
		t.Fatalf("resolve added status=%d body=%s", resolved.Code, resolved.Body.String())
	}
	stored, _ := s.auth.BookRequestsForUser(reader.ID)
	if stored[0].Status != auth.BookRequestAdded || stored[0].BookID != bookID {
		t.Fatalf("added resolution not stored: %#v", stored[0])
	}

	second := url.Values{"csrf_token": {readerCSRF.Value}, "title": {"Rare Book"}, "author": {""}}
	requestServer(s, http.MethodPost, "/requests", second, readerCSRF, readerSession)
	stored, _ = s.auth.BookRequestsForUser(reader.ID)
	var rareID string
	for _, request := range stored {
		if request.Title == "Rare Book" {
			rareID = request.ID
		}
	}
	if rareID == "" {
		t.Fatal("second request was not stored")
	}
	unavailable := url.Values{"csrf_token": {managerCSRF.Value}, "action": {"unavailable"}, "message": {"Out of print everywhere."}}
	if resolved := requestServer(s, http.MethodPost, "/admin/requests/"+rareID, unavailable, managerCSRF, managerSession); resolved.Code != http.StatusSeeOther {
		t.Fatalf("resolve unavailable status=%d body=%s", resolved.Code, resolved.Body.String())
	}

	readerPage := requestServer(s, http.MethodGet, "/requests", nil, readerSession)
	body := readerPage.Body.String()
	if readerPage.Code != http.StatusOK ||
		!strings.Contains(body, "Out of print everywhere.") ||
		!strings.Contains(body, "/books/"+bookID) {
		t.Fatalf("reader requests page status=%d body=%s", readerPage.Code, body)
	}

	adminPage := requestServer(s, http.MethodGet, "/admin", nil, adminSession)
	if adminPage.Code != http.StatusOK || !strings.Contains(adminPage.Body.String(), "Book requests") {
		t.Fatalf("admin dashboard status=%d body=%s", adminPage.Code, adminPage.Body.String())
	}
	_ = admin
	_ = adminCSRF
}
