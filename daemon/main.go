package main

import (
	"flag"
	"log"
	"os"
	"strconv"
)

func itoa(i int) string { return strconv.Itoa(i) }

func main() {
	// Static (CGO_ENABLED=0) binary: Go's TLS looks for CA certs in Linux paths
	// that don't exist on Android, so outbound HTTPS (the speedtest) fails cert
	// verification. Point it at Android's own CA store.
	if os.Getenv("SSL_CERT_DIR") == "" {
		os.Setenv("SSL_CERT_DIR", "/system/etc/security/cacerts:/apex/com.android.conscrypt/cacerts")
	}
	cfgPath := flag.String("config", "/data/adb/zflip5-modem/config.json", "path to config.json")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	srv := NewServer(cfg, deviceCollector{})
	srv.cfgPath = *cfgPath
	log.Fatal(srv.ListenAndServe())
}
