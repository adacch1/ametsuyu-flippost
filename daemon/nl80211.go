package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
)

// Station holds the counters nl80211 reports for one associated client.
type Station struct {
	MAC        string
	RXBytes    uint64
	TXBytes    uint64
	SignalDBm  int
	InactiveMs uint32
}

const (
	nlmsgError = 2
	nlmsgDone  = 3

	nlmFRequest = 0x1
	nlmFDump    = 0x300

	nl80211CmdGetStation = 17
	ctrlCmdGetFamily     = 3
	genlIDCtrl           = 0x10

	nl80211AttrIfindex = 3
	nl80211AttrMAC     = 6
	nl80211AttrStaInfo = 21

	staInfoInactive = 1
	staInfoRX       = 2
	staInfoTX       = 3
	staInfoSignal   = 7
	staInfoRX64     = 23
	staInfoTX64     = 24

	nlaTypeMask = 0x3fff
)

func align4(n int) int { return (n + 3) &^ 3 }

// eachAttr calls f for every attribute in b and returns an error on a malformed length.
func eachAttr(b []byte, f func(typ uint16, val []byte)) error {
	for len(b) > 0 {
		if len(b) < 4 {
			return errors.New("nl80211: truncated attribute header")
		}
		l := int(binary.LittleEndian.Uint16(b[0:]))
		if l < 4 || l > len(b) {
			return errors.New("nl80211: bad attribute length")
		}
		f(binary.LittleEndian.Uint16(b[2:])&nlaTypeMask, b[4:l])
		if align4(l) >= len(b) {
			break
		}
		b = b[align4(l):]
	}
	return nil
}

// parseStations decodes the concatenated reply of a GET_STATION dump.
func parseStations(b []byte) ([]Station, error) {
	var out []Station
	for len(b) > 0 {
		if len(b) < 16 {
			return nil, errors.New("nl80211: truncated message header")
		}
		l := int(binary.LittleEndian.Uint32(b[0:]))
		if l < 16 || l > len(b) {
			return nil, errors.New("nl80211: truncated message")
		}
		msg := b[:l]
		switch typ := binary.LittleEndian.Uint16(b[4:]); typ {
		case nlmsgDone:
			return out, nil
		case nlmsgError:
			if len(msg) < 20 {
				return nil, errors.New("nl80211: truncated error message")
			}
			if e := int32(binary.LittleEndian.Uint32(msg[16:])); e != 0 {
				return nil, fmt.Errorf("nl80211: %w", syscall.Errno(-e))
			}
		default:
			if len(msg) < 20 {
				return nil, errors.New("nl80211: truncated genl header")
			}
			s, ok, err := parseStation(msg[20:])
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, s)
			}
		}
		if align4(l) >= len(b) {
			break
		}
		b = b[align4(l):]
	}
	return out, nil
}

func parseStation(attrs []byte) (Station, bool, error) {
	var s Station
	var haveMAC bool
	var rx32, tx32, rx64, tx64 uint64
	var have64rx, have64tx bool
	err := eachAttr(attrs, func(typ uint16, v []byte) {
		switch {
		case typ == nl80211AttrMAC && len(v) == 6:
			s.MAC = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", v[0], v[1], v[2], v[3], v[4], v[5])
			haveMAC = true
		case typ == nl80211AttrStaInfo:
			eachAttr(v, func(t uint16, x []byte) {
				switch {
				case t == staInfoInactive && len(x) >= 4:
					s.InactiveMs = binary.LittleEndian.Uint32(x)
				case t == staInfoRX && len(x) >= 4:
					rx32 = uint64(binary.LittleEndian.Uint32(x))
				case t == staInfoTX && len(x) >= 4:
					tx32 = uint64(binary.LittleEndian.Uint32(x))
				case t == staInfoSignal && len(x) >= 1:
					s.SignalDBm = int(int8(x[0]))
				case t == staInfoRX64 && len(x) >= 8:
					rx64, have64rx = binary.LittleEndian.Uint64(x), true
				case t == staInfoTX64 && len(x) >= 8:
					tx64, have64tx = binary.LittleEndian.Uint64(x), true
				}
			})
		}
	})
	if err != nil {
		return s, false, err
	}
	s.RXBytes, s.TXBytes = rx32, tx32
	if have64rx {
		s.RXBytes = rx64
	}
	if have64tx {
		s.TXBytes = tx64
	}
	return s, haveMAC, nil
}
