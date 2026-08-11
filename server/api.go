package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

const maxAPIJSONBytes int64 = 64 << 10

type apiErrorEnvelope struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiIdentity struct {
	User     *auth.User
	Token    *auth.APIToken
	RawToken string
}

type apiIdentityContextKey struct{}

func (s *Server) initAPIRoutes() {
	s.router.POST("/api/v1/auth/login", s.handleAPILogin)
	s.router.POST("/api/v1/auth/google/start", s.handleAPIGoogleStart)
	s.router.GET("/cli/google/:challenge", s.handleCLIGooglePage)
	s.router.POST("/api/v1/auth/google/complete", s.handleAPIGoogleComplete)
	s.router.POST("/api/v1/auth/google/poll", s.handleAPIGooglePoll)
	s.router.POST("/api/v1/auth/google/cancel", s.handleAPIGoogleCancel)

	s.router.GET("/api/v1/me", s.requireAPIRole(auth.RoleReader, s.handleAPIMe))
	s.router.GET("/api/v1/tokens", s.requireAPIRole(auth.RoleReader, s.handleAPITokens))
	s.router.DELETE("/api/v1/token", s.requireAPIRole(auth.RoleReader, s.handleAPIRevokeCurrentToken))
	s.router.DELETE("/api/v1/tokens/:name", s.requireAPIRole(auth.RoleReader, s.handleAPIRevokeToken))

	s.router.GET("/api/v1/books", s.requireAPIRole(auth.RoleReader, s.handleAPIBooks))
	s.router.GET("/api/v1/books/:id", s.requireAPIRole(auth.RoleReader, s.handleAPIBook))
	s.router.GET("/api/v1/books/:id/download", s.requireAPIRole(auth.RoleReader, s.handleAPIBookDownload))

	s.router.GET("/api/v1/library/status", s.requireAPIRole(auth.RoleManager, s.handleAPILibraryStatus))
	s.router.POST("/api/v1/library/books", s.requireAPIRole(auth.RoleManager, s.handleAPILibraryUpload))
	s.router.DELETE("/api/v1/library/books/:id", s.requireAPIRole(auth.RoleManager, s.handleAPILibraryRemove))
	s.router.POST("/api/v1/library/rescan", s.requireAPIRole(auth.RoleManager, s.handleAPILibraryRescan))

	s.router.GET("/api/v1/users", s.requireAPIRole(auth.RoleAdmin, s.handleAPIUsers))
	s.router.GET("/api/v1/users/:id", s.requireAPIRole(auth.RoleAdmin, s.handleAPIUser))
	s.router.PATCH("/api/v1/users/:id", s.requireAPIRole(auth.RoleAdmin, s.handleAPIUserUpdate))
	s.router.POST("/api/v1/users/:id/password", s.requireAPIRole(auth.RoleAdmin, s.handleAPIUserPassword))
	s.router.GET("/api/v1/settings", s.requireAPIRole(auth.RoleAdmin, s.handleAPISettings))
	s.router.PATCH("/api/v1/settings", s.requireAPIRole(auth.RoleAdmin, s.handleAPISettingsUpdate))
}

func writeAPIJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeAPIJSON(w, status, apiErrorEnvelope{Error: apiErrorBody{Code: code, Message: message}})
}

func decodeAPIJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIJSONBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "The JSON request body is invalid.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "The request must contain one JSON value.")
		return false
	}
	return true
}

func (s *Server) requireAPIRole(required auth.Role, handler httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		raw := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			writeAPIError(w, http.StatusUnauthorized, "authentication_required", "A valid bearer token is required.")
			return
		}
		raw = strings.TrimSpace(parts[1])
		user, token, err := s.auth.UserForAPIToken(raw)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "authentication_failed", "Authentication could not be completed.")
			return
		}
		if user == nil || token == nil {
			writeAPIError(w, http.StatusUnauthorized, "invalid_token", "The bearer token is invalid, expired, or revoked.")
			return
		}
		if !user.Role.Allows(required) {
			writeAPIError(w, http.StatusForbidden, "forbidden", "Your account does not have permission for this operation.")
			return
		}
		identity := &apiIdentity{User: user, Token: token, RawToken: raw}
		r = r.WithContext(context.WithValue(r.Context(), apiIdentityContextKey{}, identity))
		handler(w, r, p)
	}
}

func requestAPIIdentity(r *http.Request) *apiIdentity {
	identity, _ := r.Context().Value(apiIdentityContextKey{}).(*apiIdentity)
	return identity
}

func writeAPIStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
	case errors.Is(err, auth.ErrInactive):
		writeAPIError(w, http.StatusForbidden, "account_disabled", "This account is disabled.")
	case errors.Is(err, auth.ErrInvalidRole):
		writeAPIError(w, http.StatusBadRequest, "invalid_role", "Role must be reader, manager, or admin.")
	case errors.Is(err, auth.ErrIdentityConflict):
		writeAPIError(w, http.StatusConflict, "identity_conflict", err.Error())
	case errors.Is(err, auth.ErrLastAdmin):
		writeAPIError(w, http.StatusConflict, "last_active_admin", err.Error())
	case errors.Is(err, auth.ErrAPITokenNotFound):
		writeAPIError(w, http.StatusNotFound, "token_not_found", err.Error())
	case errors.Is(err, auth.ErrAPITokenNameExists):
		writeAPIError(w, http.StatusConflict, "token_name_exists", err.Error())
	case err != nil && err.Error() == "registration is closed":
		writeAPIError(w, http.StatusForbidden, "registration_closed", err.Error())
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The server could not complete the request.")
	}
}
