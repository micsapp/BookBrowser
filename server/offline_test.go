package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOfflineContextUsesSessionWithoutCachingPrivatePages(t *testing.T) {
	s := newAuthTestServer(t)
	anonymous := requestServer(s, http.MethodGet, "/api/offline/context", nil)
	if anonymous.Code != http.StatusUnauthorized || anonymous.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("anonymous context: status=%d headers=%v", anonymous.Code, anonymous.Header())
	}
	user, err := s.auth.RegisterEmail("offline@example.com", "Offline Reader", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.NewSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := requestServer(s, http.MethodGet, "/api/offline/context", nil, &http.Cookie{Name: sessionCookieName, Value: token})
	var context map[string]string
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &context) != nil || context["user_id"] != user.ID {
		t.Fatalf("session context: status=%d body=%s", response.Code, response.Body.String())
	}
	if len(context) != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("context must contain only the uncached account identifier: %#v", context)
	}
	// A static, account-independent shell can open without an online session.
	library := requestServer(s, http.MethodGet, "/offline", nil)
	if library.Code != http.StatusOK {
		t.Fatalf("offline shell status=%d", library.Code)
	}
	manifest := requestServer(s, http.MethodGet, "/static/offline-assets.js", nil)
	for _, path := range []string{"/static/reader/pdf/build/pdf.js", "/static/reader/pdf/build/pdf.worker.js"} {
		if !strings.Contains(manifest.Body.String(), path) {
			t.Fatalf("PDF engine dependency missing from offline assets: %s", path)
		}
		asset := requestServer(s, http.MethodGet, path, nil)
		if asset.Code != http.StatusOK {
			t.Fatalf("PDF dependency %s status=%d", path, asset.Code)
		}
	}
}
