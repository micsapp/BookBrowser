package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
)

// adminLibGenCandidate is one download link for a LibGen file. Different
// mirrors often expose the same file under separate links, so a failed link
// can fall back to the next candidate.
type adminLibGenCandidate struct {
	Kind string `json:"kind"` // "get" or "ads"
	URL  string `json:"url"`
}

type adminLibGenResult struct {
	MD5          string                 `json:"md5"`
	Title        string                 `json:"title"`
	Authors      string                 `json:"authors"`
	Publisher    string                 `json:"publisher"`
	Year         string                 `json:"year"`
	Size         string                 `json:"size"`
	Language     string                 `json:"language"`
	Extension    string                 `json:"extension"`
	Mirror       string                 `json:"mirror"`
	Download     string                 `json:"download"`
	DownloadKind string                 `json:"download_kind"`
	Candidates   []adminLibGenCandidate `json:"candidates,omitempty"`
	Filename     string                 `json:"filename"`
	TitleShort   string                 `json:"title_short"`
	Large        bool                   `json:"large"`
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
	books, mirror, searchedAs, err := searchLibGenBases(libgenSession(), query, libgenResultCount)
	if err != nil {
		log.Printf("LibGen search %q: %v", query, err)
		data["Error"] = "The LibGen mirrors could not be reached. Try again or check connectivity."
		s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
		return
	}
	if len(books) == 0 {
		log.Printf("LibGen search %q: no results", query)
		data["Error"] = fmt.Sprintf("No LibGen results for %q. LibGen matches every word against title, author, and publisher, so try fewer or different words.", query)
		s.renderPage(w, r, http.StatusOK, "admin_library_add", data)
		return
	}
	if searchedAs != "" && !strings.EqualFold(searchedAs, query) {
		data["QueryUsed"] = searchedAs
	}
	results := make([]adminLibGenResult, 0, len(books))
	for _, b := range books {
		if !supportedBookExtension(b.Extension) {
			continue
		}
		kind, downloadURL := b.PrimaryDownloadURL()
		candidates := make([]adminLibGenCandidate, 0, len(b.Candidates))
		for _, candidate := range b.Candidates {
			candidates = append(candidates, adminLibGenCandidate{Kind: candidate[0], URL: candidate[1]})
		}
		results = append(results, adminLibGenResult{
			MD5:          b.MD5,
			Title:        b.Title,
			Authors:      b.Authors,
			Publisher:    b.Publisher,
			Year:         b.Year,
			Size:         b.Size,
			Language:     b.Language,
			Extension:    b.Extension,
			Mirror:       b.Mirror,
			Download:     downloadURL,
			DownloadKind: kind,
			Candidates:   candidates,
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

// CandidateList returns every download link for a result, preferring direct
// "get" links and falling back to legacy single-link payloads.
func (r *adminLibGenResult) CandidateList() []adminLibGenCandidate {
	if len(r.Candidates) > 0 {
		return r.Candidates
	}
	if r.Download != "" {
		kind := r.DownloadKind
		if kind == "" {
			kind = "get"
		}
		return []adminLibGenCandidate{{Kind: kind, URL: r.Download}}
	}
	return nil
}

// attemptLibGenDownload tries every candidate link for a book. Dead LibGen CDN
// files and per-mirror failures fall through to the next link instead of
// failing the job on the first HTTP 500.
func (s *Server) attemptLibGenDownload(client *http.Client, result *adminLibGenResult) (string, error) {
	candidates := result.CandidateList()
	if len(candidates) == 0 {
		return "", errors.New("no usable download link found")
	}

	installedName := result.Filename
	if isMobiFilename(installedName) {
		installedName = strings.TrimSuffix(installedName, filepath.Ext(installedName)) + ".epub"
	}
	destination := s.uniqueLibraryFilename(installedName)
	if destination == "" {
		return "", errors.New("unusable filename")
	}

	// Direct links first; ads pages need an extra round trip to obtain a key.
	ordered := make([]adminLibGenCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Kind == "get" {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range candidates {
		if candidate.Kind == "ads" {
			ordered = append(ordered, candidate)
		}
	}

	var lastErr error
	tried := make(map[string]bool)
	for _, candidate := range ordered {
		url := candidate.URL
		if candidate.Kind == "ads" {
			resolved, err := resolveLibGenAdsURL(client, candidate.URL)
			if err != nil {
				lastErr = fmt.Errorf("resolve %s: %w", candidate.URL, err)
				continue
			}
			url = resolved
		}
		if url == "" || tried[url] {
			continue
		}
		tried[url] = true
		if _, err := s.downloadLibGenBook(client, url, destination, isMobiFilename(result.Filename), filepath.Ext(result.Filename)); err != nil {
			lastErr = err
			log.Printf("LibGen add %q: %s link %s failed: %v", result.TitleShort, candidate.Kind, url, err)
			continue
		}
		return destination, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no usable download link found")
	}
	return "", lastErr
}

// runLibgenDownloads performs the actual downloads in a background goroutine.
// It records each book's status and refreshes the book index when at least one
// book finished successfully.
func (s *Server) runLibgenDownloads(byMD5 map[string]*adminLibGenResult, targets []string) {
	s.runLibgenDownloadsAttempt(byMD5, targets, 0)
}

func (s *Server) runLibgenDownloadsAttempt(byMD5 map[string]*adminLibGenResult, targets []string, attempt int) {
	client := libgenDownloadSession()
	added := 0
	var retry []string
	for _, md5 := range targets {
		result := byMD5[md5]
		destination, err := s.attemptLibGenDownload(client, result)
		if err != nil {
			if attempt < libgenMaxDownloadRetries && isTemporaryLibGenError(err) {
				s.updateLibgenJob(md5, "retrying", err.Error())
				log.Printf("LibGen add %q: temporary failure (try %d/%d): %v", result.TitleShort, attempt+1, libgenMaxDownloadRetries, err)
				retry = append(retry, md5)
				continue
			}
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
	if len(retry) > 0 {
		delay := libgenDownloadRetryDelay * time.Duration(attempt+1)
		log.Printf("LibGen retry: %d book(s) in %s", len(retry), delay)
		go func() {
			time.Sleep(delay)
			s.runLibgenDownloadsAttempt(byMD5, retry, attempt+1)
		}()
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
