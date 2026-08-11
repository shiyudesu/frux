package interfaceshttprouter

import (
	"errors"
	"testing"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestValidateAPIConfigProtectsEnabledInternalRoutes(t *testing.T) {
	validToken := "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y"
	tests := []struct {
		name  string
		cfg   *infraconfig.Config
		valid bool
	}{
		{name: "disabled", cfg: runtimeConfig(infraconfig.InternalConfig{}), valid: true},
		{name: "empty token", cfg: runtimeConfig(infraconfig.InternalConfig{Enabled: true}), valid: false},
		{name: "weak token", cfg: runtimeConfig(infraconfig.InternalConfig{Enabled: true, Token: "replace-with-internal-token"}), valid: false},
		{name: "strong token", cfg: runtimeConfig(infraconfig.InternalConfig{Enabled: true, Token: validToken}), valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAPIConfig(test.cfg)
			if test.valid && err != nil {
				t.Fatalf("validateAPIConfig() error = %v", err)
			}
			if !test.valid && !errors.Is(err, infraconfig.ErrInvalidInternalToken) {
				t.Fatalf("validateAPIConfig() error = %v, want ErrInvalidInternalToken", err)
			}
		})
	}
}

func runtimeConfig(internal infraconfig.InternalConfig) *infraconfig.Config {
	return &infraconfig.Config{
		Internal: internal,
		Redis:    infraconfig.RedisConfig{Addr: "localhost:6379"},
		Kafka: infraconfig.KafkaConfig{
			Enabled: true,
			Brokers: []string{"localhost:9092"},
		},
	}
}
