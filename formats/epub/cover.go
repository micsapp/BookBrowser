package epub

import (
	"archive/zip"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/beevik/etree"
)

// findCover prefers EPUB 3 cover declarations, then legacy metadata and guide
// references. Only resources inside the EPUB are used; cover discovery never
// fetches external URLs or changes the book.
func findCover(opf *etree.Document, rootfile string, archive *zip.ReadCloser) string {
	items := opf.FindElements("//manifest/item")
	for _, item := range items {
		for _, property := range strings.Fields(item.SelectAttrValue("properties", "")) {
			if property == "cover-image" {
				if cover := coverResource(archive, rootfile, item.SelectAttrValue("href", "")); cover != "" {
					return cover
				}
			}
		}
	}
	for _, meta := range opf.FindElements("//meta[@name='cover']") {
		id := meta.SelectAttrValue("content", "")
		if id == "" {
			continue
		}
		for _, item := range items {
			if item.SelectAttrValue("id", "") == id {
				if cover := coverResource(archive, rootfile, item.SelectAttrValue("href", "")); cover != "" {
					return cover
				}
			}
		}
		// Some EPUBs store the image's href here instead of its manifest ID.
		if cover := coverResource(archive, rootfile, id); cover != "" {
			return cover
		}
	}
	for _, reference := range opf.FindElements("//guide/reference[@type='cover']") {
		if cover := coverResource(archive, rootfile, reference.SelectAttrValue("href", "")); cover != "" {
			return cover
		}
	}
	return ""
}

// coverResource resolves either a direct image or an XHTML/SVG cover page.
func coverResource(archive *zip.ReadCloser, base, href string) string {
	resource := coverResourcePath(base, href)
	if resource == "" {
		return ""
	}
	f, err := archive.Open(strings.TrimPrefix(resource, "/"))
	if err != nil {
		return ""
	}
	defer f.Close()

	switch strings.ToLower(path.Ext(resource)) {
	case ".xhtml", ".html", ".htm", ".svg":
		// Cover pages are optional. Bound their decompressed size as well as
		// the image limits enforced by GetCover.
		const maxCoverPageBytes = 1 << 20
		data, err := io.ReadAll(io.LimitReader(f, maxCoverPageBytes+1))
		if err != nil || len(data) > maxCoverPageBytes {
			return ""
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			return ""
		}
		images := append(doc.FindElements("//img[@src]"), doc.FindElements("//image")...)
		for _, image := range images {
			href := image.SelectAttrValue("src", "")
			if image.Tag == "image" {
				href = image.SelectAttrValue("href", image.SelectAttrValue("xlink:href", ""))
			}
			imagePath := coverResourcePath(resource, href)
			if imagePath == "" {
				continue
			}
			if imageFile, err := archive.Open(strings.TrimPrefix(imagePath, "/")); err == nil {
				imageFile.Close()
				return imagePath
			}
		}
		return ""
	default:
		return resource
	}
}

// EPUB hrefs are URLs, with slash-separated paths independent of the host OS.
func coverResourcePath(base, href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.IsAbs() || u.Host != "" || u.Path == "" {
		return ""
	}
	if path.IsAbs(u.Path) {
		return path.Clean(u.Path)
	}
	return path.Join("/", path.Dir(base), u.Path)
}
