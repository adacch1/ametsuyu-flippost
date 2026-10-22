package main

import (
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

func TestParseStationsFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/station_dump.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseStations(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Station{
		{MAC: "aa:bb:cc:dd:ee:ff", RXBytes: 5_000_000_000, TXBytes: 9_000_000_000, SignalDBm: -48, InactiveMs: 120},
		{MAC: "11:22:33:44:55:66", RXBytes: 1000, TXBytes: 2000, SignalDBm: -71},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseStationsTruncated(t *testing.T) {
	b, _ := os.ReadFile("testdata/station_dump.bin")
	if _, err := parseStations(b[:30]); err == nil {
		t.Fatal("truncated dump parsed without error")
	}
}

func TestParseStationsNetlinkError(t *testing.T) {
	b := make([]byte, 36)
	binary.LittleEndian.PutUint32(b[0:], 36)
	binary.LittleEndian.PutUint16(b[4:], 2)
	binary.LittleEndian.PutUint32(b[16:], uint32(0xffffffed)) // -19 ENODEV
	if _, err := parseStations(b); err == nil {
		t.Fatal("NLMSG_ERROR ignored")
	}
}
