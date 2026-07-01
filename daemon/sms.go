package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// SMS reader: root `content query` on content://sms/inbox, redacted by default.
// Owner-only + rate-limited enforced at the HTTP layer. Never forwarded.

const (
	smsMaxLimit   = 20
	smsBodyPreview = 120
)

type SMSMessage struct {
	Address string `json:"address"`
	Date    string `json:"date"`
	Body    string `json:"body"` // redacted preview by default
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
		b = b[:smsBodyPreview] + "…"
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
		msgs = append(msgs, SMSMessage{Address: m[1], Date: m[2], Body: redactSMS(m[3])})
		if len(msgs) >= limit {
			break
		}
	}
	return msgs
}

// recentSMS returns up to `limit` redacted messages, or an error the caller maps
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
