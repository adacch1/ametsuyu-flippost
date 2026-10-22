package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	hc        *http.Client
	port      int
	mu        sync.Mutex
	brief     TailscaleBrief
	loginBusy atomic.Bool
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

// Login and logout are the only paths that touch AuthURL or BrowseToURL. The
// handlers sit behind the app-only guard, so a remote browser never sees them.

var (
	loginURLWait  = 20 * time.Second
	loginWatchFor = 5 * time.Minute
)

// tsPrefFields are the prefs Logout saves and the next Login restores.
var tsPrefFields = []string{"Hostname", "NetfilterMode", "CorpDNS", "RouteAll", "ShieldsUp", "RunSSH", "AdvertiseRoutes"}

// do sends one LocalAPI request. A missing or refusing socket maps to errTSNotRunning.
func (t *tailscaleClient) do(ctx context.Context, method, path string, body any, hdr map[string]string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+localAPIHost+"/localapi/v0/"+path, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := t.hc.Do(req)
	if err != nil && (errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)) {
		return nil, errTSNotRunning
	}
	return resp, err
}

// call runs do and requires a 2xx answer. It drains and closes the body.
func (t *tailscaleClient) call(ctx context.Context, method, path string, body any) error {
	resp, err := t.do(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("localapi %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return nil
}

func (t *tailscaleClient) rawStatus(ctx context.Context) (state, authURL, fqdn string, err error) {
	var raw struct {
		BackendState string
		AuthURL      string
		Self         struct{ DNSName string }
	}
	if err = t.get(ctx, "status", &raw); err != nil {
		return
	}
	return raw.BackendState, raw.AuthURL, strings.TrimSuffix(strings.TrimSpace(raw.Self.DNSName), "."), nil
}

func localAPIFail(err error) (code int, reason string) {
	if errors.Is(err, errTSNotRunning) {
		return http.StatusServiceUnavailable, "not_running"
	}
	return http.StatusBadGateway, "localapi"
}

// Login starts an interactive sign-in and returns the first login URL.
// The result is (state, loginURL, httpCode, reason, err); reason is set on failure.
func (t *tailscaleClient) Login(ctx context.Context) (string, string, int, string, error) {
	prefsPath, urlWait, watchFor := tailscalePrefsPath, loginURLWait, loginWatchFor
	if !t.loginBusy.CompareAndSwap(false, true) {
		return "", "", http.StatusConflict, "busy", errors.New("login already in progress")
	}
	defer t.loginBusy.Store(false)
	fail := func(err error) (string, string, int, string, error) {
		code, reason := localAPIFail(err)
		return "", "", code, reason, err
	}

	backend, _, _, err := t.rawStatus(ctx)
	if err != nil {
		return fail(err)
	}
	switch backend {
	case "Running":
		return "running", "", http.StatusOK, "", nil
	case "NeedsMachineAuth":
		return "", "", http.StatusConflict, "needs_machine_auth", errors.New("approve this device in the Tailscale admin console")
	}

	// The watcher outlives Login so it can re-apply serve once sign-in completes.
	wctx, cancel := context.WithTimeout(context.Background(), watchFor)
	resp, err := t.do(wctx, http.MethodGet, "watch-ipn-bus?mask=2", nil, nil)
	if err != nil {
		cancel()
		return fail(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return fail(fmt.Errorf("localapi watch-ipn-bus: HTTP %d", resp.StatusCode))
	}
	urls := make(chan string, 4)
	go func() {
		defer cancel()
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		for {
			var n struct {
				State       *int
				BrowseToURL *string
			}
			if dec.Decode(&n) != nil {
				return
			}
			if n.BrowseToURL != nil && *n.BrowseToURL != "" {
				select {
				case urls <- *n.BrowseToURL:
				default:
				}
			}
			if n.State != nil && *n.State == 6 {
				t.ensureServe(wctx)
				return
			}
		}
	}()

	abort := func(err error) (string, string, int, string, error) {
		cancel()
		return fail(err)
	}
	if backend == "NoState" {
		if err := t.call(ctx, http.MethodPost, "start", map[string]any{}); err != nil {
			return abort(err)
		}
	}
	prefs := map[string]any{"WantRunning": true, "WantRunningSet": true, "Hostname": "zflip5", "HostnameSet": true}
	if b, err := os.ReadFile(prefsPath); err == nil {
		var saved map[string]any
		if json.Unmarshal(b, &saved) == nil {
			for _, k := range tsPrefFields {
				if v, ok := saved[k]; ok && (k != "Hostname" || v != "") {
					prefs[k], prefs[k+"Set"] = v, true
				}
			}
		}
	}
	if err := t.call(ctx, http.MethodPatch, "prefs", prefs); err != nil {
		return abort(err)
	}
	if err := t.call(ctx, http.MethodPost, "login-interactive", nil); err != nil {
		return abort(err)
	}

	deadline := time.NewTimer(urlWait)
	defer deadline.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		select {
		case u := <-urls:
			return "needs_login", u, http.StatusOK, "", nil
		case <-poll.C:
			if _, u, _, err := t.rawStatus(ctx); err == nil && u != "" {
				return "needs_login", u, http.StatusOK, "", nil
			}
		case <-deadline.C:
			cancel()
			return "", "", http.StatusGatewayTimeout, "no_login_url", errors.New("tailscaled returned no login URL")
		case <-ctx.Done():
			cancel()
			return "", "", http.StatusGatewayTimeout, "no_login_url", ctx.Err()
		}
	}
}

// Logout saves the prefs worth keeping (never the node key), then logs out.
func (t *tailscaleClient) Logout(ctx context.Context) error {
	var prefs map[string]any
	if err := t.get(ctx, "prefs", &prefs); err != nil {
		return err
	}
	keep := map[string]any{}
	for _, k := range tsPrefFields {
		keep[k] = prefs[k]
	}
	b, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	if err := writeConfigAtomic(tailscalePrefsPath, b); err != nil {
		return err
	}
	return t.call(ctx, http.MethodPost, "logout", nil)
}

// ensureServe merges the HTTPS proxy to the daemon into the serve config and
// keeps every other entry. It retries once when the Etag is stale.
func (t *tailscaleClient) ensureServe(ctx context.Context) error {
	_, _, fqdn, err := t.rawStatus(ctx)
	if err != nil {
		return err
	}
	if fqdn == "" {
		return errors.New("tailscale has no DNS name yet")
	}
	for attempt := 0; ; attempt++ {
		resp, err := t.do(ctx, http.MethodGet, "serve-config", nil, nil)
		if err != nil {
			return err
		}
		cfg := map[string]any{}
		err = json.NewDecoder(resp.Body).Decode(&cfg) // "null" leaves the empty map
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || (err != nil && err != io.EOF) {
			return fmt.Errorf("localapi serve-config: HTTP %d %v", resp.StatusCode, err)
		}
		if cfg == nil {
			cfg = map[string]any{}
		}
		sub := func(m map[string]any, k string) map[string]any {
			c, _ := m[k].(map[string]any)
			if c == nil {
				c = map[string]any{}
				m[k] = c
			}
			return c
		}
		sub(cfg, "TCP")["443"] = map[string]any{"HTTPS": true}
		site := sub(sub(cfg, "Web"), fqdn+":443")
		sub(site, "Handlers")["/"] = map[string]any{"Proxy": fmt.Sprintf("http://127.0.0.1:%d", t.port)}

		pr, err := t.do(ctx, http.MethodPost, "serve-config", cfg, map[string]string{"If-Match": resp.Header.Get("Etag")})
		if err != nil {
			return err
		}
		pr.Body.Close()
		switch {
		case pr.StatusCode/100 == 2:
			return nil
		case pr.StatusCode == http.StatusPreconditionFailed && attempt == 0:
		default:
			return fmt.Errorf("localapi serve-config: HTTP %d", pr.StatusCode)
		}
	}
}

func (t *tailscaleClient) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	state, url, code, reason, err := t.Login(ctx)
	if reason != "" {
		writeErrReason(w, code, err.Error(), reason)
		return
	}
	var u any
	if url != "" {
		u = url
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "login_url": u})
}

func (t *tailscaleClient) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := t.Logout(ctx); err != nil {
		code, reason := localAPIFail(err)
		writeErrReason(w, code, err.Error(), reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "needs_login"})
}
