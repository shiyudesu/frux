package inframedia

import (
	"context"
	"errors"
	"strings"

	domainmedia "GCFeed/internal/domain/media"
)

type DeliveryRepository interface {
	FindAssetsByIDs(ctx context.Context, assetIDs []int64) (map[int64]*domainmedia.MediaAsset, error)
	ListReadyVariantsByAssetIDs(ctx context.Context, assetIDs []int64) (map[int64][]*domainmedia.MediaVariant, error)
	UpsertVariants(ctx context.Context, variants []*domainmedia.MediaVariant) error
	UpdateVariantPromotion(ctx context.Context, variantID int64, objectKey string, public bool) error
}

type DeliveryRef struct {
	VideoID      int64
	MediaAssetID int64
	CoverAssetID int64
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
	result, err := c.ResolveBatch(ctx, []DeliveryRef{ref})
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

func (c *DeliveryCatalog) ResolveBatch(ctx context.Context, refs []DeliveryRef) (map[int64]*domainmedia.ResolvedDelivery, error) {
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
	for _, assetID := range assetIDs {
		promoted, promoteErr := c.promoteVariants(ctx, variantsByAsset[assetID])
		if promoteErr != nil {
			return nil, promoteErr
		}
		variantsByAsset[assetID] = promoted
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

func (c *DeliveryCatalog) promoteVariants(ctx context.Context, variants []*domainmedia.MediaVariant) ([]*domainmedia.MediaVariant, error) {
	if len(variants) == 0 {
		return variants, nil
	}
	for _, variant := range variants {
		if variant == nil || variant.Public {
			continue
		}
		if c.store == nil {
			return nil, domainmedia.ErrMediaVariantNotFound
		}
		publicKey := publicObjectKey(variant.ObjectKey)
		reader, metadata, err := c.store.Open(ctx, variant.ObjectKey)
		if err != nil {
			return nil, err
		}
		contentType := mediaVariantContentType(variant)
		if metadata != nil && metadata.ContentType != "" {
			contentType = metadata.ContentType
		}
		_, putErr := c.store.Put(ctx, publicKey, reader, variant.SizeBytes, contentType, variant.ChecksumSHA256)
		closeErr := reader.Close()
		if putErr != nil {
			return nil, putErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		oldKey := variant.ObjectKey
		if err := c.repo.UpdateVariantPromotion(ctx, variant.ID, publicKey, true); err != nil {
			return nil, err
		}
		if publicKey != oldKey {
			if err := c.store.Delete(ctx, oldKey); err != nil {
				return nil, err
			}
		}
		variant.ObjectKey = publicKey
		variant.Public = true
	}
	return variants, nil
}

func publicObjectKey(key string) string {
	if strings.HasPrefix(key, "processed/") {
		return "media/" + strings.TrimPrefix(key, "processed/")
	}
	return key
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
