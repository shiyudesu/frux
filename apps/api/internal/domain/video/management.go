package domainvideo

import (
	"context"
	"strings"
	"time"
)

const (
	MaxBatchVideoIDs                = 100
	BatchActionMakePublic           = "make_public"
	BatchActionMakePrivate          = "make_private"
	BatchActionDelete               = "delete"
	CollectionStatusActive          = 1
	CollectionStatusDeleted         = 2
	MaxCollectionTitleLength        = 128
	MaxCollectionDescriptionLength  = 512
	MaxPublicCollectionPreviewItems = 3
	LocalAssetKindVideo             = "video"
	LocalAssetKindCover             = "cover"
)

type CreatorVideoCursor struct {
	CreatedAt time.Time
	VideoID   int64
}

type CreatorVideoFilter struct {
	AuthorID    int64
	Visibility  string
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

type Collection struct {
	ID             int64
	OwnerID        int64
	Title          string
	Description    string
	Visibility     string
	Status         int
	IdempotencyKey string
	Items          []*CollectionItem
	MemberCount    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CollectionItem struct {
	CollectionID int64
	VideoID      int64
	Position     int
	Video        *Video
	CreatedAt    time.Time
}

type CollectionCursor struct {
	UpdatedAt    time.Time
	CollectionID int64
}

type CollectionUpdate struct {
	Title       *string
	Description *string
	Visibility  *string
}

func (u CollectionUpdate) Empty() bool {
	return u.Title == nil && u.Description == nil && u.Visibility == nil
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

func NewCollection(ownerID int64, title, description, visibility, idempotencyKey string) (*Collection, error) {
	if ownerID <= 0 {
		return nil, ErrInvalidAuthorID
	}
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if title == "" {
		return nil, ErrEmptyCollectionTitle
	}
	if len(title) > MaxCollectionTitleLength {
		return nil, ErrCollectionTitleTooLong
	}
	if len(description) > MaxCollectionDescriptionLength {
		return nil, ErrCollectionDescriptionTooLong
	}
	if visibility == "" {
		visibility = VisibilityPrivate
	}
	if !ValidVisibility(visibility) {
		return nil, ErrInvalidVisibility
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}
	return &Collection{
		OwnerID: ownerID, Title: title, Description: description,
		Visibility: visibility, Status: CollectionStatusActive,
		IdempotencyKey: idempotencyKey,
	}, nil
}

func RestoreCollection(id, ownerID int64, title, description, visibility string, status int, idempotencyKey string, createdAt, updatedAt time.Time) *Collection {
	return &Collection{
		ID: id, OwnerID: ownerID, Title: strings.TrimSpace(title),
		Description: strings.TrimSpace(description), Visibility: strings.ToLower(strings.TrimSpace(visibility)),
		Status: status, IdempotencyKey: strings.TrimSpace(idempotencyKey),
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func (c *Collection) UpdateBy(ownerID int64, update CollectionUpdate) error {
	if c.OwnerID != ownerID {
		return ErrCollectionPermissionDenied
	}
	if c.Status != CollectionStatusActive {
		return ErrCollectionNotFound
	}
	if update.Title != nil {
		value := strings.TrimSpace(*update.Title)
		if value == "" {
			return ErrEmptyCollectionTitle
		}
		if len(value) > MaxCollectionTitleLength {
			return ErrCollectionTitleTooLong
		}
		c.Title = value
	}
	if update.Description != nil {
		value := strings.TrimSpace(*update.Description)
		if len(value) > MaxCollectionDescriptionLength {
			return ErrCollectionDescriptionTooLong
		}
		c.Description = value
	}
	if update.Visibility != nil {
		value := strings.ToLower(strings.TrimSpace(*update.Visibility))
		if !ValidVisibility(value) {
			return ErrInvalidVisibility
		}
		c.Visibility = value
	}
	return nil
}

func (c *Collection) DeleteBy(ownerID int64) error {
	if c.OwnerID != ownerID {
		return ErrCollectionPermissionDenied
	}
	c.Status = CollectionStatusDeleted
	return nil
}

type ManagementRepository interface {
	QueryCreatorVideos(ctx context.Context, filter CreatorVideoFilter) ([]*Video, error)
	ApplyBatch(ctx context.Context, userID int64, action string, videoIDs []int64, idempotencyKey, fingerprint string) (*BatchOperation, bool, error)
	CreateCollection(ctx context.Context, collection *Collection) (*Collection, bool, error)
	ListCollections(ctx context.Context, ownerID int64, publicOnly bool, cursor *CollectionCursor, limit int) ([]*Collection, error)
	GetCollection(ctx context.Context, collectionID int64) (*Collection, error)
	UpdateCollection(ctx context.Context, collection *Collection, update CollectionUpdate) error
	DeleteCollection(ctx context.Context, collection *Collection) error
	SetCollectionItem(ctx context.Context, ownerID, collectionID, videoID int64, active bool) error
	ListCollectionItems(ctx context.Context, collectionID int64, publicOnly bool) ([]*CollectionItem, error)
	BatchGetReadable(ctx context.Context, viewerID int64, videoIDs []int64, publicOnly bool) (map[int64]*Video, error)
	ListAssetReferences(ctx context.Context, assetURL string) ([]AssetReference, error)
	CreateLocalAsset(ctx context.Context, asset *LocalAsset) error
	FindLocalAsset(ctx context.Context, assetURL string) (*LocalAsset, error)
}
