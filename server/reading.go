package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/geek1011/BookBrowser/booklist"
	"github.com/geek1011/BookBrowser/public"
	"github.com/julienschmidt/httprouter"
)

type aboutResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	BuildID     string `json:"build_id"`
	BuildTime   string `json:"build_time"`
	BuildNumber string `json:"build_number"`
}

type readerContextResponse struct {
	About         aboutResponse      `json:"about"`
	Authenticated bool               `json:"authenticated"`
	CSRFToken     string             `json:"csrf_token,omitempty"`
	Items         []auth.ReadingItem `json:"items,omitempty"`
	Language      string             `json:"language,omitempty"`
}

type readingItemView struct {
	Item    auth.ReadingItem
	Book    *booklist.Book
	ReadURL string
}

func (s *Server) aboutData() aboutResponse {
	return aboutResponse{
		Name:        s.authSettings().SiteName,
		Description: "A private ebook library with reading, listening, personal lists, bookmarks, and notes.",
		Version:     s.version,
		BuildID:     s.buildID,
		BuildTime:   s.buildTime,
		BuildNumber: s.buildNumber,
	}
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Write JSON response: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) handleAbout(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeJSON(w, http.StatusOK, s.aboutData())
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var data []byte
	guidePath := "docs/user_guide.md"
	guideTitle := "How to use this app"
	if strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))) == "zh" {
		if zh, err := public.Box.MustBytes("docs/user_guide.zh.md"); err == nil {
			guidePath = "docs/user_guide.zh.md"
			guideTitle = "使用说明"
			data = zh
		}
	}
	if data == nil {
		var err error
		data, err = public.Box.MustBytes(guidePath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, errors.New("the user guide is unavailable"))
			return
		}
	}
	var rendered bytes.Buffer
	if err := implementationMarkdown.Convert(data, &rendered); err != nil {
		log.Printf("Render user guide: %v", err)
		writeJSONError(w, http.StatusInternalServerError, errors.New("the user guide could not be rendered"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"title":    guideTitle,
		"document": rendered.String(),
	})
}

func (s *Server) handleReaderContext(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	bookID := strings.TrimSpace(r.URL.Query().Get("book_id"))
	if s.findBook(bookID) == nil {
		writeJSONError(w, http.StatusNotFound, errors.New("book not found"))
		return
	}
	response := readerContextResponse{About: s.aboutData(), Items: []auth.ReadingItem{}}
	user, ok := s.currentUser(r)
	if !ok || !user.Role.Allows(auth.RoleReader) {
		writeJSON(w, http.StatusOK, response)
		return
	}
	items, err := s.auth.ReadingItems(user.ID, bookID, 500)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("could not load bookmarks and notes"))
		return
	}
	response.Authenticated = true
	response.CSRFToken = s.csrfToken(w, r)
	response.Items = items
	if language, err := s.auth.LanguageForUser(user.ID); err == nil {
		response.Language = language
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleReaderLanguage(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.readerAPIUser(w, r)
	if !ok {
		return
	}
	language := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if language != "en" && language != "zh" {
		writeJSONError(w, http.StatusBadRequest, errors.New("unsupported language"))
		return
	}
	if err := s.auth.SetLanguage(user.ID, language); err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("could not save language"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"language": language})
}

func (s *Server) readerAPIUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	user, ok := s.currentUser(r)
	if !ok || !user.Role.Allows(auth.RoleReader) {
		writeJSONError(w, http.StatusUnauthorized, errors.New("sign in to save bookmarks and notes"))
		return nil, false
	}
	if !s.verifyCSRF(r) {
		writeJSONError(w, http.StatusForbidden, errors.New("invalid CSRF token"))
		return nil, false
	}
	return user, true
}

func readingTags(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' })
}

func (s *Server) handleCreateReadingItemAPI(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.readerAPIUser(w, r)
	if !ok {
		return
	}
	bookID := r.FormValue("book_id")
	if s.findBook(bookID) == nil {
		writeJSONError(w, http.StatusNotFound, errors.New("book not found"))
		return
	}
	item, err := s.auth.CreateReadingItem(
		user.ID, bookID, auth.ReadingItemKind(r.FormValue("kind")), r.FormValue("locator"),
		r.FormValue("locator_label"), r.FormValue("title"), r.FormValue("body"),
		r.FormValue("excerpt"), readingTags(r.FormValue("tags")),
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateReadingItemAPI(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.readerAPIUser(w, r)
	if !ok {
		return
	}
	item, err := s.auth.UpdateReadingItem(
		user.ID, p.ByName("id"), r.FormValue("title"), r.FormValue("body"), readingTags(r.FormValue("tags")),
	)
	if errors.Is(err, auth.ErrReadingItemNotFound) {
		writeJSONError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteReadingItemAPI(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.readerAPIUser(w, r)
	if !ok {
		return
	}
	err := s.auth.DeleteReadingItem(user.ID, p.ByName("id"))
	if errors.Is(err, auth.ErrReadingItemNotFound) {
		writeJSONError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleReadingItems(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return
	}
	items, err := s.auth.ReadingItems(user.ID, "", 500)
	if err != nil {
		log.Printf("Read bookmarks and notes for %s: %v", user.ID, err)
		s.redirectPrivateLibraryError(w, r, "/my-library", err)
		return
	}
	views := make([]readingItemView, 0, len(items))
	for _, item := range items {
		views = append(views, readingItemView{Item: item, Book: s.findBook(item.BookID), ReadURL: readingItemLink(item)})
	}
	s.renderPage(w, r, http.StatusOK, "reading_items", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Bookmarks & Notes",
		"Title":      "Bookmarks & Notes",
		"Items":      views,
		"Saved":      r.URL.Query().Get("saved"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleUpdateReadingItem(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	_, err := s.auth.UpdateReadingItem(
		user.ID, p.ByName("id"), r.FormValue("title"), r.FormValue("body"), readingTags(r.FormValue("tags")),
	)
	if err != nil {
		s.redirectPrivateLibraryError(w, r, "/my-library/reading", err)
		return
	}
	http.Redirect(w, r, "/my-library/reading?saved=updated", http.StatusSeeOther)
}

func (s *Server) handleDeleteReadingItem(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	if err := s.auth.DeleteReadingItem(user.ID, p.ByName("id")); err != nil {
		s.redirectPrivateLibraryError(w, r, "/my-library/reading", err)
		return
	}
	http.Redirect(w, r, "/my-library/reading?saved=deleted", http.StatusSeeOther)
}

func readingItemLink(item auth.ReadingItem) string {
	return "/read/" + url.PathEscape(item.BookID) + "?locator=" + url.QueryEscape(item.Locator)
}
