package server

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

func (s *Server) handleAPILibraryStatus(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	s.indexStatusMu.Lock()
	lastAt, lastError := s.lastIndexAt, s.lastIndexError
	s.indexStatusMu.Unlock()
	progress := s.Indexer.ProgressValue()
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{
		"indexing":          progress != 0,
		"progress":          progress,
		"book_count":        len(s.Indexer.BookList()),
		"last_completed_at": lastAt,
		"last_error":        lastError,
	})
}

func (s *Server) handleAPILibraryUpload(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBookUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "book_too_large", "The upload is invalid or exceeds 256 MiB.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("book")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "book_required", "Choose an ebook file to upload.")
		return
	}
	defer file.Close()
	name, size, err := s.storeUploadedBook(header.Filename, file)
	if err != nil {
		operation := libraryError(err)
		writeAPIError(w, operation.Status, operation.Code, operation.Message)
		return
	}
	go s.RefreshBookIndex()
	writeAPIJSON(w, http.StatusCreated, map[string]interface{}{
		"uploaded": true, "filename": name, "size": size,
	})
}

func (s *Server) handleAPILibraryRemove(w http.ResponseWriter, _ *http.Request, p httprouter.Params) {
	trashPath, err := s.removeLibraryBook(p.ByName("id"))
	if err != nil {
		operation := libraryError(err)
		writeAPIError(w, operation.Status, operation.Code, operation.Message)
		return
	}
	go s.RefreshBookIndex()
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{
		"removed": true, "recoverable": true, "trash_name": filepath.Base(trashPath),
	})
}

func (s *Server) handleAPILibraryRescan(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	go s.RefreshBookIndex()
	writeAPIJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

type apiAdminUser struct {
	apiUser
	LastIP      string     `json:"last_ip"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	HasPassword bool       `json:"has_password"`
	HasGoogle   bool       `json:"has_google"`
}

func newAPIAdminUser(user *auth.User) apiAdminUser {
	return apiAdminUser{
		apiUser: newAPIUser(user), LastIP: user.LastIP, CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt, LastLoginAt: user.LastLoginAt,
		HasPassword: user.PasswordHash != "", HasGoogle: user.GoogleSubject != "",
	}
}

func (s *Server) handleAPIUsers(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	users, err := s.auth.Users()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "user_list_failed", "Users could not be loaded.")
		return
	}
	items := make([]apiAdminUser, 0, len(users))
	for i := range users {
		items = append(items, newAPIAdminUser(&users[i]))
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"users": items})
}

func (s *Server) handleAPIUser(w http.ResponseWriter, _ *http.Request, p httprouter.Params) {
	user, err := s.auth.UserByID(p.ByName("id"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "user_load_failed", "The user could not be loaded.")
		return
	}
	if user == nil {
		writeAPIError(w, http.StatusNotFound, "user_not_found", "User not found.")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"user": newAPIAdminUser(user)})
}

func (s *Server) handleAPIUserUpdate(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	var input struct {
		Role   *auth.Role `json:"role"`
		Active *bool      `json:"active"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	user, err := s.auth.UserByID(p.ByName("id"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "user_load_failed", "The user could not be loaded.")
		return
	}
	if user == nil {
		writeAPIError(w, http.StatusNotFound, "user_not_found", "User not found.")
		return
	}
	role, active := user.Role, user.Active
	if input.Role != nil {
		role = *input.Role
	}
	if input.Active != nil {
		active = *input.Active
	}
	if input.Role == nil && input.Active == nil {
		writeAPIError(w, http.StatusBadRequest, "empty_update", "Supply role or active.")
		return
	}
	updated, err := s.auth.UpdateUser(user.ID, role, active)
	if err != nil {
		writeAPIStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"user": newAPIAdminUser(updated)})
}

func (s *Server) handleAPIUserPassword(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	var input struct {
		Password string `json:"password"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	if len(input.Password) < 10 {
		writeAPIError(w, http.StatusBadRequest, "password_too_short", "Password must contain at least 10 characters.")
		return
	}
	if user, err := s.auth.UserByID(p.ByName("id")); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "user_load_failed", "The user could not be loaded.")
		return
	} else if user == nil {
		writeAPIError(w, http.StatusNotFound, "user_not_found", "User not found.")
		return
	}
	if err := s.auth.SetPassword(p.ByName("id"), input.Password); err != nil {
		writeAPIStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]bool{"password_updated": true, "sessions_revoked": true, "tokens_revoked": true})
}

func (s *Server) handleAPISettings(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	settings, err := s.auth.Settings()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "settings_load_failed", "Settings could not be loaded.")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"settings": settings})
}

func (s *Server) handleAPISettingsUpdate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var input struct {
		SiteName           *string `json:"site_name"`
		PWAName            *string `json:"pwa_name"`
		RegistrationOpen   *bool   `json:"registration_open"`
		AnonymousBookLinks *bool   `json:"anonymous_book_links"`
	}
	if !decodeAPIJSON(w, r, &input) {
		return
	}
	if input.SiteName == nil && input.PWAName == nil && input.RegistrationOpen == nil && input.AnonymousBookLinks == nil {
		writeAPIError(w, http.StatusBadRequest, "empty_update", "Supply at least one setting.")
		return
	}
	settings, err := s.auth.Settings()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "settings_load_failed", "Settings could not be loaded.")
		return
	}
	if input.SiteName != nil {
		settings.SiteName = *input.SiteName
	}
	if input.PWAName != nil {
		settings.PWAName = *input.PWAName
	}
	if input.RegistrationOpen != nil {
		settings.RegistrationOpen = *input.RegistrationOpen
	}
	if input.AnonymousBookLinks != nil {
		settings.AnonymousBookLinks = *input.AnonymousBookLinks
	}
	if err := s.auth.UpdateSettings(settings); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"settings": settings})
}
