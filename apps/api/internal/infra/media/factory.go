package inframedia

import (
	"context"

	domainmedia "GCFeed/internal/domain/media"
	infraconfig "GCFeed/internal/infra/config"
)

func NewObjectStore(ctx context.Context, cfg infraconfig.MediaConfig) (domainmedia.MediaObjectStore, error) {
	if cfg.Backend == domainmedia.StorageBackendS3 {
		return NewS3Store(ctx, cfg.S3)
	}
	return NewLocalStore(cfg.LocalRoot)
}
