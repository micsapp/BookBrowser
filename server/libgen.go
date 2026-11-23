package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	xproxy "golang.org/x/net/proxy"
)

var libgenMirrors = []string{
	"https://libgen.li",
	"https://libgen.vg",
	"https://libgen.rs",
	"https://libgen.is",
	"https://libgen.st",
}

var libgenUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// LibGen file pages often carry an IPFS CID even when the normal CDN mirror
// is temporarily unavailable. These gateways are tried after every direct
// link for a record has failed; tests replace this list.
var libgenIPFSGateways = []string{
	"https://ipfs.filebase.io/ipfs/",
	"https://ipfs.io/ipfs/",
	"https://dweb.link/ipfs/",
	"https://w3s.link/ipfs/",
	"https://nftstorage.link/ipfs/",
	"https://cloudflare-ipfs.com/ipfs/",
	"https://gateway.pinata.cloud/ipfs/",
}

var libgenIPFSPattern = regexp.MustCompile(`(?i)IPFS\s*CID[^a-z0-9]{0,120}((?:bafy[a-z0-9]{20,})|(?:Qm[1-9A-HJ-NP-Za-km-z]{40,}))`)
var libgenCIDPattern = regexp.MustCompile(`\b((?:bafy[a-z0-9]{20,})|(?:Qm[1-9A-HJ-NP-Za-km-z]{40,}))\b`)
var libgenMD5Hex = regexp.MustCompile(`^[0-9a-f]{32}$`)

const (
	libgenMaxAlternateCopies = 12
	libgenMaxIPFSPageBytes   = 1 << 20
	libgenIPFSPageDeadline   = 8 * time.Second
	libgenIPFSDownloadLimit  = 3 * time.Minute
)

const (
	libgenSearchRetries         = 2
	libgenRetrySleep            = 1 * time.Second
	libgenSearchDeadline        = 25 * time.Second
	libgenOverallSearchDeadline = 40 * time.Second
	libgenResultCount           = 100
	libgenSearchPageSize        = 25
	libgenMaxDLBytes            = int64(1) << 30
	libgenDownloadStall         = 30 * time.Second
	libgenDownloadDial          = 30 * time.Second
	libgenDownloadRespHdr       = 45 * time.Second
)

// Download retries are variables so tests can shorten the backoff.
var (
	libgenDownloadRetryDelay = 10 * time.Minute
	libgenMaxDownloadRetries = 3
)

// errLibGenNoResults signals that a mirror answered successfully but had no
// matching files. It is different from a connectivity or parse failure and is
// handled as an empty result set rather than an error.
var errLibGenNoResults = errors.New("libgen returned no results")

var libgenMD5Pattern = regexp.MustCompile(`(?i)md5=([0-9a-f]{32})`)
var libgenCWord = regexp.MustCompile(`\s*\bc\b\s*`)
var libgenISBNPattern = regexp.MustCompile(`(?i)\b(97[89][0-9]{10}|[0-9]{9}[0-9x])\b`)
var libgenTagPattern = regexp.MustCompile(`<[^>]*>`)
var libgenTemporaryErrorPattern = regexp.MustCompile(`(?i)HTTP (404|408|425|429|5[0-9][0-9])|not found|not synchronized|stalled|timed out|timeout|temporary|connection reset|broken pipe|unexpected EOF`)
var libgenSizePattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(KB|MB|GB|KiB|MiB|GiB|B)?$`)

// libgenLargeBookBytes is the threshold above which the admin UI flags a
// search result as a large file that may be slow to load in the reader.
const libgenLargeBookBytes = int64(50 << 20)

type libgenBook struct {
	MD5        string
	Mirror     string
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
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   libgenDownloadDial,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       libgenTLS(),
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: libgenDownloadRespHdr,
		IdleConnTimeout:       90 * time.Second,
	}
	applyLibGenDownloadProxy(tr)
	return &http.Client{Transport: tr}
}

// applyLibGenDownloadProxy routes LibGen file traffic through an optional
// proxy. The CDN used by LibGen sometimes answers 503 for one host's egress
// while serving the same URL from another host. Keeping this opt-in (and
// separate from the search client) means a broken proxy cannot take down
// ordinary LibGen searches.
//
// LIBGEN_DOWNLOAD_PROXY (preferred) and LIBGEN_PROXY accept either an HTTP(S)
// proxy URL (http://user:pass@host:port) or a SOCKS5 URL
// (socks5h://host:port). SOCKS5 uses remote DNS so CDN hostnames resolve on
// the proxy side.
func applyLibGenDownloadProxy(tr *http.Transport) {
	raw := strings.TrimSpace(os.Getenv("LIBGEN_DOWNLOAD_PROXY"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("LIBGEN_PROXY"))
	}
	if raw == "" {
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		log.Printf("LibGen download proxy ignored: invalid URL")
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		dialer, err := xproxy.SOCKS5("tcp", u.Host, nil, xproxy.Direct)
		if err != nil {
			log.Printf("LibGen download proxy ignored: %v", err)
			return
		}
		if cd, ok := dialer.(xproxy.ContextDialer); ok {
			tr.DialContext = cd.DialContext
		} else {
			tr.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
				return dialer.Dial(network, address)
			}
		}
		tr.Proxy = nil
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
	default:
		log.Printf("LibGen download proxy ignored: unsupported scheme %q", u.Scheme)
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

// searchLibGenBases combines results from the configured mirrors under a hard
// deadline. Each mirror is paginated because current mirrors ignore res=100
// and still return only 25 rows per page.
//
// LibGen matches every word of a query, so a subtitle or a translated title can
// hide an otherwise present book. The original query is tried first, then
// progressively looser variants (subtitle stripped, ISBN extracted). The query
// that produced the results is returned so the UI can explain the fallback.
func searchLibGenBases(client *http.Client, query string, res int) ([]libgenBook, string, string, error) {
	overall, cancel := context.WithTimeout(context.Background(), libgenOverallSearchDeadline)
	defer cancel()

	responded := false
	var lastErr error
	for _, candidate := range libgenSearchQueries(query) {
		ctx, cancelQuery := context.WithTimeout(overall, libgenSearchDeadline)
		books, sources, err := searchLibGenMirrors(ctx, client, libgenMirrors, candidate, res)
		cancelQuery()
		if err != nil {
			lastErr = err
			if overall.Err() != nil {
				break
			}
			continue
		}
		responded = true
		if len(books) > 0 {
			return books, sources, candidate, nil
		}
	}
	if responded {
		return nil, "", query, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no results (mirrors may be unreachable)")
	}
	return nil, "", "", lastErr
}

// libgenSearchQueries returns distinct search terms to try, from most to least
// specific. It strips subtitles ("Title: Subtitle"), parenthetical series
// notes, and extracts ISBNs.
func libgenSearchQueries(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(strings.Trim(s, `"'“”‘’`))
		if len([]rune(s)) < 2 {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	add(q)
	if isbn := libgenISBNPattern.FindString(q); isbn != "" {
		add(isbn)
	}
	for _, sep := range []string{":", "：", " - ", " — ", " – "} {
		if i := strings.Index(q, sep); i >= 3 {
			add(q[:i])
		}
	}
	if i := strings.IndexAny(q, "([（"); i >= 3 {
		add(q[:i])
	}
	return out
}

func searchLibGenMirrors(ctx context.Context, client *http.Client, mirrors []string, query string, res int) ([]libgenBook, string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, "", errors.New("empty query")
	}
	if res <= 0 || res > libgenResultCount {
		res = libgenResultCount
	}
	pages := (res + libgenSearchPageSize - 1) / libgenSearchPageSize

	type mirrorResult struct {
		books []libgenBook
		index int
		done  bool
		err   error
	}

	booksByMirror := make([][]libgenBook, len(mirrors))
	mergedResults := func() ([]libgenBook, string) {
		var books []libgenBook
		var sources []string
		seen := make(map[string]int)
		for i, found := range booksByMirror {
			if len(found) > 0 {
				sources = append(sources, mirrors[i])
			}
			for _, book := range found {
				if index, ok := seen[book.MD5]; ok {
					// Keep alternate download links when mirrors share a file.
					for _, candidate := range book.Candidates {
						exists := false
						for _, current := range books[index].Candidates {
							if current == candidate {
								exists = true
								break
							}
						}
						if !exists {
							books[index].Candidates = append(books[index].Candidates, candidate)
						}
					}
					continue
				}
				seen[book.MD5] = len(books)
				books = append(books, book)
			}
		}
		return books, strings.Join(sources, ", ")
	}

	var lastErr error
	responded := false
	for attempt := 0; attempt < libgenSearchRetries; attempt++ {
		// Buffer every page and completion so workers can finish even after
		// the deadline returns the partial results already received.
		results := make(chan mirrorResult, len(mirrors)*(pages+1))
		for i, base := range mirrors {
			i, base := i, base
			go func() {
				var searchErr error
				defer func() { results <- mirrorResult{index: i, done: true, err: searchErr} }()
				seen := make(map[string]bool)
				for page := 1; page <= pages; page++ {
					books, err := searchLibGenMirrorCtx(ctx, client, base, query, res, page)
					if err != nil {
						searchErr = err
						return
					}
					var fresh []libgenBook
					for _, book := range books {
						if !seen[book.MD5] && len(seen) < res {
							seen[book.MD5] = true
							fresh = append(fresh, book)
						}
					}
					results <- mirrorResult{books: fresh, index: i}
					if len(fresh) == 0 || len(books) < libgenSearchPageSize || len(seen) >= res {
						return
					}
				}
			}()
		}
		collect := func(r mirrorResult) {
			booksByMirror[r.index] = append(booksByMirror[r.index], r.books...)
			switch {
			case errors.Is(r.err, errLibGenNoResults):
				// A reachable mirror answered with zero matches.
				responded = true
			case r.err != nil:
				lastErr = r.err
			default:
				responded = true
			}
		}
		for pending := len(mirrors); pending > 0; {
			select {
			case <-ctx.Done():
				// Include completed pages queued just before the deadline.
				for len(results) > 0 {
					collect(<-results)
				}
				if books, sources := mergedResults(); len(books) > 0 {
					return books, sources, nil
				}
				if responded {
					return nil, "", nil
				}
				return nil, "", fmt.Errorf("LibGen search: %w (mirrors may be unreachable)", ctx.Err())
			case r := <-results:
				collect(r)
				if r.done {
					pending--
				}
			}
		}
		if books, sources := mergedResults(); len(books) > 0 {
			return books, sources, nil
		}
		if responded {
			return nil, "", nil
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

func searchLibGenMirrorCtx(ctx context.Context, client *http.Client, base, query string, res, page int) ([]libgenBook, error) {
	endpoint := strings.TrimSuffix(base, "/") + "/index.php"
	req, err := libgenRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("req", query)
	q.Add("column", "def")
	q.Add("topics[]", "l")
	q.Add("topics[]", "f")
	q.Add("topics[]", "r")
	q.Add("objects[]", "f")
	q.Add("curtab", "f")
	q.Add("res", fmt.Sprintf("%d", res))
	q.Add("page", strconv.Itoa(page))
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
		// A reachable mirror with zero matches still renders the search form
		// (and, depending on the mirror, no results table at all). Treat that
		// as an empty result set rather than a connectivity failure.
		if doc.Find("input[name=req]").Length() > 0 {
			return nil, errLibGenNoResults
		}
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
			Mirror:     base,
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
		if tds.Length() == 5 {
			book.Publisher, book.Year, book.Language, book.Pages = "", "", "", ""
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
	// Current file rows include series and ISBN links in the title cell. Use
	// the edition's title link without its badges and ancillary metadata.
	if title := sel.Find("a[href*='edition.php']").First(); title.Length() > 0 {
		sel = title
	}
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

// parseLibGenIPFSCIDs extracts IPFS CIDs from a LibGen file page. The labeled
// "IPFS CID" field is preferred; a generic CID scan is only used when the label
// is missing (some mirrors render the field differently).
func parseLibGenIPFSCIDs(page string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(cid string) {
		cid = strings.TrimSpace(cid)
		if cid == "" || seen[cid] {
			return
		}
		seen[cid] = true
		out = append(out, cid)
	}
	for _, m := range libgenIPFSPattern.FindAllStringSubmatch(page, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	if len(out) == 0 {
		for _, m := range libgenCIDPattern.FindAllStringSubmatch(page, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}
	return out
}

// fetchLibGenIPFSCIDs reads the file page for md5 and returns any IPFS CIDs it
// advertises. The page is small and is usually served even while the download
// CDN is returning 503. Mirrors are queried in parallel under a short overall
// deadline so a dead record cannot stall a download job for minutes.
func fetchLibGenIPFSCIDs(client *http.Client, md5, preferredBase string) []string {
	md5 = strings.ToLower(strings.TrimSpace(md5))
	if !libgenMD5Hex.MatchString(md5) {
		return nil
	}
	bases := make([]string, 0, len(libgenMirrors)+1)
	if preferredBase != "" {
		bases = append(bases, preferredBase)
	}
	seenBase := make(map[string]bool)
	for _, base := range bases {
		seenBase[base] = true
	}
	for _, base := range libgenMirrors {
		if !seenBase[base] {
			seenBase[base] = true
			bases = append(bases, base)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), libgenIPFSPageDeadline)
	defer cancel()
	results := make(chan []string, len(bases))
	for _, base := range bases {
		base := base
		go func() {
			endpoint := strings.TrimSuffix(base, "/") + "/file.php?md5=" + url.QueryEscape(md5)
			req, err := libgenRequest(http.MethodGet, endpoint, nil)
			if err != nil {
				results <- nil
				return
			}
			resp, err := client.Do(req.WithContext(ctx))
			if err != nil {
				results <- nil
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				results <- nil
				return
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, libgenMaxIPFSPageBytes+1))
			if err != nil || len(data) > libgenMaxIPFSPageBytes {
				results <- nil
				return
			}
			results <- parseLibGenIPFSCIDs(string(data))
		}()
	}
	for range bases {
		select {
		case cids := <-results:
			if len(cids) > 0 {
				return cids
			}
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// validateDownloadedBook rejects a nominally successful HTTP download that is
// actually an HTML error page. IPFS gateways sometimes return 200 with a
// gateway error page, and installing that as an EPUB would corrupt the
// library.
func validateDownloadedBook(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	head := bytes.TrimSpace(buf[:n])
	head = bytes.TrimPrefix(head, []byte{0xef, 0xbb, 0xbf})
	if len(head) == 0 {
		return errors.New("download is empty")
	}
	if head[0] == '<' {
		return errors.New("download is an HTML error page")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".epub":
		if len(head) < 2 || head[0] != 'P' || head[1] != 'K' {
			return errors.New("download is not a valid EPUB")
		}
	case ".pdf":
		if !bytes.HasPrefix(head, []byte("%PDF")) {
			return errors.New("download is not a valid PDF")
		}
	}
	return nil
}

// downloadLibGenViaIPFS tries every advertised CID through the configured
// gateways. It validates the result before leaving it in the library.
func (s *Server) downloadLibGenViaIPFS(client *http.Client, result *adminLibGenResult, destination string, cids []string) (int64, error) {
	if len(cids) == 0 {
		return 0, errors.New("no IPFS copy available")
	}
	ipfsClient := *client
	if ipfsClient.Timeout == 0 || ipfsClient.Timeout > libgenIPFSDownloadLimit {
		ipfsClient.Timeout = libgenIPFSDownloadLimit
	}
	var lastErr error
	for _, cid := range cids {
		for _, gateway := range libgenIPFSGateways {
			url := strings.TrimSuffix(gateway, "/") + "/" + cid
			size, err := s.downloadLibGenBook(&ipfsClient, url, destination, isMobiFilename(result.Filename), filepath.Ext(result.Filename))
			if err != nil {
				lastErr = err
				continue
			}
			if err := validateDownloadedBook(filepath.Join(s.BookDir, destination)); err != nil {
				_ = os.Remove(filepath.Join(s.BookDir, destination))
				lastErr = err
				continue
			}
			return size, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("IPFS download failed")
	}
	return 0, lastErr
}

// libgenNormalizeText lowercases text and reduces it to letters and digits.
// This makes title/author matching tolerant of punctuation, spacing and
// full-width versus half-width forms without accepting different works.
func libgenNormalizeText(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func libgenFirstAuthor(value string) string {
	value = strings.ReplaceAll(value, "(Author)", "")
	value = strings.NewReplacer(
		"；", ";", "，", ",", "、", ",", "&", ",",
		" and ", ",", " AND ", ",",
	).Replace(value)
	if i := strings.IndexAny(value, ";,"); i > 0 {
		value = value[:i]
	}
	return libgenNormalizeText(value)
}

// libgenAlternateResults returns other search results that appear to be the
// same work. LibGen often has several copies of one book; when the selected
// copy's CDN is down, another copy (often one with an IPFS CID) may work.
func libgenAlternateResults(byMD5 map[string]*adminLibGenResult, primary *adminLibGenResult, max int) []*adminLibGenResult {
	if primary == nil || max <= 0 {
		return nil
	}
	title := libgenNormalizeText(primary.Title)
	if title == "" {
		return nil
	}
	ext := strings.ToLower(strings.TrimSpace(primary.Extension))
	author := libgenFirstAuthor(primary.Authors)
	primarySize := libgenSizeBytes(primary.Size)

	type candidate struct {
		result *adminLibGenResult
		diff   float64
	}
	var candidates []candidate
	for md5, alt := range byMD5 {
		if alt == nil || md5 == primary.MD5 {
			continue
		}
		if libgenNormalizeText(alt.Title) != title {
			continue
		}
		if ext != "" && strings.ToLower(strings.TrimSpace(alt.Extension)) != ext {
			continue
		}
		altAuthor := libgenFirstAuthor(alt.Authors)
		if author != "" && altAuthor != "" && author != altAuthor {
			continue
		}
		diff := float64(0)
		if primarySize > 0 {
			if altSize := libgenSizeBytes(alt.Size); altSize > 0 {
				diff = math.Abs(float64(altSize-primarySize)) / float64(primarySize)
				if diff > 0.30 {
					continue
				}
			}
		}
		candidates = append(candidates, candidate{result: alt, diff: diff})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].diff != candidates[j].diff {
			return candidates[i].diff < candidates[j].diff
		}
		return candidates[i].result.MD5 < candidates[j].result.MD5
	})
	out := make([]*adminLibGenResult, 0, max)
	for _, candidate := range candidates {
		out = append(out, candidate.result)
		if len(out) >= max {
			break
		}
	}
	return out
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

// libgenErrorSnippet extracts a short, human-readable message from an error
// response body so job status can explain why a download failed (for example
// "File not found. The repositories may not be synchronized").
func libgenErrorSnippet(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 1024))
	text := libgenTagPattern.ReplaceAllString(string(raw), " ")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		text = text[:160] + "…"
	}
	return text
}

// isTemporaryLibGenError reports whether a failed download is worth retrying
// later. LibGen CDNs commonly answer 500 "File not found. The repositories may
// not be synchronized" for newly indexed files, and the file usually appears
// after a delay.
func isTemporaryLibGenError(err error) bool {
	return err != nil && libgenTemporaryErrorPattern.MatchString(err.Error())
}

func (s *Server) downloadLibGenBook(client *http.Client, url, destName string, convert bool, inputExt string) (int64, error) {
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
		if detail := libgenErrorSnippet(resp.Body); detail != "" {
			return 0, fmt.Errorf("download returned HTTP %d: %s", resp.StatusCode, detail)
		}
		return 0, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Calibre determines the input format from the filename extension.
	tmp, err := os.CreateTemp(s.BookDir, ".libgen-dl-*"+inputExt)
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
	finalName := tmpName
	finalSize := written
	if convert {
		finalName, finalSize, err = convertUploadedBook(tmpName, s.BookDir)
		if err != nil {
			return 0, err
		}
	}
	destination := filepath.Join(s.BookDir, destName)
	if err := os.Chmod(finalName, 0644); err != nil {
		if finalName != tmpName {
			_ = os.Remove(finalName)
		}
		return 0, fmt.Errorf("could not set permissions: %w", err)
	}
	if err := os.Rename(finalName, destination); err != nil {
		if finalName != tmpName {
			_ = os.Remove(finalName)
		}
		return 0, fmt.Errorf("could not install download: %w", err)
	}
	if finalName != tmpName {
		_ = os.Remove(tmpName)
	}
	keep = true
	return finalSize, nil
}
