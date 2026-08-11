package server

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geek1011/BookBrowser/booklist"
	"github.com/julienschmidt/httprouter"
)

type apiBook struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Author      string     `json:"author"`
	Description string     `json:"description"`
	Series      string     `json:"series"`
	SeriesIndex float64    `json:"series_index"`
	Publisher   string     `json:"publisher"`
	ISBN        string     `json:"isbn"`
	PublishDate *time.Time `json:"publish_date,omitempty"`
	Format      string     `json:"format"`
	FileSize    int64      `json:"file_size"`
	ModifiedAt  time.Time  `json:"modified_at"`
	HasCover    bool       `json:"has_cover"`
}

func newAPIBook(book *booklist.Book) apiBook {
	item := apiBook{
		ID: book.ID(), Title: book.Title, Author: book.Author,
		Description: book.Description, Series: book.Series, SeriesIndex: book.SeriesIndex,
		Publisher: book.Publisher, ISBN: book.ISBN, Format: book.FileType(),
		FileSize: book.FileSize, ModifiedAt: book.ModTime.UTC(), HasCover: book.HasCover,
	}
	if !book.PublishDate.IsZero() {
		value := book.PublishDate.UTC()
		item.PublishDate = &value
	}
	return item
}

func parsePageValue(r *http.Request, name string, defaultValue, max int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (max > 0 && value > max) {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

func containsFold(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(query)))
}

func (s *Server) handleAPIBooks(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	limit, err := parsePageValue(r, "limit", 50, 200)
	if err != nil || limit < 1 {
		writeAPIError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 200.")
		return
	}
	offset, err := parsePageValue(r, "offset", 0, 0)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_offset", "offset must be zero or greater.")
		return
	}
	query := r.URL.Query()
	q, author, series, format := query.Get("q"), query.Get("author"), query.Get("series"), strings.ToLower(strings.TrimSpace(query.Get("format")))
	books := append(booklist.BookList(nil), s.Indexer.BookList()...)
	books = books.Filtered(func(book *booklist.Book) bool {
		if q != "" && !containsFold(book.Title, q) && !containsFold(book.Author, q) && !containsFold(book.Series, q) && !containsFold(book.Description, q) {
			return false
		}
		if author != "" && !containsFold(book.Author, author) {
			return false
		}
		if series != "" && !containsFold(book.Series, series) {
			return false
		}
		return format == "" || book.FileType() == format
	})
	sortName := strings.TrimSpace(query.Get("sort"))
	if sortName == "" {
		sortName = "modified-desc"
	}
	var sorted bool
	books, sorted = books.SortBy(sortName)
	if !sorted {
		writeAPIError(w, http.StatusBadRequest, "invalid_sort", "The requested book sort is not supported.")
		return
	}
	total := len(books)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	items := make([]apiBook, 0, end-start)
	for _, book := range books[start:end] {
		items = append(items, newAPIBook(book))
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "limit": limit, "offset": offset, "total": total,
	})
}

func (s *Server) handleAPIBook(w http.ResponseWriter, _ *http.Request, p httprouter.Params) {
	book := s.findBook(p.ByName("id"))
	if book == nil {
		writeAPIError(w, http.StatusNotFound, "book_not_found", "Book not found.")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"book": newAPIBook(book)})
}

func (s *Server) handleAPIBookDownload(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	book := s.findBook(p.ByName("id"))
	if book == nil {
		writeAPIError(w, http.StatusNotFound, "book_not_found", "Book not found.")
		return
	}
	file, err := os.Open(book.FilePath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "download_failed", "The book file could not be opened.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "download_failed", "The book file could not be inspected.")
		return
	}
	name := safeUploadName(book.Title)
	if name == "" {
		name = "book-" + book.ID()
	}
	name += "." + book.FileType()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	switch book.FileType() {
	case "epub":
		w.Header().Set("Content-Type", "application/epub+zip")
	case "pdf":
		w.Header().Set("Content-Type", "application/pdf")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), file)
}
