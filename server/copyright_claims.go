package server

import (
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

type copyrightClaimContextResponse struct {
	CSRFToken string `json:"csrf_token"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
}

type copyrightClaimView struct {
	auth.CopyrightClaim
	CreatedText string
	BookExists  bool
}

// handleCopyrightClaimContext gives both anonymous and signed-in reader
// surfaces a CSRF token. It also pre-fills contact details for signed-in users.
func (s *Server) handleCopyrightClaimContext(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	response := copyrightClaimContextResponse{CSRFToken: s.csrfToken(w, r)}
	if user, ok := s.currentUser(r); ok {
		response.Name = user.DisplayName
		if response.Name == "" {
			response.Name = user.Name
		}
		response.Email = user.Email
	}
	writeJSON(w, http.StatusOK, response)
}

// handleCreateCopyrightClaim accepts public copyright concerns. The hidden
// website field is a honeypot; bots receive a normal success response without
// creating administrative work.
func (s *Server) handleCreateCopyrightClaim(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	err := r.ParseMultipartForm(64 << 10)
	if errors.Is(err, http.ErrNotMultipart) {
		err = r.ParseForm()
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, errMessage("The report form could not be read."))
		return
	}
	if !s.verifyCSRF(r) {
		writeJSONError(w, http.StatusForbidden, errMessage("This form expired. Refresh the page and try again."))
		return
	}
	if strings.TrimSpace(r.FormValue("company_website")) != "" {
		writeJSON(w, http.StatusCreated, map[string]interface{}{"ok": true})
		return
	}
	if r.FormValue("authorized") != "on" && r.FormValue("authorized") != "true" {
		writeJSONError(w, http.StatusBadRequest, errMessage("Confirm that the information is accurate and you are authorized to submit it."))
		return
	}
	userID := ""
	name := r.FormValue("claimant_name")
	email := r.FormValue("claimant_email")
	if user, ok := s.currentUser(r); ok {
		userID = user.ID
		if strings.TrimSpace(name) == "" {
			name = user.DisplayName
			if name == "" {
				name = user.Name
			}
		}
		if strings.TrimSpace(email) == "" {
			email = user.Email
		}
	}
	claim, err := s.auth.CreateCopyrightClaim(
		userID, name, email, r.FormValue("book_id"), r.FormValue("book_title"),
		r.FormValue("source_url"), r.FormValue("details"),
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	log.Printf("Copyright claim %s from %s for %q", claim.ID, claim.ClaimantEmail, claim.BookTitle)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"ok": true, "id": claim.ID})
}

func (s *Server) handleAdminCopyrightClaims(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	claims, err := s.auth.CopyrightClaimsAll()
	if err != nil {
		log.Printf("List copyright claims: %v", err)
		s.renderPage(w, r, http.StatusInternalServerError, "message", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Copyright claims error",
			"Title":      "Could not load copyright claims",
			"Message":    "The claim list could not be read.",
		})
		return
	}
	views := make([]copyrightClaimView, 0, len(claims))
	for _, claim := range claims {
		views = append(views, copyrightClaimView{
			CopyrightClaim: claim,
			CreatedText:    claim.CreatedAt.Local().Format("2006-01-02 15:04"),
			BookExists:     claim.BookID != "" && s.findBook(claim.BookID) != nil,
		})
	}
	books := make([]libraryBookOption, 0, len(s.Indexer.BookList()))
	for _, book := range s.Indexer.BookList() {
		books = append(books, libraryBookOption{ID: book.ID(), Title: book.Title})
	}
	sort.Slice(books, func(i, j int) bool { return strings.ToLower(books[i].Title) < strings.ToLower(books[j].Title) })
	s.renderPage(w, r, http.StatusOK, "admin_copyright_claims", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Copyright claims",
		"Title":      "",
		"Claims":     views,
		"Books":      books,
		"Saved":      r.URL.Query().Get("saved"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleAdminCopyrightClaimResolve(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.adminCopyrightRedirectError(w, r, "The resolution form could not be read.")
		return
	}
	claimID := p.ByName("id")
	claim, err := s.auth.CopyrightClaimByID(claimID)
	if err != nil || claim.Status != auth.CopyrightClaimPending {
		s.adminCopyrightRedirectError(w, r, "The copyright claim was not found or has already been resolved.")
		return
	}
	bookID := strings.TrimSpace(r.FormValue("book_id"))
	resolution := strings.TrimSpace(r.FormValue("resolution"))
	switch r.FormValue("action") {
	case "remove":
		if s.findBook(bookID) == nil {
			s.adminCopyrightRedirectError(w, r, "Choose the library book to remove.")
			return
		}
		if _, err := s.removeLibraryBook(bookID); err != nil {
			s.adminCopyrightRedirectError(w, r, err.Error())
			return
		}
		if resolution == "" {
			resolution = "Reported book moved to recoverable storage."
		}
		if err := s.auth.ResolveCopyrightClaim(claimID, auth.CopyrightClaimRemoved, bookID, resolution); err != nil {
			log.Printf("Resolve copyright claim %s after removing book %s: %v", claimID, bookID, err)
			s.adminCopyrightRedirectError(w, r, err.Error())
			return
		}
		go s.RefreshBookIndex()
	case "resolve":
		if err := s.auth.ResolveCopyrightClaim(claimID, auth.CopyrightClaimResolved, bookID, resolution); err != nil {
			s.adminCopyrightRedirectError(w, r, err.Error())
			return
		}
	default:
		s.adminCopyrightRedirectError(w, r, "Choose an outcome for this claim.")
		return
	}
	http.Redirect(w, r, "/admin/copyright?saved=1", http.StatusSeeOther)
}

func (s *Server) adminCopyrightRedirectError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin/copyright?error="+urlQuery(message), http.StatusSeeOther)
}
