package domainembedding

import (
	"context"
	"time"
)

// Repository 定义 embedding 模块需要的持久化能力。
type Repository interface {
	SaveVideoEmbedding(ctx context.Context, embedding *VideoEmbedding) error
	FindVideoEmbedding(ctx context.Context, videoID int64, model string) (*VideoEmbedding, error)
}

type MultimodalRepository interface {
	HandoffMultimodalJob(context.Context, *MultimodalEmbeddingJob) (*MultimodalEmbeddingJob, bool, bool, error)
	ClaimMultimodalJobs(context.Context, string, time.Duration, int) ([]*MultimodalEmbeddingJob, error)
	HeartbeatMultimodalJob(context.Context, int64, string, time.Duration) (bool, error)
	RetryMultimodalJob(context.Context, int64, string, string, time.Duration) (bool, error)
	CompleteMultimodalJob(context.Context, int64, string, *MultimodalVectorFact) (bool, error)
	TerminalMultimodalJob(context.Context, int64, string, string) (bool, error)
	RequeueMultimodalJob(context.Context, int64, string) (bool, error)
	DeleteCompletedMultimodalJobsBefore(context.Context, time.Time, int) (int64, error)
	FindMultimodalVectorFact(context.Context, int64, MultimodalContractIdentity) (*MultimodalVectorFact, error)
	UpsertMultimodalProjection(context.Context, *MultimodalProjection) (bool, error)
	DeleteMultimodalProjectionIfStale(context.Context, int64, string, string, string) (bool, error)
}
