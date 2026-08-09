package server

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
)

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

type googleTokenVerifier struct {
	mu      sync.Mutex
	client  *http.Client
	jwksURL string
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

type googleIdentity struct {
	Subject            string
	Email              string
	Name               string
	EmailAuthoritative bool
}

func newGoogleTokenVerifier() *googleTokenVerifier {
	return &googleTokenVerifier{
		client:  &http.Client{Timeout: 12 * time.Second},
		jwksURL: googleJWKSURL,
		keys:    make(map[string]*rsa.PublicKey),
	}
}

func (s *Server) handleGoogleLogin(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !s.google.Enabled() {
		writeGoogleError(w, http.StatusNotFound, "Google login is not configured.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		writeGoogleError(w, http.StatusBadRequest, "Invalid Google login request.")
		return
	}
	if !s.verifyCSRF(r) {
		writeGoogleError(w, http.StatusForbidden, "This login form expired. Refresh the page and try again.")
		return
	}
	credential := r.FormValue("credential")
	if credential == "" {
		writeGoogleError(w, http.StatusBadRequest, "Google did not return an ID token.")
		return
	}
	identity, err := s.googleVerifier.Verify(credential, s.google.ClientID, time.Now())
	if err != nil {
		writeGoogleError(w, http.StatusUnauthorized, "Google sign-in could not be verified.")
		return
	}
	user, err := s.auth.UpsertGoogle(
		identity.Email,
		identity.Name,
		identity.Subject,
		identity.EmailAuthoritative,
	)
	if err != nil {
		writeGoogleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		writeGoogleError(w, http.StatusInternalServerError, "Could not create a login session.")
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"redirect": "/books"})
}

func writeGoogleError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (v *googleTokenVerifier) Verify(token, clientID string, now time.Time) (*googleIdentity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || clientID == "" {
		return nil, errors.New("invalid Google ID token format")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, err
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return nil, errors.New("unsupported Google ID token algorithm")
	}
	key, err := v.key(header.KeyID, now)
	if err != nil {
		return nil, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return nil, errors.New("invalid Google ID token signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims struct {
		Issuer        string `json:"iss"`
		Audience      string `json:"aud"`
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		HostedDomain  string `json:"hd"`
		ExpiresAt     int64  `json:"exp"`
		IssuedAt      int64  `json:"iat"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, err
	}
	if claims.Issuer != "accounts.google.com" && claims.Issuer != "https://accounts.google.com" {
		return nil, errors.New("invalid Google ID token issuer")
	}
	if claims.Audience != clientID || claims.Subject == "" || claims.Email == "" || !claims.EmailVerified {
		return nil, errors.New("invalid Google ID token claims")
	}
	unixNow := now.Unix()
	if claims.ExpiresAt <= unixNow || claims.IssuedAt > unixNow+300 {
		return nil, errors.New("expired or future Google ID token")
	}
	return &googleIdentity{
		Subject:            claims.Subject,
		Email:              claims.Email,
		Name:               claims.Name,
		EmailAuthoritative: strings.HasSuffix(strings.ToLower(claims.Email), "@gmail.com") || claims.HostedDomain != "",
	}, nil
}

func (v *googleTokenVerifier) key(keyID string, now time.Time) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if key := v.keys[keyID]; key != nil && now.Before(v.expires) {
		return key, nil
	}
	if err := v.refreshLocked(now); err != nil {
		return nil, err
	}
	key := v.keys[keyID]
	if key == nil {
		return nil, errors.New("Google signing key was not found")
	}
	return key, nil
}

func (v *googleTokenVerifier) refreshLocked(now time.Time) error {
	response, err := v.client.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("retrieve Google signing keys: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Google signing keys returned HTTP %d", response.StatusCode)
	}
	var document struct {
		Keys []struct {
			KeyType   string `json:"kty"`
			KeyID     string `json:"kid"`
			Use       string `json:"use"`
			Algorithm string `json:"alg"`
			Modulus   string `json:"n"`
			Exponent  string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&document); err != nil {
		return fmt.Errorf("decode Google signing keys: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, item := range document.Keys {
		if item.KeyType != "RSA" || item.KeyID == "" || (item.Algorithm != "" && item.Algorithm != "RS256") {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(item.Modulus)
		if err != nil {
			continue
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(item.Exponent)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			continue
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 | int(value)
		}
		publicKey := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
		if publicKey.N.BitLen() < 2048 || publicKey.E < 3 {
			continue
		}
		keys[item.KeyID] = publicKey
	}
	if len(keys) == 0 {
		return errors.New("Google returned no usable signing keys")
	}
	v.keys = keys
	v.expires = now.Add(cacheMaxAge(response.Header.Get("Cache-Control")))
	return nil
}

var maxAgePattern = regexp.MustCompile(`(?:^|,)\s*max-age=(\d+)`)

func cacheMaxAge(value string) time.Duration {
	matches := maxAgePattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return time.Hour
	}
	seconds, err := strconv.Atoi(matches[1])
	if err != nil || seconds < 60 {
		return time.Hour
	}
	if seconds > 86400 {
		seconds = 86400
	}
	return time.Duration(seconds) * time.Second
}
