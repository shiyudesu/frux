package inframedia

import (
	"context"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func NewObjectStore(ctx context.Context, cfg infraconfig.MediaConfig) (domainmedia.MediaObjectStore, error) {
	if cfg.Backend == domainmedia.StorageBackendS3 {
		return NewS3Store(ctx, cfg.S3)
	}
	return NewLocalStore(cfg.LocalRoot)
}
