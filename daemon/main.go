package main

import (
	"flag"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone DB: Android has no zoneinfo Go can read
)

func itoa(i int) string { return strconv.Itoa(i) }

func main() {
	// Static (CGO_ENABLED=0) binary: Go's TLS looks for CA certs in Linux paths
	// that don't exist on Android, so outbound HTTPS (the speedtest) fails cert
	// verification. Point it at Android's own CA store.
	if os.Getenv("SSL_CERT_DIR") == "" {
		os.Setenv("SSL_CERT_DIR", "/system/etc/security/cacerts:/apex/com.android.conscrypt/cacerts")
	}
	// Static GOOS=linux build on Android: no /etc/localtime, no zoneinfo dir and
	// TZ unset, so time.Local silently falls back to UTC and the usage day
	// buckets roll at 07:00 local. Resolve the device zone from the embedded
	// tzdata. LoadLocation("") returns UTC with a nil error, so without the
	// explicit non-empty check below a host with no getprop (a Mac, a CI
	// runner) would have its real zone overwritten with UTC instead of left
	// alone; an unrecognized zone still falls through to today's behavior.
	if os.Getenv("TZ") == "" {
		if zone := strings.TrimSpace(runCmd("getprop", "persist.sys.timezone")); zone != "" {
			if loc, err := time.LoadLocation(zone); err == nil {
				time.Local = loc
			}
		}
	}
	cfgPath := flag.String("config", "/data/adb/zflip5-modem/config.json", "path to config.json")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	srv := NewServer(cfg, deviceCollector{})
	srv.cfgPath = *cfgPath
	// The kiosk survives reboots, so its panel brightness has to as well. Done
	// here rather than in NewServer: NewServer runs in tests, and this shells
	// out to the device.
	if coverHomeOn() {
		setCoverDim(cfg.CoverDim())
	} else {
		restoreCoverBrightness()
	}
	log.Fatal(srv.ListenAndServe())
}
