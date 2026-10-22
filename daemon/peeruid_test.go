package main

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

const pkgList = "com.termux 10325 0 /data/user/0/com.termux default 3003\n" +
	"com.zflip5.modem.debug 999 0 /x default none\n" +
	"com.zflip5.modem 10234 0 /data/user/0/com.zflip5.modem default:targetSdkVersion=34 3003\n"

const tcpHdr = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func tcpRow(local, rem string, uid int) string {
	return fmt.Sprintf("   1: %s %s 01 00000000:00000000 00:00000000 00000000 %5d        0 1 1 0000000000000000 20 4 30 10 -1\n", local, rem, uid)
}

func TestParsePackagesList(t *testing.T) {
	if uid, ok := parsePackagesList([]byte(pkgList), appPackage); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
	if _, ok := parsePackagesList([]byte("com.termux 10325 0 /x\n"), appPackage); ok {
		t.Fatal("matched a missing package")
	}
}

func TestParseProcNetTCPClientRowOnly(t *testing.T) {
	b := tcpHdr + tcpRow("0100007F:46A0", "00000000:0000", 0) + tcpRow("0100007F:46A0", "0100007F:C350", 0) + tcpRow("0100007F:C350", "0100007F:46A0", 10234)
	if uid, ok := parseProcNetTCP([]byte(b), 0xC350, 18080); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
	if _, ok := parseProcNetTCP([]byte(tcpHdr+tcpRow("0100007F:46A0", "0100007F:C350", 0)), 0xC350, 18080); ok {
		t.Fatal("matched the server-side row")
	}
}

func TestSocketUIDFallsBackToTCP6(t *testing.T) {
	read := func(p string) ([]byte, error) {
		if p == "/proc/net/tcp6" {
			return []byte(tcpHdr + tcpRow("0000000000000000FFFF00000100007F:C350", "0000000000000000FFFF00000100007F:46A0", 10234)), nil
		}
		return []byte(tcpHdr), nil
	}
	if uid, ok := socketUID(read, 0xC350, 18080); !ok || uid != 10234 {
		t.Fatalf("got %d %v", uid, ok)
	}
}

func TestIsAppNeverTrustsUID0(t *testing.T) { // Review Focus 5
	a := newAppAuth()
	a.read = func(string) ([]byte, error) { return []byte("com.zflip5.modem 0 0 /x\n"), nil }
	r := httptest.NewRequest("GET", "/", nil)
	if a.IsApp(r.WithContext(withPeerUID(r.Context(), 0))) {
		t.Fatal("uid 0 trusted")
	}
}

func TestConnContextIsApp(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	cli, _ := net.Dial("tcp", ln.Addr().String())
	defer cli.Close()
	srv, _ := ln.Accept()
	defer srv.Close()
	cp, sp := cli.LocalAddr().(*net.TCPAddr).Port, ln.Addr().(*net.TCPAddr).Port
	a := newAppAuth()
	a.read = func(p string) ([]byte, error) {
		switch p {
		case packagesListPath:
			return []byte(pkgList), nil
		case "/proc/net/tcp":
			return []byte(tcpHdr + tcpRow(fmt.Sprintf("0100007F:%04X", sp), fmt.Sprintf("0100007F:%04X", cp), 0) +
				tcpRow(fmt.Sprintf("0100007F:%04X", cp), fmt.Sprintf("0100007F:%04X", sp), 10234)), nil
		}
		return []byte(tcpHdr), nil
	}
	if !a.IsApp(httptest.NewRequest("GET", "/", nil).WithContext(a.ConnContext(context.Background(), srv))) {
		t.Fatal("app connection not recognised")
	}
	if a.IsApp(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("unknown peer trusted")
	}
}

func TestUIDCacheRefreshes(t *testing.T) {
	now := time.Unix(1000, 0)
	list := pkgList
	a := newAppAuth()
	a.now = func() time.Time { return now }
	a.read = func(string) ([]byte, error) { return []byte(list), nil }
	if uid, _ := a.UID(); uid != 10234 {
		t.Fatal(uid)
	}
	list = "com.zflip5.modem 10400 0 /x\n"
	if uid, _ := a.UID(); uid != 10234 {
		t.Fatal("cache not used within 30 s")
	}
	now = now.Add(31 * time.Second)
	if uid, _ := a.UID(); uid != 10400 {
		t.Fatal("cache not refreshed after 30 s")
	}
}
