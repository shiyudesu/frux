package infrajwt

import (
	"errors"
	"strconv"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

func TestStrictKeyRotationAndUnknownKeyRejection(t *testing.T) {
	manager, err := NewManagerWithConfig(ManagerConfig{
		Issuer:         DefaultIssuer,
		AccessTTL:      5 * time.Minute,
		AdminAccessTTL: 30 * time.Minute,
		ClockLeeway:    time.Second,
		Consumer: KeyRingConfig{
			ActiveKeyID: "consumer-v2",
			Keys: map[string]string{
				"consumer-v1": "consumer-secret-version-one",
				"consumer-v2": "consumer-secret-version-two",
			},
		},
		Admin: KeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys:        map[string]string{"admin-v1": "admin-secret-version-one"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.SignConsumerAccessToken(7, "session", 3)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.ParseAndValidateConsumerToken(token)
	if err != nil || claims.KeyID != "consumer-v2" ||
		claims.AuthVersion != 3 || claims.SessionID != "session" ||
		claims.Role != "" {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}

	now := time.Now()
	unknown := tokenClaims{
		SessionID: "session", AuthVersion: 3, TokenType: TokenTypeAccess,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: DefaultIssuer, Subject: strconv.FormatInt(7, 10),
			Audience: jwtlib.ClaimStrings{AudienceConsumer},
			IssuedAt: jwtlib.NewNumericDate(now), NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Minute)), ID: "unknown-key",
		},
	}
	raw := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, unknown)
	raw.Header["kid"] = "consumer-unknown"
	signed, err := raw.SignedString([]byte("consumer-secret-version-two"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ParseAndValidateConsumerToken(signed); !errors.Is(err, ErrParseJWTToken) {
		t.Fatalf("unknown key error = %v", err)
	}
}

func TestStrictClaimsAndLegacyDeadline(t *testing.T) {
	manager, err := NewManagerWithConfig(ManagerConfig{
		Issuer:         DefaultIssuer,
		AccessTTL:      5 * time.Minute,
		AdminAccessTTL: 30 * time.Minute,
		Consumer: KeyRingConfig{
			ActiveKeyID: "consumer-v1",
			Keys:        map[string]string{"consumer-v1": "consumer-secret-version-one"},
		},
		Admin: KeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys:        map[string]string{"admin-v1": "admin-secret-version-one"},
		},
		LegacySecret:      "legacy-secret",
		LegacyAcceptUntil: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	missingSession := tokenClaims{
		AuthVersion: 1, TokenType: TokenTypeAccess,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: DefaultIssuer, Subject: "7",
			Audience: jwtlib.ClaimStrings{AudienceConsumer},
			IssuedAt: jwtlib.NewNumericDate(now), NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Minute)), ID: "missing-session",
		},
	}
	raw := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, missingSession)
	raw.Header["kid"] = "consumer-v1"
	signed, err := raw.SignedString(manager.consumer.keys["consumer-v1"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ParseAndValidateConsumerToken(signed); !errors.Is(err, ErrParseJWTToken) &&
		!errors.Is(err, ErrInvalidClaims) {
		t.Fatalf("missing session error = %v", err)
	}

	legacy := signTestToken(t, manager, tokenClaims{
		UserID: 7, Role: "user", TokenType: TokenTypeAccess,
		RegisteredClaims: jwtlib.RegisteredClaims{
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Minute)),
		},
	})
	if _, err := manager.ParseAndValidateConsumerToken(legacy); !errors.Is(err, ErrParseJWTToken) {
		t.Fatalf("expired migration deadline error = %v", err)
	}
}

func TestConsumerAccessTTLHardLimit(t *testing.T) {
	_, err := NewManagerWithConfig(ManagerConfig{
		AccessTTL:      16 * time.Minute,
		AdminAccessTTL: 30 * time.Minute,
		Consumer: KeyRingConfig{
			ActiveKeyID: "consumer-v1",
			Keys:        map[string]string{"consumer-v1": "consumer-secret-version-one"},
		},
		Admin: KeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys:        map[string]string{"admin-v1": "admin-secret-version-one"},
		},
	})
	if !errors.Is(err, ErrParseAccessTTL) {
		t.Fatalf("access TTL error = %v", err)
	}
}
