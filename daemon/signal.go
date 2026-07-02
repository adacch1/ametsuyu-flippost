package main

import (
	"regexp"
	"strconv"
)

// Signal is a detailed radio snapshot for the serving cell: technology, the
// active LTE band + measurements, and NR (5G) fields when attached. Parsed from
// `dumpsys telephony.registry` — read-only, never touches the modem.
type Signal struct {
	Tech     string `json:"tech"`    // raw network: LTE / NR / ...
	Display  string `json:"display"` // effective tech: 5G+/5G/4G+/4G (NSA-aware)
	Operator string `json:"operator"`
	NrState  string `json:"nr_state"`            // NONE / RESTRICTED / NOT_RESTRICTED / CONNECTED
	CA       bool   `json:"carrier_aggregation"` // isUsingCarrierAggregation
	Level    int    `json:"level"`               // 0..4 bars
	Mcc      string `json:"mcc"`
	Mnc      string `json:"mnc"`

	// LTE serving cell
	Band   int `json:"band"`
	Earfcn int `json:"earfcn"`
	PCI    int `json:"pci"`
	TAC    int `json:"tac"`
	Rsrp   int `json:"rsrp_dbm"` // signal power
	Rsrq   int `json:"rsrq_db"`  // signal quality
	Sinr   int `json:"sinr_db"`  // rssnr
	Rssi   int `json:"rssi_dbm"`

	// NR (5G) when present
	NrBand  int `json:"nr_band,omitempty"`
	NrArfcn int `json:"nrarfcn,omitempty"`
	NrRsrp  int `json:"nr_rsrp_dbm,omitempty"`
	NrRsrq  int `json:"nr_rsrq_db,omitempty"`
	NrSinr  int `json:"nr_sinr_db,omitempty"`

	Available bool `json:"available"`
}

const intUnavail = 2147483647 // Integer.MAX_VALUE — Android's "measurement unavailable"

var (
	sigLteRe  = regexp.MustCompile(`CellSignalStrengthLte:\s*rssi=(-?\d+)\s+rsrp=(-?\d+)\s+rsrq=(-?\d+)\s+rssnr=(-?\d+).*?level=(\d+)`)
	sigNrRe   = regexp.MustCompile(`CellSignalStrengthNr[:{].*?ssRsrp\s*=\s*(-?\d+).*?ssRsrq\s*=\s*(-?\d+).*?ssSinr\s*=\s*(-?\d+)`)
	idLteRe   = regexp.MustCompile(`CellIdentityLte[:{](.*?)\}`)
	idNrRe    = regexp.MustCompile(`CellIdentityNr[:{](.*?)\}`)
	kvIntRe   = func(k string) *regexp.Regexp { return regexp.MustCompile(k + `\s*=\s*(-?\d+)`) }
	kvBandsRe = regexp.MustCompile(`mBands\s*=\s*\[(\d+)`)
	dataRatRe = regexp.MustCompile(`getRilDataRadioTechnology=\d+\(([A-Za-z0-9_+]+)\)`)
	caRe      = regexp.MustCompile(`isUsingCarrierAggregation=(true|false)`)
	mccRe     = kvIntRe("mMcc")
	mncRe     = kvIntRe("mMnc")
)

// atoiSane returns the parsed int, or 0 if it's Android's "unavailable" sentinel.
func atoiSane(s string) int {
	v, _ := strconv.Atoi(s)
	if v == intUnavail || v == -intUnavail {
		return 0
	}
	return v
}

// parseSignal extracts a detailed radio snapshot from `dumpsys telephony.registry`.
func parseSignal(raw string) Signal {
	s := Signal{}
	if raw == "" {
		return s
	}
	// Technology + operator + NR state reuse the network scrape's patterns.
	n := parseNetwork(raw)
	s.Tech = n.Type
	if s.Tech == "" {
		if m := dataRatRe.FindStringSubmatch(raw); m != nil {
			s.Tech = m[1]
		}
	}
	s.Display = n.Display
	if s.Display == "" && s.Tech != "" { // display info absent; derive from RAT
		s.Display = displayTech(s.Tech, "", n.NrState)
	}
	s.Operator = n.Operator
	s.NrState = n.NrState
	if m := caRe.FindStringSubmatch(raw); m != nil {
		s.CA = m[1] == "true"
	}

	if m := sigLteRe.FindStringSubmatch(raw); m != nil {
		s.Rssi = atoiSane(m[1])
		s.Rsrp = atoiSane(m[2])
		s.Rsrq = atoiSane(m[3])
		s.Sinr = atoiSane(m[4])
		s.Level, _ = strconv.Atoi(m[5])
		s.Available = true
	}
	if m := idLteRe.FindStringSubmatch(raw); m != nil {
		blk := m[1]
		s.Band = firstInt(kvBandsRe, blk)
		s.Earfcn = kv(blk, "mEarfcn")
		s.PCI = kv(blk, "mPci")
		s.TAC = kv(blk, "mTac")
		if mm := mccRe.FindStringSubmatch(blk); mm != nil {
			s.Mcc = mm[1]
		}
		if mm := mncRe.FindStringSubmatch(blk); mm != nil {
			s.Mnc = mm[1]
		}
	}

	// NR (5G): only populated once attached to an NR cell.
	if m := sigNrRe.FindStringSubmatch(raw); m != nil {
		s.NrRsrp = atoiSane(m[1])
		s.NrRsrq = atoiSane(m[2])
		s.NrSinr = atoiSane(m[3])
		s.Available = true
	}
	if m := idNrRe.FindStringSubmatch(raw); m != nil {
		blk := m[1]
		s.NrBand = firstInt(kvBandsRe, blk)
		s.NrArfcn = kv(blk, "mNrArfcn")
	}
	return s
}

func firstInt(re *regexp.Regexp, blk string) int {
	if m := re.FindStringSubmatch(blk); m != nil {
		v, _ := strconv.Atoi(m[1])
		return v
	}
	return 0
}

func kv(blk, key string) int {
	if m := kvIntRe(key).FindStringSubmatch(blk); m != nil {
		return atoiSane(m[1])
	}
	return 0
}

// deviceSignal reads the live radio snapshot on-device.
func deviceSignal() Signal {
	return parseSignal(runCmd("dumpsys", "telephony.registry"))
}
