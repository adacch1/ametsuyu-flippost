package main

import "net/http"

// writeErrReason writes an error body that also carries a machine-readable reason.
func writeErrReason(w http.ResponseWriter, code int, msg, reason string) {
	writeJSON(w, code, map[string]any{"error": msg, "code": code, "reason": reason})
}
