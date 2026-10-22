package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Read-only view of tailscaled over its LocalAPI unix socket. The status
// decode structs deliberately omit AuthURL, BrowseToURL, keys and prefs, so
// nothing sensitive can reach a JSON response.

var tailscaleSock = "/data/adb/tailscale/tailscaled.sock"
var tailscalePrefsPath = "/data/adb/zflip5-modem/tailscale-prefs.json"

var errTSNotRunning = errors.New("tailscaled not running")

// localAPIHost is the Host header tailscaled requires on LocalAPI requests.
const localAPIHost = "local-tailscaled.sock"

type TailscaleBrief struct {
	State string  `json:"state"`
	IP    *string `json:"ip"`
}

type TailscaleStatus struct {
	State        string   `json:"state"`
	BackendState *string  `json:"backend_state"`
	IP           *string  `json:"ip"`
	IPs          []string `json:"ips"`
	DNSName      *string  `json:"dns_name"`
	Hostname     *string  `json:"hostname"`
	Online       bool     `json:"online"`
	KeyExpiry    *string  `json:"key_expiry"`
	Expired      bool     `json:"expired"`
	Tailnet      *string  `json:"tailnet"`
	ServeOK      bool     `json:"serve_ok"`
	WebURL       *string  `json:"web_url"`
	Version      *string  `json:"version"`
	Health       []string `json:"health"`
	Error        *string  `json:"error"`
}

type tailscaleClient struct {
	hc    *http.Client
	port  int
	mu    sync.Mutex
	brief TailscaleBrief
}

func newTailscaleClient(sock string, servePort int) *tailscaleClient {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	return &tailscaleClient{hc: &http.Client{Transport: tr}, port: servePort, brief: TailscaleBrief{State: "not_running"}}
}

func mapBackendState(s string) string {
	switch s {
	case "Running":
		return "running"
	case "NeedsLogin", "NoState":
		return "needs_login"
	case "NeedsMachineAuth":
		return "needs_machine_auth"
	case "Stopped":
		return "stopped"
	case "Starting":
		return "starting"
	case "InUseOtherUser":
		return "in_use_other_user"
	}
	return "error"
}

// get fetches one LocalAPI path and decodes the JSON body into v.
func (t *tailscaleClient) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+localAPIHost+"/localapi/v0/"+path, nil)
	if err != nil {
		return err
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return errTSNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("localapi %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (t *tailscaleClient) Status(ctx context.Context) TailscaleStatus {
	st := TailscaleStatus{State: "error", IPs: []string{}, Health: []string{}}
	var raw struct {
		BackendState string
		TailscaleIPs []string
		Self         struct {
			DNSName, HostName string
			Online            bool
			KeyExpiry         string
		}
		CurrentTailnet struct{ Name string }
		Version        string
		Health         []string
	}
	if err := t.get(ctx, "status", &raw); err != nil {
		if errors.Is(err, errTSNotRunning) {
			st.State = "not_running"
		} else {
			msg := err.Error()
			st.Error = &msg
		}
		return st
	}
	st.State = mapBackendState(raw.BackendState)
	if st.State != "running" {
		if st.State == "error" {
			msg := "unknown backend state " + raw.BackendState
			st.Error = &msg
		}
		return st
	}
	str := func(s string) *string { return &s }
	st.BackendState = str(raw.BackendState)
	st.IPs = append(st.IPs, raw.TailscaleIPs...)
	for _, ip := range raw.TailscaleIPs {
		if !strings.Contains(ip, ":") {
			st.IP = str(ip)
			break
		}
	}
	fqdn := strings.TrimSuffix(strings.TrimSpace(raw.Self.DNSName), ".")
	st.DNSName = str(fqdn)
	st.Hostname = str(raw.Self.HostName)
	st.Online = raw.Self.Online
	if raw.Self.KeyExpiry != "" {
		st.KeyExpiry = str(raw.Self.KeyExpiry)
		if exp, err := time.Parse(time.RFC3339, raw.Self.KeyExpiry); err == nil {
			st.Expired = exp.Before(time.Now())
		}
	}
	st.Tailnet = str(raw.CurrentTailnet.Name)
	st.WebURL = str("https://" + fqdn + "/")
	st.Version = str(raw.Version)
	st.Health = append(st.Health, raw.Health...)

	var sc struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
	}
	if t.get(ctx, "serve-config", &sc) == nil {
		st.ServeOK = sc.Web[fqdn+":443"].Handlers["/"].Proxy == fmt.Sprintf("http://127.0.0.1:%d", t.port)
	}
	return st
}

// RefreshBrief caches the short state that /v1/summary reports.
func (t *tailscaleClient) RefreshBrief(ctx context.Context) {
	st := t.Status(ctx)
	t.mu.Lock()
	t.brief = TailscaleBrief{State: st.State, IP: st.IP}
	t.mu.Unlock()
}

func (t *tailscaleClient) Brief() TailscaleBrief {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.brief
}

func (t *tailscaleClient) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, t.Status(ctx))
}
