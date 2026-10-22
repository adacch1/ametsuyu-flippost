//go:build !linux

package main

import "errors"

func dumpStations(iface string) ([]Station, error) {
	return nil, errors.New("nl80211: unsupported platform")
}
