package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrSessionRevoked          = errors.New("session revoked")
	ErrSessionStoreUnavailable = errors.New("session revocation store unavailable")
)

type SessionRevocations interface {
	IsSessionRevoked(ctx context.Context, digest string) (bool, error)
	RevokeSession(ctx context.Context, digest string, expiresAt time.Time) error
}

func (s *JWTService) SetSessionRevocations(store SessionRevocations) {
	s.sessionRevocations = store
}

func sessionDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// ValidateSession applies revocations in addition to the signed token's claims.
func (s *JWTService) ValidateSession(ctx context.Context, token string) (*UserClaims, error) {
	claims, err := s.ValidateToken(token)
	if err != nil || s.sessionRevocations == nil {
		return claims, err
	}
	revoked, err := s.sessionRevocations.IsSessionRevoked(ctx, sessionDigest(token))
	if err != nil {
		return nil, ErrSessionStoreUnavailable
	}
	if revoked {
		return nil, ErrSessionRevoked
	}
	return claims, nil
}

// RevokeSession also accepts legacy tokens issued before session IDs existed.
// Checking only the signature and expiry here keeps logout idempotent.
func (s *JWTService) RevokeSession(ctx context.Context, token string) error {
	claims, err := s.ValidateToken(token)
	if err != nil {
		return err
	}
	if s.sessionRevocations == nil {
		return ErrSessionStoreUnavailable
	}
	if err := s.sessionRevocations.RevokeSession(ctx, sessionDigest(token), claims.ExpiresAt.Time); err != nil {
		return ErrSessionStoreUnavailable
	}
	return nil
}
