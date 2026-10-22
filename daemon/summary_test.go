package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSummarySignalBars(t *testing.T) {
	if summarySignal(Signal{Available: false}) != nil {
		t.Fatal("unavailable must be nil")
	}
	if s := summarySignal(Signal{Available: true, Tech: "NR", Display: "5G", Level: 0}); s.Bars != nil {
		t.Fatal("NR level 0 must be null")
	}
	if s := summarySignal(Signal{Available: true, Tech: "LTE", Display: "4G+", Operator: "Viettel", Level: 3}); *s.Bars != 3 || s.Display != "4G+" || s.Operator != "Viettel" {
		t.Fatalf("%+v", s)
	}
}

func TestSummaryUsageLeft(t *testing.T) {
	u := summaryUsage(UsageReport{PeriodBytes: 12_345_678_901, LimitBytes: 100_000_000_000, TodayBytes: 5, Period: "monthly", NextReset: "2026-11-01T00:00:00+07:00"})
	if *u.LeftBytes != 87_654_321_099 || *u.NextReset != "2026-11-01T00:00:00+07:00" {
		t.Fatalf("%+v", u)
	}
	if u := summaryUsage(UsageReport{PeriodBytes: 5}); u.LeftBytes != nil || u.NextReset != nil {
		t.Fatalf("%+v", u)
	}
	if u := summaryUsage(UsageReport{PeriodBytes: 9, LimitBytes: 5}); *u.LeftBytes != 0 {
		t.Fatal("over cap must floor at 0")
	}
}

func TestSummaryTemp(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	tp := s.summaryTemp(Thermal{BatteryC: 30, MaxC: 40.04, Safe: true, Source: "sysfs+battery"})
	if *tp.C != 40 || tp.RawC != 40 || tp.State != "SAFE" || tp.WarnC != 44 || tp.GateC != 46 || tp.Bench {
		t.Fatalf("%+v", tp)
	}
	s.smooth.push(45)
	if tp := s.summaryTemp(Thermal{MaxC: 40}); *tp.C != 45 {
		t.Fatal("median not used")
	}
	if tp := s.summaryTemp(Thermal{MaxC: 55, BatteryC: 48}); tp.State != "HOT" {
		t.Fatalf("hot state %q", tp.State)
	}
	s2 := NewServer(testCfg(), fakeCollector{safe: true})
	if tp := s2.summaryTemp(Thermal{MaxC: 30, Source: "degraded"}); tp.State != "HOT" {
		t.Fatalf("degraded must fail closed, got %q", tp.State)
	}
}

func TestSummaryMergesNoIO(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.sum.mu.Lock()
	s.sum.at = time.Now().Add(-3 * time.Second)
	s.sum.slow = Summary{V: 1, Hotspot: SummaryHotspot{Active: true}, Tailscale: TailscaleBrief{State: "running"}}
	s.sum.clients = ClientsReport{Count: 2}
	s.sum.mu.Unlock()
	if got := s.summary(); got.SlowAgeS < 3 || got.SlowAgeS > 4 || got.Speed.PerClient != "unavailable" || got.Hotspot.Clients != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSummaryEndpointKeysAlwaysPresent(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	s.sum.slow = Summary{V: 1, Tailscale: TailscaleBrief{State: "not_running"}}
	w := httptest.NewRecorder()
	s.handleSummary(w, httptest.NewRequest("GET", "/v1/summary", nil))
	var m map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &m)
	for _, k := range []string{"v", "updated_at", "slow_age_s", "signal", "usage", "speed", "hotspot", "wan_ip", "wan_iface", "temperature", "battery", "tailscale", "airplane"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q: %s", k, w.Body.String())
		}
	}
	var hs map[string]json.RawMessage
	json.Unmarshal(m["hotspot"], &hs)
	for _, k := range []string{"ssid", "band", "wifi6", "dual_band_block"} {
		if _, ok := hs[k]; !ok {
			t.Errorf("hotspot missing %q", k)
		}
	}
}
