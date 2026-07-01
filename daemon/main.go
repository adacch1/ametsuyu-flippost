package main

import (
	"flag"
	"log"
	"strconv"
)

func itoa(i int) string { return strconv.Itoa(i) }

func main() {
	cfgPath := flag.String("config", "/data/adb/zflip5-modem/config.json", "path to config.json")
	flag.Parse()

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	srv := NewServer(cfg, deviceCollector{})
	log.Fatal(srv.ListenAndServe())
}
