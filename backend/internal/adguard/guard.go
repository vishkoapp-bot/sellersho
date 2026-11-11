// Package adguard verifies short-lived, campaign-bound browser attestations.
// Browser signals identify ordinary automation; they are not proof of humanity.
package adguard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
)

type Service struct {
	Secret []byte
	Now    func() time.Time
}

func New(secret string) *Service { return &Service{Secret: []byte(secret), Now: time.Now} }

type Signals struct {
	Fingerprint string `json:"fingerprint"`
	Webdriver   bool   `json:"webdriver"`
	Automation  bool   `json:"automation"`
}
type claims struct {
	Campaign    string `json:"campaign"`
	Identity    string `json:"identity"`
	Fingerprint string `json:"fingerprint"`
	Issued      int64  `json:"issued"`
	Expires     int64  `json:"expires"`
	Nonce       string `json:"nonce"`
}
type Proof struct {
	Signals
	Challenge string `json:"challenge"`
}
type Evidence struct {
	IPHash          string
	FingerprintHash string
}

func (s *Service) Hash(value string) string {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
func Bot(userAgent string, s Signals) bool {
	ua := strings.ToLower(userAgent)
	return s.Webdriver || s.Automation || strings.Contains(ua, "headless") || strings.Contains(ua, "puppeteer") || strings.Contains(ua, "phantomjs") || strings.Contains(ua, "selenium") || strings.Contains(ua, "playwright")
}
func validFingerprint(v string) bool { b, e := hex.DecodeString(v); return e == nil && len(b) == 32 }
func (s *Service) Issue(campaign, ip, ua string, signals Signals) (string, error) {
	if len(s.Secret) < 32 || !validFingerprint(signals.Fingerprint) {
		return "", errors.New("invalid_fingerprint")
	}
	if Bot(ua, signals) {
		return "", errors.New("bot_detected")
	}
	now := s.Now()
	c := claims{campaign, s.Hash(ip + "\x00" + ua), s.Hash(signals.Fingerprint), now.UnixMilli(), now.Add(5 * time.Minute).UnixMilli(), uuid.NewString()}
	b, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(b)
	return payload + "." + s.Hash(payload), nil
}
func (s *Service) Verify(campaign, event, ip, ua string, p Proof) (Evidence, error) {
	var ev Evidence
	if Bot(ua, p.Signals) {
		return ev, errors.New("bot_detected")
	}
	parts := strings.Split(p.Challenge, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(s.Hash(parts[0])), []byte(parts[1])) {
		return ev, errors.New("invalid_challenge")
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return ev, errors.New("invalid_challenge")
	}
	var c claims
	if json.Unmarshal(b, &c) != nil || c.Campaign != campaign || !validFingerprint(p.Fingerprint) || c.Identity != s.Hash(ip+"\x00"+ua) || c.Fingerprint != s.Hash(p.Fingerprint) {
		return ev, errors.New("invalid_challenge")
	}
	now := s.Now().UnixMilli()
	if now < c.Issued || now >= c.Expires {
		return ev, errors.New("expired_challenge")
	}
	if event == "click" && now-c.Issued < 1000 {
		return ev, errors.New("too_fast")
	}
	return Evidence{IPHash: s.Hash("ip:" + ip), FingerprintHash: c.Fingerprint}, nil
}
