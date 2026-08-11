package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

const (
	googleCLIChallengeLifetime = 5 * time.Minute
	googleCLIPollInterval      = 3 * time.Second
)

type apiUser struct {
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Role       auth.Role `json:"role"`
	Active     bool      `json:"active"`
	AllowShare bool      `json:"allow_share_links"`
}

func newAPIUser(user *auth.User) apiUser {
	return apiUser{
		ID: user.ID, Email: user.Email, Name: user.Name, Role: user.Role,
		Active: user.Active, AllowShare: user.AllowShare,
	}
}

type googleCLIChallenge struct {
	ID           string
	PollHash     string
	TokenName    string
	ClientName   string
	ClientIP     string
	ExpiresAt    time.Time
	LastPollAt   time.Time
	ApprovedUser string
	Issuing      bool
}

type googleCLIChallengeStore struct {
	mu         sync.Mutex
	challenges map[string]*googleCLIChallenge
	now        func() time.Time
}

func newGoogleCLIChallengeStore() *googleCLIChallengeStore {
	return &googleCLIChallengeStore{challenges: make(map[string]*googleCLIChallenge), now: time.Now}
}

func challengeSecretHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func secretHashEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *googleCLIChallengeStore) cleanupLocked(now time.Time) {
	for id, challenge := range s.challenges {
		if !now.Before(challenge.ExpiresAt) {
			delete(s.challenges, id)
		}
	}
}

func (s *googleCLIChallengeStore) add(id, pollSecret, tokenName, clientName, ip string) (*googleCLIChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.cleanupLocked(now)
	active := 0
	for _, item := range s.challenges {
		if item.ClientIP == ip {
			active++
		}
	}
	if active >= 5 {
		return nil, errors.New("too many active Google login requests")
	}
	challenge := &googleCLIChallenge{
		ID: id, PollHash: challengeSecretHash(pollSecret), TokenName: tokenName,
		ClientName: clientName, ClientIP: ip, ExpiresAt: now.Add(googleCLIChallengeLifetime),
	}
	s.challenges[id] = challenge
	copy := *challenge
	return &copy, nil
}

func (s *googleCLIChallengeStore) get(id string) (*googleCLIChallenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.cleanupLocked(now)
	challenge := s.challenges[id]
	if challenge == nil {
		return nil, false
	}
	copy := *challenge
	return &copy, true
}

func (s *googleCLIChallengeStore) approve(id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.cleanupLocked(now)
	challenge := s.challenges[id]
	if challenge == nil {
		return errors.New("login request expired or was already used")
	}
	if challenge.ApprovedUser != "" {
		return errors.New("login request was already approved")
	}
	challenge.ApprovedUser = userID
	return nil
}

func (s *googleCLIChallengeStore) poll(id, secret string) (*googleCLIChallenge, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	challenge := s.challenges[id]
	if challenge == nil {
		return nil, "invalid"
	}
	if !now.Before(challenge.ExpiresAt) {
		delete(s.challenges, id)
		return nil, "expired"
	}
	if !secretHashEqual(challenge.PollHash, challengeSecretHash(secret)) {
		return nil, "invalid"
	}
	if !challenge.LastPollAt.IsZero() && now.Sub(challenge.LastPollAt) < googleCLIPollInterval {
		return nil, "slow"
	}
	challenge.LastPollAt = now
	if challenge.ApprovedUser == "" {
		return nil, "pending"
	}
	if challenge.Issuing {
		return nil, "slow"
	}
	challenge.Issuing = true
	copy := *challenge
	return &copy, "approved"
}

func (s *googleCLIChallengeStore) finish(id string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if success {
		delete(s.challenges, id)
	} else if challenge := s.challenges[id]; challenge != nil {
		challenge.Issuing = false
	}
}

func (s *googleCLIChallengeStore) cancel(id, secret string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge := s.challenges[id]
	if challenge == nil || !secretHashEqual(challenge.PollHash, challengeSecretHash(secret)) {
		return false
	}
	delete(s.challenges, id)
	return true
}

func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var input struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		TokenName string `json:"token_name"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	key := clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(input.Email))
	if !s.loginAttempts.allowed(key, time.Now()) {
		writeAPIError(w, http.StatusTooManyRequests, "login_rate_limited", "Too many failed attempts. Try again later.")
		return
	}
	user, err := s.auth.AuthenticateEmail(input.Email, input.Password)
	if err != nil {
		s.loginAttempts.fail(key, time.Now())
		writeAPIStoreError(w, err)
		return
	}
	s.loginAttempts.clear(key)
	if err := s.auth.RecordLastIP(user.ID, clientIP(r)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "login_failed", "The login could not be recorded.")
		return
	}
	raw, token, err := s.auth.CreateAPIToken(user.ID, input.TokenName, nil)
	if err != nil {
		writeAPIStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]interface{}{
		"token": raw, "api_token": token, "user": newAPIUser(user),
	})
}

func (s *Server) handleAPIMe(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	identity := requestAPIIdentity(r)
	settings := s.authSettings()
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{
		"user":  newAPIUser(identity.User),
		"token": identity.Token,
		"server": map[string]interface{}{
			"site_name": settings.SiteName, "pwa_name": settings.PWAName,
			"version": s.version, "build_id": s.buildID, "build_time": s.buildTime,
		},
	})
}

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	items, err := s.auth.APITokens(requestAPIIdentity(r).User.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "token_list_failed", "API tokens could not be loaded.")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"tokens": items})
}

func (s *Server) handleAPIRevokeToken(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	if err := s.auth.RevokeAPIToken(requestAPIIdentity(r).User.ID, p.ByName("name")); err != nil {
		writeAPIStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) handleAPIRevokeCurrentToken(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := s.auth.RevokeCurrentAPIToken(requestAPIIdentity(r).RawToken); err != nil {
		writeAPIStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) handleAPIGoogleStart(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.google.Enabled() {
		writeAPIError(w, http.StatusNotFound, "google_login_unavailable", "Google login is not configured.")
		return
	}
	var input struct {
		TokenName  string `json:"token_name"`
		ClientName string `json:"client_name"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	if err := auth.ValidateAPITokenName(input.TokenName); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_token_name", err.Error())
		return
	}
	input.ClientName = strings.TrimSpace(input.ClientName)
	if input.ClientName == "" {
		input.ClientName = "bookbrowser-cli"
	}
	if len(input.ClientName) > 120 {
		input.ClientName = input.ClientName[:120]
	}
	id, err := secureToken(24)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "challenge_failed", "The login request could not be created.")
		return
	}
	pollSecret, err := secureToken(32)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "challenge_failed", "The login request could not be created.")
		return
	}
	challenge, err := s.googleCLI.add(id, pollSecret, input.TokenName, input.ClientName, clientIP(r))
	if err != nil {
		writeAPIError(w, http.StatusTooManyRequests, "challenge_rate_limited", err.Error())
		return
	}
	base := strings.TrimSuffix(pageURL(r), r.URL.RequestURI())
	writeAPIJSON(w, http.StatusCreated, map[string]interface{}{
		"challenge_id":     id,
		"poll_secret":      pollSecret,
		"verification_url": base + "/cli/google/" + id,
		"expires_at":       challenge.ExpiresAt,
		"interval_seconds": int(googleCLIPollInterval.Seconds()),
	})
}

var cliGooglePage = template.Must(template.New("cli-google").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Authorize BookBrowser CLI</title>
<style>body{font:16px system-ui,sans-serif;max-width:620px;margin:8vh auto;padding:24px;color:#202124}main{border:1px solid #ddd;border-radius:14px;padding:28px}button{padding:10px 16px}.muted{color:#666}.error{color:#a00}</style></head>
<body><main><h1>Authorize BookBrowser CLI</h1>
<p>Request from <strong>{{.ClientName}}</strong> to create token <code>{{.TokenName}}</code>.</p>
<p class="muted">This request expires at {{.ExpiresAt}}.</p>
<div id="google-button"></div><p id="account"></p>
<button id="approve" hidden>Approve CLI login</button><p id="status" role="status"></p>
</main>
<script>
let credential="";
function googleCredential(response){
  credential=response.credential||"";
  try{const p=JSON.parse(atob(credential.split('.')[1].replace(/-/g,'+').replace(/_/g,'/')));document.getElementById('account').textContent='Google account: '+(p.email||'selected account');}catch(e){}
  document.getElementById('approve').hidden=false;
}
function renderGoogle(){google.accounts.id.initialize({client_id:"{{.ClientID}}",callback:googleCredential});google.accounts.id.renderButton(document.getElementById('google-button'),{theme:'outline',size:'large'});}
document.getElementById('approve').onclick=function(){
  const body=new URLSearchParams({credential:credential,challenge_id:"{{.ChallengeID}}",csrf_token:"{{.CSRFToken}}"});
  fetch('/api/v1/auth/google/complete',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:body.toString()})
  .then(async r=>{const b=await r.json();if(!r.ok)throw new Error((b.error&&b.error.message)||'Approval failed');return b;})
  .then(()=>{document.getElementById('status').textContent='Approved. You can return to the terminal.';document.getElementById('approve').hidden=true;})
  .catch(e=>{const s=document.getElementById('status');s.className='error';s.textContent=e.message;});
};
</script><script src="https://accounts.google.com/gsi/client" async defer onload="renderGoogle()"></script></body></html>`))

func (s *Server) handleCLIGooglePage(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	challenge, ok := s.googleCLI.get(p.ByName("challenge"))
	if !ok || !s.google.Enabled() {
		http.Error(w, "This CLI login request is invalid or expired.", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = cliGooglePage.Execute(w, map[string]interface{}{
		"ClientName": challenge.ClientName, "TokenName": challenge.TokenName,
		"ExpiresAt": challenge.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"),
		"ClientID":  s.google.ClientID, "ChallengeID": challenge.ID,
		"CSRFToken": s.csrfToken(w, r),
	})
}

func (s *Server) handleAPIGoogleComplete(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.google.Enabled() {
		writeAPIError(w, http.StatusNotFound, "google_login_unavailable", "Google login is not configured.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil || !s.verifyCSRF(r) {
		writeAPIError(w, http.StatusForbidden, "invalid_csrf", "This authorization page expired. Reload it and try again.")
		return
	}
	challengeID := r.FormValue("challenge_id")
	if _, ok := s.googleCLI.get(challengeID); !ok {
		writeAPIError(w, http.StatusGone, "challenge_expired", "This CLI login request is invalid or expired.")
		return
	}
	key := clientIP(r) + "|google-cli"
	if !s.loginAttempts.allowed(key, time.Now()) {
		writeAPIError(w, http.StatusTooManyRequests, "login_rate_limited", "Too many failed attempts. Try again later.")
		return
	}
	identity, err := s.googleVerifier.Verify(r.FormValue("credential"), s.google.ClientID, time.Now())
	if err != nil {
		s.loginAttempts.fail(key, time.Now())
		writeAPIError(w, http.StatusUnauthorized, "google_verification_failed", "Google sign-in could not be verified.")
		return
	}
	user, err := s.auth.UpsertGoogle(identity.Email, identity.Name, identity.Subject, identity.EmailAuthoritative)
	if err != nil {
		writeAPIStoreError(w, err)
		return
	}
	s.loginAttempts.clear(key)
	if err := s.googleCLI.approve(challengeID, user.ID); err != nil {
		writeAPIError(w, http.StatusConflict, "challenge_unavailable", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]bool{"approved": true})
}

func (s *Server) handleAPIGooglePoll(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var input struct {
		ChallengeID string `json:"challenge_id"`
		PollSecret  string `json:"poll_secret"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	challenge, status := s.googleCLI.poll(input.ChallengeID, input.PollSecret)
	switch status {
	case "pending":
		w.Header().Set("Retry-After", "3")
		writeAPIJSON(w, http.StatusAccepted, map[string]interface{}{"status": "pending", "retry_after": 3})
		return
	case "slow":
		w.Header().Set("Retry-After", "3")
		writeAPIError(w, http.StatusTooManyRequests, "poll_too_fast", "Wait before polling again.")
		return
	case "expired":
		writeAPIError(w, http.StatusGone, "challenge_expired", "The Google login request expired.")
		return
	case "invalid":
		writeAPIError(w, http.StatusUnauthorized, "invalid_challenge", "The Google login request is invalid.")
		return
	}
	raw, token, err := s.auth.CreateAPIToken(challenge.ApprovedUser, challenge.TokenName, nil)
	if err != nil {
		s.googleCLI.finish(challenge.ID, false)
		writeAPIStoreError(w, err)
		return
	}
	user, err := s.auth.UserByID(challenge.ApprovedUser)
	if err != nil || user == nil {
		_ = s.auth.RevokeCurrentAPIToken(raw)
		s.googleCLI.finish(challenge.ID, false)
		writeAPIError(w, http.StatusInternalServerError, "login_failed", "The approved account could not be loaded.")
		return
	}
	if err := s.auth.RecordLastIP(user.ID, clientIP(r)); err != nil {
		_ = s.auth.RevokeCurrentAPIToken(raw)
		s.googleCLI.finish(challenge.ID, false)
		writeAPIError(w, http.StatusInternalServerError, "login_failed", "The login could not be recorded.")
		return
	}
	s.googleCLI.finish(challenge.ID, true)
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{
		"status": "approved", "token": raw, "api_token": token, "user": newAPIUser(user),
	})
}

func (s *Server) handleAPIGoogleCancel(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var input struct {
		ChallengeID string `json:"challenge_id"`
		PollSecret  string `json:"poll_secret"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	if !s.googleCLI.cancel(input.ChallengeID, input.PollSecret) {
		writeAPIError(w, http.StatusUnauthorized, "invalid_challenge", "The Google login request is invalid.")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}
