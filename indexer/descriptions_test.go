package indexer

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geek1011/BookBrowser/booklist"
	"github.com/geek1011/BookBrowser/formats"
)

type introductionFixture struct{ book *booklist.Book }

func (f introductionFixture) Book() *booklist.Book { return f.book }
func (f introductionFixture) HasCover() bool       { return false }
func (f introductionFixture) GetCover() (image.Image, error) {
	return nil, fmt.Errorf("fixture has no cover")
}

func init() {
	formats.Register("introfixture", func(filename string) (formats.BookInfo, error) {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		var book booklist.Book
		if err := json.Unmarshal(data, &book); err != nil {
			return nil, err
		}
		book.FilePath = filename
		book.Hash = fmt.Sprintf("%x", sha1.Sum(data))
		return introductionFixture{&book}, nil
	})
}

func TestSupplementalIntroductionSurvivesRescansWithoutChangingBooks(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "book.introfixture")
	original := []byte(`{"Title":"Missing introduction","Description":""}`)
	if err := os.WriteFile(filename, original, 0600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha1.Sum(original))
	index, err := New([]string{dir}, nil, []string{"introfixture"})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "book-descriptions.json")
	index.SetDescriptionFile(file)
	for _, text := range []string{"First introduction.", "Updated introduction."} {
		data, err := json.Marshal(map[string]interface{}{
			"version": 1,
			"descriptions": map[string]interface{}{hash[:10]: supplementalDescription{
				BookHash: hash, Text: text, Method: "web_summary",
				Sources: []booklist.DescriptionSource{{Label: "Publisher", URL: "https://example.com/book"}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		for scan := 0; scan < 2; scan++ {
			problems, err := index.Refresh()
			if err != nil || len(problems) != 0 {
				t.Fatalf("refresh: %v, %v", problems, err)
			}
			books := index.BookList()
			if len(books) != 1 || books[0].ID() != hash[:10] || books[0].Description != "<p>"+text+"</p>" || len(books[0].DescriptionSources) != 1 {
				t.Fatalf("unexpected indexed introduction: %#v", books)
			}
		}
	}
	current, err := os.ReadFile(filename)
	if err != nil || string(current) != string(original) {
		t.Fatalf("original ebook changed: %v", err)
	}
	if err := os.WriteFile(file, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	problems, err := index.Refresh()
	if err != nil || len(problems) != 1 || len(index.BookList()) != 1 {
		t.Fatalf("malformed introduction file must leave the book indexed: %v, %v", problems, err)
	}
}

func TestIntroductionPreservesExistingTextAndValidatesProvenance(t *testing.T) {
	const hash = "0123456789012345678901234567890123456789"
	entry := supplementalDescription{
		BookHash: hash, Text: `A <script>alert("x")</script> & a second paragraph.`, Method: "web_summary",
		Sources: []booklist.DescriptionSource{{Label: "Publisher", URL: "https://example.com/book"}},
	}
	for _, existing := range []string{"An original introduction.", "<p>Original <b>description</b>.</p>", `<img src="introduction.jpg">`} {
		book := &booklist.Book{Hash: hash, Description: existing}
		applyDescription(book, entry)
		if book.Description != existing || len(book.DescriptionSources) != 0 {
			t.Fatalf("existing introduction replaced: %q", existing)
		}
	}
	for _, empty := range []string{"", " \n\t", "<p>&nbsp;</p>"} {
		book := &booklist.Book{Hash: hash, Description: empty}
		applyDescription(book, entry)
		if strings.Contains(book.Description, "<script>") || !strings.Contains(book.Description, "&lt;script&gt;") {
			t.Fatalf("imported plain text must be escaped: %q", book.Description)
		}
	}
	wrong := &booklist.Book{Hash: strings.Repeat("f", 40)}
	applyDescription(wrong, entry)
	if wrong.Description != "" {
		t.Fatal("a description was attached to a different ebook hash")
	}
	for _, source := range []booklist.DescriptionSource{
		{Label: "Bad", URL: "javascript:alert(1)"},
		{Label: "Bad", URL: "https://user:password@example.com/"},
		{Label: "Missing web provenance"},
	} {
		bad := entry
		bad.Sources = []booklist.DescriptionSource{source}
		book := &booklist.Book{Hash: hash}
		applyDescription(book, bad)
		if book.Description != "" {
			t.Fatalf("invalid source accepted: %#v", source)
		}
	}
	entry.Method = "embedded_summary"
	entry.Sources = []booklist.DescriptionSource{{Label: "This book"}}
	book := &booklist.Book{Hash: hash}
	applyDescription(book, entry)
	if book.Description == "" || len(book.DescriptionSources) != 1 {
		t.Fatal("an introduction recovered from the ebook should retain its source")
	}
}

func TestIntroductionFileVersionAndMissingFile(t *testing.T) {
	if descriptions, err := loadDescriptions(filepath.Join(t.TempDir(), "missing.json")); err != nil || len(descriptions) != 0 {
		t.Fatalf("absent supplement should be optional: %v", err)
	}
	filename := filepath.Join(t.TempDir(), "descriptions.json")
	if err := os.WriteFile(filename, []byte(`{"version":2,"descriptions":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDescriptions(filename); err == nil {
		t.Fatal("unsupported file version was accepted")
	}
}
