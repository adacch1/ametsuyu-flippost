package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const appPackage = "com.zflip5.modem"

var packagesListPath = "/data/system/packages.list"
var procNetTCPPaths = []string{"/proc/net/tcp", "/proc/net/tcp6"}

type ctxKey int

const ctxPeerUID ctxKey = 0

const uidCacheTTL = 30 * time.Second

// appAuth identifies the Flippost app by the UID of the socket peer.
type appAuth struct {
	mu   sync.Mutex
	uid  int
	ok   bool
	at   time.Time
	now  func() time.Time
	read func(string) ([]byte, error)
}

func newAppAuth() *appAuth {
	return &appAuth{now: time.Now, read: os.ReadFile}
}

// UID returns the app UID from packages.list. It serves the cache for 30 s
// and re-reads when the cache is stale or the package was not found.
func (a *appAuth) UID() (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ok && a.now().Sub(a.at) < uidCacheTTL {
		return a.uid, true
	}
	b, err := a.read(packagesListPath)
	if err != nil {
		a.ok = false
		return 0, false
	}
	a.uid, a.ok = parsePackagesList(b, appPackage)
	a.at = a.now()
	return a.uid, a.ok
}

// ConnContext stores the peer UID of a loopback connection in the context.
// When the lookup fails it returns ctx unchanged.
func (a *appAuth) ConnContext(ctx context.Context, c net.Conn) context.Context {
	cl, ok1 := c.RemoteAddr().(*net.TCPAddr)
	sv, ok2 := c.LocalAddr().(*net.TCPAddr)
	if !ok1 || !ok2 {
		return ctx
	}
	if uid, ok := socketUID(a.read, cl.Port, sv.Port); ok {
		return withPeerUID(ctx, uid)
	}
	return ctx
}

// IsApp reports whether the request came from the app. UID 0 is never trusted:
// tailscale serve and root callers arrive as 0.
func (a *appAuth) IsApp(r *http.Request) bool {
	peer, ok := peerUIDFrom(r.Context())
	if !ok || peer == 0 {
		return false
	}
	app, ok := a.UID()
	return ok && app != 0 && peer == app
}

func withPeerUID(ctx context.Context, uid int) context.Context {
	return context.WithValue(ctx, ctxPeerUID, uid)
}

func peerUIDFrom(ctx context.Context) (int, bool) {
	uid, ok := ctx.Value(ctxPeerUID).(int)
	return uid, ok
}

// parsePackagesList returns the UID on the line whose first field is pkg.
func parsePackagesList(b []byte, pkg string) (int, bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == pkg {
			if uid, err := strconv.Atoi(f[1]); err == nil {
				return uid, true
			}
		}
	}
	return 0, false
}

func hexPort(addr string) (int, bool) {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0, false
	}
	p, err := strconv.ParseUint(addr[i+1:], 16, 16)
	return int(p), err == nil
}

// parseProcNetTCP returns the UID of the client-side row: local port is the
// client port and remote port is the server port.
func parseProcNetTCP(b []byte, clientPort, serverPort int) (int, bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		lp, ok1 := hexPort(f[1])
		rp, ok2 := hexPort(f[2])
		if !ok1 || !ok2 || lp != clientPort || rp != serverPort {
			continue
		}
		if uid, err := strconv.Atoi(f[7]); err == nil {
			return uid, true
		}
	}
	return 0, false
}

func socketUID(read func(string) ([]byte, error), clientPort, serverPort int) (int, bool) {
	for _, p := range procNetTCPPaths {
		if b, err := read(p); err == nil {
			if uid, ok := parseProcNetTCP(b, clientPort, serverPort); ok {
				return uid, true
			}
		}
	}
	return 0, false
}
