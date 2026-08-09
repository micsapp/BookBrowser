package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/geek1011/BookBrowser/booklist"
	"github.com/geek1011/BookBrowser/formats"
	"github.com/geek1011/BookBrowser/public"
	"github.com/julienschmidt/httprouter"
)

const maxBookUploadBytes int64 = 256 << 20

type adminUserView struct {
	ID        string
	Email     string
	Name      string
	Role      auth.Role
	Active    bool
	IsReader  bool
	IsManager bool
	IsAdmin   bool
	HasEmail  bool
	HasGoogle bool
	CreatedAt time.Time
	LastLogin string
}

func (s *Server) handleImplementation(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	data, err := public.Box.MustBytes("docs/auth_rbac_implementation.md")
	if err != nil {
		http.Error(w, "implementation guide is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, _ := s.currentUser(r)
	s.renderPage(w, r, http.StatusOK, "admin", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Administration",
		"Title":      "Administration",
		"UserCount":  s.userCount(),
		"BookCount":  len(s.Indexer.BookList()),
		"IsAdmin":    user != nil && user.Role == auth.RoleAdmin,
	})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	users, err := s.auth.Users()
	if err != nil {
		log.Printf("List users: %v", err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "User error",
			"Title":      "Could not load users",
			"Message":    "The user database could not be read.",
		})
		return
	}
	views := make([]adminUserView, 0, len(users))
	for _, user := range users {
		lastLogin := "Never"
		if user.LastLoginAt != nil {
			lastLogin = user.LastLoginAt.Local().Format("2006-01-02 15:04")
		}
		views = append(views, adminUserView{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			Role:      user.Role,
			Active:    user.Active,
			IsReader:  user.Role == auth.RoleReader,
			IsManager: user.Role == auth.RoleManager,
			IsAdmin:   user.Role == auth.RoleAdmin,
			HasEmail:  user.PasswordHash != "",
			HasGoogle: user.GoogleSubject != "",
			CreatedAt: user.CreatedAt,
			LastLogin: lastLogin,
		})
	}
	s.renderPage(w, r, http.StatusOK, "admin_users", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Manage users",
		"Title":      "Manage users",
		"Users":      views,
		"Saved":      r.URL.Query().Get("saved") == "1",
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleAdminUserUpdate(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	role := auth.Role(r.FormValue("role"))
	active := r.FormValue("active") == "on"
	if _, err := s.auth.UpdateUser(p.ByName("id"), role, active); err != nil {
		http.Redirect(w, r, "/admin/users?error="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?saved=1", http.StatusSeeOther)
}

func (s *Server) handleAdminLibrary(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	books := append(booklist.BookList(nil), s.Indexer.BookList()...)
	sort.Slice(books, func(i, j int) bool {
		return strings.ToLower(books[i].Title) < strings.ToLower(books[j].Title)
	})
	s.renderPage(w, r, http.StatusOK, "admin_library", map[string]interface{}{
		"CurVersion":  s.version,
		"PageTitle":   "Manage library",
		"Title":       "Manage library",
		"Books":       books,
		"Saved":       r.URL.Query().Get("saved"),
		"Error":       r.URL.Query().Get("error"),
		"MaxUploadMB": maxBookUploadBytes >> 20,
	})
}

func (s *Server) handleAdminLibraryUpload(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBookUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.libraryRedirectError(w, r, "The upload is invalid or exceeds 256 MiB.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	file, header, err := r.FormFile("book")
	if err != nil {
		s.libraryRedirectError(w, r, "Choose an ebook file to upload.")
		return
	}
	defer file.Close()
	filename := safeUploadName(header.Filename)
	if filename == "" || !supportedBookExtension(filepath.Ext(filename)) {
		s.libraryRedirectError(w, r, "That file type is not supported.")
		return
	}
	destination := filepath.Join(s.BookDir, filename)
	if _, err := os.Stat(destination); err == nil {
		s.libraryRedirectError(w, r, "A book with that filename already exists.")
		return
	} else if !os.IsNotExist(err) {
		s.libraryRedirectError(w, r, "The destination could not be checked.")
		return
	}
	tmp, err := os.CreateTemp(s.BookDir, ".book-upload-*")
	if err != nil {
		log.Printf("Create book upload: %v", err)
		s.libraryRedirectError(w, r, "The upload could not be stored.")
		return
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmpName)
		}
	}()
	written, err := io.Copy(tmp, io.LimitReader(file, maxBookUploadBytes+1))
	if err != nil || written > maxBookUploadBytes {
		s.libraryRedirectError(w, r, "The upload failed or exceeds 256 MiB.")
		return
	}
	if err := tmp.Sync(); err != nil {
		s.libraryRedirectError(w, r, "The upload could not be saved.")
		return
	}
	if err := tmp.Close(); err != nil {
		s.libraryRedirectError(w, r, "The upload could not be saved.")
		return
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		s.libraryRedirectError(w, r, "The uploaded file permissions could not be set.")
		return
	}
	if err := os.Rename(tmpName, destination); err != nil {
		log.Printf("Install uploaded book: %v", err)
		s.libraryRedirectError(w, r, "The uploaded book could not be installed.")
		return
	}
	keep = true
	go s.RefreshBookIndex()
	http.Redirect(w, r, "/admin/library?saved=uploaded", http.StatusSeeOther)
}

func (s *Server) handleAdminLibraryDelete(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	book := s.findBook(p.ByName("id"))
	if book == nil {
		s.libraryRedirectError(w, r, "Book not found.")
		return
	}
	relative, err := filepath.Rel(s.BookDir, book.FilePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		log.Printf("Refused to remove book outside library: %s", book.FilePath)
		s.libraryRedirectError(w, r, "The book path is outside the managed library.")
		return
	}
	trashDir := filepath.Join(s.BookDir, ".bookbrowser", "trash")
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		log.Printf("Create book trash: %v", err)
		s.libraryRedirectError(w, r, "The recoverable-delete folder could not be created.")
		return
	}
	trashName := fmt.Sprintf("%s-%d-%s.deleted", book.ID(), time.Now().Unix(), safeUploadName(filepath.Base(book.FilePath)))
	if err := os.Rename(book.FilePath, filepath.Join(trashDir, trashName)); err != nil {
		log.Printf("Move book to trash: %v", err)
		s.libraryRedirectError(w, r, "The book could not be moved to recoverable storage.")
		return
	}
	go s.RefreshBookIndex()
	http.Redirect(w, r, "/admin/library?saved=removed", http.StatusSeeOther)
}

func (s *Server) handleAdminLibraryRescan(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	go s.RefreshBookIndex()
	http.Redirect(w, r, "/admin/library?saved=rescan", http.StatusSeeOther)
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method == http.MethodPost {
		if !s.verifyCSRF(r) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		settings := auth.Settings{
			SiteName:           r.FormValue("site_name"),
			RegistrationOpen:   r.FormValue("registration_open") == "on",
			AnonymousBookLinks: r.FormValue("anonymous_book_links") == "on",
		}
		if err := s.auth.UpdateSettings(settings); err != nil {
			s.renderAdminSettings(w, r, settings, err.Error(), false)
			return
		}
		http.Redirect(w, r, "/admin/settings?saved=1", http.StatusSeeOther)
		return
	}
	settings, err := s.auth.Settings()
	if err != nil {
		log.Printf("Read settings: %v", err)
		s.renderAdminSettings(w, r, auth.DefaultSettings(), "The settings database could not be read.", false)
		return
	}
	s.renderAdminSettings(w, r, settings, "", r.URL.Query().Get("saved") == "1")
}

func (s *Server) renderAdminSettings(w http.ResponseWriter, r *http.Request, settings auth.Settings, errorMessage string, saved bool) {
	s.renderPage(w, r, http.StatusOK, "admin_settings", map[string]interface{}{
		"CurVersion":       s.version,
		"PageTitle":        "Settings",
		"Title":            "Settings",
		"Settings":         settings,
		"GoogleConfigured": s.google.Enabled(),
		"AuthDataPath":     s.auth.Path(),
		"Error":            errorMessage,
		"Saved":            saved,
	})
}

func (s *Server) findBook(id string) *booklist.Book {
	for _, book := range s.Indexer.BookList() {
		if book.ID() == id {
			return book
		}
	}
	return nil
}

func (s *Server) libraryRedirectError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin/library?error="+urlQuery(message), http.StatusSeeOther)
}

func urlQuery(value string) string {
	return url.QueryEscape(value)
}

var unsafeUploadCharacters = regexp.MustCompile(`[^A-Za-z0-9._()\- ]+`)

func safeUploadName(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	name = unsafeUploadCharacters.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if len(name) > 180 {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		limit := 180 - len(ext)
		if limit < 1 {
			return ""
		}
		name = base[:limit] + ext
	}
	return name
}

func supportedBookExtension(extension string) bool {
	extension = strings.TrimPrefix(strings.ToLower(extension), ".")
	for _, supported := range formats.GetExts() {
		if extension == strings.ToLower(supported) {
			return true
		}
	}
	return false
}
