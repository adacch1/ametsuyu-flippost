package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf8"
)

// SMS reader: root `content query` on content://sms/inbox, served verbatim
// (see redactBodies below).
// Owner-only + rate-limited enforced at the HTTP layer. Never forwarded.

const (
	smsMaxLimit    = 20
	smsBodyPreview = 120
)

type SMSMessage struct {
	Address string `json:"address"`
	Date    string `json:"date"`
	Body    string `json:"body"` // verbatim unless redactBodies is on
}

// redactBodies is the master switch for masking. OFF by owner's call on this
// donor phone (2026-08-14): the dashboard shows message and notification text
// verbatim, one-time codes included, and nothing is truncated. Flip it back to
// true to restore the masked previews — redactSMS below is kept intact for that.
const redactBodies = false

// bodyText applies (or bypasses) redaction for everything served to the
// dashboard: SMS bodies and notification titles/text all go through here.
func bodyText(s string) string {
	if redactBodies {
		return redactSMS(s)
	}
	return s
}

// otpRe masks standalone 4–8 digit runs (typical OTP/2FA codes).
var otpRe = regexp.MustCompile(`\b\d{4,8}\b`)

// longNumRe masks longer digit sequences (phone/account numbers).
var longNumRe = regexp.MustCompile(`\d{9,}`)

// redactSMS returns a safe preview: OTP-like and long numeric runs masked,
// truncated. Never returns a one-time code in the default path.
func redactSMS(body string) string {
	b := longNumRe.ReplaceAllString(body, "[REDACTED-NUM]")
	b = otpRe.ReplaceAllString(b, "[REDACTED-CODE]")
	b = strings.ReplaceAll(b, "\n", " ")
	if len(b) > smsBodyPreview {
		// Truncate on a rune boundary so a multi-byte character (e.g. Vietnamese
		// diacritics) is never split into a mojibake half-rune.
		cut := smsBodyPreview
		for cut > 0 && !utf8.RuneStart(b[cut]) {
			cut--
		}
		b = b[:cut] + "…"
	}
	return b
}

// smsQuery is the reader seam (overridable in tests).
var smsQuery = func(limit int) (string, error) {
	out, err := exec.Command("content", "query",
		"--uri", "content://sms/inbox",
		"--projection", "address:date:body",
		"--sort", "date DESC",
	).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

var rowRe = regexp.MustCompile(`address=(.*?), date=(.*?), body=(.*)$`)

// parseSMSRows parses `content query` "Row: N ..." output into messages,
// redacting each body. Caps to limit.
func parseSMSRows(raw string, limit int) []SMSMessage {
	var msgs []SMSMessage
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "Row:") {
			continue
		}
		rest := ln
		if i := strings.Index(ln, "address="); i >= 0 {
			rest = ln[i:]
		}
		m := rowRe.FindStringSubmatch(rest)
		if m == nil {
			continue
		}
		msgs = append(msgs, SMSMessage{Address: m[1], Date: m[2], Body: bodyText(m[3])})
		if len(msgs) >= limit {
			break
		}
	}
	return msgs
}

// recentSMS returns up to `limit` messages, or an error the caller maps
// to a safe no-permission response.
func recentSMS(limit int) ([]SMSMessage, error) {
	if limit <= 0 || limit > smsMaxLimit {
		return nil, fmt.Errorf("limit out of range (1..%d)", smsMaxLimit)
	}
	raw, err := smsQuery(limit)
	if err != nil {
		return nil, err
	}
	return parseSMSRows(raw, limit), nil
}
