package main

import "strings"

// BandsReport describes the radio-access-technology allowance and the active
// serving band. It is READ-ONLY: the daemon never writes a modem band mask
// (that is a baseband engineering action, outside this module's safety envelope
// which forbids baseband modification). "Unlocking all bands" here means every
// RAT — including 5G NR — is permitted, after which the modem selects among all
// bands it supports on its own.
type BandsReport struct {
	AllowedTypes string `json:"allowed_types"` // raw from get-allowed-network-types-for-users
	NrEnabled    bool   `json:"nr_enabled"`
	IsMax        bool   `json:"is_max"` // all RATs incl NR permitted
	CurrentTech  string `json:"current_tech"`
	CurrentBand  int    `json:"current_band"`
	NrBand       int    `json:"nr_band,omitempty"`
	Note         string `json:"note"`
	Available    bool   `json:"available"`
}

func deviceBands() BandsReport {
	at := readAllowedTypes()
	sig := deviceSignal()
	nr := strings.Contains(at, "NR")
	b := BandsReport{
		AllowedTypes: at,
		NrEnabled:    nr,
		IsMax:        nr && strings.Contains(at, "LTE"),
		CurrentTech:  sig.Tech,
		CurrentBand:  sig.Band,
		NrBand:       sig.NrBand,
		Available:    at != "",
		Note: "All RATs (2G/3G/4G/5G-NR) permitted; the modem selects among every " +
			"band it supports. Per-band locking is a baseband engineering action and " +
			"is intentionally not forced (no baseband modification).",
	}
	return b
}
