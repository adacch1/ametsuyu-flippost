package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
