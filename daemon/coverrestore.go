package main

import (
	"os"
	"strings"
)

// Cover-panel brightness saved by older builds. The kiosk no longer dims the
// panel, but upgrades and uninstall.sh still read these two files to put the
// owner's level back.
const (
	coverBrightPrev     = "/data/adb/zflip5-modem/cover-bright.prev"
	coverBrightModePrev = "/data/adb/zflip5-modem/cover-bright-mode.prev"
)

// restoreCoverBrightness puts back whatever was saved, then deletes the saved
// files so a stale value never overrides a later owner choice.
func restoreCoverBrightness() {
	for _, r := range []struct{ path, key string }{
		{coverBrightModePrev, "sub_screen_brightness_mode"},
		{coverBrightPrev, "sub_screen_brightness"},
	} {
		b, err := os.ReadFile(r.path)
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(b)); v != "" && v != "null" {
			runCmd("settings", "put", "system", r.key, v)
		}
		os.Remove(r.path)
	}
}
