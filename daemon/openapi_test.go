package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var liveRoutes = []string{"/v1/airplane", "/v1/clients", "/v1/cooldown", "/v1/cpu", "/v1/cpu/mode", "/v1/dashboard/control", "/v1/dashboard/open",
	"/v1/device/reboot", "/v1/hotspot", "/v1/hotspot/band", "/v1/hotspot/dual", "/v1/hotspot/override", "/v1/hotspot/scan", "/v1/hotspot/whitelist",
	"/v1/presets", "/v1/presets/apply", "/v1/presets/auto", "/v1/presets/delete", "/v1/qr", "/v1/service/restart", "/v1/status", "/v1/summary",
	"/v1/tailscale", "/v1/tailscale/login", "/v1/tailscale/logout", "/v1/tether", "/v1/thermal", "/v1/thermal/bench", "/v1/thermal/limits",
	"/v1/usage", "/v1/usage/quota", "/v1/usage/reset", "/v1/usbtether", "/v1/usbtether/toggle"}
var removedRoutes = []string{"/v1/health", "/v1/network", "/v1/thermal/history", "/v1/bands", "/v1/prefer5g", "/v1/speedtest", "/v1/sms/recent",
	"/v1/notifications/recent", "/v1/cover/home", "/v1/signal", "/v1/battery", "/v1/cover/accent", "/dashboard"}
var appOnly = []string{"/v1/tailscale/login", "/v1/tailscale/logout", "/v1/dashboard/open", "/v1/dashboard/control"}

func loadSpec(t *testing.T) map[string]any {
	b, err := os.ReadFile("../schemas/api.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestOpenAPIPathsMatchRoutes(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	var got []string
	for p := range paths {
		got = append(got, p)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, liveRoutes) {
		t.Fatalf("spec paths\n got %v\nwant %v", got, liveRoutes)
	}
	s := NewServer(testCfg(), fakeCollector{safe: true})
	for _, p := range liveRoutes {
		if w := do(s, "PATCH", p, ""); w.Code == 404 {
			t.Errorf("%s not registered", p)
		}
	}
	for _, p := range removedRoutes {
		if w := do(s, "GET", p, strings.Repeat("a", 64)); w.Code != 404 {
			t.Errorf("%s = %d, want 404", p, w.Code)
		}
	}
}
func TestOpenAPIAppOnlyOps(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.openControl.Store(true)
	for _, p := range appOnly {
		if paths[p].(map[string]any)["post"].(map[string]any)["x-app-only"] != true {
			t.Errorf("%s lacks x-app-only", p)
		}
		if w := postJSON(s, p, strings.Repeat("c", 64), `{"open":true}`); w.Code != 403 || !strings.Contains(w.Body.String(), `"reason":"app_only"`) {
			t.Errorf("%s token caller: %d %s", p, w.Code, w.Body)
		}
	}
}
func TestOpenAPIDocumentsWebRoutes(t *testing.T) {
	paths := loadSpec(t)["paths"].(map[string]any)
	for _, p := range webRoutes() {
		if _, ok := paths[p]; !ok {
			t.Errorf("web page calls undocumented %s", p)
		}
	}
}
func TestOpenAPINoRemovedScopes(t *testing.T) {
	b, _ := os.ReadFile("../schemas/api.openapi.json")
	for _, bad := range []string{`"sms"`, "SMS", "prefer5g", "speedtest", "cover_accent"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("spec mentions %s", bad)
		}
	}
}
