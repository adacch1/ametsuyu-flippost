package main

import "strings"

// USB tethering control: start/stop/status via the root helper (see
// helper/tether/UsbTether.java), run the same way as the Wi-Fi hotspot helper
// (runHelper -> app_process). Unlike the hotspot, there's no fixed kernel
// interface name to check directly (varies by build), so status comes from
// the helper querying the framework's own tethered-interface list.

type UsbTetherStatus struct {
	Active bool     `json:"active"`
	Ifaces []string `json:"ifaces"`
}

// parseUsbTetherStatus extracts ACTIVE/IFACES from the UsbTether "status"
// helper output. Only trusted if RESULT=OK arrived.
func parseUsbTetherStatus(out string) (UsbTetherStatus, bool) {
	var st UsbTetherStatus
	ok := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "RESULT=OK":
			ok = true
		case strings.HasPrefix(line, "ACTIVE="):
			st.Active = strings.TrimPrefix(line, "ACTIVE=") == "true"
		case strings.HasPrefix(line, "IFACES="):
			if csv := strings.TrimPrefix(line, "IFACES="); csv != "" {
				st.Ifaces = strings.Split(csv, ",")
			}
		}
	}
	if st.Ifaces == nil {
		st.Ifaces = []string{}
	}
	return st, ok
}

func usbTetherStatus() UsbTetherStatus {
	st, _ := parseUsbTetherStatus(runHelper("com.zflip5.tether.UsbTether", "status"))
	return st
}

func startUsbTether() bool {
	return strings.Contains(runHelper("com.zflip5.tether.UsbTether", "start"), "RESULT=STARTED")
}

func stopUsbTether() bool {
	return strings.Contains(runHelper("com.zflip5.tether.UsbTether", "stop"), "RESULT=STOPPED")
}
