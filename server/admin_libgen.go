package server

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/julienschmidt/httprouter"
)

type adminLibGenResult struct {
	MD5          string `json:"md5"`
	Title        string `json:"title"`
	Authors      string `json:"authors"`
	Publisher    string `json:"publisher"`
	Year         string `json:"year"`
	Size         string `json:"size"`
	Language     string `json:"language"`
	Extension    string `json:"extension"`
	Mirror       string `json:"mirror"`
	Download     string `json:"download"`
	DownloadKind string `json:"download_kind"`
	Filename     string `json:"filename"`
	TitleShort   string `json:"title_short"`
	Large        bool   `json:"large"`
}

type libgenPayload struct {
	Mirror  string              `json:"mirror"`
	Results []adminLibGenResult `json:"results"`
}

// libgenJobStatus tracks a background download started from the admin "add
// books" page. Books are downloaded asynchronously because LibGen CDNs can be
// slow (a large book may take tens of minutes), far longer than any HTTP
// request or reverse-proxy timeout.
type libgenJobStatus struct {
	MD5   string `json:"md5"`
	Title string `json:"title"`
	State string `json:"state"` // "downloading", "added", "failed"
	Error string `json:"error,omitempty"`
}

// handleAdminLibraryFind renders the "add books" page. With a q parameter it
// searches LibGen mirrors and shows selectable results.
func (s *Server) handleAdminLibraryFind(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]interface{}{
		"CurVersion": s.version,
		"PageTitle":  "Add books",
		"Title":      "Add books from LibGen",
		"Query":      query,
	}
	if query == "" {
		s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
		return
	}
	books, mirror, err := searchLibGenBases(libgenSession(), query, libgenResultCount)
	if err != nil {
		log.Printf("LibGen search %q: %v", query, err)
		data["Error"] = "The LibGen mirrors could not be reached or returned no results. Try again or check connectivity."
		s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
		return
	}
	results := make([]adminLibGenResult, 0, len(books))
	for _, b := range books {
		if !supportedBookExtension(b.Extension) {
			continue
		}
		kind, downloadURL := b.PrimaryDownloadURL()
		results = append(results, adminLibGenResult{
			MD5:          b.MD5,
			Title:        b.Title,
			Authors:      b.Authors,
			Publisher:    b.Publisher,
			Year:         b.Year,
			Size:         b.Size,
			Language:     b.Language,
			Extension:    b.Extension,
			Mirror:       mirror,
			Download:     downloadURL,
			DownloadKind: kind,
			Filename:     libGenSafeFilename(b.Title, b.MD5, b.Extension),
			TitleShort:   clipDisplay(b.Title, 90),
			Large:        libgenSizeBytes(b.Size) > libgenLargeBookBytes,
		})
	}
	if len(results) == 0 {
		log.Printf("LibGen search %q: no supported formats in %d results", query, len(books))
		data["Error"] = "The LibGen results contained no books in a supported format (epub, pdf, mobi, azw, azw3). Try a different search."
		s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
		return
	}
	payload := libgenPayload{Mirror: mirror, Results: results}
	raw, _ := json.Marshal(payload)
	data["Payload"] = base64.RawStdEncoding.EncodeToString(raw)
	data["Results"] = results
	data["Mirror"] = mirror
	s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
}

// handleAdminLibraryAddBooks queues the selected LibGen results for background
// download and returns immediately. It never blocks on the download: large
// books over slow mirrors would otherwise exceed the reverse-proxy timeout.
func (s *Server) handleAdminLibraryAddBooks(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if !s.verifyCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/library?error="+urlQuery("The selection could not be read."), http.StatusSeeOther)
		return
	}
	picked := r.Form["pick"]
	if len(picked) == 0 {
		http.Redirect(w, r, "/admin/library/add?error="+urlQuery("Select at least one book to add."), http.StatusSeeOther)
		return
	}
	raw, err := base64.RawStdEncoding.DecodeString(r.FormValue("payload"))
	if err != nil {
		http.Redirect(w, r, "/admin/library/add?error="+urlQuery("The search payload could not be read. Search again."), http.StatusSeeOther)
		return
	}
	var payload libgenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		http.Redirect(w, r, "/admin/library/add?error="+urlQuery("The search payload could not be read. Search again."), http.StatusSeeOther)
		return
	}
	byMD5 := make(map[string]*adminLibGenResult, len(payload.Results))
	for i := range payload.Results {
		byMD5[payload.Results[i].MD5] = &payload.Results[i]
	}

	var targets []string
	for _, md5 := range picked {
		if _, ok := byMD5[md5]; !ok {
			continue
		}
		s.setLibgenJob(&libgenJobStatus{MD5: md5, Title: byMD5[md5].TitleShort, State: "downloading"})
		targets = append(targets, md5)
	}
	if len(targets) == 0 {
		http.Redirect(w, r, "/admin/library/add?error="+urlQuery("Select at least one book to add."), http.StatusSeeOther)
		return
	}

	go s.runLibgenDownloads(byMD5, targets)

	msg := "Downloading " + itoa(len(targets)) + " book(s) in the background. The library will refresh automatically when they finish."
	http.Redirect(w, r, "/admin/library?saved=queued&msg="+urlQuery(msg), http.StatusSeeOther)
}

// runLibgenDownloads performs the actual downloads in a background goroutine.
// It records each book's status and refreshes the book index when at least one
// book finished successfully.
func (s *Server) runLibgenDownloads(byMD5 map[string]*adminLibGenResult, targets []string) {
	client := libgenDownloadSession()
	added := 0
	for _, md5 := range targets {
		result := byMD5[md5]
		downloadURL, err := result.DownloadURL(client)
		if err != nil {
			s.updateLibgenJob(md5, "failed", err.Error())
			log.Printf("LibGen add %q: resolve: %v", result.TitleShort, err)
			continue
		}
		destination := s.uniqueLibraryFilename(result.Filename)
		if destination == "" {
			s.updateLibgenJob(md5, "failed", "unusable filename")
			log.Printf("LibGen add %q: unusable filename", result.TitleShort)
			continue
		}
		if _, err := s.downloadLibGenBook(client, downloadURL, destination); err != nil {
			s.updateLibgenJob(md5, "failed", err.Error())
			log.Printf("LibGen add %q: download: %v", result.TitleShort, err)
			continue
		}
		s.updateLibgenJob(md5, "added", "")
		added++
		log.Printf("LibGen add %q -> %s", result.TitleShort, destination)
	}
	if added > 0 {
		s.RefreshBookIndex()
	}
}

// handleAdminLibraryAddStatus reports the state of in-flight and recent
// background downloads as JSON.
func (s *Server) handleAdminLibraryAddStatus(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeAPIJSON(w, http.StatusOK, map[string]interface{}{"jobs": s.libgenJobsSnapshot()})
}

func (s *Server) setLibgenJob(job *libgenJobStatus) {
	s.libgenJobsMu.Lock()
	defer s.libgenJobsMu.Unlock()
	if _, exists := s.libgenJobs[job.MD5]; !exists {
		s.libgenJobOrder = append(s.libgenJobOrder, job.MD5)
	}
	s.libgenJobs[job.MD5] = job
}

func (s *Server) updateLibgenJob(md5, state, errMsg string) {
	s.libgenJobsMu.Lock()
	defer s.libgenJobsMu.Unlock()
	if job, ok := s.libgenJobs[md5]; ok {
		job.State = state
		job.Error = errMsg
	}
}

func (s *Server) libgenJobsSnapshot() []libgenJobStatus {
	s.libgenJobsMu.Lock()
	defer s.libgenJobsMu.Unlock()
	out := make([]libgenJobStatus, 0, len(s.libgenJobOrder))
	for _, md5 := range s.libgenJobOrder {
		if job, ok := s.libgenJobs[md5]; ok {
			out = append(out, *job)
		}
	}
	return out
}

// uniqueLibraryFilename returns a safe, not-yet-existing filename in the
// library directory, or "" if the base name is unusable.
func (s *Server) uniqueLibraryFilename(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(filepath.Base(name), "."))
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if _, err := os.Lstat(s.filenameInBookDir(name)); os.IsNotExist(err) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		candidate := stem + " (" + itoa(n) + ")" + ext
		if _, err := os.Lstat(s.filenameInBookDir(candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *Server) filenameInBookDir(name string) string {
	return s.BookDir + string(os.PathSeparator) + name
}
