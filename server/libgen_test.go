package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func TestParseLibGenCurrentVGResults(t *testing.T) {
	// Current VG file rows have series, ISBNs, and badges in the title cell.
	page := `<table id="tablelibgen"><tr><th>Title</th></tr><tr>
	<td><b>Alice's adventures 1</b><br>
	  <a href="edition.php?id=123">Alice's Adventures in Wonderland <i></i></a><br>
	  <a href="edition.php?id=123"><font color="green">9781593080150</font></a>
	  <nobr><span class="badge">b</span><span>f 123</span></nobr></td>
	<td>Lewis Carroll (Author)</td><td>Macmillan</td><td>1865</td>
	<td>English</td><td>192</td><td>5 MB</td><td>epub</td>
	<td><a href="/ads.php?md5=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA">1</a></td>
	</tr></table>`
	books, err := parseLibGenResults(strings.NewReader(page), "https://libgen.vg")
	if err != nil || len(books) != 1 {
		t.Fatalf("books=%+v err=%v", books, err)
	}
	book := books[0]
	if book.Title != "Alice's Adventures in Wonderland" || book.Authors != "Lewis Carroll" {
		t.Errorf("title=%q authors=%q", book.Title, book.Authors)
	}
	if book.Mirror != "https://libgen.vg" || book.MD5 != strings.Repeat("a", 32) {
		t.Errorf("mirror=%q md5=%q", book.Mirror, book.MD5)
	}
	kind, download := book.PrimaryDownloadURL()
	if kind != "ads" || download != "https://libgen.vg/ads.php?md5="+strings.Repeat("A", 32) {
		t.Errorf("download=%q kind=%q", download, kind)
	}
}

func TestParseLibGenCompactFictionResults(t *testing.T) {
	page := `<table id="tablelibgen"><tr>
	<td><a href="fiction/123">A Christmas Carol</a></td><td>Charles Dickens</td>
	<td>500 KB</td><td>EPUB</td>
	<td><a href="ads.php?md5=11111111111111111111111111111111">1</a></td>
	</tr></table>`
	books, err := parseLibGenResults(strings.NewReader(page), "https://libgen.vg")
	if err != nil || len(books) != 1 {
		t.Fatalf("books=%+v err=%v", books, err)
	}
	book := books[0]
	if book.Title != "A Christmas Carol" || book.Size != "500 KB" || book.Extension != "epub" {
		t.Errorf("book=%+v", book)
	}
	if book.Publisher != "" || book.Year != "" || book.Language != "" || book.Pages != "" {
		t.Errorf("compact row contains spurious metadata: %+v", book)
	}
}

func libgenTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func libgenTestPage(start, count int) string {
	var page strings.Builder
	page.WriteString(`<table id="tablelibgen">`)
	for i := start; i < start+count; i++ {
		fmt.Fprintf(&page, `<tr><td><a href="edition.php?id=%d">Book %d</a></td>
		<td>Author</td><td>Publisher</td><td>2020</td><td>English</td><td>100</td>
		<td>1 MB</td><td>epub</td><td><a href="ads.php?md5=%032x">1</a></td></tr>`, i, i, i)
	}
	page.WriteString(`</table>`)
	return page.String()
}

func TestSearchLibGenMirrorsCombinesResults(t *testing.T) {
	mirrors := []string{"https://libgen.li", "https://libgen.vg", "https://unreachable.test"}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "libgen.li":
			return libgenTestResponse(http.StatusOK, libgenTestPage(1, 2)), nil
		case "libgen.vg":
			return libgenTestResponse(http.StatusOK, libgenTestPage(2, 2)), nil
		default:
			return libgenTestResponse(http.StatusBadGateway, "unreachable"), nil
		}
	})}
	books, sources, err := searchLibGenMirrors(context.Background(), client, mirrors, "book", 100)
	if err != nil || len(books) != 3 {
		t.Fatalf("books=%+v err=%v", books, err)
	}
	if sources != "https://libgen.li, https://libgen.vg" {
		t.Errorf("sources=%q", sources)
	}
	// Output and primary sources stay stable regardless of response order.
	for i, book := range books {
		if book.MD5 != fmt.Sprintf("%032x", i+1) {
			t.Errorf("book %d md5=%q", i, book.MD5)
		}
	}
	if len(books[1].Candidates) != 2 {
		t.Errorf("duplicate file lost alternate download links: %+v", books[1])
	}
	if books[2].Mirror != "https://libgen.vg" {
		t.Errorf("VG result mirror=%q", books[2].Mirror)
	}
	if kind, download := books[2].PrimaryDownloadURL(); kind != "ads" || !strings.HasPrefix(download, "https://libgen.vg/ads.php?") {
		t.Errorf("VG result download=%q kind=%q", download, kind)
	}
}

func TestSearchLibGenMirrorsPagination(t *testing.T) {
	for _, test := range []struct {
		name         string
		limit        int
		repeatPage   bool
		wantBooks    int
		wantRequests int
	}{
		{name: "four pages", limit: 100, wantBooks: 100, wantRequests: 4},
		{name: "partial last page", limit: 30, wantBooks: 30, wantRequests: 2},
		{name: "default limit", limit: 0, wantBooks: 100, wantRequests: 4},
		{name: "bounded limit", limit: 1000, wantBooks: 100, wantRequests: 4},
		{name: "mirror ignores page", limit: 100, repeatPage: true, wantBooks: 25, wantRequests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				q := r.URL.Query()
				if r.URL.Path != "/index.php" || q.Get("req") != "Alice & Carroll" {
					t.Errorf("search URL=%s", r.URL)
				}
				if strings.Join(q["topics[]"], ",") != "l,f,r" || q.Get("objects[]") != "f" || q.Get("curtab") != "f" {
					t.Errorf("search omitted book catalogs or file results: %s", r.URL)
				}
				if q.Get("page") != itoa(requests) {
					t.Errorf("page=%q want %d", q.Get("page"), requests)
				}
				start := (requests-1)*25 + 1
				if test.repeatPage {
					start = 1
				}
				// Simulate VG ignoring res and always returning 25 rows.
				return libgenTestResponse(http.StatusOK, libgenTestPage(start, 25)), nil
			})}
			books, _, err := searchLibGenMirrors(context.Background(), client, []string{"https://libgen.vg"}, "Alice & Carroll", test.limit)
			if err != nil || len(books) != test.wantBooks || requests != test.wantRequests {
				t.Fatalf("books=%d requests=%d err=%v; want %d books, %d requests", len(books), requests, err, test.wantBooks, test.wantRequests)
			}
		})
	}
}

func TestSearchLibGenMirrorsKeepsCompletedPages(t *testing.T) {
	for _, failure := range []string{"HTTP error", "deadline", "end of results"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("page") == "1" {
					return libgenTestResponse(http.StatusOK, libgenTestPage(1, 25)), nil
				}
				switch failure {
				case "HTTP error":
					return libgenTestResponse(http.StatusBadGateway, "unreachable"), nil
				case "deadline":
					<-r.Context().Done()
					return nil, r.Context().Err()
				default:
					return libgenTestResponse(http.StatusOK, libgenTestPage(1, 0)), nil
				}
			})}
			books, sources, err := searchLibGenMirrors(ctx, client, []string{"https://libgen.vg"}, "book", 100)
			if err != nil || len(books) != 25 || sources != "https://libgen.vg" {
				t.Fatalf("books=%d sources=%q err=%v", len(books), sources, err)
			}
		})
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

func TestParseLibGenNoResultsPage(t *testing.T) {
	page := `<html><body><form><input name="req" value="nothing here"></form></body></html>`
	_, err := parseLibGenResults(strings.NewReader(page), "https://libgen.vg")
	if !errors.Is(err, errLibGenNoResults) {
		t.Fatalf("err=%v want errLibGenNoResults", err)
	}
}

func TestParseLibGenBrokenPageStillFails(t *testing.T) {
	_, err := parseLibGenResults(strings.NewReader("<html><body><h1>maintenance</h1></body></html>"), "https://libgen.vg")
	if err == nil || errors.Is(err, errLibGenNoResults) {
		t.Fatalf("err=%v want generic parse error", err)
	}
}

func TestLibgenSearchQueries(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		{
			query: "Demis Hassabis: The AI Brain of Google",
			want:  []string{"Demis Hassabis: The AI Brain of Google", "Demis Hassabis"},
		},
		{
			query: "Suicidal Empathy: Dying to Be Kind",
			want:  []string{"Suicidal Empathy: Dying to Be Kind", "Suicidal Empathy"},
		},
		{
			query: "A Book (series note)",
			want:  []string{"A Book (series note)", "A Book"},
		},
		{
			query: "ISBN 9780063446533",
			want:  []string{"ISBN 9780063446533", "9780063446533"},
		},
		{
			query: "  Space   Between  ",
			want:  []string{"Space   Between"},
		},
	}
	for _, c := range cases {
		got := libgenSearchQueries(c.query)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("libgenSearchQueries(%q)=%v want %v", c.query, got, c.want)
		}
	}
}

func TestSearchLibGenBasesFallsBackToSubtitle(t *testing.T) {
	const emptyPage = `<html><body><form><input name="req" value="x"></form></body></html>`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Query().Get("req") {
		case "Demis Hassabis: The AI Brain of Google":
			return libgenTestResponse(http.StatusOK, emptyPage), nil
		case "Demis Hassabis":
			return libgenTestResponse(http.StatusOK, libgenTestPage(1, 2)), nil
		default:
			return libgenTestResponse(http.StatusOK, emptyPage), nil
		}
	})}
	books, _, used, err := searchLibGenBases(client, "Demis Hassabis: The AI Brain of Google", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("books=%d want 2", len(books))
	}
	if used != "Demis Hassabis" {
		t.Errorf("used query=%q", used)
	}
}

func TestSearchLibGenBasesNoResultsIsNotAnError(t *testing.T) {
	const emptyPage = `<html><body><form><input name="req" value="x"></form></body></html>`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return libgenTestResponse(http.StatusOK, emptyPage), nil
	})}
	books, _, _, err := searchLibGenBases(client, "nothing matches this", 100)
	if err != nil || len(books) != 0 {
		t.Fatalf("books=%d err=%v want empty and nil", len(books), err)
	}
}

func TestIsTemporaryLibGenError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("download returned HTTP 500: File not found"), true},
		{errors.New("download returned HTTP 503"), true},
		{errors.New("download stalled: no data received"), true},
		{errors.New("download returned HTTP 404"), true},
		{errors.New(`dial tcp 127.0.0.1:1: connect: connection refused`), false},
		{errors.New("unusable filename"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isTemporaryLibGenError(c.err); got != c.want {
			t.Errorf("isTemporaryLibGenError(%v)=%v want %v", c.err, got, c.want)
		}
	}
}

func TestAttemptLibGenDownloadFallsBackToNextCandidate(t *testing.T) {
	s := newAuthTestServer(t)
	epub := []byte("PK\x03\x04 fake epub bytes")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/ads.php":
			return libgenTestResponse(http.StatusOK, `<html><body><a href="get.php?md5=aaa&key=KEY">download</a></body></html>`), nil
		case r.URL.Path == "/get.php" && strings.Contains(r.URL.RawQuery, "key=KEY"):
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/epub+zip"}}, Body: io.NopCloser(bytes.NewReader(epub))}, nil
		case r.URL.Path == "/get.php":
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<h1>Error</h1><p>File not found. The repositories may not be synchronized, try downloading later</p>"))}, nil
		default:
			return libgenTestResponse(http.StatusNotFound, "missing"), nil
		}
	})}
	result := &adminLibGenResult{
		MD5: "aaa", TitleShort: "Fallback Book", Filename: "Fallback Book.epub", Extension: "epub",
		Candidates: []adminLibGenCandidate{
			{Kind: "get", URL: "https://mirror.test/get.php?md5=aaa"},
			{Kind: "ads", URL: "https://mirror.test/ads.php?md5=aaa"},
		},
	}
	dest, err := s.attemptLibGenDownload(client, result)
	if err != nil {
		t.Fatalf("attempt: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(s.BookDir, dest))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, epub) {
		t.Errorf("downloaded %q want %q", got, epub)
	}
}

func TestAttemptLibGenDownloadReportsDeadFile(t *testing.T) {
	s := newAuthTestServer(t)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<h1>Error</h1><p>File not found. The repositories may not be synchronized</p>"))}, nil
	})}
	result := &adminLibGenResult{
		MD5: "bbb", TitleShort: "Dead Book", Filename: "Dead Book.epub", Extension: "epub",
		Candidates: []adminLibGenCandidate{{Kind: "get", URL: "https://mirror.test/get.php?md5=bbb"}},
	}
	_, err := s.attemptLibGenDownload(client, result)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "File not found") {
		t.Errorf("error lost response detail: %v", err)
	}
	if !isTemporaryLibGenError(err) {
		t.Errorf("dead file should be retried later: %v", err)
	}
}

func TestParseLibGenIPFSCIDs(t *testing.T) {
	cid := "bafykbzacealjhrt6k4tz26qrh7f7fy4as6lfisgamxickl2k63ibp6fkmewmu"
	page := `<td><label>IPFS CID: </strong></label>` + cid + `</td>`
	got := parseLibGenIPFSCIDs(page)
	if len(got) != 1 || got[0] != cid {
		t.Fatalf("parse labeled CID = %v, want [%s]", got, cid)
	}
	page = `<a href="https://example.test/ipfs/` + cid + `">` + cid + `</a>`
	got = parseLibGenIPFSCIDs(page)
	if len(got) != 1 || got[0] != cid {
		t.Fatalf("parse generic CID = %v, want [%s]", got, cid)
	}
}

func TestAttemptLibGenDownloadFallsBackToIPFS(t *testing.T) {
	s := newAuthTestServer(t)
	oldGateways := libgenIPFSGateways
	libgenIPFSGateways = []string{"https://gateway.test/ipfs/"}
	defer func() { libgenIPFSGateways = oldGateways }()

	epub := []byte("PK\x03\x04 ipfs epub bytes")
	cid := "bafykbzacealjhrt6k4tz26qrh7f7fy4as6lfisgamxickl2k63ibp6fkmewmu"
	var usedIPFS bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/get.php"):
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("503 Service Temporarily Unavailable"))}, nil
		case strings.HasSuffix(r.URL.Path, "/file.php"):
			return libgenTestResponse(http.StatusOK, `<label>IPFS CID: </strong></label>`+cid), nil
		case strings.HasPrefix(r.URL.Path, "/ipfs/"):
			usedIPFS = true
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/epub+zip"}}, Body: io.NopCloser(bytes.NewReader(epub))}, nil
		default:
			return libgenTestResponse(http.StatusNotFound, "missing"), nil
		}
	})}
	result := &adminLibGenResult{
		MD5: strings.Repeat("a", 32), TitleShort: "IPFS Book", Filename: "IPFS Book.epub", Extension: "epub",
		Mirror: "https://mirror.test",
		Candidates: []adminLibGenCandidate{
			{Kind: "get", URL: "https://mirror.test/get.php?md5=" + strings.Repeat("a", 32)},
		},
	}
	dest, err := s.attemptLibGenDownload(client, result)
	if err != nil {
		t.Fatalf("attempt: %v", err)
	}
	if !usedIPFS {
		t.Fatal("expected IPFS gateway fallback")
	}
	got, err := os.ReadFile(filepath.Join(s.BookDir, dest))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, epub) {
		t.Errorf("downloaded %q want %q", got, epub)
	}
}

func TestLibgenAlternateResults(t *testing.T) {
	primary := &adminLibGenResult{
		MD5: strings.Repeat("a", 32), Title: "历史的温度：寻找历史背面的故事、热血和真性情",
		Authors: "张玮", Extension: "epub", Size: "13 MB",
	}
	byMD5 := map[string]*adminLibGenResult{
		primary.MD5:             primary,
		strings.Repeat("b", 32): {MD5: strings.Repeat("b", 32), Title: primary.Title, Authors: "张玮", Extension: "epub", Size: "15 MB"},
		strings.Repeat("c", 32): {MD5: strings.Repeat("c", 32), Title: primary.Title, Authors: "另一位作者", Extension: "epub", Size: "13 MB"},
		strings.Repeat("d", 32): {MD5: strings.Repeat("d", 32), Title: "另一本书", Authors: "张玮", Extension: "epub", Size: "13 MB"},
		strings.Repeat("e", 32): {MD5: strings.Repeat("e", 32), Title: primary.Title, Authors: "张玮", Extension: "epub", Size: "80 MB"},
		strings.Repeat("f", 32): {MD5: strings.Repeat("f", 32), Title: primary.Title, Authors: "张玮", Extension: "mobi", Size: "13 MB"},
	}
	got := libgenAlternateResults(byMD5, primary, 10)
	if len(got) != 1 || got[0].MD5 != strings.Repeat("b", 32) {
		t.Fatalf("alternates = %v, want only copy b", got)
	}
}

func TestAttemptLibGenDownloadWithAlternates(t *testing.T) {
	s := newAuthTestServer(t)
	epub := []byte("PK\x03\x04 alternate epub bytes")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.RawQuery, "primary"):
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("503 Service Temporarily Unavailable"))}, nil
		case strings.Contains(r.URL.RawQuery, "alternate"):
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/epub+zip"}}, Body: io.NopCloser(bytes.NewReader(epub))}, nil
		case strings.HasSuffix(r.URL.Path, "/file.php"):
			return libgenTestResponse(http.StatusNotFound, "missing"), nil
		default:
			return libgenTestResponse(http.StatusNotFound, "missing"), nil
		}
	})}
	primary := &adminLibGenResult{
		MD5: strings.Repeat("a", 32), Title: "Alt Book", Authors: "Author", Extension: "epub", Size: "1 MB",
		TitleShort: "Alt Book", Filename: "Alt Book.epub",
		Candidates: []adminLibGenCandidate{{Kind: "get", URL: "https://mirror.test/get.php?primary"}},
	}
	alternate := &adminLibGenResult{
		MD5: strings.Repeat("b", 32), Title: "Alt Book", Authors: "Author", Extension: "epub", Size: "1 MB",
		TitleShort: "Alt Book", Filename: "Alt Book.epub",
		Candidates: []adminLibGenCandidate{{Kind: "get", URL: "https://mirror.test/get.php?alternate"}},
	}
	dest, err := s.attemptLibGenDownloadWithAlternates(client, primary, []*adminLibGenResult{alternate})
	if err != nil {
		t.Fatalf("attempt: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(s.BookDir, dest))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, epub) {
		t.Errorf("downloaded %q want %q", got, epub)
	}
}

func TestValidateDownloadedBookRejectsHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(path, []byte("  <!DOCTYPE html><html>error</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateDownloadedBook(path); err == nil {
		t.Fatal("expected HTML error page to be rejected")
	}
	if err := os.WriteFile(path, []byte("PK\x03\x04 valid epub"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateDownloadedBook(path); err != nil {
		t.Fatalf("valid EPUB rejected: %v", err)
	}
}

func TestApplyLibGenDownloadProxy(t *testing.T) {
	t.Setenv("LIBGEN_PROXY", "")
	t.Setenv("LIBGEN_DOWNLOAD_PROXY", "http://proxy.test:3128")
	tr := &http.Transport{}
	applyLibGenDownloadProxy(tr)
	req, _ := http.NewRequest(http.MethodGet, "https://libgen.li/get.php?md5=aaa", nil)
	proxyURL, err := tr.Proxy(req)
	if err != nil || proxyURL == nil || proxyURL.Host != "proxy.test:3128" {
		t.Fatalf("HTTP proxy = %v, %v", proxyURL, err)
	}

	t.Setenv("LIBGEN_DOWNLOAD_PROXY", "socks5h://127.0.0.1:1080")
	tr = &http.Transport{}
	applyLibGenDownloadProxy(tr)
	if tr.Proxy != nil || tr.DialContext == nil {
		t.Fatalf("SOCKS proxy not installed: proxy_set=%v dial_set=%v", tr.Proxy != nil, tr.DialContext != nil)
	}
}
