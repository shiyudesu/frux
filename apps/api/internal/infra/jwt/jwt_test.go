package infrajwt

import (
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

func TestManagerSeparatesConsumerAndAdminCredentials(t *testing.T) {
	manager, err := NewManager("test-secret", "15m", "30m")
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	consumer, err := manager.SignAccessToken(7, "admin")
	if err != nil {
		t.Fatalf("sign consumer token: %v", err)
	}
	admin, err := manager.SignAdminAccessToken(7, "user")
	if err != nil {
		t.Fatalf("sign admin token: %v", err)
	}
	consumerClaims, err := manager.ParseAndValidateToken(
		consumer, TokenTypeAccess, AudienceConsumer,
	)
	if err != nil || consumerClaims.Audience != AudienceConsumer {
		t.Fatalf("consumer claims = %#v err=%v", consumerClaims, err)
	}
	adminClaims, err := manager.ParseAndValidateToken(
		admin, TokenTypeAdminAccess, AudienceAdmin,
	)
	if err != nil || adminClaims.Audience != AudienceAdmin || adminClaims.Role != "" ||
		adminClaims.KeyID != "admin-v1" || adminClaims.AuthVersion != 1 {
		t.Fatalf("admin claims = %#v err=%v", adminClaims, err)
	}
	if _, err := manager.ParseAndValidateToken(
		consumer, TokenTypeAdminAccess, AudienceAdmin,
	); !errors.Is(err, ErrParseJWTToken) && !errors.Is(err, ErrInvalidTokenType) {
		t.Fatalf("consumer-as-admin error = %v", err)
	}
	if _, err := manager.ParseAndValidateToken(
		admin, TokenTypeAccess, AudienceConsumer,
	); !errors.Is(err, ErrParseJWTToken) && !errors.Is(err, ErrInvalidTokenType) {
		t.Fatalf("admin-as-consumer error = %v", err)
	}
	legacy := signTestToken(t, manager, tokenClaims{
		UserID: 7, Role: "user", TokenType: TokenTypeAccess,
		RegisteredClaims: jwtlib.RegisteredClaims{
			IssuedAt:  jwtlib.NewNumericDate(time.Now()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	})
	legacyClaims, err := manager.ParseAndValidateConsumerToken(legacy)
	if err != nil || legacyClaims.Audience != "" {
		t.Fatalf("legacy claims = %#v err=%v", legacyClaims, err)
	}
	if claims, err := manager.ParseAndValidateToken(
		legacy, TokenTypeAccess, AudienceConsumer,
	); err != nil || !claims.Legacy {
		t.Fatalf("legacy migration claims = %#v err=%v", claims, err)
	}
}

func TestAdminTokenExpiryAndTTLBounds(t *testing.T) {
	if _, err := NewManager("test-secret", "15m", "9h"); !errors.Is(err, ErrParseAdminAccessTTL) {
		t.Fatalf("oversized admin TTL error = %v", err)
	}

	manager, err := NewManager("test-secret", "15m", "30m")
	if err != nil {
		t.Fatal(err)
	}
	manager.adminAccessTTL = time.Millisecond
	manager.clockLeeway = 0
	token, err := manager.SignAdminAccessToken(9, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := manager.ParseAndValidateToken(
		token, TokenTypeAdminAccess, AudienceAdmin,
	); !errors.Is(err, ErrParseJWTToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestTokenRequiresExpiration(t *testing.T) {
	manager, err := NewManager("test-secret", "15m", "30m")
	if err != nil {
		t.Fatal(err)
	}
	withoutExpiry := signTestToken(t, manager, tokenClaims{
		UserID: 1, Role: "admin", TokenType: TokenTypeAdminAccess,
		RegisteredClaims: jwtlib.RegisteredClaims{
			IssuedAt: jwtlib.NewNumericDate(time.Now()),
			Audience: jwtlib.ClaimStrings{AudienceAdmin},
		},
	})
	if _, err := manager.ParseAndValidateToken(
		withoutExpiry, TokenTypeAdminAccess, AudienceAdmin,
	); !errors.Is(err, ErrParseJWTToken) {
		t.Fatalf("missing expiration error = %v", err)
	}
}

func signTestToken(t *testing.T, manager *Manager, claims tokenClaims) string {
	t.Helper()
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(manager.secret)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
