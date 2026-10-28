package server

import (
	"net/http"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/julienschmidt/httprouter"
)

// Only the current account identifier is exposed; authenticated pages are never
// cached as an offline library. An expired session does not erase saved books.
func (s *Server) handleOfflineContext(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok || !user.Role.Allows(auth.RoleReader) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Sign in to save books offline."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"user_id": user.ID})
}
