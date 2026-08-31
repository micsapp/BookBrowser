package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadLibGenMobiInstallsConvertedEpub(t *testing.T) {
	s := newAuthTestServer(t)

	binDir := t.TempDir()
	converter := filepath.Join(binDir, "ebook-convert")
	if err := os.WriteFile(converter, []byte("#!/bin/sh\nprintf converted > \"$2\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("mobi input")),
			Header:     make(http.Header),
		}, nil
	})}

	size, err := s.downloadLibGenBook(client, "https://example.test/dune.mobi", "Dune Messiah.epub", true, ".mobi")
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("converted")) {
		t.Fatalf("converted size=%d want %d", size, len("converted"))
	}
	installed := filepath.Join(s.BookDir, "Dune Messiah.epub")
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("read installed EPUB: %v", err)
	}
	if string(data) != "converted" {
		t.Fatalf("installed EPUB contents=%q", data)
	}
	matches, err := filepath.Glob(filepath.Join(s.BookDir, ".book-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after install: %v", matches)
	}
}
