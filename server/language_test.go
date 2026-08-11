package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReaderLanguagePreferenceSyncsWithAccount(t *testing.T) {
	s := newAuthTestServer(t)
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

	user, err := s.auth.RegisterEmail("lang@example.com", "Lang Learner", "correct horse battery staple")
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

	bad := requestServer(s, http.MethodPost, "/api/reader/language", url.Values{
		"language":   {"fr"},
		"csrf_token": {csrf.Value},
	}, session, csrf)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unsupported language status=%d body=%s", bad.Code, bad.Body.String())
	}

	anon := requestServer(s, http.MethodPost, "/api/reader/language", url.Values{
		"language":   {"zh"},
		"csrf_token": {csrf.Value},
	}, csrf)
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous language status=%d body=%s", anon.Code, anon.Body.String())
	}

	set := requestServer(s, http.MethodPost, "/api/reader/language", url.Values{
		"language":   {"zh"},
		"csrf_token": {csrf.Value},
	}, session, csrf)
	if set.Code != http.StatusOK || !strings.Contains(set.Body.String(), `"language":"zh"`) {
		t.Fatalf("set language status=%d body=%s", set.Code, set.Body.String())
	}

	contextZh := requestServer(s, http.MethodGet, "/api/reader/context?book_id="+book.ID(), nil, session)
	if contextZh.Code != http.StatusOK || !strings.Contains(contextZh.Body.String(), `"language":"zh"`) {
		t.Fatalf("context does not carry language status=%d body=%s", contextZh.Code, contextZh.Body.String())
	}

	setEn := requestServer(s, http.MethodPost, "/api/reader/language", url.Values{
		"language":   {"en"},
		"csrf_token": {csrf.Value},
	}, session, csrf)
	if setEn.Code != http.StatusOK {
		t.Fatalf("set en status=%d body=%s", setEn.Code, setEn.Body.String())
	}
	contextEn := requestServer(s, http.MethodGet, "/api/reader/context?book_id="+book.ID(), nil, session)
	if contextEn.Code != http.StatusOK || !strings.Contains(contextEn.Body.String(), `"language":"en"`) {
		t.Fatalf("context must carry en language status=%d body=%s", contextEn.Code, contextEn.Body.String())
	}
}
