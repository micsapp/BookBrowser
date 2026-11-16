package indexer

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/geek1011/BookBrowser/booklist"
	xhtml "golang.org/x/net/html"
)

type supplementalDescription struct {
	BookHash    string                       `json:"book_hash"`
	Text        string                       `json:"text"`
	Method      string                       `json:"method"`
	Language    string                       `json:"language"`
	Sources     []booklist.DescriptionSource `json:"sources"`
	RetrievedAt string                       `json:"retrieved_at"`
	SourcePath  string                       `json:"source_path,omitempty"`
}

func loadDescriptions(filename string) (map[string]supplementalDescription, error) {
	if filename == "" {
		return nil, nil
	}
	f, err := os.Open(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open introduction file: %w", err)
	}
	defer f.Close()
	const maxBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read introduction file: %w", err)
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("introduction file exceeds 8 MiB")
	}
	var file struct {
		Version      int                                `json:"version"`
		Descriptions map[string]supplementalDescription `json:"descriptions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse introduction file: %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("unsupported introduction file version %d", file.Version)
	}
	return file.Descriptions, nil
}

func hasDescription(value string) bool {
	tokens := xhtml.NewTokenizer(strings.NewReader(value))
	var hidden int
	for {
		switch tokens.Next() {
		case xhtml.ErrorToken:
			return false
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokens.TagName()
			switch string(name) {
			case "script", "style":
				hidden++
			case "img":
				if hidden == 0 {
					return true
				}
			}
		case xhtml.EndTagToken:
			name, _ := tokens.TagName()
			if hidden > 0 && (string(name) == "script" || string(name) == "style") {
				hidden--
			}
		case xhtml.TextToken:
			if hidden == 0 && strings.TrimSpace(string(tokens.Text())) != "" {
				return true
			}
		}
	}
}

func applyDescription(book *booklist.Book, entry supplementalDescription) {
	if hasDescription(book.Description) || entry.BookHash != book.Hash || strings.TrimSpace(entry.Text) == "" || len(entry.Text) > 64000 {
		return
	}
	var sources []booklist.DescriptionSource
	for _, source := range entry.Sources {
		source.Label = strings.TrimSpace(source.Label)
		if source.Label == "" {
			continue
		}
		if source.URL == "" {
			if entry.Method != "embedded_summary" && entry.Method != "manual" {
				continue
			}
		} else {
			u, err := url.Parse(source.URL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
				continue
			}
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return
	}
	// Imported introductions are plain text, never trusted HTML from a website.
	text := strings.ReplaceAll(strings.TrimSpace(entry.Text), "\r\n", "\n")
	var paragraphs []string
	for _, paragraph := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(paragraph) != "" {
			paragraphs = append(paragraphs, "<p>"+html.EscapeString(strings.TrimSpace(paragraph))+"</p>")
		}
	}
	book.Description = strings.Join(paragraphs, "\n")
	book.DescriptionSources = sources
}
