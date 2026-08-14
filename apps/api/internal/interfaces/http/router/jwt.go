package interfaceshttprouter

import (
	"strings"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
)

func newJWTManager(config infraconfig.JWTConfig) (*infrajwt.Manager, error) {
	accessTTL, err := time.ParseDuration(config.AccessTTL)
	if err != nil {
		return nil, err
	}
	adminTTL, err := time.ParseDuration(config.AdminAccessTTL)
	if err != nil {
		return nil, err
	}
	clockLeeway, err := time.ParseDuration(config.ClockLeeway)
	if err != nil {
		return nil, err
	}
	legacyDeadline := time.Time{}
	if strings.TrimSpace(config.LegacyAcceptUntil) != "" {
		legacyDeadline, err = time.Parse(time.RFC3339, strings.TrimSpace(config.LegacyAcceptUntil))
		if err != nil {
			return nil, err
		}
	}
	return infrajwt.NewManagerWithConfig(infrajwt.ManagerConfig{
		Issuer:            config.Issuer,
		AccessTTL:         accessTTL,
		AdminAccessTTL:    adminTTL,
		ClockLeeway:       clockLeeway,
		Consumer:          jwtKeyRing(config.Consumer),
		Admin:             jwtKeyRing(config.Admin),
		LegacySecret:      config.LegacySecret,
		LegacyAcceptUntil: legacyDeadline,
	})
}

func jwtKeyRing(config infraconfig.JWTKeyRingConfig) infrajwt.KeyRingConfig {
	keys := make(map[string]string, len(config.Keys))
	for _, key := range config.Keys {
		keys[strings.TrimSpace(key.ID)] = strings.TrimSpace(key.Secret)
	}
	return infrajwt.KeyRingConfig{
		ActiveKeyID: strings.TrimSpace(config.ActiveKeyID),
		Keys:        keys,
	}
}
