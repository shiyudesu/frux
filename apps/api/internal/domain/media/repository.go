package domainmedia

import (
	"context"
	"time"
)

type AssetRepository interface {
	CreateAsset(ctx context.Context, asset *MediaAsset) error
	FindAssetByID(ctx context.Context, assetID int64) (*MediaAsset, error)
	FindAssetsByIDs(ctx context.Context, assetIDs []int64) (map[int64]*MediaAsset, error)
	FindAssetByObjectKey(ctx context.Context, backend, objectKey string) (*MediaAsset, error)
	UpdateAsset(ctx context.Context, asset *MediaAsset) error
}

type VariantRepository interface {
	UpsertVariants(ctx context.Context, variants []*MediaVariant) error
	ListReadyVariants(ctx context.Context, assetID int64) ([]*MediaVariant, error)
	ListReadyVariantsByAssetIDs(ctx context.Context, assetIDs []int64) (map[int64][]*MediaVariant, error)
	ListReadyVariantsByVideoIDs(ctx context.Context, videoIDs []int64) (map[int64][]*MediaVariant, error)
	UpdateVariantPromotion(ctx context.Context, variantID int64, objectKey string, public bool) error
}

type ProcessingRepository interface {
	UpsertProcessingProfile(ctx context.Context, profile *ProcessingProfile) error
	FindProcessingProfile(ctx context.Context, version string) (*ProcessingProfile, error)
	CreateOrGetProcessingJob(ctx context.Context, job *MediaProcessingJob) (*MediaProcessingJob, bool, error)
	LeaseProcessingJob(ctx context.Context, assetID int64, profileVersion, owner string, now time.Time, leaseUntil time.Time) (*MediaProcessingJob, error)
	LeaseProcessingJobs(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*MediaProcessingJob, error)
	UpdateProcessingJob(ctx context.Context, job *MediaProcessingJob) error
	UpdateProcessingJobOwned(ctx context.Context, job *MediaProcessingJob, leaseOwner string) error
	ExtendProcessingLease(ctx context.Context, jobID int64, leaseOwner string, leaseUntil time.Time) error
	ReleaseExpiredProcessingLeases(ctx context.Context, now time.Time) (int64, error)
	ListAssetsForReconciliation(ctx context.Context, limit int) ([]*MediaAsset, error)
	FindProcessingJobByAsset(ctx context.Context, assetID int64) (*MediaProcessingJob, error)
	ResetProcessingJob(ctx context.Context, assetID int64, profileVersion string, now time.Time) error
	ListKnownObjectKeys(ctx context.Context, prefix string) (map[string]struct{}, error)
	MarkAssetReconciled(ctx context.Context, assetID int64, reconciledAt time.Time) error
}

type UploadSessionRepository interface {
	CreateUploadSession(ctx context.Context, session *UploadSession) (*UploadSession, bool, error)
	FindUploadSession(ctx context.Context, sessionID string) (*UploadSession, error)
	CompleteUploadSession(ctx context.Context, sessionID string, asset *MediaAsset, completedAt time.Time) (*UploadSession, *MediaAsset, bool, error)
	ExpireUploadSessions(ctx context.Context, now time.Time, limit int) ([]*UploadSession, error)
}

type CleanupRepository interface {
	CreateCleanupTasks(ctx context.Context, tasks []*CleanupTask) error
	LeaseCleanupTasks(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*CleanupTask, error)
	UpdateCleanupTask(ctx context.Context, task *CleanupTask) error
	ReleaseExpiredCleanupLeases(ctx context.Context, now time.Time) (int64, error)
}

type Repository interface {
	AssetRepository
	VariantRepository
	ProcessingRepository
	UploadSessionRepository
	CleanupRepository
}
