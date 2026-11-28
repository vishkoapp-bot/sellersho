package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// UserClaims defines JWT payload schema
type UserClaims struct {
	UserID       string   `json:"userId"`
	Mobile       string   `json:"mobile"`
	FullName     string   `json:"fullName"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
	jwt.RegisteredClaims
}

// CapabilityReader lets request authentication refresh active memberships
// without coupling JWT parsing to the repository package.
type CapabilityReader interface {
	ListActive(context.Context, string) ([]string, error)
}

func (c *UserClaims) HasCapability(capability string) bool {
	if c == nil {
		return false
	}
	for _, active := range c.Capabilities {
		if active == capability {
			return true
		}
		if capability == "admin" && active == accesspolicy.SuperAdminCapability {
			return true
		}
	}
	return c.Capabilities == nil && c.Role == capability
}

// HasDirectCapability checks a capability without compatibility aliases. Use
// this for resource ownership and data-scope decisions where super_admin must
// not implicitly become the owner of another user's commercial resources.
func (c *UserClaims) HasDirectCapability(capability string) bool {
	if c == nil {
		return false
	}
	for _, active := range c.Capabilities {
		if active == capability {
			return true
		}
	}
	return c.Capabilities == nil && c.Role == capability
}

type JWTService struct {
	secretKey          []byte
	capabilityReader   CapabilityReader
	sessionRevocations SessionRevocations
}

func NewJWTService(secret string) *JWTService {
	return &JWTService{secretKey: []byte(secret)}
}

func (s *JWTService) SetCapabilityReader(reader CapabilityReader) {
	s.capabilityReader = reader
}

// UserActiveReader is the optional live-parity status check behind
// JWTService.IsUserBlocked (D57). CapabilityRepository implements it;
// readers that do not (unit-test fakes, legacy wiring) simply skip the
// check, exactly as a nil capabilityReader skips RefreshCapabilities.
type UserActiveReader interface {
	IsUserActive(context.Context, string) (bool, error)
}

// IsUserBlocked reports whether the token owner must be denied (D57,
// live symbols JWTService.IsUserBlocked / CapabilityRepository.IsUserActive).
// Blocked here means users.is_active = FALSE (the same flag the admin
// block/unblock endpoint toggles) or no users row at all. It deliberately
// does NOT consult locked_until: temporary login lockout stays a
// login-time rule (handlePasswordLogin), because the exact live
// semantics are unverified (MASTER-AUDIT U53) and inventing a second
// blocked signal would change lockout behaviour for live tokens.
// A nil reader, an empty user id, or a reader without IsUserActive
// means "no status information": not blocked, preserving the previous
// signature-only behaviour for those wirings.
func (s *JWTService) IsUserBlocked(ctx context.Context, userID string) (bool, error) {
	if s.capabilityReader == nil || userID == "" {
		return false, nil
	}
	reader, ok := s.capabilityReader.(UserActiveReader)
	if !ok {
		return false, nil
	}
	active, err := reader.IsUserActive(ctx, userID)
	if err != nil {
		return false, err
	}
	return !active, nil
}

// ActiveStateReader is the single-query authorization-state read
// behind JWTService.RefreshAuthState (PERF-03 G1).
// CapabilityRepository implements it; readers that do not (unit-test
// fakes, legacy wiring) fall back to the two-call RefreshCapabilities
// + IsUserBlocked path, exactly as before.
type ActiveStateReader interface {
	LoadActiveState(context.Context, string) (found bool, active bool, capabilities []string, err error)
}

// RefreshAuthState refreshes the token's capabilities and reports
// whether the token owner must be denied, in ONE database round trip
// when the reader implements ActiveStateReader (PERF-03 G1). Blocked
// means users.is_active = FALSE or no users row at all — the same
// rule as IsUserBlocked — so a block or capability revocation is
// visible on the very next request, with no TTL and no cache. The
// capabilities are refreshed even for a blocked user, matching the
// old order (RefreshCapabilities ran before the IsUserBlocked check).
// A nil reader, an empty user id, or a reader without either state
// method means "no status information": not blocked, preserving the
// previous signature-only behaviour for those wirings.
func (s *JWTService) RefreshAuthState(ctx context.Context, claims *UserClaims) (bool, error) {
	if s.capabilityReader == nil || claims == nil || claims.UserID == "" {
		return false, nil
	}
	if reader, ok := s.capabilityReader.(ActiveStateReader); ok {
		found, active, capabilities, err := reader.LoadActiveState(ctx, claims.UserID)
		if err != nil {
			return false, err
		}
		claims.Capabilities = append([]string{}, capabilities...)
		return !found || !active, nil
	}
	if err := s.RefreshCapabilities(ctx, claims); err != nil {
		return false, err
	}
	return s.IsUserBlocked(ctx, claims.UserID)
}

func (s *JWTService) RefreshCapabilities(ctx context.Context, claims *UserClaims) error {
	if s.capabilityReader == nil || claims == nil || claims.UserID == "" {
		return nil
	}
	capabilities, err := s.capabilityReader.ListActive(ctx, claims.UserID)
	if err != nil {
		return err
	}
	claims.Capabilities = append([]string{}, capabilities...)
	return nil
}

// GenerateToken creates an access token valid for given duration
func (s *JWTService) GenerateToken(userID, mobile, fullName, role string, ttl time.Duration) (string, error) {
	return s.GenerateTokenWithCapabilities(userID, mobile, fullName, role, []string{role}, ttl)
}

// GenerateTokenWithCapabilities preserves role as the selected compatibility
// role while carrying the complete active capability set for new middleware.
func (s *JWTService) GenerateTokenWithCapabilities(userID, mobile, fullName, role string, capabilities []string, ttl time.Duration) (string, error) {
	if len(s.secretKey) == 0 {
		return "", errors.New("JWT secret is not configured")
	}
	sessionID, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("create session ID: %w", err)
	}
	now := time.Now()
	claims := UserClaims{
		UserID:       userID,
		Mobile:       mobile,
		FullName:     fullName,
		Role:         role,
		Capabilities: append([]string{}, capabilities...),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        sessionID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "drup-platform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secretKey)
}

// ValidateToken verifies token validity and parses claims
func (s *JWTService) ValidateToken(tokenString string) (*UserClaims, error) {
	if len(s.secretKey) == 0 {
		return nil, errors.New("JWT secret is not configured")
	}
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return s.secretKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithStrictDecoding())

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid or expired token")
}
