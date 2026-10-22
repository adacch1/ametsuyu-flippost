package main

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"
)

// The summary collector keeps one in-memory snapshot of everything the app and
// web UI poll every second. A slow loop (5 s) fills the parts that cost a
// subprocess or file read; summary() adds the cheap live parts (speed, usage)
// on each request, so a GET does no I/O.

type SummarySignal struct {
	Display  string `json:"display"`
	Tech     string `json:"tech"`
	Operator string `json:"operator"`
	Bars     *int   `json:"bars"`
}

type SummaryUsage struct {
	PeriodBytes uint64  `json:"period_bytes"`
	LimitBytes  int64   `json:"limit_bytes"`
	TodayBytes  uint64  `json:"today_bytes"`
	LeftBytes   *int64  `json:"left_bytes"`
	Period      string  `json:"period"`
	NextReset   *string `json:"next_reset"`
}

type SummaryHotspot struct {
	Active        bool    `json:"active"`
	SSID          *string `json:"ssid"`
	Band          *string `json:"band"`
	Wifi6         *bool   `json:"wifi6"`
	Clients       int     `json:"clients"`
	DualBand      bool    `json:"dual_band"`
	DualActive    bool    `json:"dual_active"`
	DualBandBlock *string `json:"dual_band_block"`
}

type SummaryTemp struct {
	C     *float64 `json:"c"`
	RawC  float64  `json:"raw_c"`
	State string   `json:"state"`
	Safe  bool     `json:"safe"`
	WarnC float64  `json:"warn_c"`
	GateC float64  `json:"gate_c"`
	Bench bool     `json:"bench"`
}

type SummaryBattery struct {
	Level   int     `json:"level"`
	Plugged string  `json:"plugged"`
	TempC   float64 `json:"temp_c"`
}

type Summary struct {
	V           int             `json:"v"`
	UpdatedAt   string          `json:"updated_at"`
	SlowAgeS    int             `json:"slow_age_s"`
	Signal      *SummarySignal  `json:"signal"`
	Usage       SummaryUsage    `json:"usage"`
	Speed       SpeedSnap       `json:"speed"`
	Hotspot     SummaryHotspot  `json:"hotspot"`
	WanIP       *string         `json:"wan_ip"`
	WanIface    *string         `json:"wan_iface"`
	Temperature SummaryTemp     `json:"temperature"`
	Battery     *SummaryBattery `json:"battery"`
	Tailscale   TailscaleBrief  `json:"tailscale"`
	Airplane    *bool           `json:"airplane"`
}

type summaryState struct {
	mu      sync.RWMutex
	slow    Summary
	at      time.Time
	clients ClientsReport
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func summarySignal(sig Signal) *SummarySignal {
	if !sig.Available {
		return nil
	}
	out := &SummarySignal{Display: sig.Display, Tech: sig.Tech, Operator: sig.Operator}
	if !(sig.Tech == "NR" && sig.Level == 0) {
		b := sig.Level
		out.Bars = &b
	}
	return out
}

func summaryUsage(r UsageReport) SummaryUsage {
	u := SummaryUsage{PeriodBytes: r.PeriodBytes, LimitBytes: r.LimitBytes, TodayBytes: r.TodayBytes, Period: r.Period}
	if r.LimitBytes > 0 {
		left := r.LimitBytes - int64(r.PeriodBytes)
		if left < 0 {
			left = 0
		}
		u.LeftBytes = &left
	}
	if r.NextReset != "" {
		u.NextReset = &r.NextReset
	}
	return u
}

func (s *Server) summaryTemp(th Thermal) SummaryTemp {
	raw := th.BatteryC
	if th.MaxC > raw {
		raw = th.MaxC
	}
	// A degraded sweep evaluates at the gate so the state fails closed.
	eval := raw
	if th.Source == "degraded" {
		eval = s.effectiveGateC()
	}
	c := raw
	if m, ok := s.smooth.median(); ok {
		c = m
	}
	c = round1(c)
	return SummaryTemp{C: &c, RawC: round1(raw), State: string(s.pol.Evaluate(eval)), Safe: th.Safe,
		WarnC: s.effectiveWarnC(), GateC: s.effectiveGateC(), Bench: s.bench.Active()}
}

// collectSlow gathers the parts that need subprocesses or file reads and
// stores them for summary().
func (s *Server) collectSlow() {
	var sum Summary
	sum.V = 1
	sum.Signal = summarySignal(deviceSignal())
	st := s.hs.Status()
	ap, apOK := s.hs.APConfig()
	sum.Hotspot = SummaryHotspot{Active: st.Active, Wifi6: st.Wifi6, DualBand: st.DualBand, DualActive: st.Dual}
	if apOK {
		ssid, band := ap.SSID, ap.Band
		if st.Dual {
			band = "dual"
		}
		sum.Hotspot.SSID, sum.Hotspot.Band = &ssid, &band
	}
	if _, reason := s.dualBandBlock(); reason != "" {
		sum.Hotspot.DualBandBlock = &reason
	}
	if w := deviceWanIP(); w.Available {
		sum.WanIP, sum.WanIface = &w.IP, &w.Iface
	}
	sum.Temperature = s.summaryTemp(s.col.Thermal(s.effectiveWarnC(), s.effectiveGateC(), s.cfg.Thermal.FailClosed))
	if b := s.col.Battery(); b.Available {
		sum.Battery = &SummaryBattery{Level: b.Level, Plugged: b.Plugged, TempC: round1(b.TempC)}
	}
	if s.ts != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		s.ts.RefreshBrief(ctx)
		cancel()
		sum.Tailscale = s.ts.Brief()
	} else {
		sum.Tailscale = TailscaleBrief{State: "not_running"}
	}
	air := airplaneOn()
	sum.Airplane = &air
	clients := deviceClients()

	s.sum.mu.Lock()
	s.sum.slow, s.sum.at, s.sum.clients = sum, time.Now(), clients
	s.sum.mu.Unlock()
}

// summary merges the stored slow part with the live speed and usage. It does no I/O.
func (s *Server) summary() Summary {
	s.sum.mu.RLock()
	out, at, clients := s.sum.slow, s.sum.at, s.sum.clients
	s.sum.mu.RUnlock()
	out.V = 1
	if !at.IsZero() {
		out.SlowAgeS = int(time.Since(at).Seconds())
		out.UpdatedAt = at.Format(time.RFC3339)
	}
	out.Speed = s.speed.Snapshot()
	out.Usage = summaryUsage(s.usage.Snapshot())
	if pc, ok := out.Speed.PerClient.([]ClientSpeed); ok {
		out.Hotspot.Clients = len(pc)
	} else {
		out.Hotspot.Clients = clients.Count
	}
	return out
}

// runSummary blocks: a 1 s loop feeds the speed sampler, a 5 s loop refreshes
// the slow snapshot. The SoftAP config is re-read every 12th slow pass.
func (s *Server) runSummary() {
	go func() {
		for range time.Tick(time.Second) {
			s.sum.mu.RLock()
			cl := s.sum.clients.Clients
			s.sum.mu.RUnlock()
			s.speed.tick(apIfaces(), cl)
		}
	}()
	for n := 0; ; n++ {
		if n%12 == 0 {
			s.hs.RefreshAPConfig()
		}
		s.collectSlow()
		time.Sleep(5 * time.Second)
	}
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.summary())
}
