package infraconfig

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeJWTKeyRingsAndBounds(t *testing.T) {
	cfg := JWTConfig{
		Issuer:         "frux",
		AccessTTL:      "5m",
		AdminAccessTTL: "30m",
		ClockLeeway:    "30s",
		Consumer: JWTKeyRingConfig{
			ActiveKeyID: "consumer-v2",
			Keys: []JWTKeyConfig{
				{ID: "consumer-v1", Secret: "consumer-secret-version-one"},
				{ID: "consumer-v2", Secret: "consumer-secret-version-two"},
			},
		},
		Admin: JWTKeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys: []JWTKeyConfig{
				{ID: "admin-v1", Secret: "admin-secret-version-one"},
			},
		},
		LegacySecret:      "legacy-secret-value",
		LegacyIssuedUntil: time.Now().UTC().Format(time.RFC3339),
		LegacyAcceptUntil: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}
	if err := normalizeAndValidateJWTConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	cfg.AccessTTL = "16m"
	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("overlong access TTL error = %v", err)
	}
}

func TestNormalizeJWTRejectsSharedOrUnknownActiveKey(t *testing.T) {
	cfg := JWTConfig{
		Consumer: JWTKeyRingConfig{
			ActiveKeyID: "shared",
			Keys:        []JWTKeyConfig{{ID: "shared", Secret: "consumer-secret-value"}},
		},
		Admin: JWTKeyRingConfig{
			ActiveKeyID: "shared",
			Keys:        []JWTKeyConfig{{ID: "shared", Secret: "admin-secret-value"}},
		},
	}

	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("shared key id error = %v", err)
	}

	cfg.Consumer.ActiveKeyID = "missing"
	cfg.Admin.ActiveKeyID = "admin"
	cfg.Admin.Keys[0].ID = "admin"
	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("missing active key error = %v", err)
	}
}

func TestNormalizeSecurityRequiresDedicatedHMACSecret(t *testing.T) {
	cfg := SecurityConfig{HMACSecret: "application-hmac-secret-value-123"}
	if err := normalizeAndValidateSecurityConfig(&cfg, ""); err != nil {
		t.Fatal(err)
	}

	cfg.HMACSecret = ""
	if err := normalizeAndValidateSecurityConfig(&cfg, "legacy-jwt-secret"); err != nil {
		t.Fatalf("legacy derivation: %v", err)
	}
	cfg.HMACSecret = "short"
	if err := normalizeAndValidateSecurityConfig(&cfg, ""); !errors.Is(err, ErrInvalidSecurityConfig) {
		t.Fatalf("short HMAC error = %v", err)
	}
}

func TestNormalizeJWTRejectsUnsafeLegacyDeadline(t *testing.T) {
	cfg := JWTConfig{
		AccessTTL:      "5m",
		AdminAccessTTL: "30m",
		ClockLeeway:    "30s",
		Consumer: JWTKeyRingConfig{
			ActiveKeyID: "consumer",
			Keys: []JWTKeyConfig{{
				ID: "consumer", Secret: "consumer-secret-version-one",
			}},
		},
		Admin: JWTKeyRingConfig{
			ActiveKeyID: "admin",
			Keys: []JWTKeyConfig{{
				ID: "admin", Secret: "admin-secret-version-one",
			}},
		},
		LegacySecret:      "legacy-secret-value",
		LegacyIssuedUntil: time.Now().UTC().Format(time.RFC3339),
		LegacyAcceptUntil: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}

	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("unsafe legacy deadline error = %v", err)
	}
}

func TestNormalizeJWTAcceptsExpiredSafeLegacyDeadline(t *testing.T) {
	issuedUntil := time.Now().UTC().Add(-2 * time.Hour)
	cfg := JWTConfig{
		AccessTTL:      "5m",
		AdminAccessTTL: "30m",
		ClockLeeway:    "30s",
		Consumer: JWTKeyRingConfig{
			ActiveKeyID: "consumer",
			Keys: []JWTKeyConfig{{
				ID: "consumer", Secret: "consumer-secret-version-one",
			}},
		},
		Admin: JWTKeyRingConfig{
			ActiveKeyID: "admin",
			Keys: []JWTKeyConfig{{
				ID: "admin", Secret: "admin-secret-version-one",
			}},
		},
		LegacySecret:      "legacy-secret-value",
		LegacyIssuedUntil: issuedUntil.Format(time.RFC3339),
		LegacyAcceptUntil: issuedUntil.Add(time.Hour).Format(time.RFC3339),
	}
	if err := normalizeAndValidateJWTConfig(&cfg); err != nil {
		t.Fatalf("expired safe legacy deadline: %v", err)
	}
}
