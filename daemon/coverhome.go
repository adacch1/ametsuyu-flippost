package main

import (
	"fmt"
	"os"
	"strings"
)

// Cover-screen home. The kiosk owns the Flex Window because the module's wake
// watcher relaunches it and the stock cover home is disabled — "normal mode" is
// exactly the reverse of those two. The watcher reads the same flag file this
// writes, so the daemon and the watcher can never disagree about which mode the
// phone is in, including across a reboot.
//
// This firmware has no generic secondary-home resolution (SystemUI starts
// SubHomeActivity explicitly), which is why leaving the mode half-applied shows
// a BLACK cover screen rather than falling back to anything.
const coverHomeOffFlag = "/data/adb/zflip5-modem/cover-home-off"

var coverHomeRivals = []string{
	"com.android.systemui/.subscreen.SubHomeActivity",
	"com.sec.android.app.launcher/com.honeyspace.dexservice.SecondaryLauncher",
}

const coverSubHome = "com.android.systemui/.subscreen.SubHomeActivity"

// Cover-panel brightness. The Flex Window is now an always-on display (the
// kiosk holds FLAG_KEEP_SCREEN_ON and service.sh keeps the phone awake on a
// charger), and a permanently lit OLED at the stock level both burns in and
// costs real current. So the kiosk runs the panel dim while it owns it and
// hands the owner's own level back when it does not.
//
// The previous values are recorded in the data dir rather than in memory: the
// daemon restarts far more often than the owner changes brightness, and
// uninstall.sh reads the same two files to put the panel back.
const (
	coverBrightPrev     = "/data/adb/zflip5-modem/cover-bright.prev"
	coverBrightModePrev = "/data/adb/zflip5-modem/cover-bright-mode.prev"
	coverDimDefault     = 30
)

// setCoverDim runs the cover panel at level, saving the owner's brightness the
// first time so restoreCoverBrightness can undo it. level 0 restores instead.
// Auto-brightness has to go with it: left on, the framework writes the level
// straight back the next time the light sensor moves.
func setCoverDim(level int) {
	if level <= 0 {
		restoreCoverBrightness()
		return
	}
	if _, err := os.Stat(coverBrightPrev); err != nil {
		saveSetting(coverBrightPrev, "sub_screen_brightness")
		saveSetting(coverBrightModePrev, "sub_screen_brightness_mode")
	}
	runCmd("settings", "put", "system", "sub_screen_brightness_mode", "0")
	runCmd("settings", "put", "system", "sub_screen_brightness", itoa(level))
}

// restoreCoverBrightness puts back whatever was saved, then forgets it — so a
// later dim re-reads the owner's current choice instead of a stale one.
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

// saveSetting records one Settings.System value for the restore path. A read
// that fails writes nothing, so restore skips the key rather than shoving a
// guessed default at the panel.
func saveSetting(path, key string) {
	v := strings.TrimSpace(runCmd("settings", "get", "system", key))
	if v == "" || v == "null" {
		return
	}
	_ = os.WriteFile(path, []byte(v), 0o600)
}

// coverHomeOn reports whether the kiosk is the cover home.
func coverHomeOn() bool {
	_, err := os.Stat(coverHomeOffFlag)
	return err != nil
}

// setCoverHome switches the cover screen between the kiosk and the stock clock.
// Turning it OFF hands the panel back immediately; turning it ON only clears the
// flag and disables the rivals, because the watcher relaunches the kiosk within
// a few seconds on its own — one relaunch path instead of two that can diverge.
func setCoverHome(on bool, dim int) error {
	pm := "enable"
	if on {
		pm = "disable"
	}
	for _, c := range coverHomeRivals {
		runCmd("pm", pm, c)
	}
	if on {
		if err := os.Remove(coverHomeOffFlag); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear flag: %w", err)
		}
		setCoverDim(dim)
		return nil
	}
	// Handing the panel back means handing the brightness back with it.
	restoreCoverBrightness()
	// Write the flag BEFORE handing the panel over: if the write fails the
	// watcher would fight the stock clock for the screen.
	if err := os.WriteFile(coverHomeOffFlag, []byte("kiosk is not the cover home\n"), 0o600); err != nil {
		return fmt.Errorf("write flag: %w", err)
	}
	runCmd("am", "start", "--user", "0", "--display", "1", "-n", coverSubHome)
	return nil
}
