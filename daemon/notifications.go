package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Notification reader: root `dumpsys notification --noredact`, parsed down to
// the ACTIVE list only. Text runs through bodyText like SMS does, which on this
// phone is a pass-through. Shares the `sms` scope, gate and rate limit at the
// HTTP layer — same asset class. Read-only; never forwarded.

const notifMaxLimit = 20

type Notification struct {
	Pkg   string `json:"pkg"`
	Title string `json:"title"` // verbatim unless redactBodies is on
	Text  string `json:"text"`  // verbatim unless redactBodies is on
	When  string `json:"when"`  // epoch ms, as printed by dumpsys
}

var (
	notifPkgRe   = regexp.MustCompile(`NotificationRecord\(.*?pkg=(\S+)`)
	notifWhenRe  = regexp.MustCompile(`^when=(\d+)$`)
	notifExtraRe = regexp.MustCompile(`^android\.(title|text)=String \((.*)\)$`)
)

// notifDump is the reader seam (overridable in tests).
var notifDump = func() (string, error) {
	out, err := exec.Command("dumpsys", "notification", "--noredact").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// extraValue pulls an android.title/android.text extra off one dumps line.
// The strict form (greedy to the trailing paren) keeps nested parens intact —
// "Charging (3 m until full)". A body containing a newline is printed across
// several lines by dumpsys and has no trailing paren on the first one; that
// falls back to the first line, which is all a 120-char preview shows anyway.
func extraValue(line string) (key, val string, ok bool) {
	if m := notifExtraRe.FindStringSubmatch(line); m != nil {
		return m[1], m[2], true
	}
	for _, k := range []string{"title", "text"} {
		p := "android." + k + "=String ("
		if strings.HasPrefix(line, p) {
			return k, line[len(p):], true
		}
	}
	return "", "", false
}

// parseNotifications reads the "Notification List:" block only. Every later
// block — snoozed, and above all "History Notification List:" — is dismissed
// content the owner already cleared, so parsing must stop at the first
// following section header.
func parseNotifications(raw string, limit int) []Notification {
	var out []Notification
	var cur *Notification
	inList := false
	flush := func() {
		// Group-summary records carry neither title nor text; they would render
		// as a blank row.
		if cur != nil && (cur.Title != "" || cur.Text != "") {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, ln := range strings.Split(raw, "\n") {
		if !inList {
			if strings.TrimSpace(ln) == "Notification List:" {
				inList = true
			}
			continue
		}
		// Section headers sit at exactly two spaces of indent; records are deeper.
		if len(ln) > 2 && ln[0] == ' ' && ln[1] == ' ' && ln[2] != ' ' {
			break
		}
		t := strings.TrimSpace(ln)
		if m := notifPkgRe.FindStringSubmatch(t); m != nil {
			flush()
			if len(out) >= limit {
				return out
			}
			cur = &Notification{Pkg: m[1]}
			continue
		}
		if cur == nil {
			continue
		}
		if m := notifWhenRe.FindStringSubmatch(t); m != nil && cur.When == "" {
			cur.When = m[1]
			continue
		}
		if k, v, ok := extraValue(t); ok {
			// First extras block wins: a record's own notification is printed
			// before its `publicNotification=` (lock-screen) copy, and the real
			// title is what the owner asked to read.
			v = bodyText(v)
			if k == "title" && cur.Title == "" {
				cur.Title = v
			} else if k == "text" && cur.Text == "" {
				cur.Text = v
			}
		}
	}
	flush()
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// recentNotifications returns up to `limit` active notifications, or an
// error the caller maps to a safe no-permission response.
func recentNotifications(limit int) ([]Notification, error) {
	if limit <= 0 || limit > notifMaxLimit {
		return nil, fmt.Errorf("limit out of range (1..%d)", notifMaxLimit)
	}
	raw, err := notifDump()
	if err != nil {
		return nil, err
	}
	return parseNotifications(raw, limit), nil
}
