package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/geek1011/BookBrowser/auth"
)

func TestSystemMonitorPostsOneTTSAlertAndAdminsCanReadIt(t *testing.T) {
	s := newAuthTestServer(t)
	_, _, adminSession := registerTestUser(t, s, "admin-alerts@example.com", "Alerts Admin")
	_, _, readerSession := registerTestUser(t, s, "reader-alerts@example.com", "Alerts Reader")

	var mu sync.Mutex
	healthBody := `{"status":"ok","consecutive_failures":0,"last_success_at":1760700000}`
	tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(healthBody))
	}))
	setHealth := func(body string) {
		mu.Lock()
		healthBody = body
		mu.Unlock()
	}
	monitor := newTTSMonitor(tts.URL + "/health")

	s.checkTTSHealth(monitor)
	if alerts, err := s.auth.SystemAlerts(0); err != nil || len(alerts) != 0 {
		t.Fatalf("healthy TTS created alerts=%#v err=%v", alerts, err)
	}

	failing := `{"status":"failing","consecutive_failures":%d,"failing_since":1760356468,"last_success_at":1760100000,
		"last_error":"WSServerHandshakeError: 403, message='Invalid response status'","edge_tts_version":"7.2.8"}`
	for _, count := range []string{"4", "57", "1122"} {
		setHealth(strings.Replace(failing, "%d", count, 1))
		s.checkTTSHealth(monitor)
	}
	alerts, err := s.auth.SystemAlerts(0)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("persistent failure must post exactly one alert, got %d err=%v", len(alerts), err)
	}
	alert := alerts[0]
	if alert.Key != alertKeyTTSUpstream || alert.Status != auth.SystemAlertOpen ||
		!strings.Contains(alert.Details, "failed 1122 times") ||
		!strings.Contains(alert.Details, "edge-tts version: 7.2.8") ||
		!strings.Contains(alert.Details, "Upgrade edge-tts") {
		t.Fatalf("unexpected alert: %#v", alert)
	}

	if forbidden := requestServer(s, http.MethodGet, "/admin/system", nil, readerSession); forbidden.Code != http.StatusForbidden {
		t.Fatalf("reader system alerts status=%d", forbidden.Code)
	}
	if strings.Contains(requestServer(s, http.MethodGet, "/books", nil, readerSession).Body.String(), "nav-system-alerts") {
		t.Fatal("readers must not see the system alert navigation")
	}
	dashboard := requestServer(s, http.MethodGet, "/admin", nil, adminSession)
	if !strings.Contains(dashboard.Body.String(), `href="/admin/system"`) || !strings.Contains(dashboard.Body.String(), "admin-card-alert") {
		t.Fatalf("admin dashboard does not flag the open alert: %s", dashboard.Body.String())
	}
	page := requestServer(s, http.MethodGet, "/admin/system", nil, adminSession)
	body := page.Body.String()
	for _, expected := range []string{
		`class="nav-system-alerts"`, `unread system alert(s)">1<`,
		"Neural text-to-speech is failing", "system-alert-unread", `data-i18n-alert-status="open"`,
		`name="alert_id" value="` + alert.ID + `"`, `data-i18n="admin_system_read_all"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("system alerts page is missing %q", expected)
		}
	}
	adminCSRF := cookieNamed(page, csrfCookieName)
	if adminCSRF == nil {
		t.Fatal("system alerts page did not set a CSRF cookie")
	}

	form := url.Values{"csrf_token": {adminCSRF.Value}, "alert_id": {alert.ID}}
	if denied := requestServer(s, http.MethodPost, "/admin/system/read", url.Values{"alert_id": {alert.ID}}, adminSession); denied.Code != http.StatusForbidden {
		t.Fatalf("mark read without CSRF status=%d", denied.Code)
	}
	if read := requestServer(s, http.MethodPost, "/admin/system/read", form, adminSession, adminCSRF); read.Code != http.StatusSeeOther ||
		read.Header().Get("Location") != "/admin/system?saved=1" {
		t.Fatalf("mark read status=%d location=%q", read.Code, read.Header().Get("Location"))
	}
	body = requestServer(s, http.MethodGet, "/admin/system", nil, adminSession).Body.String()
	if strings.Contains(body, "nav-system-alerts") || !strings.Contains(body, "Read by</span> Alerts Admin") {
		t.Fatalf("read alert is still flagged or lacks its reader: %s", body)
	}

	// Still failing after being read: refreshed in place, not re-flagged.
	s.checkTTSHealth(monitor)
	if _, unread, err := s.auth.SystemAlertCounts(); err != nil || unread != 0 {
		t.Fatalf("ongoing read alert was re-flagged: unread=%d err=%v", unread, err)
	}

	setHealth(`{"status":"suspect","consecutive_failures":1}`)
	s.checkTTSHealth(monitor)
	if open, _, _ := s.auth.SystemAlertCounts(); open != 1 {
		t.Fatalf("an unconfirmed result must not close the alert, open=%d", open)
	}
	setHealth(`{"status":"ok","consecutive_failures":0,"last_success_at":1760710000}`)
	s.checkTTSHealth(monitor)
	alerts, _ = s.auth.SystemAlerts(0)
	if len(alerts) != 1 || alerts[0].Status != auth.SystemAlertResolved || !strings.Contains(alerts[0].Resolution, "succeeding again") {
		t.Fatalf("recovery did not resolve the alert: %#v", alerts)
	}

	// The service itself going away is reported only after repeated failures.
	tts.Close()
	for i := 1; i < ttsUnreachableThreshold; i++ {
		s.checkTTSHealth(monitor)
	}
	if open, _, _ := s.auth.SystemAlertCounts(); open != 0 {
		t.Fatalf("unreachable alert raised before the threshold, open=%d", open)
	}
	s.checkTTSHealth(monitor)
	s.checkTTSHealth(monitor)
	alerts, _ = s.auth.SystemAlerts(0)
	if len(alerts) != 2 || alerts[0].Key != alertKeyTTSService || alerts[0].Status != auth.SystemAlertOpen ||
		!strings.Contains(alerts[0].Details, "consecutive checks") {
		t.Fatalf("unreachable TTS service alert = %#v", alerts)
	}

	all := url.Values{"csrf_token": {adminCSRF.Value}, "all": {"1"}}
	if read := requestServer(s, http.MethodPost, "/admin/system/read", all, adminSession, adminCSRF); read.Code != http.StatusSeeOther {
		t.Fatalf("mark all read status=%d", read.Code)
	}
	if _, unread, _ := s.auth.SystemAlertCounts(); unread != 0 {
		t.Fatalf("mark all read left unread=%d", unread)
	}
}

func TestSystemMonitorToleratesTTSWithoutHealthEndpoint(t *testing.T) {
	s := newAuthTestServer(t)
	legacy := httptest.NewServer(http.NotFoundHandler())
	defer legacy.Close()
	monitor := newTTSMonitor(legacy.URL + "/health")
	for i := 0; i < ttsUnreachableThreshold+1; i++ {
		s.checkTTSHealth(monitor)
	}
	if alerts, err := s.auth.SystemAlerts(0); err != nil || len(alerts) != 0 {
		t.Fatalf("a TTS server without /health must not raise alerts: %#v err=%v", alerts, err)
	}
}
