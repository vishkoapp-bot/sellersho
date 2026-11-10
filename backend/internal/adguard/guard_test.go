package adguard

import (
	"strings"
	"testing"
	"time"
)

func TestOperationsAdProof(t *testing.T) {
	s := New(strings.Repeat("s", 32))
	now := time.Now()
	s.Now = func() time.Time { return now }
	sig := Signals{Fingerprint: strings.Repeat("a", 64)}
	token, e := s.Issue("c", "1.2.3.4", "Mozilla", sig)
	if e != nil {
		t.Fatal(e)
	}
	p := Proof{sig, token}
	if _, e = s.Verify("c", "click", "1.2.3.4", "Mozilla", p); e == nil {
		t.Fatal("accepted immediate click")
	}
	now = now.Add(time.Second)
	if _, e = s.Verify("c", "click", "1.2.3.4", "Mozilla", p); e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"spoof"} {
		if _, e = s.Verify("c", "click", ip, "Mozilla", p); e == nil {
			t.Fatal("identity spoof")
		}
	}
	if _, e = s.Verify("other", "click", "1.2.3.4", "Mozilla", p); e == nil {
		t.Fatal("campaign spoof")
	}
	now = now.Add(6 * time.Minute)
	if _, e = s.Verify("c", "click", "1.2.3.4", "Mozilla", p); e == nil {
		t.Fatal("expired")
	}
	if _, e = s.Issue("c", "ip", "HeadlessChrome", sig); e == nil {
		t.Fatal("headless accepted")
	}
	sig.Webdriver = true
	if _, e = s.Issue("c", "ip", "Mozilla", sig); e == nil {
		t.Fatal("webdriver accepted")
	}
}
