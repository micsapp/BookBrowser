package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestProfilePageShowsInfoAndSavesOptionalFields(t *testing.T) {
	s := newAuthTestServer(t)
	reader, csrf, session := registerTestUser(t, s, "profile2@example.com", "Profile Two")

	page := requestServer(s, http.MethodGet, "/profile", nil, session)
	if page.Code != http.StatusOK {
		t.Fatalf("profile GET status=%d", page.Code)
	}
	body := page.Body.String()
	for _, expected := range []string{
		"profile2@example.com", "Profile", "profile_display_name",
		`name="display_name"`, `name="bio"`, `name="location"`, "Save profile",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("profile page missing %q", expected)
		}
	}

	form := url.Values{
		"csrf_token":   {csrf.Value},
		"display_name": {"Bookworm"},
		"bio":          {"Reads everything."},
		"location":     {"Shanghai"},
	}
	saved := requestServer(s, http.MethodPost, "/profile", form, csrf, session)
	if saved.Code != http.StatusSeeOther || saved.Header().Get("Location") != "/profile?saved=1" {
		t.Fatalf("profile POST status=%d location=%q", saved.Code, saved.Header().Get("Location"))
	}
	updated, err := s.auth.UserByID(reader.ID)
	if err != nil || updated == nil {
		t.Fatalf("reload user: %v", err)
	}
	if updated.DisplayName != "Bookworm" || updated.Bio != "Reads everything." || updated.Location != "Shanghai" {
		t.Fatalf("profile fields = %#v", updated)
	}

	reloaded := requestServer(s, http.MethodGet, "/profile", nil, session)
	if !strings.Contains(reloaded.Body.String(), "Bookworm") || !strings.Contains(reloaded.Body.String(), "Shanghai") {
		t.Fatalf("profile page does not show saved fields: %s", reloaded.Body.String())
	}

	anonymous := requestServer(s, http.MethodGet, "/profile", nil)
	if anonymous.Code != http.StatusSeeOther || !strings.HasPrefix(anonymous.Header().Get("Location"), "/login") {
		t.Fatalf("anonymous profile status=%d location=%q", anonymous.Code, anonymous.Header().Get("Location"))
	}
}

func TestNavbarSiteI18NAndLanguageSwitch(t *testing.T) {
	s := newAuthTestServer(t)
	page := requestServer(s, http.MethodGet, "/login", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("login GET status=%d", page.Code)
	}
	body := page.Body.String()
	for _, expected := range []string{
		"data-lang-switch",
		"data-i18n=\"nav_home\"",
		"data-i18n=\"nav_signin\"",
		"site-i18n.js",
		`meta name="csrf-token"`,
		"data-i18n=\"nav_about\"",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("navbar missing %q", expected)
		}
	}

	// The switch persists the choice for signed-in readers.
	reader, csrf, session := registerTestUser(t, s, "i18n@example.com", "I18n")
	books := requestServer(s, http.MethodGet, "/books", nil, session)
	if !strings.Contains(books.Body.String(), "data-i18n=\"nav_request\"") ||
		!strings.Contains(books.Body.String(), `href="/profile"`) {
		t.Fatalf("logged-in navbar missing i18n or profile link")
	}
	form := url.Values{"csrf_token": {csrf.Value}, "language": {"zh"}}
	saved := requestServer(s, http.MethodPost, "/api/reader/language", form, csrf, session)
	if saved.Code != http.StatusOK {
		t.Fatalf("language save status=%d body=%s", saved.Code, saved.Body.String())
	}
	if language, err := s.auth.LanguageForUser(reader.ID); err != nil || language != "zh" {
		t.Fatalf("stored language = %q, %v", language, err)
	}
}
