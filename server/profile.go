package server

import (
	"log"
	"net/http"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

// handleProfile shows the signed-in user's basic account information and lets
// them save optional profile details (display name, about, location). The
// account fields come from the session user, which the auth store refreshes
// after the save redirect.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok || !user.Role.Allows(auth.RoleReader) {
		s.redirectToLogin(w, r)
		return
	}
	if r.Method == http.MethodPost {
		if !s.verifyCSRF(r) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/profile?error="+urlQuery("The profile form could not be read."), http.StatusSeeOther)
			return
		}
		if err := s.auth.UpdateProfile(user.ID, r.FormValue("display_name"), r.FormValue("bio"), r.FormValue("location")); err != nil {
			log.Printf("Update profile for %s: %v", user.ID, err)
			http.Redirect(w, r, "/profile?error="+urlQuery(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/profile?saved=1", http.StatusSeeOther)
		return
	}
	s.renderPage(w, r, http.StatusOK, "profile", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Profile",
		"Title":      "Profile",
		"Saved":      r.URL.Query().Get("saved") == "1",
		"Error":      r.URL.Query().Get("error"),
	})
}
