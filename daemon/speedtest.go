package main

import (
	"context"
	"math"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

// In-process Ookla speedtest (nearest server), reachable at POST /v1/speedtest.
// It is DELIBERATE: a run blocks ~15-40s, pushes the radio + CPU (heat), and
// uses tens-to-hundreds of MB of mobile data — so it's a button, not a poll.

type SpeedResult struct {
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	PingMs       float64 `json:"ping_ms"`
	JitterMs     float64 `json:"jitter_ms"`
	Server       string  `json:"server"`
	Available    bool    `json:"available"`
	Error        string  `json:"error,omitempty"`
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// runSpeedtest picks the nearest Ookla server and runs ping/download/upload.
func runSpeedtest() SpeedResult {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := speedtest.New()
	_, _ = client.FetchUserInfo() // primes client location/config (standard flow)
	servers, err := client.FetchServers()
	if err != nil {
		return SpeedResult{Error: "fetch servers: " + err.Error()}
	}
	targets, err := servers.FindServer([]int{})
	if err != nil || len(targets) == 0 {
		return SpeedResult{Error: "no reachable server"}
	}
	s := targets[0]
	if err := s.PingTestContext(ctx, nil); err != nil {
		return SpeedResult{Error: "ping: " + err.Error()}
	}
	if err := s.DownloadTestContext(ctx); err != nil {
		return SpeedResult{Error: "download: " + err.Error()}
	}
	if err := s.UploadTestContext(ctx); err != nil {
		return SpeedResult{Error: "upload: " + err.Error()}
	}
	name := s.Name
	if s.Sponsor != "" && s.Sponsor != "?" {
		name += " · " + s.Sponsor
	}
	return SpeedResult{
		DownloadMbps: round1(s.DLSpeed.Mbps()),
		UploadMbps:   round1(s.ULSpeed.Mbps()),
		PingMs:       round1(s.Latency.Seconds() * 1000),
		JitterMs:     round1(s.Jitter.Seconds() * 1000),
		Server:       name,
		Available:    true,
	}
}
