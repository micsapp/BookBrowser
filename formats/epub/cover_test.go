package epub

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/geek1011/BookBrowser/indexer"
)

func writeCoverFixture(t *testing.T, metadata, manifest, guide string, extraFiles map[string][]byte) string {
	t.Helper()
	var cover, modern bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 12, 18))); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&modern, image.NewRGBA(image.Rect(0, 0, 16, 24))); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"mimetype":                []byte("application/epub+zip"),
		"META-INF/container.xml":  []byte(`<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="EPUB/content.opf"/></rootfiles></container>`),
		"EPUB/content.opf":        []byte(fmt.Sprintf(`<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Cover fixture</dc:title>%s</metadata><manifest>%s</manifest><guide>%s</guide></package>`, metadata, manifest, guide)),
		"EPUB/Images/cover.png":   cover.Bytes(),
		"EPUB/Images/modern.png":  modern.Bytes(),
		"EPUB/Images/Cover 书.png": cover.Bytes(),
	}
	for name, data := range extraFiles {
		files[name] = data
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(filename, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestEmbeddedCovers(t *testing.T) {
	const legacyMeta = `<meta name="cover" content="cover-image"/>`
	const legacyItem = `<item id="cover-image" href="Images/cover.png" media-type="image/png"/>`
	const modernItem = `<item id="modern" href="Images/modern.png" media-type="image/png" properties="cover-image"/>`
	const guide = `<reference type="cover" href="Pages/cover.xhtml#front"/>`
	tests := []struct {
		name, metadata, manifest, guide string
		files                           map[string][]byte
		wantSize                        image.Point
	}{
		{name: "legacy metadata", metadata: legacyMeta, manifest: legacyItem, wantSize: image.Pt(12, 18)},
		{name: "EPUB 3 cover property", manifest: modernItem, wantSize: image.Pt(16, 24)},
		{name: "cover property tokens", manifest: `<item href="Images/cover.png" properties="svg cover-image scripted"/>`, wantSize: image.Pt(12, 18)},
		{name: "modern cover takes precedence", metadata: legacyMeta, manifest: legacyItem + modernItem, wantSize: image.Pt(16, 24)},
		{name: "missing modern cover falls back", metadata: legacyMeta, manifest: legacyItem + `<item href="missing.png" properties="cover-image"/>`, wantSize: image.Pt(12, 18)},
		{name: "legacy metadata containing a path", metadata: `<meta name="cover" content="Images/cover.png"/>`, manifest: legacyItem, wantSize: image.Pt(12, 18)},
		{name: "legacy ID containing a quote", metadata: `<meta name="cover" content="cover'img"/>`, manifest: `<item id="cover'img" href="Images/cover.png"/>`, wantSize: image.Pt(12, 18)},
		{name: "guide cover page", guide: guide, files: map[string][]byte{"EPUB/Pages/cover.xhtml": []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><body><img src="../Images/cover.png"/></body></html>`)}, wantSize: image.Pt(12, 18)},
		{name: "guide direct image", guide: `<reference type="cover" href="Images/cover.png"/>`, wantSize: image.Pt(12, 18)},
		{name: "SVG cover page", guide: `<reference type="cover" href="Pages/cover.svg"/>`, files: map[string][]byte{"EPUB/Pages/cover.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="../Images/cover.png"/></svg>`)}, wantSize: image.Pt(12, 18)},
		{name: "SVG href cover page", guide: guide, files: map[string][]byte{"EPUB/Pages/cover.xhtml": []byte(`<html><body><svg><image href="../Images/cover.png"/></svg></body></html>`)}, wantSize: image.Pt(12, 18)},
		{name: "escaped relative image URL", guide: guide, files: map[string][]byte{"EPUB/Pages/cover.xhtml": []byte(`<html><body><img src="../Images/Cover%20%E4%B9%A6.png#front"/></body></html>`)}, wantSize: image.Pt(12, 18)},
		{name: "broken legacy cover falls back to guide", metadata: legacyMeta, manifest: `<item id="cover-image" href="missing.png"/>`, guide: `<reference type="cover" href="Images/cover.png"/>`, wantSize: image.Pt(12, 18)},
		{name: "external image is skipped", guide: guide, files: map[string][]byte{"EPUB/Pages/cover.xhtml": []byte(`<html><body><img src="https://example.com/Images/cover.png"/><img src="../Images/cover.png"/></body></html>`)}, wantSize: image.Pt(12, 18)},
		{name: "external cover URL", manifest: `<item href="https://example.com/EPUB/Images/cover.png" properties="cover-image"/>`},
		{name: "missing image", metadata: legacyMeta, manifest: `<item id="cover-image" href="missing.png"/>`},
		{name: "malformed cover page", guide: guide, files: map[string][]byte{"EPUB/Pages/cover.xhtml": []byte(`<html><body>`)}},
		{name: "no cover metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := writeCoverFixture(t, tt.metadata, tt.manifest, tt.guide, tt.files)
			book, err := load(filename)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			wantCover := tt.wantSize != (image.Point{})
			if book.HasCover() != wantCover {
				t.Fatalf("HasCover = %v, want %v", book.HasCover(), wantCover)
			}
			if !wantCover {
				return
			}
			cover, err := book.GetCover()
			if err != nil {
				t.Fatalf("GetCover: %v", err)
			}
			if cover.Bounds().Size() != tt.wantSize {
				t.Fatalf("cover size = %v, want %v", cover.Bounds().Size(), tt.wantSize)
			}
		})
	}
}

func TestDiscoveredCoverIsIndexed(t *testing.T) {
	filename := writeCoverFixture(t, "", `<item href="Images/cover.png" properties="cover-image"/>`, "", nil)
	original, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	coverDir := t.TempDir()
	idx, err := indexer.New([]string{filepath.Dir(filename)}, &coverDir, []string{"epub"})
	if err != nil {
		t.Fatal(err)
	}
	for scan := 0; scan < 2; scan++ {
		errs, err := idx.Refresh()
		if err != nil || len(errs) != 0 {
			t.Fatalf("Refresh: %v, %v", errs, err)
		}
		books := idx.BookList()
		if len(books) != 1 || !books[0].HasCover {
			t.Fatalf("indexed books = %v, want one book with a cover", books)
		}
		for _, suffix := range []string{".jpg", "_thumb.jpg"} {
			data, err := os.ReadFile(filepath.Join(coverDir, books[0].ID()+suffix))
			if err != nil {
				t.Fatal(err)
			}
			cover, format, err := image.Decode(bytes.NewReader(data))
			if err != nil || format != "jpeg" || cover.Bounds().Size() != image.Pt(12, 18) {
				t.Fatalf("invalid indexed cover %s: format=%s, err=%v", suffix, format, err)
			}
		}
	}
	current, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatalf("indexing changed the original EPUB: %v", err)
	}
}
