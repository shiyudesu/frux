package infraembedding

import (
	"context"
	"fmt"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func NewReadyHTTPMultimodalProvider(
	ctx context.Context,
	cfg infraconfig.MultimodalConfig,
	requiredCapability string,
) (*HTTPMultimodalProvider, error) {
	contract, err := cfg.Contract.Identity()
	if err != nil {
		return nil, fmt.Errorf("build multimodal provider contract: %w", err)
	}
	deadline, err := time.ParseDuration(cfg.Provider.Deadline)
	if err != nil {
		return nil, fmt.Errorf("parse multimodal provider deadline: %w", err)
	}
	startupTimeout, err := time.ParseDuration(cfg.Provider.StartupTimeout)
	if err != nil {
		return nil, fmt.Errorf("parse multimodal provider startup timeout: %w", err)
	}
	provider, err := NewHTTPMultimodalProvider(MultimodalHTTPProviderConfig{
		Endpoint: cfg.Provider.Endpoint, HMACSecret: cfg.Provider.HMACSecret,
		ProtocolVersion:    cfg.Provider.ProtocolVersion,
		AllowInsecureLocal: cfg.Provider.AllowInsecureLocal,
		Timeout:            deadline, MaxRequestBytes: cfg.Provider.MaxRequestBytes,
		MaxResponseBytes:   cfg.Provider.MaxResponseBytes,
		MaxIdleConnections: cfg.Provider.AdmissionLimit,
		MaxVideoTextRunes:  cfg.MaxVideoTextRunes,
		MaxQueryRunes:      cfg.Query.MaxRunes,
		MaxImages:          cfg.Images.MaxCount,
		MaxImageBytes:      cfg.Images.MaxBytesEach,
		MaxTotalImageBytes: cfg.Images.MaxTotalBytes,
		MaxImagePixels:     cfg.Images.MaxPixelsEach,
		AllowedMIMETypes:   append([]string(nil), cfg.Images.AllowedMIMETypes...),
	}, contract)
	if err != nil {
		return nil, fmt.Errorf("build multimodal HTTP provider: %w", err)
	}
	readyContext, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := provider.CheckReady(readyContext, requiredCapability); err != nil {
		return nil, fmt.Errorf("check multimodal provider readiness: %w", err)
	}
	return provider, nil
}
