package main

import (
	"strings"
	"testing"
)

// Shape copied from a live `dumpsys notification --noredact` on the Z Flip 5
// (indentation is load-bearing: section headers at 2 spaces, records deeper).
const notifDumpSample = `Current Notification Manager state:
  Notification List:
    NotificationRecord(0x02c6a92d: pkg=com.google.android.apps.messaging user=UserHandle{0} id=7 importance=4)
      uid=10263
      notification=
            when=1786522883298
            extras={
                android.title=String (Mum)
                android.text=String (Your code is 483920, do not share)
            }
    NotificationRecord(0x01110001: pkg=com.android.systemui user=UserHandle{0} id=9 importance=2)
      notification=
            when=1786522999000
            extras={
                android.title=String (Charging (3 m until full))
                android.text=String (99% (3 m until full))
            }
    NotificationRecord(0x0999abcd: pkg=com.google.android.apps.messaging user=UserHandle{0} id=7 tag=group)
      notification=
            when=1786522883298
            extras={
                android.title=null
                android.text=null
            }
  History Notification List:
    NotificationRecord(0x0deadbee: pkg=com.evil.dismissed user=UserHandle{0} id=1)
      notification=
            when=1786000000000
            extras={
                android.title=String (Dismissed already)
                android.text=String (should never surface)
            }
`

func TestParseNotificationsRedactsAndSkipsEmpty(t *testing.T) {
	ns := parseNotifications(notifDumpSample, notifMaxLimit)
	if len(ns) != 2 {
		t.Fatalf("want 2 readable records (group summary skipped), got %d: %+v", len(ns), ns)
	}
	if ns[0].Pkg != "com.google.android.apps.messaging" || ns[0].Title != "Mum" {
		t.Fatalf("header parse: %+v", ns[0])
	}
	if ns[0].When != "1786522883298" {
		t.Fatalf("when parse: %q", ns[0].When)
	}
	if ns[0].Text != "Your code is 483920, do not share" {
		t.Fatalf("text not verbatim: %q", ns[0].Text)
	}
	// Nested parens must survive the greedy match.
	if ns[1].Title != "Charging (3 m until full)" {
		t.Fatalf("nested parens: %q", ns[1].Title)
	}
}

func TestParseNotificationsExcludesHistory(t *testing.T) {
	if got := parseNotifications(notifDumpSample, notifMaxLimit); strings.Contains(got[0].Text+got[1].Text, "should never surface") ||
		len(got) > 2 {
		t.Fatalf("history section leaked: %+v", got)
	}
}

func TestParseNotificationsCaps(t *testing.T) {
	if ns := parseNotifications(notifDumpSample, 1); len(ns) != 1 {
		t.Fatalf("cap failed: got %d", len(ns))
	}
}

func TestParseNotificationsMultilineText(t *testing.T) {
	raw := "  Notification List:\n    NotificationRecord(0x1: pkg=a.b user=UserHandle{0})\n" +
		"            extras={\n                android.text=String (first line\nsecond line)\n            }\n"
	ns := parseNotifications(raw, 5)
	if len(ns) != 1 || ns[0].Text != "first line" {
		t.Fatalf("multiline fallback: %+v", ns)
	}
}

func TestParseNotificationsPrefersOwnExtrasOverPublicCopy(t *testing.T) {
	raw := "  Notification List:\n    NotificationRecord(0x1: pkg=com.google.android.gms user=UserHandle{0})\n" +
		"            extras={\n                android.title=String (Security Alert)\n            }\n" +
		"      publicNotification=\n            extras={\n                android.title=String (Account action required)\n            }\n"
	ns := parseNotifications(raw, 5)
	if len(ns) != 1 || ns[0].Title != "Security Alert" {
		t.Fatalf("public copy overwrote the real title: %+v", ns)
	}
}

func TestRecentNotificationsLimitBounds(t *testing.T) {
	if _, err := recentNotifications(0); err == nil {
		t.Fatal("limit 0 should error")
	}
	if _, err := recentNotifications(notifMaxLimit + 1); err == nil {
		t.Fatal("over-limit should error")
	}
}

func TestOpenReadsCoversInboxButNotWrites(t *testing.T) {
	orig, origSMS := notifDump, smsQuery
	notifDump = func() (string, error) { return notifDumpSample, nil }
	smsQuery = func(int) (string, error) { return "Row: 0 address=X, date=1, body=hi\n", nil }
	defer func() { notifDump, smsQuery = orig, origSMS }()
	s := NewServer(testCfg(), fakeCollector{safe: true})

	// Off: no token, no inbox.
	if w := do(s, "GET", "/v1/notifications/recent", ""); w.Code != 401 {
		t.Fatalf("open reads off: want 401, got %d", w.Code)
	}
	s.openReads.Store(true)
	if w := do(s, "GET", "/v1/notifications/recent", ""); w.Code != 200 {
		t.Fatalf("open reads on: want 200, got %d %s", w.Code, w.Body.String())
	}
	if w := do(s, "GET", "/v1/sms/recent", ""); w.Code != 200 {
		t.Fatalf("open reads on (sms): want 200, got %d %s", w.Code, w.Body.String())
	}
	// Reads only: open reads must never open a radio write.
	if w := do(s, "POST", "/v1/airplane", ""); w.Code != 401 {
		t.Fatalf("open reads must not open writes: got %d", w.Code)
	}
}

func TestNotificationsHandlerScopeAndSafeFailure(t *testing.T) {
	orig := notifDump
	notifDump = func() (string, error) { return "", errFake }
	defer func() { notifDump = orig }()
	s := NewServer(testCfg(), fakeCollector{safe: true})

	// read-status token must not reach notification content.
	if w := do(s, "GET", "/v1/notifications/recent", strings.Repeat("a", 64)); w.Code != 403 {
		t.Fatalf("read-status token: want 403, got %d", w.Code)
	}
	w := do(s, "GET", "/v1/notifications/recent?limit=3", strings.Repeat("b", 64))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("no-permission path: got %d %s", w.Code, w.Body.String())
	}
}
