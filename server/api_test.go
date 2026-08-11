package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requestAPI(t *testing.T, s *Server, method, target, token string, input interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader = strings.NewReader("")
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, target, body)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	return response
}

func decodeAPIResponse(t *testing.T, response *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response status=%d body=%q: %v", response.Code, response.Body.String(), err)
	}
}

func TestAPIPasswordLoginTokenRolesAndSettings(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("api-admin@example.com", "API Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.auth.RegisterEmail("api-reader@example.com", "API Reader", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}

	login := requestAPI(t, s, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": reader.Email, "password": "correct horse battery staple", "token_name": "test-cli",
	})
	if login.Code != http.StatusCreated {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var loggedIn struct {
		Token string  `json:"token"`
		User  apiUser `json:"user"`
	}
	decodeAPIResponse(t, login, &loggedIn)
	if !strings.HasPrefix(loggedIn.Token, "bbk_") || loggedIn.User.ID != reader.ID {
		t.Fatalf("login response=%#v", loggedIn)
	}
	me := requestAPI(t, s, http.MethodGet, "/api/v1/me", loggedIn.Token, nil)
	if me.Code != http.StatusOK || strings.Contains(me.Body.String(), "password_hash") || strings.Contains(me.Body.String(), "google_subject") {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
	forbidden := requestAPI(t, s, http.MethodGet, "/api/v1/library/status", loggedIn.Token, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("reader library status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	adminToken, _, err := s.auth.CreateAPIToken(admin.ID, "admin-cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := requestAPI(t, s, http.MethodGet, "/api/v1/settings", adminToken, nil)
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"site_name"`) || strings.Contains(settings.Body.String(), `"SiteName"`) {
		t.Fatalf("settings status=%d body=%s", settings.Code, settings.Body.String())
	}
	updated := requestAPI(t, s, http.MethodPatch, "/api/v1/settings", adminToken, map[string]interface{}{
		"site_name": "CLI Library", "registration_open": false,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("settings update status=%d body=%s", updated.Code, updated.Body.String())
	}
	stored, err := s.auth.Settings()
	if err != nil || stored.SiteName != "CLI Library" || stored.RegistrationOpen || stored.PWAName == "" {
		t.Fatalf("stored settings=%#v err=%v", stored, err)
	}
}

func TestAPICatalogUploadDownloadAndRecoverableRemoval(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("library-api@example.com", "Library API", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.auth.CreateAPIToken(admin.ID, "library-cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := os.ReadFile("../formats/pdf/pdf_test.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("book", "api-managed.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pdf); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/library/books", &uploadBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	uploaded := httptest.NewRecorder()
	s.router.ServeHTTP(uploaded, request)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploaded.Code, uploaded.Body.String())
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
	books := requestAPI(t, s, http.MethodGet, "/api/v1/books?q=api-managed&limit=10", token, nil)
	if books.Code != http.StatusOK || strings.Contains(books.Body.String(), s.BookDir) || strings.Contains(books.Body.String(), "FilePath") {
		t.Fatalf("books status=%d body=%s", books.Code, books.Body.String())
	}
	var catalog struct {
		Items []apiBook `json:"items"`
		Total int       `json:"total"`
	}
	decodeAPIResponse(t, books, &catalog)
	if catalog.Total != 1 || len(catalog.Items) != 1 {
		t.Fatalf("catalog=%#v", catalog)
	}
	id := catalog.Items[0].ID
	download := requestAPI(t, s, http.MethodGet, "/api/v1/books/"+id+"/download", token, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), pdf) || !strings.Contains(download.Header().Get("Content-Disposition"), ".pdf") {
		t.Fatalf("download status=%d bytes=%d disposition=%q", download.Code, download.Body.Len(), download.Header().Get("Content-Disposition"))
	}
	removed := requestAPI(t, s, http.MethodDelete, "/api/v1/library/books/"+id, token, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.BookDir, "api-managed.pdf")); !os.IsNotExist(err) {
		t.Fatalf("removed book remains: %v", err)
	}
	trash, err := filepath.Glob(filepath.Join(s.BookDir, ".bookbrowser", "trash", id+"-*.deleted"))
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash=%v err=%v", trash, err)
	}
	if err := s.RefreshBookIndex(); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleCLIChallengeKeepsPollingSecretOutOfBrowserAndConsumesOnce(t *testing.T) {
	t.Setenv("BOOKBROWSER_GOOGLE_CLIENT_ID", "browser-client.apps.googleusercontent.com")
	s := newAuthTestServer(t)
	user, err := s.auth.RegisterEmail("google-cli@example.com", "Google CLI", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, time.August, 11, 14, 0, 0, 0, time.UTC)
	s.googleCLI.now = func() time.Time { return clock }
	started := requestAPI(t, s, http.MethodPost, "/api/v1/auth/google/start", "", map[string]string{
		"token_name": "google-cli", "client_name": "test-laptop",
	})
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	var challenge struct {
		ID     string `json:"challenge_id"`
		Secret string `json:"poll_secret"`
		URL    string `json:"verification_url"`
	}
	decodeAPIResponse(t, started, &challenge)
	page := requestServer(s, http.MethodGet, strings.TrimPrefix(challenge.URL, "http://example.com"), nil)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), challenge.Secret) || !strings.Contains(page.Body.String(), "test-laptop") {
		t.Fatalf("verification page status=%d body=%s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), `client_id:"browser-client.apps.googleusercontent.com"`) {
		t.Fatalf("verification page did not render the Google client ID as a JavaScript string: %s", page.Body.String())
	}
	if !strings.Contains(page.Body.String(), `challenge_id:"`+challenge.ID+`"`) {
		t.Fatalf("verification page did not render the challenge ID as a JavaScript string: %s", page.Body.String())
	}
	if strings.Contains(page.Body.String(), `client_id:"\"`) || strings.Contains(page.Body.String(), `challenge_id:"\"`) {
		t.Fatalf("verification page double-quoted a JavaScript value: %s", page.Body.String())
	}
	pending := requestAPI(t, s, http.MethodPost, "/api/v1/auth/google/poll", "", map[string]string{
		"challenge_id": challenge.ID, "poll_secret": challenge.Secret,
	})
	if pending.Code != http.StatusAccepted {
		t.Fatalf("pending status=%d body=%s", pending.Code, pending.Body.String())
	}
	if err := s.googleCLI.approve(challenge.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(googleCLIPollInterval)
	approved := requestAPI(t, s, http.MethodPost, "/api/v1/auth/google/poll", "", map[string]string{
		"challenge_id": challenge.ID, "poll_secret": challenge.Secret,
	})
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"token":"bbk_`) {
		t.Fatalf("approved status=%d body=%s", approved.Code, approved.Body.String())
	}
	second := requestAPI(t, s, http.MethodPost, "/api/v1/auth/google/poll", "", map[string]string{
		"challenge_id": challenge.ID, "poll_secret": challenge.Secret,
	})
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second poll status=%d body=%s", second.Code, second.Body.String())
	}
}
