package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func dev(rx0, tx0 uint64) string {
	return fmt.Sprintf("Inter-|   Receive\n face |bytes\n  rmnet_data0: %d 1 0 0 0 0 0 0 %d 1 0 0 0 0 0 0\n    swlan0: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n", rx0, tx0)
}

// newTestSampler: readDev and dump each advance on their own counter.
func newTestSampler(devs []string, dumps [][]Station) (*speedSampler, *time.Time) {
	now := time.Unix(1000, 0)
	i, j := 0, 0
	sp := newSpeedSampler()
	sp.now = func() time.Time { return now }
	sp.readDev = func() (string, error) { s := devs[min(j, len(devs)-1)]; j++; return s, nil }
	sp.dump = func(string) ([]Station, error) { d := dumps[min(i, len(dumps)-1)]; i++; return d, nil }
	return sp, &now
}

var reach = []Client{{MAC: "aa:bb:cc:dd:ee:ff", IPv4: "192.168.1.23", State: "REACHABLE"}}

func TestSpeedPerClientAndPhone(t *testing.T) {
	sp, now := newTestSampler([]string{dev(1_000_000, 100_000), dev(3_500_000, 400_000)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff", SignalDBm: -48}}, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 2_488_000, RXBytes: 296_000, SignalDBm: -48}}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	s := sp.Snapshot()
	pc, ok := s.PerClient.([]ClientSpeed)
	if !ok || len(pc) != 1 || pc[0].DownBytesS != 2_488_000 || pc[0].UpBytesS != 296_000 || *pc[0].IPv4 != "192.168.1.23" || *pc[0].SignalDBm != -48 {
		t.Fatalf("per_client %+v", s.PerClient)
	}
	if s.WanDownBytesS != 2_500_000 || s.WanUpBytesS != 300_000 || *s.PhoneDownBytesS != 12_000 || *s.PhoneUpBytesS != 4_000 || s.IntervalMs != 1000 {
		t.Fatalf("snap %+v", s)
	}
}

func TestSpeedDumpErrorUnavailable(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000)}, [][]Station{nil})
	sp.dump = func(string) ([]Station, error) { return nil, errors.New("ENODEV") }
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	if s := sp.Snapshot(); s.PerClient != "unavailable" || s.PhoneDownBytesS != nil {
		t.Fatalf("%+v", s)
	}
}

func TestSpeedZeroStationsWithReachableClient(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000)}, [][]Station{{}, {}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	if sp.Snapshot().PerClient != "unavailable" {
		t.Fatal("0 stations with a REACHABLE neighbour must be unavailable")
	}
}

func TestSpeedUnchangedCountersMarkUnavailable(t *testing.T) { // Review Focus 4 (sibling)
	st := []Station{{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 5, RXBytes: 5}}
	sp, now := newTestSampler([]string{dev(0, 0), dev(1e6, 1e5), dev(2e6, 2e5), dev(3e6, 3e5)}, [][]Station{st})
	for i := 0; i < 4; i++ {
		sp.tick([]string{"swlan0"}, reach)
		*now = now.Add(time.Second)
	}
	if sp.Snapshot().PerClient != "unavailable" {
		t.Fatal("3 unchanged ticks with WAN traffic must be unavailable")
	}
}

func TestSpeedRecoversAfterUnavailable(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 1000), dev(2000, 2000)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff"}}, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 500}}})
	errOnce := true
	inner := sp.dump
	sp.dump = func(i string) ([]Station, error) {
		if errOnce && sp.prevAt.Unix() == 1000 {
			errOnce = false
			return nil, errors.New("x")
		}
		return inner(i)
	}
	for i := 0; i < 3; i++ {
		sp.tick([]string{"swlan0"}, reach)
		*now = now.Add(time.Second)
	}
	if _, ok := sp.Snapshot().PerClient.([]ClientSpeed); !ok {
		t.Fatal("did not recover")
	}
}

func TestSpeedCounterResetIsZero(t *testing.T) { // Review Focus 4
	sp, now := newTestSampler([]string{dev(9_000_000, 9_000_000), dev(100, 100)},
		[][]Station{{{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 1e9}}, {{MAC: "aa:bb:cc:dd:ee:ff", TXBytes: 10}}})
	sp.tick([]string{"swlan0"}, reach)
	*now = now.Add(time.Second)
	sp.tick([]string{"swlan0"}, reach)
	s := sp.Snapshot()
	if s.WanDownBytesS != 0 || s.PerClient.([]ClientSpeed)[0].DownBytesS != 0 {
		t.Fatalf("reset produced a huge rate: %+v", s)
	}
}

func TestSpeedNoAPIfaces(t *testing.T) {
	sp, now := newTestSampler([]string{dev(0, 0), dev(1000, 0)}, nil)
	sp.tick(nil, nil)
	*now = now.Add(time.Second)
	sp.tick(nil, nil)
	s := sp.Snapshot()
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"per_client":[]`) || *s.PhoneDownBytesS != 1000 {
		t.Fatalf("%s", b)
	}
}
