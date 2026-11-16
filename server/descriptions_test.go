package server

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupplementalIntroductionReachesBookPageAndAPI(t *testing.T) {
	s := newAuthTestServer(t)
	filename := writeTestEpub(t, s.BookDir, "introduction.epub", "An introduction fixture")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha1.Sum(data))
	supplement := fmt.Sprintf(`{"version":1,"descriptions":{"%s":{"book_hash":"%s","text":"A researched introduction with <untrusted> text.","method":"web_summary","sources":[{"label":"Publisher","url":"https://example.com/a-book"}]}}}`, hash[:10], hash)
	file := filepath.Join(filepath.Dir(s.auth.Path()), "book-descriptions.json")
	if err := os.WriteFile(file, []byte(supplement), 0600); err != nil {
		t.Fatal(err)
	}
	if problems, err := s.Indexer.Refresh(); err != nil || len(problems) != 0 {
		t.Fatalf("refresh: %v, %v", problems, err)
	}
	page := requestServer(s, http.MethodGet, "/books/"+hash[:10], nil)
	for _, expected := range []string{
		`A researched introduction with &lt;untrusted&gt; text.`,
		`data-i18n="introduction_source"`,
		`href="https://example.com/a-book" target="_blank" rel="noopener noreferrer">Publisher</a>`,
	} {
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), expected) {
			t.Fatalf("book page missing %q: status %d", expected, page.Code)
		}
	}
	book := s.Indexer.BookList()[0]
	encoded, err := json.Marshal(newAPIBook(book))
	if err != nil {
		t.Fatal(err)
	}
	var api struct {
		ID                 string `json:"id"`
		Description        string `json:"description"`
		DescriptionSources []struct {
			Label string `json:"label"`
			URL   string `json:"url"`
		} `json:"description_sources"`
	}
	if err := json.Unmarshal(encoded, &api); err != nil {
		t.Fatal(err)
	}
	if api.ID != hash[:10] || api.Description != book.Description || len(api.DescriptionSources) != 1 || api.DescriptionSources[0].URL != "https://example.com/a-book" {
		t.Fatalf("introduction provenance lost in API: %s", encoded)
	}
}
