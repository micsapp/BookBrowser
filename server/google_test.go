package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geek1011/BookBrowser/auth"
)

func TestGoogleIDTokenVerificationAndFirstAdmin(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "test-key"
	jwksBody, err := json.Marshal(map[string]interface{}{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": keyID,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	token := signedGoogleToken(t, privateKey, keyID, map[string]interface{}{
		"iss":            "https://accounts.google.com",
		"aud":            "test-client",
		"sub":            "google-subject",
		"email":          "first@gmail.com",
		"email_verified": true,
		"name":           "First Google User",
		"iat":            now.Add(-time.Minute).Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	})
	verifier := newGoogleTokenVerifier()
	verifier.jwksURL = "https://google.test/certs"
	verifier.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": {"public, max-age=600"}},
			Body:       io.NopCloser(strings.NewReader(string(jwksBody))),
			Request:    request,
		}, nil
	})}
	identity, err := verifier.Verify(token, "test-client", now)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "google-subject" || !identity.EmailAuthoritative {
		t.Fatalf("identity=%#v", identity)
	}
	if _, err := verifier.Verify(token, "wrong-client", now); err == nil {
		t.Fatal("wrong Google audience was accepted")
	}

	s := newAuthTestServer(t)
	s.google.ClientID = "test-client"
	s.googleVerifier = verifier
	login := requestServer(s, http.MethodGet, "/login", nil)
	csrf := cookieNamed(login, csrfCookieName)
	if csrf == nil {
		t.Fatal("login page did not set CSRF cookie")
	}
	response := requestServer(s, http.MethodPost, "/auth/google", url.Values{
		"csrf_token": {csrf.Value},
		"credential": {token},
	}, csrf)
	if response.Code != http.StatusOK {
		t.Fatalf("Google login status=%d body=%s", response.Code, response.Body.String())
	}
	user, err := s.auth.UserByEmail("first@gmail.com")
	if err != nil || user == nil || user.Role != auth.RoleAdmin {
		t.Fatalf("first Google user=%#v err=%v", user, err)
	}
	if cookieNamed(response, sessionCookieName) == nil {
		t.Fatal("Google login did not create a session")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func signedGoogleToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims map[string]interface{}) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": keyID, "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}
