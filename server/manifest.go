package server

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type manifestIcon struct {
	Source  string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type manifestShortcut struct {
	Name        string `json:"name"`
	ShortName   string `json:"short_name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

type webManifest struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	ShortName       string             `json:"short_name"`
	Description     string             `json:"description"`
	Language        string             `json:"lang"`
	Direction       string             `json:"dir"`
	StartURL        string             `json:"start_url"`
	Scope           string             `json:"scope"`
	Display         string             `json:"display"`
	Orientation     string             `json:"orientation"`
	BackgroundColor string             `json:"background_color"`
	ThemeColor      string             `json:"theme_color"`
	Categories      []string           `json:"categories"`
	Icons           []manifestIcon     `json:"icons"`
	Shortcuts       []manifestShortcut `json:"shortcuts"`
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	name := s.authSettings().PWAName
	manifest := webManifest{
		ID:              "/",
		Name:            name,
		ShortName:       name,
		Description:     "Browse, read, and listen to your ebook library.",
		Language:        "en",
		Direction:       "ltr",
		StartURL:        "/books",
		Scope:           "/",
		Display:         "standalone",
		Orientation:     "any",
		BackgroundColor: "#061a36",
		ThemeColor:      "#061a36",
		Categories:      []string{"books", "education", "productivity"},
		Icons: []manifestIcon{
			{Source: "/static/icons/icon-192.png", Sizes: "192x192", Type: "image/png", Purpose: "any"},
			{Source: "/static/icons/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "any"},
			{Source: "/static/icons/icon-192.png", Sizes: "192x192", Type: "image/png", Purpose: "maskable"},
			{Source: "/static/icons/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
		},
		Shortcuts: []manifestShortcut{
			{Name: "Books", ShortName: "Books", Description: "Browse the ebook library", URL: "/books"},
			{Name: "Search", ShortName: "Search", Description: "Search for an ebook", URL: "/search"},
			{Name: "Random book", ShortName: "Random", Description: "Open a random book", URL: "/random"},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		http.Error(w, "could not render web app manifest", http.StatusInternalServerError)
	}
}
