package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// scopeForToken returns the scope name whose token matches the presented bearer,
// using a constant-time compare. Empty string means no match.
func (c *Config) scopeForToken(bearer string) string {
	for scope, tok := range c.Tokens {
		if tok == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(bearer), []byte(tok)) == 1 {
			return scope
		}
	}
	return ""
}

func bearerFrom(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

// authScope resolves the caller's scope, or "" if unauthenticated.
func (c *Config) authScope(r *http.Request) string {
	b := bearerFrom(r)
	if b == "" {
		return ""
	}
	return c.scopeForToken(b)
}

// scopeAllows reports whether a token scope may use an endpoint requiring `need`.
// read-status is the least-privileged; sms and radio-control are distinct and do
// NOT imply read-status escalation beyond their own endpoints.
func scopeAllows(have, need string) bool {
	return have == need
}
