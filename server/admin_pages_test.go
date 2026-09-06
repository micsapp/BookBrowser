package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// newAdminSession registers the first account in a fresh test server (which
// becomes the administrator) and returns its web session cookie.
func newAdminSession(t *testing.T, s *Server, email string) *http.Cookie {
	t.Helper()
	user, err := s.auth.RegisterEmail(email, "Admin User", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.NewSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}
}

func TestAdminLibraryFiltersAndNewestFirstDefault(t *testing.T) {
	s := newAuthTestServer(t)
	pdf, err := os.ReadFile(filepath.Join("..", "formats", "pdf", "pdf_test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(s.BookDir, "older.pdf")
	newer := filepath.Join(s.BookDir, "newer.pdf")
	if err := os.WriteFile(older, pdf, 0644); err != nil {
		t.Fatal(err)
	}
	// Append a byte so the two copies hash differently and are both indexed.
	withSuffix := append(append([]byte{}, pdf...), '\n')
	if err := os.WriteFile(newer, withSuffix, 0644); err != nil {
		t.Fatal(err)
	}
	olderTime := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	newerTime := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(older, olderTime, olderTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, newerTime, newerTime); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	if count := len(s.Indexer.BookList()); count != 2 {
		t.Fatalf("expected 2 indexed books, got %d", count)
	}

	session := newAdminSession(t, s, "admin@example.com")
	page := requestServer(s, http.MethodGet, "/admin/library", nil, session)
	if page.Code != http.StatusOK {
		t.Fatalf("library page status=%d body=%s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, marker := range []string{
		`id="library-filter"`, `id="library-format"`, `id="library-sort"`,
		`<option value="added-desc" selected`, `data-added="`, `data-title="`,
		`>Added<`, `id="library-empty"`, `class="admin-filters"`, `id="library-count"`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("library page is missing %q", marker)
		}
	}

	added := regexp.MustCompile(`data-added="(\d+)"`).FindAllStringSubmatch(body, -1)
	if len(added) != 2 {
		t.Fatalf("expected 2 book rows with data-added, got %d", len(added))
	}
	if got, want := added[0][1], fmt.Sprintf("%d", newerTime.Unix()); got != want {
		t.Errorf("first book row added=%s, want newest %s", got, want)
	}
	if got, want := added[1][1], fmt.Sprintf("%d", olderTime.Unix()); got != want {
		t.Errorf("second book row added=%s, want older %s", got, want)
	}
}

func TestAdminUsersShowsLatestActivityFeed(t *testing.T) {
	s := newAuthTestServer(t)
	session := newAdminSession(t, s, "admin@example.com")
	reader, err := s.auth.RegisterEmail("reader2@example.com", "Reader Two", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.auth.RecordBookRead(reader.ID, "sample-book-123"); err != nil {
		t.Fatal(err)
	}

	page := requestServer(s, http.MethodGet, "/admin/users", nil, session)
	if page.Code != http.StatusOK {
		t.Fatalf("users page status=%d body=%s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, marker := range []string{
		`>Latest activity<`, `class="user-feed"`, `user-feed-time`, "sample-book-123",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("users page is missing %q", marker)
		}
	}
	if strings.Contains(body, "Books read") {
		t.Error("users page still uses the removed Books read details list")
	}
}
