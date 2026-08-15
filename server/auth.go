package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

const (
	sessionCookieName = "bookbrowser_session"
	csrfCookieName    = "bookbrowser_csrf"
)

type googleConfig struct {
	ClientID string
}

func (c googleConfig) Enabled() bool { return c.ClientID != "" }

type attemptEntry struct {
	Failures int
	ResetAt  time.Time
}

type attemptLimiter struct {
	mu      sync.Mutex
	entries map[string]attemptEntry
}

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{entries: make(map[string]attemptEntry)}
}

func (l *attemptLimiter) allowed(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.ResetAt) {
		delete(l.entries, key)
		return true
	}
	return entry.Failures < 5
}

func (l *attemptLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.ResetAt) {
		entry = attemptEntry{ResetAt: now.Add(15 * time.Minute)}
	}
	entry.Failures++
	l.entries[key] = entry
}

func (l *attemptLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (s *Server) initAuth() {
	dataDir := strings.TrimSpace(os.Getenv("BOOKBROWSER_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(s.BookDir, ".bookbrowser")
	}
	store, err := auth.NewSQLiteStore(dataDir)
	if err != nil {
		panic(fmt.Errorf("initialize authentication: %w", err))
	}
	s.auth = store
	s.google = googleConfig{ClientID: strings.TrimSpace(os.Getenv("BOOKBROWSER_GOOGLE_CLIENT_ID"))}
	s.googleVerifier = newGoogleTokenVerifier()
	s.loginAttempts = newAttemptLimiter()
	s.googleCLI = newGoogleCLIChallengeStore()
	log.Printf("Authentication database: %s", store.Path())
	if !s.google.Enabled() {
		log.Printf("Google login disabled: BOOKBROWSER_GOOGLE_CLIENT_ID is not configured")
	}
}

func (s *Server) userCount() int {
	count, err := s.auth.CountUsers()
	if err != nil {
		log.Printf("Count users: %v", err)
		return 0
	}
	return count
}

func (s *Server) registrationAllowed() bool {
	allowed, err := s.auth.CanRegister()
	if err != nil {
		log.Printf("Read registration setting: %v", err)
		return false
	}
	return allowed
}

func (s *Server) authSettings() auth.Settings {
	settings, err := s.auth.Settings()
	if err != nil {
		log.Printf("Read auth settings: %v", err)
		return auth.DefaultSettings()
	}
	return settings
}

func (s *Server) currentUser(r *http.Request) (*auth.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	user, err := s.auth.UserForSession(cookie.Value)
	if err != nil {
		log.Printf("Read session: %v", err)
		return nil, false
	}
	return user, user != nil
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, status int, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	settings := s.authSettings()
	data["SiteName"] = settings.SiteName
	data["PWAName"] = settings.PWAName
	data["CurrentYear"] = time.Now().Year()
	data["RegistrationOpen"] = s.registrationAllowed()
	data["GoogleEnabled"] = s.google.Enabled()
	data["GoogleClientID"] = s.google.ClientID
	data["NeedsSetup"] = s.userCount() == 0
	data["BuildID"] = s.buildID
	data["BuildTime"] = s.buildTime
	data["BuildNumber"] = s.buildNumber
	if r != nil {
		data["PageURL"] = pageURL(r)
		data["CSRFToken"] = s.csrfToken(w, r)
		if user, ok := s.currentUser(r); ok {
			data["CurrentUser"] = user
			data["LoggedIn"] = true
			data["IsAdmin"] = user.Role == auth.RoleAdmin
			data["CanManageLibrary"] = user.Role.Allows(auth.RoleManager)
			if count, err := s.auth.PendingBookRequestCountForUser(user.ID); err == nil {
				data["MyPendingRequests"] = count
			} else {
				log.Printf("Count pending book requests for %s: %v", user.ID, err)
			}
		}
	}
	s.render.HTML(w, status, name, data)
}

func (s *Server) requireRole(required auth.Role, handler httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		user, ok := s.currentUser(r)
		if !ok {
			s.redirectToLogin(w, r)
			return
		}
		if !user.Role.Allows(required) {
			s.renderPage(w, r, http.StatusForbidden, "message", map[string]interface{}{
				"CurVersion": s.version,
				"PageTitle":  "Forbidden",
				"Title":      "Access denied",
				"Message":    "Your account does not have permission to access this page.",
			})
			return
		}
		handler(w, r, p)
	}
}

func (s *Server) allowAnonymousBook(handler httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		if s.authSettings().AnonymousBookLinks {
			handler(w, r, p)
			return
		}
		user, ok := s.currentUser(r)
		if !ok || !user.Role.Allows(auth.RoleReader) {
			s.redirectToLogin(w, r)
			return
		}
		handler(w, r, p)
	}
}

func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.RequestURI()
	if !safeNext(next) {
		next = "/books"
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if _, ok := s.currentUser(r); ok {
		http.Redirect(w, r, "/books", http.StatusSeeOther)
		return
	}
	data := map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Sign in",
		"AuthPage":   true,
		"Next":       safeNextValue(r.URL.Query().Get("next")),
	}
	if r.Method == http.MethodGet {
		s.renderPage(w, r, http.StatusOK, "login", data)
		return
	}
	if !s.verifyCSRF(r) {
		s.renderPage(w, r, http.StatusForbidden, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Request expired",
			"Title":      "Request expired",
			"Message":    "Refresh the page and try again.",
		})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	key := clientIP(r) + "|" + email
	if !s.loginAttempts.allowed(key, time.Now()) {
		data["Error"] = "Too many failed attempts. Try again in 15 minutes."
		s.renderPage(w, r, http.StatusTooManyRequests, "login", data)
		return
	}
	user, err := s.auth.AuthenticateEmail(email, r.FormValue("password"))
	if err != nil {
		s.loginAttempts.fail(key, time.Now())
		data["Email"] = email
		data["Error"] = "Invalid email or password."
		s.renderPage(w, r, http.StatusUnauthorized, "login", data)
		return
	}
	s.loginAttempts.clear(key)
	if err := s.startSession(w, r, user); err != nil {
		log.Printf("Create session: %v", err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Sign-in error",
			"Title":      "Could not sign in",
			"Message":    "Please try again.",
		})
		return
	}
	next := safeNextValue(r.FormValue("next"))
	if next == "" {
		next = "/books"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if _, ok := s.currentUser(r); ok {
		http.Redirect(w, r, "/books", http.StatusSeeOther)
		return
	}
	data := map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Create account",
		"AuthPage":   true,
	}
	if !s.registrationAllowed() {
		data["RegistrationClosed"] = true
		s.renderPage(w, r, http.StatusForbidden, "register", data)
		return
	}
	if r.Method == http.MethodGet {
		s.renderPage(w, r, http.StatusOK, "register", data)
		return
	}
	if !s.verifyCSRF(r) {
		data["Error"] = "This form expired. Refresh the page and try again."
		s.renderPage(w, r, http.StatusForbidden, "register", data)
		return
	}
	password := r.FormValue("password")
	if password != r.FormValue("password_confirm") {
		data["Error"] = "Passwords do not match."
		data["Email"] = r.FormValue("email")
		data["Name"] = r.FormValue("name")
		s.renderPage(w, r, http.StatusBadRequest, "register", data)
		return
	}
	user, err := s.auth.RegisterEmail(r.FormValue("email"), r.FormValue("name"), password)
	if err != nil {
		data["Error"] = err.Error()
		data["Email"] = r.FormValue("email")
		data["Name"] = r.FormValue("name")
		s.renderPage(w, r, http.StatusBadRequest, "register", data)
		return
	}
	if r.FormValue("allow_share_links") != "on" {
		if err := s.auth.SetShareLinks(user.ID, false); err != nil {
			log.Printf("Disable share links for new user %s: %v", user.ID, err)
		}
	}
	if err := s.startSession(w, r, user); err != nil {
		log.Printf("Create registration session: %v", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/books", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.auth.DeleteSession(cookie.Value); err != nil {
			log.Printf("Delete session: %v", err)
		}
	}
	s.clearCookie(w, r, sessionCookieName)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *auth.User) error {
	token, err := s.auth.NewSession(user.ID)
	if err != nil {
		return err
	}
	if err := s.auth.RecordLastIP(user.ID, clientIP(r)); err != nil {
		log.Printf("Record login IP for %s: %v", user.ID, err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && len(cookie.Value) >= 32 {
		return cookie.Value
	}
	token, err := secureToken(24)
	if err != nil {
		log.Printf("Generate CSRF token: %v", err)
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((24 * time.Hour).Seconds()),
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func (s *Server) verifyCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	return constantStringEqual(cookie.Value, r.FormValue("csrf_token"))
}

func (s *Server) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: requestIsSecure(r), SameSite: http.SameSiteLaxMode,
	})
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host).IsLoopback() {
		return strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]) == "https"
	}
	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	parsed := net.ParseIP(host)
	if parsed != nil && parsed.IsLoopback() {
		if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
			return realIP.String()
		}
	}
	return host
}

func pageURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if requestIsSecure(r) {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

func safeNextValue(value string) string {
	if safeNext(value) {
		return value
	}
	return ""
}

func safeNext(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\r\n")
}

func secureToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func constantStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
