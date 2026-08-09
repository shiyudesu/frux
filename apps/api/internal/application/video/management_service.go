package applicationvideo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

const defaultManagementLimit = 20

type ManagementService struct {
	repo             domainvideo.ManagementRepository
	cacheInvalidator VideoCacheInvalidator
	mediaCleanup     MediaCleanupScheduler
	publisher        PublishedEventPublisher
	mediaPublisher   interface {
		MediaReady(ctx context.Context, assetID int64) error
		ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	}
}

type CreatorQueryRequest struct {
	VideoID     int64
	Visibility  string
	Statuses    []int
	Query       string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Cursor      string
	Limit       int
}

type CreatorQueryResult struct {
	Items      []*domainvideo.Video
	NextCursor string
	HasMore    bool
}

type BatchResult struct {
	Action   string  `json:"action"`
	VideoIDs []int64 `json:"video_ids"`
	Replayed bool    `json:"replayed"`
}

type MediaAssetRef struct {
	VideoID        int64
	MediaAssetID   int64
	CoverAssetID   int64
	Status         int
	Visibility     string
	MediaStatus    string
	MediaErrorCode string
}

type MediaAssetRefReader interface {
	ListMediaAssetRefs(ctx context.Context, videoIDs []int64) ([]MediaAssetRef, error)
}

type LifecyclePublicationTracker interface {
	LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
	MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
}

type ManagementOption func(*ManagementService)

func WithManagementMediaCleanup(scheduler MediaCleanupScheduler) ManagementOption {
	return func(service *ManagementService) {
		service.mediaCleanup = scheduler
	}
}

func WithManagementMediaPublisher(publisher interface {
	MediaReady(ctx context.Context, assetID int64) error
	ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
}) ManagementOption {
	return func(service *ManagementService) {
		service.mediaPublisher = publisher
	}
}

func WithManagementPublishedPublisher(publisher PublishedEventPublisher) ManagementOption {
	return func(service *ManagementService) {
		service.publisher = publisher
	}
}

type VideoByIDReader interface {
	FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
}

type CollectionListResult struct {
	Items      []*domainvideo.Collection
	NextCursor string
	HasMore    bool
}

func NewManagement(repo domainvideo.ManagementRepository, invalidator VideoCacheInvalidator, options ...ManagementOption) *ManagementService {
	service := &ManagementService{repo: repo, cacheInvalidator: invalidator}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *ManagementService) QueryCreatorVideos(ctx context.Context, userID int64, request CreatorQueryRequest) (*CreatorQueryResult, error) {
	if userID <= 0 {
		return nil, domainvideo.ErrInvalidAuthorID
	}
	if request.VideoID < 0 {
		return nil, domainvideo.ErrInvalidVideoID
	}
	visibility := strings.ToLower(strings.TrimSpace(request.Visibility))
	if visibility != "" && !domainvideo.ValidVisibility(visibility) {
		return nil, domainvideo.ErrInvalidVisibility
	}
	if request.CreatedFrom != nil && request.CreatedTo != nil && request.CreatedFrom.After(*request.CreatedTo) {
		return nil, domainvideo.ErrInvalidDateRange
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultManagementLimit
	}
	if limit < 1 || limit > 100 {
		return nil, domainvideo.ErrInvalidLimit
	}
	cursor, err := decodeCreatorCursor(request.Cursor)
	if err != nil {
		return nil, err
	}
	statuses := make([]int, 0, len(request.Statuses))
	seenStatuses := make(map[int]struct{}, len(request.Statuses))
	for _, status := range request.Statuses {
		if !domainvideo.ValidStatus(status) || status == domainvideo.StatusDeleted {
			return nil, domainvideo.ErrInvalidStatus
		}
		if _, exists := seenStatuses[status]; exists {
			continue
		}
		seenStatuses[status] = struct{}{}
		statuses = append(statuses, status)
	}
	items, err := s.repo.QueryCreatorVideos(ctx, domainvideo.CreatorVideoFilter{
		AuthorID: userID, VideoID: request.VideoID,
		Visibility: visibility, Statuses: statuses, Query: strings.TrimSpace(request.Query),
		CreatedFrom: request.CreatedFrom, CreatedTo: request.CreatedTo, Cursor: cursor, Limit: limit + 1,
	})
	if err != nil {
		return nil, ErrLoadVideoFailed
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	next := ""
	if hasMore && len(items) > 0 {
		next = encodeCreatorCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID)
	}
	return &CreatorQueryResult{Items: items, NextCursor: next, HasMore: hasMore}, nil
}

func (s *ManagementService) ApplyBatch(ctx context.Context, userID int64, action string, videoIDs []int64, idempotencyKey string) (*BatchResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case domainvideo.BatchActionMakePublic, domainvideo.BatchActionMakePrivate, domainvideo.BatchActionDelete:
	default:
		return nil, domainvideo.ErrInvalidBatchAction
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, domainvideo.ErrIdempotencyKeyRequired
	}
	if len(idempotencyKey) > domainvideo.MaxIdempotencyKeyLength {
		return nil, domainvideo.ErrIdempotencyKeyTooLong
	}
	ids, err := normalizeVideoIDs(videoIDs)
	if err != nil {
		return nil, err
	}
	fingerprint := batchFingerprint(action, ids)
	operation, replayed, err := s.repo.ApplyBatch(ctx, userID, action, ids, idempotencyKey, fingerprint)
	if err != nil {
		return nil, err
	}
	var mediaRefs []MediaAssetRef
	if (action == domainvideo.BatchActionDelete && (s.mediaCleanup != nil || s.mediaPublisher != nil)) ||
		action == domainvideo.BatchActionMakePublic ||
		(action == domainvideo.BatchActionMakePrivate && s.mediaPublisher != nil) {
		if reader, ok := s.repo.(MediaAssetRefReader); ok {
			mediaRefs, err = reader.ListMediaAssetRefs(ctx, ids)
			if err != nil {
				return nil, err
			}
		}
	}
	if s.cacheInvalidator != nil && !replayed {
		for _, videoID := range operation.VideoIDs {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, videoID)
		}
	}
	var sideEffectErr error
	for _, ref := range mediaRefs {
		var mediaErr error
		switch action {
		case domainvideo.BatchActionMakePublic:
			if s.mediaPublisher != nil && ref.MediaAssetID > 0 &&
				ref.Visibility == domainvideo.VisibilityPublic &&
				ref.Status != domainvideo.StatusDeleted && ref.Status != domainvideo.StatusOffline &&
				(domainmedia.IsPublicReadyStatus(ref.MediaStatus) ||
					ref.MediaErrorCode == "publication_event_failed") {
				mediaErr = errors.Join(mediaErr, s.mediaPublisher.MediaReady(ctx, ref.MediaAssetID))
			} else if ref.MediaAssetID == 0 &&
				ref.Visibility == domainvideo.VisibilityPublic &&
				ref.Status == domainvideo.StatusPublished &&
				domainmedia.IsPublicReadyStatus(ref.MediaStatus) {
				// The repository commits the stable publication handoff atomically
				// with the visibility transition.
			}
		case domainvideo.BatchActionMakePrivate:
			if s.mediaPublisher != nil && ref.Visibility == domainvideo.VisibilityPrivate &&
				ref.Status != domainvideo.StatusDeleted {
				mediaErr = errors.Join(
					mediaErr,
					s.mediaPublisher.ProtectVideo(ctx, ref.VideoID, ref.MediaAssetID, ref.CoverAssetID),
				)
			}
		case domainvideo.BatchActionDelete:
			if ref.Status != domainvideo.StatusDeleted {
				continue
			}
			if s.mediaPublisher != nil {
				mediaErr = errors.Join(
					mediaErr,
					s.mediaPublisher.ProtectVideo(ctx, ref.VideoID, ref.MediaAssetID, ref.CoverAssetID),
				)
			}
			if s.mediaCleanup != nil {
				mediaErr = errors.Join(
					mediaErr,
					s.mediaCleanup.ScheduleMediaCleanup(ctx, ref.MediaAssetID, ref.CoverAssetID),
				)
			}
		}
		if mediaErr != nil {
			sideEffectErr = errors.Join(sideEffectErr, mediaErr)
		}
	}
	if sideEffectErr != nil {
		return nil, sideEffectErr
	}
	return &BatchResult{Action: operation.Action, VideoIDs: operation.VideoIDs, Replayed: replayed}, nil
}

func (s *ManagementService) CreateCollection(ctx context.Context, userID int64, title, description, visibility, idempotencyKey string) (*domainvideo.Collection, bool, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, false, domainvideo.ErrIdempotencyKeyRequired
	}
	collection, err := domainvideo.NewCollection(userID, title, description, visibility, idempotencyKey)
	if err != nil {
		return nil, false, err
	}
	return s.repo.CreateCollection(ctx, collection)
}

func (s *ManagementService) ListCollections(ctx context.Context, ownerID int64, publicOnly bool, cursorValue string, limit int) (*CollectionListResult, error) {
	if ownerID <= 0 {
		return nil, domainvideo.ErrInvalidAuthorID
	}
	if limit == 0 {
		limit = defaultManagementLimit
	}
	if limit < 1 || limit > 100 {
		return nil, domainvideo.ErrInvalidLimit
	}
	cursor, err := decodeCollectionCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListCollections(ctx, ownerID, publicOnly, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	next := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		next = encodeCollectionCursor(last.UpdatedAt, last.ID)
	}
	return &CollectionListResult{Items: items, NextCursor: next, HasMore: hasMore}, nil
}

func (s *ManagementService) UpdateCollection(ctx context.Context, userID, collectionID int64, title, description, visibility *string) (*domainvideo.Collection, error) {
	collection, err := s.repo.GetCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	update := domainvideo.CollectionUpdate{Title: title, Description: description, Visibility: visibility}
	if err := collection.UpdateBy(userID, update); err != nil {
		return nil, err
	}
	if !update.Empty() {
		if err := s.repo.UpdateCollection(ctx, collection, update); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.GetCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListCollectionItems(ctx, collectionID, false)
	if err != nil {
		return nil, err
	}
	updated.Items = items
	updated.MemberCount = len(items)
	return updated, nil
}

func (s *ManagementService) DeleteCollection(ctx context.Context, userID, collectionID int64) error {
	collection, err := s.repo.GetCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	if err := collection.DeleteBy(userID); err != nil {
		return err
	}
	return s.repo.DeleteCollection(ctx, collection)
}

func (s *ManagementService) SetCollectionItem(ctx context.Context, userID, collectionID, videoID int64, active bool) error {
	return s.repo.SetCollectionItem(ctx, userID, collectionID, videoID, active)
}

func (s *ManagementService) RecordLocalUpload(ctx context.Context, ownerID int64, assetURL, kind string) error {
	if ownerID <= 0 {
		return domainvideo.ErrInvalidAuthorID
	}
	classification, err := classifyPublishAsset(assetURL, kind)
	if err != nil || !classification.protected {
		return domainvideo.ErrInvalidLocalAsset
	}
	return s.repo.CreateLocalAsset(ctx, &domainvideo.LocalAsset{
		AssetURL: classification.assetURL,
		OwnerID:  ownerID,
		Kind:     kind,
	})
}

func (s *ManagementService) ValidateLocalAssetOwner(ctx context.Context, ownerID int64, assetURL, kind string) error {
	asset, err := s.repo.FindLocalAsset(ctx, assetURL)
	if err != nil {
		if errors.Is(err, domainvideo.ErrLocalAssetNotFound) {
			return domainvideo.ErrLocalAssetPermissionDenied
		}
		return err
	}
	if asset.OwnerID != ownerID || asset.Kind != kind {
		return domainvideo.ErrLocalAssetPermissionDenied
	}
	return nil
}

func (s *ManagementService) AuthorizeLocalAsset(ctx context.Context, assetURL string, viewerID int64) (referenced, publiclyReadable, allowed bool, err error) {
	assetURL = strings.TrimSpace(assetURL)
	kind, protected := protectedLocalAssetKind(assetURL)
	if !protected {
		return false, false, false, nil
	}
	asset, err := s.repo.FindLocalAsset(ctx, assetURL)
	if err != nil {
		if errors.Is(err, domainvideo.ErrLocalAssetNotFound) {
			return false, false, false, nil
		}
		return false, false, false, err
	}
	if asset.Kind != kind {
		return false, false, false, nil
	}
	references, err := s.repo.ListAssetReferences(ctx, assetURL)
	if err != nil {
		return false, false, false, err
	}
	for _, reference := range references {
		if reference.AuthorID != asset.OwnerID {
			continue
		}
		referenced = true
		if reference.Status == domainvideo.StatusPublished && reference.Visibility == domainvideo.VisibilityPublic {
			publiclyReadable = true
			allowed = true
			continue
		}
		if viewerID > 0 && viewerID == asset.OwnerID && reference.Status != domainvideo.StatusDeleted {
			allowed = true
		}
	}
	return referenced, publiclyReadable, allowed, nil
}

type publishAssetClassification struct {
	assetURL  string
	protected bool
}

func validatePublishAsset(ctx context.Context, ownership LocalAssetOwnership, ownerID int64, assetURL, expectedKind string) error {
	classification, err := classifyPublishAsset(assetURL, expectedKind)
	if err != nil {
		return err
	}
	if !classification.protected {
		return nil
	}
	if ownership == nil {
		return domainvideo.ErrLocalAssetPermissionDenied
	}
	return ownership.ValidateLocalAssetOwner(ctx, ownerID, classification.assetURL, expectedKind)
}

func classifyPublishAsset(rawURL, expectedKind string) (publishAssetClassification, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return publishAssetClassification{}, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if parsed.IsAbs() {
		if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			return publishAssetClassification{}, nil
		}
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/uploads/") {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if strings.Contains(parsed.Path, `\`) || path.Clean(parsed.Path) != parsed.Path {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	relative := strings.TrimPrefix(parsed.Path, "/uploads/")
	kind, remainder, hasKind := strings.Cut(relative, "/")
	if !hasKind {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if remainder == "" || strings.Contains(remainder, "/") {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if expectedKind != domainvideo.LocalAssetKindVideo && expectedKind != domainvideo.LocalAssetKindCover {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	if kind != expectedKind {
		return publishAssetClassification{}, domainvideo.ErrInvalidLocalAsset
	}
	return publishAssetClassification{assetURL: parsed.Path, protected: true}, nil
}

func protectedLocalAssetKind(assetURL string) (string, bool) {
	for _, kind := range []string{domainvideo.LocalAssetKindVideo, domainvideo.LocalAssetKindCover} {
		classification, err := classifyPublishAsset(assetURL, kind)
		if err == nil && classification.protected {
			return kind, true
		}
	}
	return "", false
}

func normalizeVideoIDs(values []int64) ([]int64, error) {
	unique := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, domainvideo.ErrInvalidVideoID
		}
		unique[value] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, domainvideo.ErrEmptyVideoIDs
	}
	if len(unique) > domainvideo.MaxBatchVideoIDs {
		return nil, domainvideo.ErrTooManyVideoIDs
	}
	result := make([]int64, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func batchFingerprint(action string, ids []int64) string {
	content, _ := json.Marshal(struct {
		Action   string  `json:"action"`
		VideoIDs []int64 `json:"video_ids"`
	}{Action: action, VideoIDs: ids})
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type timeIDCursor struct {
	Time time.Time `json:"time"`
	ID   int64     `json:"id"`
}

func encodeCreatorCursor(value time.Time, id int64) string {
	return encodeTimeIDCursor(value, id)
}

func decodeCreatorCursor(value string) (*domainvideo.CreatorVideoCursor, error) {
	cursor, err := decodeTimeIDCursor(value)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, nil
	}
	return &domainvideo.CreatorVideoCursor{CreatedAt: cursor.Time, VideoID: cursor.ID}, nil
}

func encodeCollectionCursor(value time.Time, id int64) string {
	return encodeTimeIDCursor(value, id)
}

func decodeCollectionCursor(value string) (*domainvideo.CollectionCursor, error) {
	cursor, err := decodeTimeIDCursor(value)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, nil
	}
	return &domainvideo.CollectionCursor{UpdatedAt: cursor.Time, CollectionID: cursor.ID}, nil
}

func encodeTimeIDCursor(value time.Time, id int64) string {
	content, _ := json.Marshal(timeIDCursor{Time: value.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeTimeIDCursor(value string) (*timeIDCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	content, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, domainvideo.ErrInvalidCursor
	}
	var cursor timeIDCursor
	if err := json.Unmarshal(content, &cursor); err != nil || cursor.Time.IsZero() || cursor.ID <= 0 {
		return nil, domainvideo.ErrInvalidCursor
	}
	return &cursor, nil
}
