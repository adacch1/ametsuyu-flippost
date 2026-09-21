package main

import (
	"regexp"
	"strings"
	"testing"
)

// The widget deck picks the selected page from its scroll offset, so nav tab N
// must be section N. A tab added without its section (or vice versa) would
// silently select the wrong page from the swipe onwards.
func TestDeckNavMatchesPages(t *testing.T) {
	tabs := regexp.MustCompile(`data-screen="([^"]+)"`).FindAllStringSubmatch(dashboardHTML, -1)
	pages := regexp.MustCompile(`<section class="screen" id="([^"]+)"`).FindAllStringSubmatch(dashboardHTML, -1)
	if len(tabs) == 0 || len(tabs) != len(pages) {
		t.Fatalf("nav tabs %d, deck pages %d", len(tabs), len(pages))
	}
	for i := range tabs {
		if tabs[i][1] != pages[i][1] {
			t.Errorf("page %d: nav tab %q, section %q", i, tabs[i][1], pages[i][1])
		}
	}
}

// TestNoHardcodedDataCap proves the quota is real config, not a baked-in
// 512GB guess: neither page's markup carries the old constant, and both carry
// the controls the configurable limit needs. Exact-string checks rather than
// a bare "512" search, since other legitimate "512"s (ports, sizes) exist.
func TestNoHardcodedDataCap(t *testing.T) {
	for _, bad := range []string{"512*GIB", "of 512 GB", "1024*1024*1024"} {
		if strings.Contains(dashboardHTML, bad) {
			t.Errorf("dashboardHTML still contains %q", bad)
		}
		if strings.Contains(coverHTML, bad) {
			t.Errorf("coverHTML still contains %q", bad)
		}
	}
	for _, want := range []string{`id="qLimit"`, `id="usageResetBtn"`} {
		if !strings.Contains(dashboardHTML, want) {
			t.Errorf("dashboardHTML missing %q", want)
		}
	}
	if !strings.Contains(coverHTML, `id="uLbl"`) {
		t.Error(`coverHTML missing id="uLbl"`)
	}
}

// TestHotspotOverrideUI is a smoke test that the timed force-on override made
// it into the dashboard: the endpoint the buttons post to, and the pause
// banner element that makes a stuck scan visible instead of silent.
func TestHotspotOverrideUI(t *testing.T) {
	for _, want := range []string{"/v1/hotspot/override", `id="hsPause"`} {
		if !strings.Contains(dashboardHTML, want) {
			t.Errorf("dashboardHTML missing %q", want)
		}
	}
}

// TestTempHistoryUI is a smoke test that the temperature-history card made it
// into the dashboard: the endpoint it fetches, the card itself, and the
// polyline chart element.
func TestTempHistoryUI(t *testing.T) {
	for _, want := range []string{"/v1/thermal/history", `id="tempHist"`, `id="thLine"`} {
		if !strings.Contains(dashboardHTML, want) {
			t.Errorf("dashboardHTML missing %q", want)
		}
	}
}

// TestPollCadence proves the fixed 5s poll-everything tick was replaced by a
// per-endpoint cadence table: every capstone-task field's endpoint appears in
// the table, the two expensive/never-self-changing reads (bands, usbtether)
// and the history read are all SLOW (not refetched every heartbeat), and the
// old single-cadence heartbeat literal is gone.
func TestPollCadence(t *testing.T) {
	rows := regexp.MustCompile(`\["(/v1/[^"]+)",[^,\]]+,(LIVE|MID|SLOW)\]`).FindAllStringSubmatch(dashboardHTML, -1)
	if len(rows) == 0 {
		t.Fatal("no cadence-table rows found in dashboardHTML")
	}
	tier := map[string]string{}
	for _, r := range rows {
		tier[r[1]] = r[2]
	}
	for _, want := range []string{
		"/v1/status", "/v1/signal", "/v1/usage", "/v1/hotspot",
		"/v1/clients", "/v1/cpu", "/v1/bands", "/v1/usbtether", "/v1/thermal/history",
	} {
		if _, ok := tier[want]; !ok {
			t.Errorf("cadence table missing a row for %q", want)
		}
	}
	for _, slow := range []string{"/v1/bands", "/v1/usbtether", "/v1/thermal/history"} {
		if got := tier[slow]; got != "SLOW" {
			t.Errorf("%q cadence = %q, want SLOW", slow, got)
		}
	}
	for _, want := range []string{"visibilitychange", "data-mac="} {
		if !strings.Contains(dashboardHTML, want) {
			t.Errorf("dashboardHTML missing %q", want)
		}
	}
	if strings.Contains(dashboardHTML, "setInterval(tick,5000)") {
		t.Error("dashboardHTML still has the old fixed setInterval(tick,5000) heartbeat")
	}
}

// TestRingUsedSI proves the Home usage ring shows the daemon's own SI string
// (period_human, e.g. "40.56 MB") rather than fmtBytes(used), which only ever
// emits GB/TB and would flatten a sub-GB period to "0.0 GB".
func TestRingUsedSI(t *testing.T) {
	if !strings.Contains(dashboardHTML, `getElementById("ringUsed").textContent=u.period_human`) {
		t.Error(`dashboardHTML: ringUsed must be set from u.period_human, not fmtBytes(used)`)
	}
}
