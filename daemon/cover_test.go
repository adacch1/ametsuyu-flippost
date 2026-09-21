package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// rootVars pulls the ":root{...}" custom properties out of a page's inline CSS.
// Splitting on ";" (rather than regexing "--name") keeps nested var() references
// on the value side where they belong.
func rootVars(html string) map[string]string {
	out := map[string]string{}
	i := strings.Index(html, ":root{")
	if i < 0 {
		return out
	}
	block := html[i+len(":root{"):]
	if j := strings.Index(block, "}"); j >= 0 {
		block = block[:j]
	}
	for _, decl := range strings.Split(block, ";") {
		decl = strings.TrimSpace(decl)
		if !strings.HasPrefix(decl, "--") {
			continue
		}
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// The cover page carries its own copy of the design tokens (it is a separate
// single-file page, not a fragment of the dashboard). A token that means one
// thing on one page and another on the other is the exact drift DESIGN.md's
// "web and native values must be identical" rule exists to stop, so every
// shared name must hold the same value.
func TestCoverTokensMatchDashboard(t *testing.T) {
	dash, cover := rootVars(dashboardHTML), rootVars(coverHTML)
	if len(dash) == 0 || len(cover) == 0 {
		t.Fatalf("no :root vars parsed (dashboard %d, cover %d)", len(dash), len(cover))
	}
	shared := 0
	for k, cv := range cover {
		dv, ok := dash[k]
		if !ok {
			continue
		}
		shared++
		if dv != cv {
			t.Errorf("%s: dashboard %q, cover %q", k, dv, cv)
		}
	}
	if shared < 20 {
		t.Fatalf("only %d shared tokens — the cover page stopped using the system", shared)
	}
}

// The kiosk WebView loads "/" with no path of its own, so the cover page must
// be what the root serves and the panel must still be reachable one path down.
// Swapping these silently puts the five-tab panel back on a 352px screen.
func TestCoverAndDashboardRoutes(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	tok := strings.Repeat("a", 64)

	w := do(s, "GET", "/", tok)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `id="rotBtn"`) {
		t.Fatalf(`GET / : code %d, cover page served: %v`, w.Code, strings.Contains(w.Body.String(), `id="rotBtn"`))
	}
	w = do(s, "GET", "/dashboard", tok)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-screen="home"`) {
		t.Fatalf(`GET /dashboard : code %d, panel served: %v`, w.Code, strings.Contains(w.Body.String(), `data-screen="home"`))
	}
	if w := do(s, "GET", "/nope", tok); w.Code != 404 {
		t.Fatalf("GET /nope: want 404, got %d", w.Code)
	}
}

// The refresh action is the one control the cover screen and the panel share;
// both must actually ship it.
func TestRefreshControlsPresent(t *testing.T) {
	if !strings.Contains(dashboardHTML, `id="refreshFab"`) {
		t.Error("dashboard is missing the floating refresh button")
	}
	if !strings.Contains(coverHTML, `id="refBtn"`) {
		t.Error("cover page is missing the refresh button")
	}
}

// postJSON is do() with a body — the accent endpoint is the first cover test
// that writes.
func postJSON(s *Server, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// The accent name is substituted straight into the served HTML, so the closed
// set is a security boundary, not a nicety: anything outside it must be
// refused before it reaches the page.
func TestCoverAccentRejectsUnknown(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	rc := strings.Repeat("c", 64)
	for _, bad := range []string{`"neon"`, `"cover"`, `""`, `"\"><script>"`} {
		w := postJSON(s, "/v1/cover/accent", rc, `{"accent":`+bad+`}`)
		if w.Code != 400 {
			t.Errorf("accent %s: want 400, got %d", bad, w.Code)
		}
	}
	if got := do(s, "GET", "/", strings.Repeat("a", 64)).Body.String(); !strings.Contains(got, `data-accent="sunflower"`) {
		t.Error("a rejected accent still changed the served page")
	}
}

// A picked accent has to reach both the page it paints and the status the
// control panel reads its selected swatch from.
func TestCoverAccentAppliesToPageAndStatus(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	rc, rs := strings.Repeat("c", 64), strings.Repeat("a", 64)
	if w := postJSON(s, "/v1/cover/accent", rc, `{"accent":"coral"}`); w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := do(s, "GET", "/", rs).Body.String(); !strings.Contains(got, `data-accent="coral"`) {
		t.Error("cover page is not painted with the picked accent")
	}
	if got := do(s, "GET", "/v1/status", rs).Body.String(); !strings.Contains(got, `"cover_accent":"coral"`) {
		t.Error("status does not report the picked accent")
	}
	// Every accent the panel offers must exist as a binding on the page, or a
	// swatch would silently paint nothing.
	page := do(s, "GET", "/", rs).Body.String()
	for _, a := range coverAccents {
		if !strings.Contains(page, `[data-accent="`+a+`"]`) {
			t.Errorf("cover page has no binding for accent %q", a)
		}
	}
}

// The dim level is the one cover-screen value the owner tunes by hand, so the
// three cases that are easy to get wrong — absent, off, out of range — each
// have to land somewhere sane rather than at a black panel.
func TestCoverDimDefaultsAndClamps(t *testing.T) {
	c := &Config{}
	if got := c.CoverDim(); got != coverDimDefault {
		t.Errorf("absent: want %d, got %d", coverDimDefault, got)
	}
	for _, tc := range []struct{ set, want int }{{0, 0}, {-5, 0}, {45, 45}, {999, 255}} {
		v := tc.set
		c.Cover.Dim = &v
		if got := c.CoverDim(); got != tc.want {
			t.Errorf("dim %d: want %d, got %d", tc.set, tc.want, got)
		}
	}
}

// The two pages have to link to each other. A one-way trip strands the kiosk on
// the control panel until the cover panel next sleeps and the watcher relaunches
// it — which is exactly how this looked before the link existed.
func TestPagesLinkBothWays(t *testing.T) {
	if !strings.Contains(coverHTML, `href="/dashboard"`) {
		t.Error("cover screen has no link to the control panel")
	}
	if !strings.Contains(dashboardHTML, `class="hdrbtn" href="/"`) {
		t.Error("control panel has no link back to the cover screen")
	}
}
