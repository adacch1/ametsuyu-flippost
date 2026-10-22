package main

import (
	_ "embed"
	"net/http"
)

//go:embed web/index.html
var webIndexHTML []byte

// handleRoot serves the web app at "/" and 404s every other unmatched path.
// The page never carries a token: it reads the pairing token from the URL.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webIndexHTML)
}
