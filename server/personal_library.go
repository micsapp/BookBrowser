package server

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/geek1011/BookBrowser/auth"
	"github.com/geek1011/BookBrowser/booklist"
	"github.com/julienschmidt/httprouter"
)

func (s *Server) handleReadBook(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	book := s.findBook(p.ByName("id"))
	if book == nil {
		s.renderPage(w, r, http.StatusNotFound, "notfound", map[string]interface{}{
			"CurVersion": s.version,
			"PageTitle":  "Not Found",
			"Title":      "Not Found",
			"Message":    "Book not found.",
		})
		return
	}
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return
	}
	if err := s.auth.RecordBookRead(user.ID, book.ID()); err != nil {
		log.Printf("Record recent read for %s: %v", user.ID, err)
	}
	var destination string
	switch book.FileType() {
	case "epub":
		destination = fmt.Sprintf("/static/reader/epub/#!/download/%s.%s", book.ID(), book.FileType())
	case "pdf":
		destination = fmt.Sprintf("/static/reader/pdf/web/viewer.html?file=/download/%s.%s", book.ID(), book.FileType())
	default:
		destination = "/books/" + url.PathEscape(book.ID())
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (s *Server) handleMyLibrary(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return
	}
	recentIDs, recentErr := s.auth.RecentBookIDs(user.ID, 12)
	lists, listsErr := s.auth.BookListsForUser(user.ID)
	tags, tagsErr := s.auth.TagsForUser(user.ID)
	if recentErr != nil || listsErr != nil || tagsErr != nil {
		log.Printf("Read private library for %s: recent=%v lists=%v tags=%v", user.ID, recentErr, listsErr, tagsErr)
	}
	s.renderPage(w, r, http.StatusOK, "my_library", map[string]interface{}{
		"CurVersion":       s.version,
		"PageTitle":        "My Library",
		"Title":            "My Library",
		"ShowViewSelector": false,
		"RecentBooks":      s.booksByIDs(recentIDs),
		"BookLists":        lists,
		"BookTags":         tags,
		"Saved":            r.URL.Query().Get("saved"),
		"Error":            r.URL.Query().Get("error"),
	})
}

func (s *Server) handleCreateBookList(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	list, err := s.auth.CreateBookList(user.ID, r.FormValue("name"))
	if err != nil {
		s.redirectPrivateLibraryError(w, r, "/my-library", err)
		return
	}
	http.Redirect(w, r, "/my-library/lists/"+url.PathEscape(list.ID)+"?saved=created", http.StatusSeeOther)
}

func (s *Server) handleBookList(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return
	}
	list, err := s.auth.BookListForUser(user.ID, p.ByName("id"))
	if err != nil {
		s.renderPrivateCollectionNotFound(w, r, "That book list does not exist.")
		return
	}
	ids, err := s.auth.BookIDsForList(user.ID, list.ID)
	if err != nil {
		log.Printf("Read book list %s for %s: %v", list.ID, user.ID, err)
		s.redirectPrivateLibraryError(w, r, "/my-library", err)
		return
	}
	s.renderPage(w, r, http.StatusOK, "book_collection", map[string]interface{}{
		"CurVersion":       s.version,
		"PageTitle":        list.Name,
		"Title":            list.Name,
		"CollectionKind":   "list",
		"BookList":         list,
		"Books":            s.booksByIDs(ids),
		"ShowViewSelector": false,
		"Saved":            r.URL.Query().Get("saved"),
		"Error":            r.URL.Query().Get("error"),
	})
}

func (s *Server) handleDeleteBookList(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	if err := s.auth.DeleteBookList(user.ID, p.ByName("id")); err != nil {
		s.redirectPrivateLibraryError(w, r, "/my-library", err)
		return
	}
	http.Redirect(w, r, "/my-library?saved=list-deleted", http.StatusSeeOther)
}

func (s *Server) handleAddBookToList(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	s.changeBookListMembership(w, r, p, true)
}

func (s *Server) handleRemoveBookFromList(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	s.changeBookListMembership(w, r, p, false)
}

func (s *Server) changeBookListMembership(w http.ResponseWriter, r *http.Request, p httprouter.Params, add bool) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	bookID := p.ByName("book_id")
	destination := privateLibraryNext(r, "/books/"+url.PathEscape(bookID))
	if s.findBook(bookID) == nil {
		s.redirectPrivateLibraryError(w, r, destination, fmt.Errorf("book not found"))
		return
	}
	var err error
	if add {
		err = s.auth.AddBookToList(user.ID, p.ByName("id"), bookID)
	} else {
		err = s.auth.RemoveBookFromList(user.ID, p.ByName("id"), bookID)
	}
	if err != nil {
		s.redirectPrivateLibraryError(w, r, destination, err)
		return
	}
	status := "list-added"
	if !add {
		status = "list-removed"
	}
	http.Redirect(w, r, addQuery(destination, "saved", status), http.StatusSeeOther)
}

func (s *Server) handleAddBookTag(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	s.changeBookTag(w, r, p, true)
}

func (s *Server) handleRemoveBookTag(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	s.changeBookTag(w, r, p, false)
}

func (s *Server) changeBookTag(w http.ResponseWriter, r *http.Request, p httprouter.Params, add bool) {
	user, ok := s.privateLibraryPostUser(w, r)
	if !ok {
		return
	}
	bookID := p.ByName("id")
	destination := privateLibraryNext(r, "/books/"+url.PathEscape(bookID))
	if s.findBook(bookID) == nil {
		s.redirectPrivateLibraryError(w, r, destination, fmt.Errorf("book not found"))
		return
	}
	var err error
	if add {
		err = s.auth.AddBookTag(user.ID, bookID, r.FormValue("tag"))
	} else {
		err = s.auth.RemoveBookTag(user.ID, bookID, r.FormValue("tag"))
	}
	if err != nil {
		s.redirectPrivateLibraryError(w, r, destination, err)
		return
	}
	status := "tag-added"
	if !add {
		status = "tag-removed"
	}
	http.Redirect(w, r, addQuery(destination, "saved", status), http.StatusSeeOther)
}

func (s *Server) handleTaggedBooks(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return
	}
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	ids, err := s.auth.BookIDsForTag(user.ID, tag)
	if err != nil {
		s.redirectPrivateLibraryError(w, r, "/my-library", err)
		return
	}
	s.renderPage(w, r, http.StatusOK, "book_collection", map[string]interface{}{
		"CurVersion":       s.version,
		"PageTitle":        "Tag: " + tag,
		"Title":            "Tag: " + tag,
		"CollectionKind":   "tag",
		"Tag":              tag,
		"Books":            s.booksByIDs(ids),
		"ShowViewSelector": false,
	})
}

func (s *Server) privateLibraryPostUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return nil, false
	}
	user, ok := s.currentUser(r)
	if !ok {
		s.redirectToLogin(w, r)
		return nil, false
	}
	return user, true
}

func (s *Server) booksByIDs(ids []string) booklist.BookList {
	books := make(map[string]*booklist.Book, len(s.Indexer.BookList()))
	for _, book := range s.Indexer.BookList() {
		books[book.ID()] = book
	}
	result := make(booklist.BookList, 0, len(ids))
	for _, id := range ids {
		if book := books[id]; book != nil {
			result = append(result, book)
		}
	}
	return result
}

func (s *Server) redirectPrivateLibraryError(w http.ResponseWriter, r *http.Request, destination string, err error) {
	log.Printf("Private library change: %v", err)
	http.Redirect(w, r, addQuery(destination, "error", err.Error()), http.StatusSeeOther)
}

func (s *Server) renderPrivateCollectionNotFound(w http.ResponseWriter, r *http.Request, message string) {
	s.renderPage(w, r, http.StatusNotFound, "notfound", map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Not Found",
		"Title":      "Not Found",
		"Message":    message,
	})
}

func privateLibraryNext(r *http.Request, fallback string) string {
	if next := safeNextValue(r.FormValue("next")); next != "" {
		return next
	}
	return fallback
}

func addQuery(destination, key, value string) string {
	separator := "?"
	if strings.Contains(destination, "?") {
		separator = "&"
	}
	return destination + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}
