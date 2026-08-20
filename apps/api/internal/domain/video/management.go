package domainvideo

import (
	"context"
	"time"
)

const (
	MaxBatchVideoIDs       = 100
	BatchActionMakePublic  = "make_public"
	BatchActionMakePrivate = "make_private"
	BatchActionDelete      = "delete"
	LocalAssetKindVideo    = "video"
	LocalAssetKindCover    = "cover"
)

type CreatorVideoCursor struct {
	CreatedAt time.Time
	VideoID   int64
}

type CreatorVideoFilter struct {
	AuthorID    int64
	VideoID     int64
	Visibility  string
	Statuses    []int
	Query       string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Cursor      *CreatorVideoCursor
	Limit       int
}

type BatchOperation struct {
	UserID      int64
	Key         string
	Fingerprint string
	Action      string
	VideoIDs    []int64
	ResultJSON  string
	CreatedAt   time.Time
}

type AssetReference struct {
	AuthorID   int64
	Status     int
	Visibility string
}

type LocalAsset struct {
	AssetURL  string
	OwnerID   int64
	Kind      string
	CreatedAt time.Time
}

type ManagementRepository interface {
	QueryCreatorVideos(ctx context.Context, filter CreatorVideoFilter) ([]*Video, error)
	ListCreatorArchiveMonths(ctx context.Context, authorID int64, visibility string) ([]time.Time, error)
	ApplyBatch(ctx context.Context, userID int64, action string, videoIDs []int64, idempotencyKey, fingerprint string) (*BatchOperation, bool, error)
	BatchGetReadable(ctx context.Context, viewerID int64, videoIDs []int64, publicOnly bool) (map[int64]*Video, error)
	ListAssetReferences(ctx context.Context, assetURL string) ([]AssetReference, error)
	CreateLocalAsset(ctx context.Context, asset *LocalAsset) error
	FindLocalAsset(ctx context.Context, assetURL string) (*LocalAsset, error)
}
