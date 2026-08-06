package inframedia

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

var exposureGenerationFallback atomic.Uint64
var errVariantExposureConflict = errors.New("media variant exposure changed concurrently")

type DeliveryRepository interface {
	FindAssetsByIDs(ctx context.Context, assetIDs []int64) (map[int64]*domainmedia.MediaAsset, error)
	ListReadyVariantsByAssetIDs(ctx context.Context, assetIDs []int64) (map[int64][]*domainmedia.MediaVariant, error)
	UpsertVariants(ctx context.Context, variants []*domainmedia.MediaVariant) error
	UpdateVariantPromotion(
		ctx context.Context,
		variantID int64,
		expectedObjectKey string,
		expectedPublic bool,
		objectKey string,
		public bool,
	) (bool, error)
	CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error
	ListIncompletePublicCleanupTasks(ctx context.Context, assetIDs []int64) ([]*domainmedia.CleanupTask, error)
	UpdateCleanupTask(ctx context.Context, task *domainmedia.CleanupTask) error
}

func needsBundleNormalization(variants []*domainmedia.MediaVariant) bool {
	publicCount := 0
	privateCount := 0
	generations := map[string]struct{}{}
	for _, variant := range variants {
		if variant == nil {
			continue
		}
		if !variant.Public {
			privateCount++
			continue
		}
		publicCount++
		if generation := exposureGeneration(variant.ObjectKey); generation != "" {
			generations[generation] = struct{}{}
		}
	}
	return publicCount > 0 && (privateCount > 0 || len(generations) > 1)
}

type DeliveryRef struct {
	VideoID      int64
	MediaAssetID int64
	CoverAssetID int64
}

func exposureGeneration(key string) string {
	if !strings.HasPrefix(key, "media/v2/") {
		return ""
	}
	parts := strings.SplitN(strings.TrimPrefix(key, "media/v2/"), "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

type DeliveryCatalog struct {
	repo     DeliveryRepository
	resolver domainmedia.MediaURLResolver
	store    domainmedia.MediaObjectStore
}

func NewDeliveryCatalog(repo DeliveryRepository, resolver domainmedia.MediaURLResolver, store domainmedia.MediaObjectStore) *DeliveryCatalog {
	return &DeliveryCatalog{repo: repo, resolver: resolver, store: store}
}

func (c *DeliveryCatalog) Resolve(ctx context.Context, ref DeliveryRef) (*domainmedia.ResolvedDelivery, error) {
	result, err := c.resolveBatch(ctx, []DeliveryRef{ref}, true)
	if err != nil {
		return nil, err
	}
	delivery := result[ref.VideoID]
	if delivery == nil {
		return nil, domainmedia.ErrMediaVariantNotFound
	}
	return delivery, nil
}

func (c *DeliveryCatalog) ResolveVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) (*domainmedia.ResolvedDelivery, error) {
	return c.Resolve(ctx, DeliveryRef{VideoID: videoID, MediaAssetID: mediaAssetID, CoverAssetID: coverAssetID})
}

func (c *DeliveryCatalog) HasPublicVideo(ctx context.Context, _, mediaAssetID, coverAssetID int64) (bool, error) {
	if c == nil || c.repo == nil || mediaAssetID <= 0 {
		return false, nil
	}
	assetIDs := []int64{mediaAssetID}
	if coverAssetID > 0 && coverAssetID != mediaAssetID {
		assetIDs = append(assetIDs, coverAssetID)
	}
	assets, err := c.repo.FindAssetsByIDs(ctx, assetIDs)
	if err != nil {
		return false, err
	}
	if media := assets[mediaAssetID]; media == nil || media.State != domainmedia.AssetStateReady {
		return false, nil
	}
	variantsByAsset, err := c.repo.ListReadyVariantsByAssetIDs(ctx, assetIDs)
	if err != nil {
		return false, err
	}
	baselinePublic := false
	for _, assetID := range assetIDs {
		asset := assets[assetID]
		if asset == nil || asset.State != domainmedia.AssetStateReady {
			if assetID == coverAssetID {
				continue
			}
			return false, nil
		}
		variants := variantsByAsset[assetID]
		if len(variants) == 0 || needsBundleNormalization(variants) || hasLegacyPublicVariant(variants) {
			return false, nil
		}
		for _, variant := range variants {
			if variant == nil || !variant.Public {
				return false, nil
			}
			if assetID == mediaAssetID && variant.Role == domainmedia.VariantRoleBaseline {
				baselinePublic = true
			}
		}
	}
	return baselinePublic, nil
}

func (c *DeliveryCatalog) ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error {
	if c == nil || c.repo == nil || mediaAssetID <= 0 {
		return nil
	}
	assetIDs := []int64{mediaAssetID}
	if coverAssetID > 0 && coverAssetID != mediaAssetID {
		assetIDs = append(assetIDs, coverAssetID)
	}
	variantsByAsset, err := c.repo.ListReadyVariantsByAssetIDs(ctx, assetIDs)
	if err != nil {
		return err
	}
	assets, err := c.repo.FindAssetsByIDs(ctx, assetIDs)
	if err != nil {
		return err
	}
	var protectErr error
	for _, assetID := range assetIDs {
		if _, err := c.protectVariants(ctx, variantsByAsset[assetID], assets); err != nil {
			protectErr = errors.Join(protectErr, err)
		}
	}
	return errors.Join(protectErr, c.retryPublicCleanupTasks(ctx, assetIDs))
}

func (c *DeliveryCatalog) ResolveBatch(ctx context.Context, refs []DeliveryRef) (map[int64]*domainmedia.ResolvedDelivery, error) {
	return c.resolveBatch(ctx, refs, false)
}

func (c *DeliveryCatalog) resolveBatch(
	ctx context.Context,
	refs []DeliveryRef,
	publish bool,
) (map[int64]*domainmedia.ResolvedDelivery, error) {
	result := make(map[int64]*domainmedia.ResolvedDelivery, len(refs))
	if c == nil || c.repo == nil || c.resolver == nil || len(refs) == 0 {
		return result, nil
	}
	assetIDs := make([]int64, 0, len(refs)*2)
	seen := map[int64]struct{}{}
	for _, ref := range refs {
		for _, assetID := range []int64{ref.MediaAssetID, ref.CoverAssetID} {
			if assetID <= 0 {
				continue
			}
			if _, exists := seen[assetID]; exists {
				continue
			}
			seen[assetID] = struct{}{}
			assetIDs = append(assetIDs, assetID)
		}
	}
	assets, err := c.repo.FindAssetsByIDs(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	variantsByAsset, err := c.repo.ListReadyVariantsByAssetIDs(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	if publish {
		generations := make(map[int64]string, len(assetIDs))
		for _, ref := range refs {
			generation := newExposureGeneration()
			if ref.MediaAssetID > 0 {
				generations[ref.MediaAssetID] = generation
			}
			if ref.CoverAssetID > 0 {
				generations[ref.CoverAssetID] = generation
			}
		}
		for _, assetID := range assetIDs {
			asset := assets[assetID]
			if asset == nil || asset.State != domainmedia.AssetStateReady {
				continue
			}
			variants := variantsByAsset[assetID]
			if hasLegacyPublicVariant(variants) || needsBundleNormalization(variants) {
				variants, err = c.protectVariants(ctx, variants, assets)
				if err != nil {
					return nil, err
				}
			}
			promoted, promoteErr := c.promoteVariants(ctx, variants, generations[assetID], assets)
			if promoteErr != nil {
				return nil, promoteErr
			}
			variantsByAsset[assetID] = promoted
		}
	}
	for _, ref := range refs {
		mediaAsset := assets[ref.MediaAssetID]
		coverAsset := assets[ref.CoverAssetID]
		if mediaAsset == nil || mediaAsset.State != domainmedia.AssetStateReady {
			continue
		}
		delivery := &domainmedia.ResolvedDelivery{}
		if coverAsset != nil && coverAsset.State == domainmedia.AssetStateReady {
			for _, variant := range variantsByAsset[ref.CoverAssetID] {
				if variant != nil && variant.Public && variant.Role == domainmedia.VariantRoleCover {
					delivery.CoverURL, err = c.resolver.PublicURL(variant.ObjectKey)
					if err != nil {
						return nil, err
					}
					break
				}
			}
		}

		for _, variant := range variantsByAsset[ref.MediaAssetID] {
			if variant == nil || !variant.Public || variant.Role == domainmedia.VariantRoleSegment {
				continue
			}
			resolvedURL, resolveErr := c.resolver.PublicURL(variant.ObjectKey)
			if resolveErr != nil {
				return nil, resolveErr
			}
			source := domainmedia.PlaybackSource{
				Type: variant.SourceType, URL: resolvedURL, Codec: variant.Codec, AudioCodec: variant.AudioCodec,
				Width: variant.Width, Height: variant.Height, Bitrate: variant.Bitrate,
				Quality: variant.Quality, Role: variant.Role, SortOrder: variant.SortOrder,
			}
			delivery.PlaybackSources = append(delivery.PlaybackSources, source)
			if variant.Role == domainmedia.VariantRoleBaseline {
				delivery.MediaURL = resolvedURL
			}
		}
		delivery.PlaybackSources = domainmedia.SortPlaybackSources(delivery.PlaybackSources)
		if delivery.MediaURL == "" {
			continue
		}
		result[ref.VideoID] = delivery
	}
	return result, nil
}

func hasLegacyPublicVariant(variants []*domainmedia.MediaVariant) bool {
	for _, variant := range variants {
		if variant != nil && variant.Public && !strings.HasPrefix(variant.ObjectKey, "media/v2/") {
			return true
		}
	}
	return false
}

func (c *DeliveryCatalog) promoteVariants(
	ctx context.Context,
	variants []*domainmedia.MediaVariant,
	generation string,
	assets map[int64]*domainmedia.MediaAsset,
) ([]*domainmedia.MediaVariant, error) {
	if len(variants) == 0 {
		return variants, nil
	}

	promotedThisCall := make([]*domainmedia.MediaVariant, 0, len(variants))
	rollback := func(cause error) ([]*domainmedia.MediaVariant, error) {
		if len(promotedThisCall) == 0 {
			return nil, cause
		}
		_, rollbackErr := c.protectVariants(ctx, promotedThisCall, assets)
		return nil, errors.Join(cause, rollbackErr)
	}
	for _, variant := range variants {
		if variant == nil || variant.Public {
			continue
		}
		if c.store == nil {
			return nil, domainmedia.ErrMediaVariantNotFound
		}
		protectedKey := protectedObjectKey(variant.ObjectKey)
		publicKey := publicObjectKey(protectedKey, generation)
		reader, metadata, err := c.store.Open(ctx, protectedKey)
		if err != nil {
			return rollback(err)
		}
		contentType := mediaVariantContentType(variant)
		if metadata != nil && metadata.ContentType != "" {
			contentType = metadata.ContentType
		}
		_, putErr := c.store.Put(ctx, publicKey, reader, variant.SizeBytes, contentType, variant.ChecksumSHA256)
		closeErr := reader.Close()
		if putErr != nil {
			return rollback(putErr)
		}
		if closeErr != nil {
			return rollback(errors.Join(closeErr, c.removeUntrackedPublicObject(ctx, variant.AssetID, publicKey, assets)))
		}
		updated, err := c.repo.UpdateVariantPromotion(
			ctx,
			variant.ID,
			variant.ObjectKey,
			false,
			publicKey,
			true,
		)
		if err != nil {
			return rollback(errors.Join(err, c.removeUntrackedPublicObject(ctx, variant.AssetID, publicKey, assets)))
		}

		if !updated {
			cleanupErr := c.removeUntrackedPublicObject(ctx, variant.AssetID, publicKey, assets)
			return rollback(errors.Join(errVariantExposureConflict, cleanupErr))
		}
		variant.ObjectKey = publicKey
		variant.Public = true
		promotedThisCall = append(promotedThisCall, variant)
	}
	return variants, nil
}

func (c *DeliveryCatalog) removeUntrackedPublicObject(
	ctx context.Context,
	assetID int64,
	publicKey string,
	assets map[int64]*domainmedia.MediaAsset,
) error {
	if err := c.store.Delete(ctx, publicKey); err == nil {
		return nil
	} else if cleanupErr := c.schedulePublicObjectCleanup(ctx, assetID, publicKey, assets); cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	} else {
		return err
	}
}

func (c *DeliveryCatalog) protectVariants(
	ctx context.Context,
	variants []*domainmedia.MediaVariant,
	assets map[int64]*domainmedia.MediaAsset,
) ([]*domainmedia.MediaVariant, error) {
	if len(variants) == 0 {
		return variants, nil
	}
	var protectErr error
	for _, variant := range variants {
		if variant == nil {
			continue
		}
		protected, err := c.protectVariant(ctx, variant.AssetID, variant.ID, assets)
		if err != nil {
			protectErr = errors.Join(protectErr, err)
			continue
		}
		if protected != nil {
			variant.ObjectKey = protected.ObjectKey
			variant.Public = protected.Public
		}
	}

	return variants, protectErr
}

func (c *DeliveryCatalog) protectVariant(
	ctx context.Context,
	assetID int64,
	variantID int64,
	assets map[int64]*domainmedia.MediaAsset,
) (*domainmedia.MediaVariant, error) {
	for attempt := 0; attempt < 8; attempt++ {
		current, err := c.reloadVariant(ctx, assetID, variantID)
		if err != nil || current == nil || !current.Public {
			return current, err
		}
		if c.store == nil {
			return nil, domainmedia.ErrMediaVariantNotFound
		}
		protectedKey := protectedObjectKey(current.ObjectKey)
		if _, err := c.store.Head(ctx, protectedKey); err != nil {
			reader, metadata, openErr := c.store.Open(ctx, current.ObjectKey)
			if openErr != nil {
				return nil, openErr
			}
			contentType := mediaVariantContentType(current)
			if metadata != nil && metadata.ContentType != "" {
				contentType = metadata.ContentType
			}
			_, putErr := c.store.Put(ctx, protectedKey, reader, current.SizeBytes, contentType, current.ChecksumSHA256)
			closeErr := reader.Close()
			if putErr != nil {
				return nil, putErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
		publicKey := current.ObjectKey
		updated, err := c.repo.UpdateVariantPromotion(
			ctx,
			current.ID,
			current.ObjectKey,
			true,
			protectedKey,
			false,
		)
		if err != nil {
			return nil, err
		}
		if publicKey != protectedKey {
			if deleteErr := c.store.Delete(ctx, publicKey); deleteErr != nil {
				if cleanupErr := c.schedulePublicObjectCleanup(ctx, current.AssetID, publicKey, assets); cleanupErr != nil {
					_, restoreErr := c.repo.UpdateVariantPromotion(
						ctx,
						current.ID,
						protectedKey,
						false,
						publicKey,
						true,
					)
					return nil, errors.Join(deleteErr, cleanupErr, restoreErr)
				}
				return nil, deleteErr
			}
		}
		if updated {
			current.ObjectKey = protectedKey
			current.Public = false
			return current, nil
		}
	}
	return nil, errVariantExposureConflict
}

func (c *DeliveryCatalog) reloadVariant(
	ctx context.Context,
	assetID int64,
	variantID int64,
) (*domainmedia.MediaVariant, error) {
	variantsByAsset, err := c.repo.ListReadyVariantsByAssetIDs(ctx, []int64{assetID})
	if err != nil {
		return nil, err
	}
	for _, variant := range variantsByAsset[assetID] {
		if variant != nil && variant.ID == variantID {
			return variant, nil
		}
	}
	return nil, domainmedia.ErrMediaVariantNotFound
}

func publicObjectKey(key, generation string) string {
	if strings.HasPrefix(key, "processed/") {
		return fmt.Sprintf(
			"media/v2/%s/%s",
			generation,
			strings.TrimPrefix(key, "processed/"),
		)
	}

	return key
}

func (c *DeliveryCatalog) schedulePublicObjectCleanup(
	ctx context.Context,
	assetID int64,
	objectKey string,
	assets map[int64]*domainmedia.MediaAsset,
) error {
	asset := assets[assetID]
	if asset == nil {
		return domainmedia.ErrMediaAssetNotFound
	}

	task, err := domainmedia.NewCleanupTask(
		assetID,
		asset.StorageBackend,
		objectKey,
		time.Now().UTC(),
		10,
	)
	if err != nil {
		return err
	}
	return c.repo.CreateCleanupTasks(ctx, []*domainmedia.CleanupTask{task})
}

func (c *DeliveryCatalog) retryPublicCleanupTasks(ctx context.Context, assetIDs []int64) error {
	tasks, err := c.repo.ListIncompletePublicCleanupTasks(ctx, assetIDs)
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if err := c.store.Delete(ctx, task.ObjectKey); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		now := time.Now().UTC()
		task.State = domainmedia.CleanupStateCompleted
		task.ErrorMessage = ""
		task.LeaseOwner = ""
		task.LeaseUntil = nil
		task.CompletedAt = &now
		if err := c.repo.UpdateCleanupTask(ctx, task); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func protectedObjectKey(key string) string {
	if strings.HasPrefix(key, "media/v2/") {
		parts := strings.SplitN(strings.TrimPrefix(key, "media/v2/"), "/", 2)
		if len(parts) == 2 {
			return "processed/" + parts[1]
		}
	}
	if strings.HasPrefix(key, "media/") {
		return "processed/" + strings.TrimPrefix(key, "media/")
	}
	return key
}

func newExposureGeneration() string {
	content := make([]byte, 8)
	if _, err := rand.Read(content); err == nil {
		return hex.EncodeToString(content)
	}
	return fmt.Sprintf(
		"%x",
		uint64(time.Now().UTC().UnixNano())^exposureGenerationFallback.Add(1),
	)
}

func mediaVariantContentType(variant *domainmedia.MediaVariant) string {
	switch variant.Role {
	case domainmedia.VariantRoleManifest:
		return "application/dash+xml"
	case domainmedia.VariantRoleSegment:
		return "video/iso.segment"
	case domainmedia.VariantRoleCover:
		if variant.Format == "png" {
			return "image/png"
		}
		if variant.Format == "webp" {
			return "image/webp"
		}
		return "image/jpeg"
	default:
		return "video/mp4"
	}
}

func IsDeliveryUnavailable(err error) bool {
	return errors.Is(err, domainmedia.ErrMediaVariantNotFound) || errors.Is(err, domainmedia.ErrMediaAssetNotFound)
}
