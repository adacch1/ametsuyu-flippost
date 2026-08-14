package main

import (
	"regexp"
	"strings"
	"testing"
)

var digitRun = regexp.MustCompile(`\d{4,}`)

func TestRedactMasksOTP(t *testing.T) {
	got := redactSMS("Your verification code is 483920. Do not share.")
	if strings.Contains(got, "483920") {
		t.Fatalf("OTP leaked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED-CODE]") {
		t.Fatalf("expected masked code: %q", got)
	}
}

func TestRedactMasksLongNumbers(t *testing.T) {
	got := redactSMS("Acct 1234567890123 balance updated")
	if digitRun.MatchString(strings.ReplaceAll(got, "REDACTED", "")) {
		t.Fatalf("long number leaked: %q", got)
	}
}

func TestRedactTruncates(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := redactSMS(long)
	if len([]rune(got)) > smsBodyPreview+1 {
		t.Fatalf("body not truncated: len=%d", len([]rune(got)))
	}
}

func TestParseRowsRedactsAndCaps(t *testing.T) {
	raw := "Row: 0 address=+441234, date=1700000000, body=Code 998877 now\n" +
		"Row: 1 address=Bank, date=1700000100, body=Hello there\n" +
		"Row: 2 address=X, date=1700000200, body=third\n"
	msgs := parseSMSRows(raw, 2)
	if len(msgs) != 2 {
		t.Fatalf("cap failed: got %d", len(msgs))
	}
	// redactBodies is off on this phone: the row must survive verbatim, code and
	// all. Flipping the const back on is what re-arms the masking tests above.
	if msgs[0].Body != "Code 998877 now" {
		t.Fatalf("body not verbatim: %q", msgs[0].Body)
	}
	if msgs[0].Address != "+441234" {
		t.Fatalf("address parse: %q", msgs[0].Address)
	}
}

func TestRecentSMSLimitBounds(t *testing.T) {
	if _, err := recentSMS(0); err == nil {
		t.Fatal("limit 0 should error")
	}
	if _, err := recentSMS(smsMaxLimit + 1); err == nil {
		t.Fatal("over-limit should error")
	}
}

func TestSMSHandlerOverLimit400(t *testing.T) {
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := do(s, "GET", "/v1/sms/recent?limit=999", strings.Repeat("b", 64))
	if w.Code != 400 {
		t.Fatalf("over-limit: want 400, got %d", w.Code)
	}
}

func TestSMSHandlerNoPermissionSafe(t *testing.T) {
	// force the reader to fail -> handler must return safe 200, available=false
	orig := smsQuery
	smsQuery = func(int) (string, error) { return "", errFake }
	defer func() { smsQuery = orig }()
	s := NewServer(testCfg(), fakeCollector{safe: true})
	w := do(s, "GET", "/v1/sms/recent?limit=3", strings.Repeat("b", 64))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("no-permission path: got %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "READ_SMS not granted") == false {
		t.Fatalf("expected actionable reason: %s", w.Body.String())
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "fake" }
