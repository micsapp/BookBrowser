package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const sampleLibGenPage = `<!DOCTYPE html>
<html><body>
<table id="tablelibgen">
<tr>
  <td><a href="edition.php?id=1">The Hidden Sea</a></td>
  <td>Ursula K. Le Guin (Author)</td>
  <td>Gollancz</td>
  <td>1972</td>
  <td>English</td>
  <td>305</td>
  <td>2.4 MB</td>
  <td>EPUB</td>
  <td><a href="ads.php?md5=11111111111111111111111111111111">ads</a><a href="get.php?md5=11111111111111111111111111111111">get</a></td>
</tr>
<tr>
  <td><a href="edition.php?id=2">A <font>C</font> Wizard of Earthsea <font>c</font></a></td>
  <td>Ursula K. Le Guin</td>
  <td>Puffin</td>
  <td>1968</td>
  <td>English</td>
  <td>197</td>
  <td>1.1 MB</td>
  <td>PDF</td>
  <td><a href="get.php?md5=22222222222222222222222222222222">get</a></td>
</tr>
<tr>
  <td><a href="edition.php?id=3">Today I Learned</a></td>
  <td>Author X</td>
  <td>Publisher</td>
  <td>2020</td>
  <td>English</td>
  <td>100</td>
  <td>500 KB</td>
  <td>DJVU</td>
  <td><a href="get.php?md5=33333333333333333333333333333333">get</a></td>
</tr>
</table>
</body></html>`

func TestParseLibGenResults(t *testing.T) {
	books, err := parseLibGenResults(strings.NewReader(sampleLibGenPage), "https://libgen.vg")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 3 {
		t.Fatalf("expected 3 books, got %d", len(books))
	}
	first := books[0]
	if first.Title != "The Hidden Sea" {
		t.Errorf("title=%q want %q", first.Title, "The Hidden Sea")
	}
	if first.MD5 != "11111111111111111111111111111111" {
		t.Errorf("md5=%q", first.MD5)
	}
	if first.Authors != "Ursula K. Le Guin" {
		t.Errorf("authors=%q", first.Authors)
	}
	if first.Extension != "epub" {
		t.Errorf("extension=%q", first.Extension)
	}
	kind, url := first.PrimaryDownloadURL()
	if kind != "get" || !strings.HasPrefix(url, "https://libgen.vg/get.php") {
		t.Errorf("primary url=%q kind=%q", url, kind)
	}
	if len(first.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(first.Candidates))
	}
}

func TestParseLibGenResultsEmptyTable(t *testing.T) {
	_, err := parseLibGenResults(strings.NewReader("<html><body><p>nothing</p></body></html>"), "https://libgen.vg")
	if err == nil {
		t.Fatal("expected error when the results table is missing")
	}
}

func TestLibGenSafeFilename(t *testing.T) {
	cases := []struct {
		title, md5, ext, want string
	}{
		{`The "Hidden" Sea / Part 1`, "aabbcc", "epub", "The _Hidden_ Sea _ Part 1.epub"},
		{"Fénix", "aabbcc", "pdf", "Fénix.pdf"},
		{"", "aabbcc", "MOBI", "aabbcc.mobi"},
		{"  lots   of   spaces   ", "aabbcc", "azw3", "lots of spaces.azw3"},
	}
	for _, c := range cases {
		got := libGenSafeFilename(c.title, c.md5, c.ext)
		if got != c.want {
			t.Errorf("libGenSafeFilename(%q,%q,%q)=%q want %q", c.title, c.md5, c.ext, got, c.want)
		}
	}
}

func TestUniqueLibraryFilename(t *testing.T) {
	s := newAuthTestServer(t)
	name := s.uniqueLibraryFilename("book.epub")
	if name != "book.epub" {
		t.Fatalf("first=%q", name)
	}
	if _, _, err := s.storeUploadedBook("book.epub", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	name = s.uniqueLibraryFilename("book.epub")
	if name != "book (2).epub" {
		t.Fatalf("second=%q", name)
	}
	if name = s.uniqueLibraryFilename("../evil"); name != "evil" {
		t.Fatalf("traversal name=%q want %q", name, "evil")
	}
}

func TestLibGenSizeBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"419 MB", 419 << 20},
		{"2.4 MB", 2516582},
		{"500 KB", 500 << 10},
		{"1.5 GB", 1610612736},
		{"", 0},
		{"n/a", 0},
		{"12 MiB", 12 << 20},
	}
	for _, c := range cases {
		if got := libgenSizeBytes(c.in); got != c.want {
			t.Errorf("libgenSizeBytes(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestLibGenResultDownloadURLKinds(t *testing.T) {
	get := adminLibGenResult{DownloadKind: "get", Download: "https://libgen.vg/get.php?md5=x"}
	if u, err := get.DownloadURL(libgenSession()); err != nil || u != "https://libgen.vg/get.php?md5=x" {
		t.Errorf("get URL=%q err=%v", u, err)
	}
	none := adminLibGenResult{DownloadKind: "other"}
	if _, err := none.DownloadURL(libgenSession()); err == nil {
		t.Error("expected error for unsupported download kind")
	}
}

// slowReader yields one byte per `per` interval so a stall timeout is the only
// thing that can bound the copy.
type slowReader struct {
	remaining int
	per       time.Duration
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	time.Sleep(r.per)
	p[0] = 'x'
	r.remaining--
	return 1, nil
}

func TestCopyWithStallDetection(t *testing.T) {
	var buf bytes.Buffer
	// Progressing reader must complete even if total time is long.
	n, err := copyWithStallDetection(&buf, &slowReader{remaining: 1000, per: time.Microsecond}, 2*time.Second, 1<<20)
	if err != nil {
		t.Fatalf("progressing copy: n=%d err=%v", n, err)
	}
	if n != 1000 || buf.Len() != 1000 {
		t.Fatalf("copied n=%d len=%d", n, buf.Len())
	}

	// A reader that never yields must trip the stall timeout.
	stalled := &stuckReader{}
	n, err = copyWithStallDetection(&bytes.Buffer{}, stalled, 20*time.Millisecond, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stalled copy: n=%d err=%v", n, err)
	}

	// Limit caps the copy without error.
	n, err = copyWithStallDetection(&buf, &slowReader{remaining: 100000, per: time.Microsecond}, 2*time.Second, 10)
	if err != nil {
		t.Fatalf("limited copy: n=%d err=%v", n, err)
	}
	if n != 10 {
		t.Fatalf("limited copy n=%d want 10", n)
	}
}

// stuckReader never returns data or EOF.
type stuckReader struct{}

func (r *stuckReader) Read(p []byte) (int, error) {
	time.Sleep(time.Second)
	return 0, nil
}

func (r *stuckReader) Close() error { return nil }

func TestAdminLibraryFindPageRenders(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("libgen-admin@example.com", "LibGen Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	page := requestServer(s, http.MethodGet, "/login", nil)
	csrf := cookieNamed(page, csrfCookieName)
	form := url.Values{"csrf_token": {csrf.Value}, "email": {admin.Email}, "password": {"correct horse battery staple"}}
	login := requestServer(s, http.MethodPost, "/login", form, csrf)
	session := cookieNamed(login, sessionCookieName)
	if session == nil {
		t.Fatalf("login failed: status=%d", login.Code)
	}
	add := requestServer(s, http.MethodGet, "/admin/library/add", nil, session, csrf)
	if add.Code != http.StatusOK {
		t.Fatalf("add page status=%d body=%s", add.Code, add.Body.String())
	}
	body := add.Body.String()
	for _, expected := range []string{
		`action="/admin/library/add"`,
		"Search LibGen mirrors",
		"Rescan library",
		"Add books",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("add page missing %q", expected)
		}
	}
}

func TestAdminLibraryAddBooksQueuesAsync(t *testing.T) {
	s := newAuthTestServer(t)
	admin, err := s.auth.RegisterEmail("libgen-queue@example.com", "LibGen Queue", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	page := requestServer(s, http.MethodGet, "/login", nil)
	csrf := cookieNamed(page, csrfCookieName)
	form := url.Values{"csrf_token": {csrf.Value}, "email": {admin.Email}, "password": {"correct horse battery staple"}}
	login := requestServer(s, http.MethodPost, "/login", form, csrf)
	session := cookieNamed(login, sessionCookieName)
	if session == nil {
		t.Fatalf("login failed: status=%d", login.Code)
	}

	// Craft a payload with two results pointing at an unroutable download host
	// so the background goroutine fails quickly without network access.
	results := []adminLibGenResult{
		{MD5: strings.Repeat("a", 32), Title: "Queued One", TitleShort: "Queued One", Extension: "epub",
			DownloadKind: "get", Download: "http://127.0.0.1:1/get.php", Filename: "Queued One.epub"},
		{MD5: strings.Repeat("b", 32), Title: "Queued Two", TitleShort: "Queued Two", Extension: "epub",
			DownloadKind: "get", Download: "http://127.0.0.1:1/get.php", Filename: "Queued Two.epub"},
	}
	raw, _ := json.Marshal(libgenPayload{Mirror: "https://libgen.li", Results: results})
	payload := base64.RawStdEncoding.EncodeToString(raw)

	addForm := url.Values{
		"csrf_token": {csrf.Value},
		"payload":    {payload},
		"pick":       {strings.Repeat("a", 32), strings.Repeat("b", 32)},
	}
	resp := requestServer(s, http.MethodPost, "/admin/library/add", addForm, session, csrf)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("add status=%d body=%s", resp.Code, resp.Body.String())
	}
	loc := resp.Header().Get("Location")
	if !strings.Contains(loc, "saved=queued") {
		t.Fatalf("redirect location=%q want saved=queued", loc)
	}

	// The status endpoint should report the jobs (downloading, then failed).
	var status struct {
		Jobs []libgenJobStatus `json:"jobs"`
	}
	for i := 0; i < 50; i++ {
		st := requestServer(s, http.MethodGet, "/admin/library/add/status", nil, session, csrf)
		decodeAPIResponse(t, st, &status)
		if len(status.Jobs) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(status.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(status.Jobs))
	}
	// Wait for the background goroutines to fail and mark their state.
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := requestServer(s, http.MethodGet, "/admin/library/add/status", nil, session, csrf)
		decodeAPIResponse(t, st, &status)
		done := true
		for _, j := range status.Jobs {
			if j.State == "downloading" {
				done = false
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs did not finish: %+v", status.Jobs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, j := range status.Jobs {
		if j.State != "failed" {
			t.Errorf("job %q state=%q want failed", j.MD5, j.State)
		}
	}
}
