package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootServesWebApp(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := do(s, "GET", "/", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	for _, want := range []string{`<link rel="manifest" href="/manifest.webmanifest">`, `<link rel="apple-touch-icon" href="/logo.png">`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}
func TestRootNeverEmbedsTokens(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.openReads.Store(true)
	s.openControl.Store(true)
	b := do(s, "GET", "/", "").Body.String()
	for _, bad := range []string{strings.Repeat("a", 64), strings.Repeat("c", 64), "EMBEDDED_TOKENS"} {
		if strings.Contains(b, bad) {
			t.Errorf("page leaks %q", bad[:8])
		}
	}
}
func TestRootIsOnlyRoot(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if w := do(s, "GET", "/index.html", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
func TestRemovedPagesAndRoutes404(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	rs, rc := strings.Repeat("a", 64), strings.Repeat("c", 64)
	for _, c := range []struct{ m, p, tok string }{{"GET", "/dashboard", ""}, {"GET", "/v1/signal", rs}, {"GET", "/v1/battery", rs}, {"POST", "/v1/cover/accent", rc}} {
		if w := do(s, c.m, c.p, c.tok); w.Code != 404 {
			t.Errorf("%s %s = %d, want 404", c.m, c.p, w.Code)
		}
	}
}
func TestStatusDropsCoverAccent(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if b := do(s, "GET", "/v1/status", strings.Repeat("a", 64)).Body.String(); strings.Contains(b, "cover_accent") {
		t.Fatal(b)
	}
}
func TestManifestStartsAtRoot(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	if b := do(s, "GET", "/manifest.webmanifest", "").Body.String(); !strings.Contains(b, `"start_url": "/"`) || strings.Contains(b, "/dashboard") {
		t.Fatal(b)
	}
}
func TestPairURL(t *testing.T) {
	web, tok := "https://zflip5.tail1.ts.net/", strings.Repeat("a", 64)
	for _, c := range []struct {
		host, xfp string
		web       *string
		tok, want string
	}{
		{"zflip5.tail1.ts.net", "", nil, "", "https://zflip5.tail1.ts.net/"},
		{"zflip5.tail1.ts.net", "", nil, tok, "https://zflip5.tail1.ts.net/?token=" + tok},
		{"127.0.0.1:18080", "", &web, "", web},
		{"127.0.0.1:18080", "", &web, tok, web + "?token=" + tok},
		{"127.0.0.1:18080", "", nil, "", "http://127.0.0.1:18080/"},
		{"phone.lan", "https", nil, "", "https://phone.lan/"},
	} {
		r := httptest.NewRequest("GET", "/v1/qr", nil)
		r.Host = c.host
		if c.xfp != "" {
			r.Header.Set("X-Forwarded-Proto", c.xfp)
		}
		if got := pairURL(r, c.web, c.tok); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}
func TestPairTargetTokenRule(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	r := httptest.NewRequest("GET", "/v1/qr", nil)
	r.Host = "zflip5.tail1.ts.net"
	s.openReads.Store(true)
	if got := s.pairTarget(r); got != "https://zflip5.tail1.ts.net/" {
		t.Errorf("open reads: %q", got)
	}
	s.openReads.Store(false)
	if got := s.pairTarget(r); got != "https://zflip5.tail1.ts.net/?token="+strings.Repeat("a", 64) {
		t.Errorf("closed reads: %q", got)
	}
}
