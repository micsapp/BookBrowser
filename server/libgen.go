package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var libgenMirrors = []string{
	"https://libgen.li",
	"https://libgen.vg",
	"https://libgen.rs",
	"https://libgen.is",
	"https://libgen.st",
}

var libgenUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

const (
	libgenSearchRetries   = 2
	libgenRetrySleep      = 1 * time.Second
	libgenSearchDeadline  = 25 * time.Second
	libgenResultCount     = 25
	libgenMaxDLBytes      = int64(1) << 30
	libgenDownloadStall   = 30 * time.Second
	libgenDownloadDial    = 30 * time.Second
	libgenDownloadRespHdr = 45 * time.Second
)

var libgenMD5Pattern = regexp.MustCompile(`(?i)md5=([0-9a-f]{32})`)
var libgenCWord = regexp.MustCompile(`\s*c\s*`)
var libgenSizePattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(KB|MB|GB|KiB|MiB|GiB|B)?$`)

// libgenLargeBookBytes is the threshold above which the admin UI flags a
// search result as a large file that may be slow to load in the reader.
const libgenLargeBookBytes = int64(50 << 20)

type libgenBook struct {
	MD5        string
	Title      string
	Authors    string
	Publisher  string
	Year       string
	Language   string
	Pages      string
	Size       string
	Extension  string
	Candidates [][2]string // ("get"|"ads", url)
}

// libgenTLS returns a TLS config that skips certificate verification for the
// LibGen mirror clients. LibGen is an unofficial network of mirrors that is
// inherently untrusted and whose certificates routinely expire or break while
// the mirrors stay up (e.g. 2026-08-12). Without this, an expired cert makes
// every search and download fail. Only LibGen search/ads/download traffic uses
// this config.
func libgenTLS() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, // #nosec G402 -- LibGen mirrors only; see above
	}
}

func libgenSession() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: libgenTLS(),
		},
	}
}

// libgenDownloadSession returns a client for downloading book files. It has
// no overall timeout: LibGen CDNs can be very slow (see the TUI's STALL_LIMIT
// behaviour), so a slow-but-progressing transfer must be allowed to finish.
// Liveness is enforced per-read via a stall timeout in downloadLibGenBook.
func libgenDownloadSession() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   libgenDownloadDial,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSClientConfig:       libgenTLS(),
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: libgenDownloadRespHdr,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

func libgenRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", libgenUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// searchLibGenBases searches the configured LibGen mirrors concurrently and
// returns the first mirror that yields results. Mirrors are flaky, so the
// whole set is retried a few times under a hard deadline.
func searchLibGenBases(client *http.Client, query string, res int) ([]libgenBook, string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, "", errors.New("empty query")
	}
	if res <= 0 {
		res = libgenResultCount
	}

	ctx, cancel := context.WithTimeout(context.Background(), libgenSearchDeadline)
	defer cancel()

	type mirrorResult struct {
		books []libgenBook
		base  string
		err   error
	}

	// Best-effort return once any mirror responds with results; otherwise
	// report the last error after all mirrors fail.
	var lastErr error
	for attempt := 0; attempt < libgenSearchRetries; attempt++ {
		results := make(chan mirrorResult, len(libgenMirrors))
		for _, base := range libgenMirrors {
			base := base
			go func() {
				reqCtx, cancelReq := context.WithTimeout(ctx, libgenSearchDeadline)
				defer cancelReq()
				books, err := searchLibGenMirrorCtx(reqCtx, client, base, query, res)
				results <- mirrorResult{books, base, err}
			}()
		}
		for i := 0; i < len(libgenMirrors); i++ {
			select {
			case <-ctx.Done():
				if lastErr == nil {
					lastErr = errors.New("search timed out (mirrors may be unreachable)")
				}
				return nil, "", lastErr
			case r := <-results:
				if r.err == nil && len(r.books) > 0 {
					return r.books, r.base, nil
				}
				if r.err != nil {
					lastErr = r.err
				}
			}
		}
		if attempt < libgenSearchRetries-1 {
			select {
			case <-ctx.Done():
				if lastErr == nil {
					lastErr = errors.New("search timed out (mirrors may be unreachable)")
				}
				return nil, "", lastErr
			case <-time.After(libgenRetrySleep):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no results (mirrors may be unreachable)")
	}
	return nil, "", lastErr
}

func searchLibGenMirrorCtx(ctx context.Context, client *http.Client, base, query string, res int) ([]libgenBook, error) {
	endpoint := strings.TrimSuffix(base, "/") + "/index.php"
	req, err := libgenRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("req", query)
	q.Add("column", "def")
	q.Add("topics[]", "l")
	q.Add("res", fmt.Sprintf("%d", res))
	req.URL.RawQuery = q.Encode()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mirror %s returned HTTP %d", base, resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return nil, fmt.Errorf("mirror %s returned %s", base, contentType)
	}
	return parseLibGenResults(resp.Body, base)
}

func parseLibGenResults(r io.Reader, base string) ([]libgenBook, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	table := doc.Find("table#tablelibgen")
	if table.Length() == 0 {
		return nil, errors.New("libgen results table not found")
	}
	var books []libgenBook
	seen := make(map[string]bool)
	table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() == 0 {
			return
		}
		var txt = func(i int) string {
			if i < tds.Length() {
				return strings.TrimSpace(tds.Eq(i).Text())
			}
			return ""
		}
		title := cleanLibGenTitle(tds.Eq(0))
		if title == "" {
			title = txt(0)
		}
		authors := strings.Trim(strings.ReplaceAll(txt(1), "(Author)", ""), " ,;"+"\n\t")
		size, extension := txt(6), txt(7)
		if tds.Length() == 5 {
			size, extension = txt(2), txt(3)
		}
		book := libgenBook{
			Title:      title,
			Authors:    authors,
			Publisher:  txt(2),
			Year:       txt(3),
			Language:   txt(4),
			Pages:      txt(5),
			Size:       size,
			Extension:  strings.ToLower(strings.TrimSpace(strings.TrimPrefix(extension, "."))),
			Candidates: libGenRowCandidates(tr, base),
		}
		for _, cand := range book.Candidates {
			if m := libgenMD5Pattern.FindStringSubmatch(cand[1]); len(m) == 2 {
				book.MD5 = strings.ToLower(m[1])
				break
			}
		}
		if book.MD5 != "" && !seen[book.MD5] {
			seen[book.MD5] = true
			books = append(books, book)
		}
	})
	return books, nil
}

func cleanLibGenTitle(sel *goquery.Selection) string {
	clone := sel.Clone()
	clone.Find("font, span, nobr").Each(func(_ int, el *goquery.Selection) {
		el.Remove()
	})
	text := strings.Join(strings.Fields(clone.Text()), " ")
	return strings.TrimSpace(libgenCWord.ReplaceAllString(text, " "))
}

func libGenRowCandidates(tr *goquery.Selection, base string) [][2]string {
	var cands [][2]string
	seen := make(map[string]bool)
	tr.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" {
			return
		}
		kind := ""
		switch {
		case regexp.MustCompile(`(?i)get\.php\?md5=`).MatchString(href):
			kind = "get"
		case regexp.MustCompile(`(?i)ads\.php\?md5=`).MatchString(href):
			kind = "ads"
		}
		if kind == "" || seen[kind+"|"+href] {
			return
		}
		seen[kind+"|"+href] = true
		if !strings.HasPrefix(href, "http") {
			href = strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(href, "/")
		}
		cands = append(cands, [2]string{kind, href})
	})
	return cands
}

func resolveLibGenAdsURL(client *http.Client, adsURL string) (string, error) {
	req, err := libgenRequest(http.MethodGet, adsURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ads page returned HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}
	var found string
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		if found != "" {
			return
		}
		href, _ := a.Attr("href")
		href = strings.TrimSpace(href)
		if !strings.Contains(strings.ToLower(href), "get.php?md5=") || !strings.Contains(href, "key=") {
			return
		}
		found = href
	})
	if found == "" {
		return "", errors.New("no keyed download link on ads page")
	}
	if !strings.HasPrefix(found, "http") {
		base, err := url.Parse(adsURL)
		if err != nil {
			return "", err
		}
		rel, err := url.Parse(found)
		if err != nil {
			return "", err
		}
		return base.ResolveReference(rel).String(), nil
	}
	return found, nil
}

func (b *libgenBook) PrimaryDownloadURL() (string, string) {
	for _, cand := range b.Candidates {
		if cand[0] == "get" {
			return "get", cand[1]
		}
	}
	for _, cand := range b.Candidates {
		if cand[0] == "ads" {
			return "ads", cand[1]
		}
	}
	return "", ""
}

func (r *adminLibGenResult) DownloadURL(client *http.Client) (string, error) {
	if r.DownloadKind == "ads" {
		return resolveLibGenAdsURL(client, r.Download)
	}
	if r.DownloadKind == "get" {
		return r.Download, nil
	}
	return "", errors.New("no usable download link found")
}

var libGenUnsafeFilename = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// libGenSafeFilename mirrors the TUI's clean_filename: only characters that
// are illegal in filenames are replaced, so International titles survive.
func libGenSafeFilename(title, md5, extension string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = md5
	}
	base = libGenUnsafeFilename.ReplaceAllString(base, "_")
	base = strings.Join(strings.Fields(base), " ")
	base = strings.Trim(base, ". ")
	ext := strings.TrimPrefix(strings.ToLower(extension), ".")
	if ext == "" {
		return base
	}
	return clipBytes(base+"."+ext, 180)
}

// clipBytes keeps name within limit bytes, always preserving the extension
// and never splitting a UTF-8 character.
func clipBytes(name string, limit int) string {
	if len([]byte(name)) <= limit {
		return name
	}
	extAt := strings.LastIndex(name, ".")
	ext := ""
	if extAt > 0 {
		ext = name[extAt:]
	}
	root := name
	if extAt > 0 {
		root = name[:extAt]
	}
	budget := limit - len([]byte(ext))
	if budget < 1 {
		return name[len(name)-limit:]
	}
	rootR := []rune(root)
	out := rootR
	for len([]byte(string(out)))+len([]byte(ext)) > limit && len(out) > 1 {
		out = out[:len(out)-1]
	}
	return string(out) + ext
}

func clipDisplay(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// libgenSizeBytes parses a LibGen size string (e.g. "419 MB", "2.4 MB",
// "500 KB") into a byte count. Returns 0 when the value can't be parsed.
func libgenSizeBytes(size string) int64 {
	size = strings.TrimSpace(size)
	if size == "" {
		return 0
	}
	m := libgenSizePattern.FindStringSubmatch(size)
	if len(m) != 3 {
		return 0
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult := float64(1)
	switch strings.ToUpper(m[2]) {
	case "KB", "KIB":
		mult = 1 << 10
	case "MB", "MIB":
		mult = 1 << 20
	case "GB", "GIB":
		mult = 1 << 30
	case "B", "":
		mult = 1
	}
	return int64(value * mult)
}

// copyWithStallDetection copies from src to dst, aborting with an error if no
// bytes arrive for `stall`. It stops after `limit` bytes so callers can detect
// oversized downloads. Unlike an http.Client.Timeout (an overall deadline),
// this allows slow-but-progressing transfers to complete, mirroring the TUI's
// STALL_LIMIT behaviour.
func copyWithStallDetection(dst io.Writer, src io.Reader, stall time.Duration, limit int64) (int64, error) {
	buf := make([]byte, 128*1024)
	var written int64
	for {
		type result struct {
			n   int
			err error
		}
		ch := make(chan result, 1)
		go func() {
			n, err := src.Read(buf)
			ch <- result{n, err}
		}()
		select {
		case <-time.After(stall):
			if closer, ok := src.(io.Closer); ok {
				_ = closer.Close()
			}
			return written, errors.New("download stalled: no data received")
		case r := <-ch:
			if r.n > 0 {
				remaining := limit - written
				if remaining < int64(r.n) {
					r.n = int(remaining)
				}
				if _, err := dst.Write(buf[:r.n]); err != nil {
					return written, err
				}
				written += int64(r.n)
				if written >= limit {
					return written, nil
				}
			}
			if r.err != nil {
				if r.err == io.EOF {
					return written, nil
				}
				return written, r.err
			}
		}
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func (s *Server) downloadLibGenBook(client *http.Client, url, destName string) (int64, error) {
	req, err := libgenRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(s.BookDir, ".libgen-dl-*")
	if err != nil {
		return 0, fmt.Errorf("could not stage download: %w", err)
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	written, err := copyWithStallDetection(tmp, resp.Body, libgenDownloadStall, libgenMaxDLBytes+1)
	if err != nil {
		return 0, fmt.Errorf("could not write download: %w", err)
	}
	if written > libgenMaxDLBytes {
		return 0, errors.New("download exceeds 1 GiB")
	}
	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("could not sync download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("could not close download: %w", err)
	}
	destination := filepath.Join(s.BookDir, destName)
	if err := os.Chmod(tmpName, 0644); err != nil {
		return 0, fmt.Errorf("could not set permissions: %w", err)
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return 0, fmt.Errorf("could not install download: %w", err)
	}
	keep = true
	return written, nil
}
