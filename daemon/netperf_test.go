package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetPerfApplyRestore(t *testing.T) {
	dir := t.TempDir()
	for rel, orig := range map[string]string{
		"net/ipv4/tcp_congestion_control":    "bic",
		"net/ipv4/tcp_slow_start_after_idle": "1",
		"net/ipv4/tcp_mtu_probing":           "0",
		"net/core/default_qdisc":             "pfifo_fast",
	} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZF5_NETPERF_ROOT", dir)
	var n NetPerf
	n.Apply()
	got, _ := os.ReadFile(filepath.Join(dir, "net/ipv4/tcp_congestion_control"))
	if strings.TrimSpace(string(got)) != "cubic" {
		t.Fatalf("congestion control = %q, want cubic", got)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "net/ipv4/tcp_slow_start_after_idle"))
	if strings.TrimSpace(string(got)) != "0" {
		t.Fatalf("slow_start_after_idle = %q, want 0", got)
	}
	// knobs missing from the fake tree must be skipped, not crash
	if len(n.originals) != 4 {
		t.Fatalf("originals = %d, want 4", len(n.originals))
	}
	n.Restore()
	got, _ = os.ReadFile(filepath.Join(dir, "net/ipv4/tcp_congestion_control"))
	if strings.TrimSpace(string(got)) != "bic" {
		t.Fatalf("restore = %q, want bic", got)
	}
}
