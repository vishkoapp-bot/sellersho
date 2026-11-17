package auth

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func TestCredentialRotationInvalidatesOldSessions(t *testing.T) {
	key := func() string {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			t.Fatal("entropy failure")
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	old := NewJWTService(key())
	replacement := NewJWTService(key())
	token, e := old.GenerateToken("fixture-user", "synthetic", "fixture", "admin", time.Hour)
	if e != nil {
		t.Fatal("fixture token generation failed")
	}
	if _, e = old.ValidateToken(token); e != nil {
		t.Fatal("old session fixture invalid")
	}
	if _, e = replacement.ValidateToken(token); e == nil {
		t.Fatal("old session survived rotation")
	}
	fresh, e := replacement.GenerateToken("fixture-user", "synthetic", "fixture", "admin", time.Hour)
	if e != nil {
		t.Fatal("new session fixture failed")
	}
	if _, e = replacement.ValidateToken(fresh); e != nil {
		t.Fatal("fresh session invalid")
	}
}
