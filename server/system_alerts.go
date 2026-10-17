package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

// System alert keys raised by the background monitor. Each key identifies one
// kind of problem, so the channel holds at most one open alert per key.
const (
	alertKeyTTSUpstream = "tts.upstream"
	alertKeyTTSService  = "tts.service"
)

const (
	systemMonitorInterval = time.Minute
	// Consecutive failed health checks before the TTS service counts as
	// unreachable; this rides out an ordinary service restart.
	ttsUnreachableThreshold = 3
	systemAlertsPageLimit   = 200
)

var errTTSHealthUnsupported = errors.New("TTS service has no /health endpoint")

// ttsHealth mirrors the JSON returned by GET /health on scripts/tts_server.py.
type ttsHealth struct {
	Status              string  `json:"status"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	FailingSince        float64 `json:"failing_since"`
	LastSuccessAt       float64 `json:"last_success_at"`
	LastError           string  `json:"last_error"`
	EdgeTTSVersion      string  `json:"edge_tts_version"`
}

// ttsMonitor holds the state carried between TTS health checks.
type ttsMonitor struct {
	url               string
	client            *http.Client
	unreachable       int
	unreachableSince  time.Time
	unsupportedLogged bool
}

func newTTSMonitor(url string) *ttsMonitor {
	return &ttsMonitor{url: url, client: &http.Client{Timeout: 10 * time.Second}}
}

func (m *ttsMonitor) fetch() (*ttsHealth, error) {
	response, err := m.client.Get(m.url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		io.Copy(ioutil.Discard, response.Body)
		return nil, errTTSHealthUnsupported
	}
	if response.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, response.Body)
		return nil, fmt.Errorf("health check returned HTTP %d", response.StatusCode)
	}
	var health ttsHealth
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&health); err != nil {
		return nil, fmt.Errorf("read health response: %w", err)
	}
	return &health, nil
}

// StartSystemMonitor polls the TTS service's health endpoint once a minute and
// reports problems to administrators through system alerts. An empty URL
// disables the monitor.
func (s *Server) StartSystemMonitor(ttsHealthURL string) {
	ttsHealthURL = strings.TrimSpace(ttsHealthURL)
	if ttsHealthURL == "" {
		log.Printf("System monitor disabled: no TTS health URL configured")
		return
	}
	log.Printf("System monitor checking %s every %s", ttsHealthURL, systemMonitorInterval)
	monitor := newTTSMonitor(ttsHealthURL)
	go func() {
		ticker := time.NewTicker(systemMonitorInterval)
		defer ticker.Stop()
		for {
			s.checkTTSHealth(monitor)
			<-ticker.C
		}
	}()
}

func (s *Server) checkTTSHealth(m *ttsMonitor) {
	health, err := m.fetch()
	if errors.Is(err, errTTSHealthUnsupported) {
		if !m.unsupportedLogged {
			log.Printf("System monitor: %s; update scripts/tts_server.py to enable TTS alerts", err)
			m.unsupportedLogged = true
		}
		m.unreachable = 0
		s.resolveSystemAlert(alertKeyTTSService, "The text-to-speech service is answering again.")
		return
	}
	if err != nil {
		if m.unreachable == 0 {
			m.unreachableSince = time.Now()
		}
		m.unreachable++
		if m.unreachable >= ttsUnreachableThreshold {
			s.raiseSystemAlert(alertKeyTTSService, "tts", "Text-to-speech service is unreachable", fmt.Sprintf(
				"BookBrowser has not reached the text-to-speech service at %s for %d consecutive checks since %s.\n"+
					"Readers hear the browser's built-in fallback voice, and their voice setting is ignored.\n\n"+
					"Last error: %s\n\n"+
					"Check the bookbrowser-tts service and its log. This alert closes automatically once the service answers again.",
				m.url, m.unreachable, formatAlertTime(m.unreachableSince), err))
		}
		return
	}
	m.unreachable = 0
	s.resolveSystemAlert(alertKeyTTSService, "The text-to-speech service is answering again.")

	switch health.Status {
	case "failing":
		s.raiseSystemAlert(alertKeyTTSUpstream, "tts", "Neural text-to-speech is failing", ttsFailingDetails(health))
	case "ok":
		s.resolveSystemAlert(alertKeyTTSUpstream, "Neural speech synthesis is succeeding again.")
	}
}

func ttsFailingDetails(health *ttsHealth) string {
	lastSuccess := "none since the TTS service started"
	if health.LastSuccessAt > 0 {
		lastSuccess = formatAlertTime(unixFloat(health.LastSuccessAt))
	}
	var details strings.Builder
	fmt.Fprintf(&details, "Microsoft Edge neural speech synthesis has failed %d times in a row since %s (last success: %s).\n",
		health.ConsecutiveFailures, formatAlertTime(unixFloat(health.FailingSince)), lastSuccess)
	details.WriteString("Readers hear the browser's built-in fallback voice, and their voice setting is ignored. Passages already in the TTS cache still play normally.\n\n")
	fmt.Fprintf(&details, "Last error: %s\n", health.LastError)
	if health.EdgeTTSVersion != "" {
		fmt.Fprintf(&details, "edge-tts version: %s\n", health.EdgeTTSVersion)
	}
	if strings.Contains(health.LastError, "403") {
		details.WriteString("\nA 403 from speech.platform.bing.com usually means Microsoft changed its Edge TTS token. " +
			"Upgrade edge-tts in the TTS virtual environment and restart bookbrowser-tts (see current_deployment.md).\n")
	}
	details.WriteString("\nThis alert closes automatically once synthesis succeeds again.")
	return details.String()
}

func unixFloat(seconds float64) time.Time {
	return time.Unix(int64(seconds), 0)
}

func formatAlertTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04 MST")
}

func (s *Server) raiseSystemAlert(key, source, title, details string) {
	alert, opened, err := s.auth.RaiseSystemAlert(key, source, title, details)
	if err != nil {
		log.Printf("Raise system alert %s: %v", key, err)
		return
	}
	if opened {
		log.Printf("System alert opened: %s (%s)", alert.Title, key)
	}
}

func (s *Server) resolveSystemAlert(key, resolution string) {
	closed, err := s.auth.ResolveSystemAlert(key, resolution)
	if err != nil {
		log.Printf("Resolve system alert %s: %v", key, err)
		return
	}
	if closed {
		log.Printf("System alert resolved: %s", key)
	}
}

type systemAlertView struct {
	auth.SystemAlert
	Unread        bool
	FirstSeenText string
	LastSeenText  string
	ResolvedText  string
	ReadText      string
}

func (s *Server) handleAdminSystemAlerts(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	alerts, err := s.auth.SystemAlerts(systemAlertsPageLimit)
	if err != nil {
		log.Printf("List system alerts: %v", err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "System alerts error",
			"Title":      "Could not load system alerts",
			"Message":    "The system alert list could not be read.",
		})
		return
	}
	views := make([]systemAlertView, 0, len(alerts))
	unread := 0
	for _, alert := range alerts {
		view := systemAlertView{
			SystemAlert:   alert,
			Unread:        alert.AcknowledgedAt == nil,
			FirstSeenText: alert.FirstSeenAt.Local().Format("2006-01-02 15:04"),
			LastSeenText:  alert.LastSeenAt.Local().Format("2006-01-02 15:04"),
		}
		if alert.ResolvedAt != nil {
			view.ResolvedText = alert.ResolvedAt.Local().Format("2006-01-02 15:04")
		}
		if alert.AcknowledgedAt != nil {
			view.ReadText = alert.AcknowledgedAt.Local().Format("2006-01-02 15:04")
		} else {
			unread++
		}
		views = append(views, view)
	}
	s.renderPage(w, r, http.StatusOK, "admin_system", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "System alerts",
		"Title":      "",
		"Alerts":     views,
		"Unread":     unread,
		"Saved":      r.URL.Query().Get("saved"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// handleAdminSystemAlertsRead marks one alert, or with all=1 every alert, as
// read by the signed-in administrator.
func (s *Server) handleAdminSystemAlertsRead(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/system?error="+urlQuery("The form could not be read."), http.StatusSeeOther)
		return
	}
	user, _ := s.currentUser(r)
	if r.FormValue("all") == "1" {
		if _, err := s.auth.AcknowledgeAllSystemAlerts(user.ID); err != nil {
			log.Printf("Mark all system alerts read: %v", err)
			http.Redirect(w, r, "/admin/system?error="+urlQuery("The alerts could not be updated."), http.StatusSeeOther)
			return
		}
	} else if err := s.auth.AcknowledgeSystemAlert(r.FormValue("alert_id"), user.ID); err != nil {
		http.Redirect(w, r, "/admin/system?error="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/system?saved=1", http.StatusSeeOther)
}
