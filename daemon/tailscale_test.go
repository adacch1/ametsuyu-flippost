package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeTS(t *testing.T, h http.HandlerFunc) *tailscaleClient {
	t.Helper()
	dir, _ := os.MkdirTemp("", "zts") // short path: unix socket names are capped at 104 bytes on macOS
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return newTailscaleClient(sock, 18080)
}

const tsRunning = `{"BackendState":"Running","AuthURL":"https://login.tailscale.com/a/SECRET","TailscaleIPs":["fd7a:115c:a1e0::1","100.110.36.27"],
"Self":{"DNSName":"zflip5.tail1234.ts.net.","HostName":"zflip5","Online":true,"KeyExpiry":"2027-04-01T00:00:00Z"},
"CurrentTailnet":{"Name":"user@example.com"},"Version":"1.80.0","Health":[]}`

func TestMapBackendState(t *testing.T) {
	for in, want := range map[string]string{"Running": "running", "NeedsLogin": "needs_login", "NoState": "needs_login",
		"NeedsMachineAuth": "needs_machine_auth", "Stopped": "stopped", "Starting": "starting", "InUseOtherUser": "in_use_other_user", "Weird": "error"} {
		if got := mapBackendState(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestTailscaleStatusRunning(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != localAPIHost {
			http.Error(w, "bad host", 403)
			return
		}
		switch r.URL.Path {
		case "/localapi/v0/status":
			io.WriteString(w, tsRunning)
		case "/localapi/v0/serve-config":
			io.WriteString(w, `{"Web":{"zflip5.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:18080"}}}}}`)
		}
	})
	st := ts.Status(context.Background())
	b, _ := json.Marshal(st)
	if st.State != "running" || *st.IP != "100.110.36.27" || *st.DNSName != "zflip5.tail1234.ts.net" || !st.ServeOK ||
		*st.WebURL != "https://zflip5.tail1234.ts.net/" || *st.Tailnet != "user@example.com" || !st.Online {
		t.Fatalf("%s", b)
	}
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "AuthURL") {
		t.Fatalf("leaked auth URL: %s", b)
	}
}

func TestTailscaleStatusNotRunning(t *testing.T) {
	dir, _ := os.MkdirTemp("", "ts") // short path keeps the socket name under the unix limit
	t.Cleanup(func() { os.RemoveAll(dir) })
	st := newTailscaleClient(filepath.Join(dir, "missing.sock"), 18080).Status(context.Background())
	b, _ := json.Marshal(st)
	if st.State != "not_running" || !strings.Contains(string(b), `"ips":[]`) || !strings.Contains(string(b), `"ip":null`) {
		t.Fatalf("%s", b)
	}
}

func TestTailscaleStatusNeedsLoginNullsFields(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/SECRET","TailscaleIPs":["100.1.2.3"],"Version":"1.80.0"}`)
	})
	if st := ts.Status(context.Background()); st.State != "needs_login" || st.IP != nil || st.Version != nil || st.WebURL != nil || st.ServeOK {
		t.Fatalf("%+v", st)
	}
}

func TestTailscaleBriefRefresh(t *testing.T) {
	ts := fakeTS(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tsRunning) })
	if ts.Brief().State != "not_running" {
		t.Fatal("default brief")
	}
	ts.RefreshBrief(context.Background())
	if b := ts.Brief(); b.State != "running" || *b.IP != "100.110.36.27" {
		t.Fatalf("%+v", b)
	}
}

type tsFake struct {
	mu                sync.Mutex
	state             string
	calls, posts, ifm []string
	notes             chan string
	code412           int
}

func (f *tsFake) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	state := f.state
	f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/localapi/v0/")
	switch {
	case r.Method == "GET" && p == "status":
		fmt.Fprintf(w, `{"BackendState":%q,"Self":{"DNSName":"zflip5.tail1234.ts.net."}}`, state)
	case r.Method == "GET" && p == "watch-ipn-bus":
		w.(http.Flusher).Flush()
		for {
			select {
			case n, ok := <-f.notes:
				if !ok {
					return
				}
				io.WriteString(w, n+"\n")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	case r.Method == "POST" && (p == "start" || p == "login-interactive" || p == "logout"):
		w.WriteHeader(204)
	case r.Method == "PATCH" && p == "prefs":
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, "BODY "+string(b))
		f.mu.Unlock()
		io.WriteString(w, "{}")
	case r.Method == "GET" && p == "prefs":
		io.WriteString(w, `{"Hostname":"zflip5","NetfilterMode":2,"CorpDNS":true,"RouteAll":false,"ShieldsUp":false,"RunSSH":false,"AdvertiseRoutes":null,"Persist":{"PrivateNodeKey":"privkey:SECRET"}}`)
	case r.Method == "GET" && p == "serve-config":
		w.Header().Set("Etag", "abc")
		io.WriteString(w, `{"TCP":{"8443":{"HTTPS":true}},"Web":{"other.ts.net:8443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9"}}}}}`)
	case r.Method == "POST" && p == "serve-config":
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.posts = append(f.posts, string(b))
		f.ifm = append(f.ifm, r.Header.Get("If-Match"))
		if f.code412 > 0 {
			f.code412--
			f.mu.Unlock()
			w.WriteHeader(412)
			return
		}
		f.mu.Unlock()
		io.WriteString(w, "{}")
	default:
		w.WriteHeader(404)
	}
}

func TestLoginReturnsBrowseURL(t *testing.T) {
	f := &tsFake{state: "NoState", notes: make(chan string, 4)}
	ts := fakeTS(t, f.handler)
	old := tailscalePrefsPath
	tailscalePrefsPath = filepath.Join(t.TempDir(), "p.json")
	t.Cleanup(func() { tailscalePrefsPath = old; close(f.notes) })
	f.notes <- `{"State":2}`
	f.notes <- `{"BrowseToURL":"https://login.tailscale.com/a/abc"}`
	state, url, code, _, err := ts.Login(context.Background())
	if err != nil || code != 200 || state != "needs_login" || url != "https://login.tailscale.com/a/abc" {
		t.Fatalf("%q %q %d %v", state, url, code, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j := strings.Join(f.calls, "|")
	if !(strings.Index(j, "watch-ipn-bus") < strings.Index(j, "POST /localapi/v0/start") &&
		strings.Index(j, "POST /localapi/v0/start") < strings.Index(j, "PATCH") &&
		strings.Index(j, "PATCH") < strings.Index(j, "login-interactive")) ||
		!strings.Contains(j, `"WantRunningSet":true`) || !strings.Contains(j, `"Hostname":"zflip5"`) {
		t.Fatalf("order or body: %s", j)
	}
}

func TestLoginAlreadyRunning(t *testing.T) {
	f := &tsFake{state: "Running"}
	state, url, code, _, _ := fakeTS(t, f.handler).Login(context.Background())
	if state != "running" || url != "" || code != 200 || strings.Contains(strings.Join(f.calls, "|"), "login-interactive") {
		t.Fatalf("%q %q %d %v", state, url, code, f.calls)
	}
}

func TestLoginNeedsMachineAuth(t *testing.T) {
	if _, _, code, reason, _ := fakeTS(t, (&tsFake{state: "NeedsMachineAuth"}).handler).Login(context.Background()); code != 409 || reason != "needs_machine_auth" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginBusy(t *testing.T) {
	ts := fakeTS(t, (&tsFake{state: "NeedsLogin"}).handler)
	ts.loginBusy.Store(true)
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 409 || reason != "busy" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginNotRunning(t *testing.T) {
	ts := newTailscaleClient(filepath.Join(t.TempDir(), "x.sock"), 18080)
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 503 || reason != "not_running" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginNoURLTimesOut(t *testing.T) {
	f := &tsFake{state: "NeedsLogin", notes: make(chan string)}
	ts := fakeTS(t, f.handler)
	old := loginURLWait
	loginURLWait = 1500 * time.Millisecond
	t.Cleanup(func() { loginURLWait = old; close(f.notes) })
	if _, _, code, reason, _ := ts.Login(context.Background()); code != 504 || reason != "no_login_url" {
		t.Fatalf("%d %q", code, reason)
	}
}

func TestLoginWatcherReappliesServe(t *testing.T) {
	f := &tsFake{state: "NeedsLogin", notes: make(chan string, 4)}
	ts := fakeTS(t, f.handler)
	t.Cleanup(func() { close(f.notes) })
	f.notes <- `{"BrowseToURL":"https://login.tailscale.com/a/abc"}`
	ts.Login(context.Background())
	f.mu.Lock()
	f.state = "Running"
	f.mu.Unlock()
	f.notes <- `{"State":6}`
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		f.mu.Lock()
		n := len(f.posts)
		f.mu.Unlock()
		if n > 0 {
			return
		}
	}
	t.Fatal("ensureServe not called after Running")
}

func TestLogoutSavesPrefs(t *testing.T) {
	f := &tsFake{state: "Running"}
	ts := fakeTS(t, f.handler)
	old := tailscalePrefsPath
	tailscalePrefsPath = filepath.Join(t.TempDir(), "p.json")
	t.Cleanup(func() { tailscalePrefsPath = old })
	if err := ts.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tailscalePrefsPath)
	fi, _ := os.Stat(tailscalePrefsPath)
	if fi.Mode().Perm() != 0o600 || strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "Persist") ||
		!strings.Contains(string(b), `"NetfilterMode":2`) || !strings.Contains(strings.Join(f.calls, "|"), "POST /localapi/v0/logout") {
		t.Fatalf("%s %v", b, f.calls)
	}
}

func TestEnsureServeMergesWithIfMatch(t *testing.T) {
	f := &tsFake{state: "Running", code412: 1}
	if err := fakeTS(t, f.handler).ensureServe(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := f.posts[len(f.posts)-1]
	if len(f.posts) != 2 || f.ifm[1] != "abc" || !strings.Contains(last, `"8443"`) || !strings.Contains(last, `other.ts.net:8443`) ||
		!strings.Contains(last, `"zflip5.tail1234.ts.net:443"`) || !strings.Contains(last, `"http://127.0.0.1:18080"`) {
		t.Fatalf("posts %v ifm %v", f.posts, f.ifm)
	}
}
