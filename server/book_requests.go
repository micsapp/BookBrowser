package server

import (
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

type bookRequestView struct {
	auth.BookRequest
	BookTitle   string
	CreatedText string
}

type libraryBookOption struct {
	ID    string
	Title string
}

// handleCreateBookRequest accepts a signed-in reader's request for a book that
// is missing from the library. The modal on the navigation bar submits it with
// X-Requested-With so the response is JSON; a plain form post (JavaScript
// disabled) redirects to the reader's requests page.
func (s *Server) handleCreateBookRequest(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok || !user.Role.Allows(auth.RoleReader) {
		s.redirectToLogin(w, r)
		return
	}
	if !s.verifyCSRF(r) {
		if isFetchRequest(r) {
			writeJSONError(w, http.StatusForbidden, errMessage("This form expired. Refresh the page and try again."))
		} else {
			http.Redirect(w, r, "/requests?error="+urlQuery("This form expired. Refresh the page and try again."), http.StatusSeeOther)
		}
		return
	}
	if err := r.ParseForm(); err != nil {
		s.bookRequestFailure(w, r, "The request form could not be read.")
		return
	}
	request, err := s.auth.CreateBookRequest(user.ID, r.FormValue("title"), r.FormValue("author"), r.FormValue("notes"))
	if err != nil {
		s.bookRequestFailure(w, r, err.Error())
		return
	}
	log.Printf("Book request %s from %s: %q", request.ID, user.Email, request.Title)
	if isFetchRequest(r) {
		writeJSON(w, http.StatusCreated, map[string]interface{}{"ok": true, "id": request.ID})
		return
	}
	http.Redirect(w, r, "/requests?saved=1", http.StatusSeeOther)
}

func (s *Server) bookRequestFailure(w http.ResponseWriter, r *http.Request, message string) {
	if isFetchRequest(r) {
		writeJSONError(w, http.StatusBadRequest, errMessage(message))
		return
	}
	http.Redirect(w, r, "/requests?error="+urlQuery(message), http.StatusSeeOther)
}

func isFetchRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Requested-With"), "fetch") ||
		strings.Contains(r.Header.Get("Accept"), "application/json")
}

func errMessage(message string) error {
	return &plainError{message: message}
}

type plainError struct{ message string }

func (e *plainError) Error() string { return e.message }

// handleBookRequests renders the signed-in reader's requests and the
// resolution added by a manager or administrator.
func (s *Server) handleBookRequests(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, _ := s.currentUser(r)
	requests, err := s.auth.BookRequestsForUser(user.ID)
	if err != nil {
		log.Printf("List book requests for %s: %v", user.ID, err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Book requests error",
			"Title":      "Could not load your requests",
			"Message":    "The request list could not be read.",
		})
		return
	}
	views := make([]bookRequestView, 0, len(requests))
	for _, request := range requests {
		view := bookRequestView{BookRequest: request, CreatedText: request.CreatedAt.Local().Format("2006-01-02 15:04")}
		if request.Status == auth.BookRequestAdded && request.BookID != "" {
			if book := s.findBook(request.BookID); book != nil {
				view.BookTitle = book.Title
			}
		}
		views = append(views, view)
	}
	s.renderPage(w, r, http.StatusOK, "book_requests", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "My book requests",
		"Title":      "Book requests",
		"Requests":   views,
		"Saved":      r.URL.Query().Get("saved"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// handleAdminBookRequests renders every book request for managers and
// administrators, newest pending first, together with the resolve forms.
func (s *Server) handleAdminBookRequests(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	requests, err := s.auth.BookRequestsAll()
	if err != nil {
		log.Printf("List all book requests: %v", err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Book requests error",
			"Title":      "Could not load requests",
			"Message":    "The request list could not be read.",
		})
		return
	}
	views := make([]bookRequestView, 0, len(requests))
	for _, request := range requests {
		view := bookRequestView{BookRequest: request, CreatedText: request.CreatedAt.Local().Format("2006-01-02 15:04")}
		if request.Status == auth.BookRequestAdded && request.BookID != "" {
			if book := s.findBook(request.BookID); book != nil {
				view.BookTitle = book.Title
			}
		}
		views = append(views, view)
	}
	books := make([]libraryBookOption, 0, len(s.Indexer.BookList()))
	for _, book := range s.Indexer.BookList() {
		books = append(books, libraryBookOption{ID: book.ID(), Title: book.Title})
	}
	sort.Slice(books, func(i, j int) bool {
		return strings.ToLower(books[i].Title) < strings.ToLower(books[j].Title)
	})
	s.renderPage(w, r, http.StatusOK, "admin_requests", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Book requests",
		"Title":      "Book requests",
		"Requests":   views,
		"Books":      books,
		"Saved":      r.URL.Query().Get("saved"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// handleAdminBookRequestResolve records the outcome of a request: the book was
// added to the library (with the matching library book ID), or it could not
// be found (with a message the requester will see).
func (s *Server) handleAdminBookRequestResolve(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.adminRequestsRedirectError(w, r, "The resolution form could not be read.")
		return
	}
	requestID := p.ByName("id")
	switch action := r.FormValue("action"); action {
	case "added":
		bookID := strings.TrimSpace(r.FormValue("book_id"))
		if s.findBook(bookID) == nil {
			s.adminRequestsRedirectError(w, r, "Choose the library book that fulfils this request.")
			return
		}
		if err := s.auth.ResolveBookRequest(requestID, auth.BookRequestAdded, bookID, r.FormValue("message")); err != nil {
			s.adminRequestsRedirectError(w, r, err.Error())
			return
		}
	case "unavailable":
		if err := s.auth.ResolveBookRequest(requestID, auth.BookRequestUnavailable, "", r.FormValue("message")); err != nil {
			s.adminRequestsRedirectError(w, r, err.Error())
			return
		}
	default:
		s.adminRequestsRedirectError(w, r, "Choose an outcome for this request.")
		return
	}
	http.Redirect(w, r, "/admin/requests?saved=1", http.StatusSeeOther)
}

func (s *Server) adminRequestsRedirectError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin/requests?error="+urlQuery(message), http.StatusSeeOther)
}
