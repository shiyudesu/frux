package infraplayback

import (
	domainmedia "GCFeed/internal/domain/media"
	domainplayback "GCFeed/internal/domain/playback"
	domainvideo "GCFeed/internal/domain/video"
	inframediastore "GCFeed/internal/infra/media"
	infrapersistence "GCFeed/internal/infra/persistence"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db           *gorm.DB
	mediaCatalog *inframediastore.DeliveryCatalog
}

type Option func(*Repository)

// New 创建播放优化仓储实现。
func New(db *gorm.DB, options ...Option) *Repository {
	repository := &Repository{db: db}
	for _, option := range options {
		option(repository)
	}
	return repository
}

func WithMediaCatalog(catalog *inframediastore.DeliveryCatalog) Option {
	return func(repository *Repository) {
		repository.mediaCatalog = catalog
	}
}

// FindConfig 按端和网络类型读取配置。
func (r *Repository) FindConfig(ctx context.Context, platform string, networkType string) (*domainplayback.Config, error) {
	var model ConfigModel
	err := r.db.WithContext(ctx).
		Where("platform = ? AND network_type = ?", platform, networkType).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return restoreConfig(model), nil
}

// ListPreloadVideos 按发布时间读取兼容补充资源；currentVideoID 为空时从最新资源开始。
func (r *Repository) ListPreloadVideos(ctx context.Context, currentVideoID int64, limit int) ([]*domainplayback.PreloadVideo, error) {
	query := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.media_url, v.cover_url, v.media_asset_id, v.cover_asset_id, v.media_status").
		Where("v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady})

	if currentVideoID > 0 {
		current, err := r.findCurrentVideo(ctx, currentVideoID)
		if err != nil {
			return nil, err
		}
		if current != nil {
			query = query.Where(
				"(v.published_at < ? OR (v.published_at = ? AND v.id < ?))",
				current.PublishedAt,
				current.PublishedAt,
				current.VideoID,
			)
		}
	}

	var models []PreloadVideoModel
	err := query.
		Order("v.published_at DESC").
		Order("v.id DESC").
		Limit(limit).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	if r.mediaCatalog != nil {
		refs := make([]inframediastore.DeliveryRef, 0, len(models))
		for _, model := range models {
			if model.MediaAssetID > 0 {
				refs = append(refs, inframediastore.DeliveryRef{VideoID: model.VideoID, MediaAssetID: model.MediaAssetID, CoverAssetID: model.CoverAssetID})
			}
		}
		deliveries, err := r.mediaCatalog.ResolveBatch(ctx, refs)
		if err != nil {
			return nil, err
		}
		for index := range models {
			if delivery := deliveries[models[index].VideoID]; delivery != nil {
				models[index].MediaURL = delivery.MediaURL
				models[index].CoverURL = delivery.CoverURL
				models[index].PlaybackSources = delivery.PlaybackSources
			}
		}
	}

	items := make([]*domainplayback.PreloadVideo, 0, len(models))
	for _, model := range models {
		item := domainplayback.RestorePreloadVideo(model.VideoID, model.MediaURL, model.CoverURL)
		item.MediaStatus = model.MediaStatus
		item.PlaybackSources = model.PlaybackSources
		items = append(items, item)
	}
	return items, nil
}

// CreateQoSReport 保存播放质量流水，支持 user_id + idempotency_key 幂等。
func (r *Repository) CreateQoSReport(ctx context.Context, report *domainplayback.QoSReport) (*domainplayback.QoSReport, bool, error) {
	model := QoSLogModel{
		UserID:         report.UserID,
		VideoID:        report.VideoID,
		FirstFrameMs:   report.FirstFrameMs,
		StutterCount:   report.StutterCount,
		WatchMs:        report.WatchMs,
		IdempotencyKey: optionalString(report.IdempotencyKey),
	}

	err := r.db.WithContext(ctx).Create(&model).Error
	if err == nil {
		return restoreQoSReport(model), true, nil
	}
	if !infrapersistence.IsDuplicatedKeyError(err) {
		return nil, false, err
	}

	existing, findErr := r.findExistingQoS(ctx, report.UserID, report.IdempotencyKey)
	if findErr != nil {
		return nil, false, findErr
	}
	return restoreQoSReport(existing), false, nil
}

// CreateTelemetryBatch stores a bounded telemetry batch atomically and accounts for safe event replays.
func (r *Repository) CreateTelemetryBatch(ctx context.Context, batch *domainplayback.TelemetryBatch) (*domainplayback.TelemetryBatchWriteResult, error) {
	if batch == nil || len(batch.Events) == 0 || len(batch.Events) > domainplayback.MaxTelemetryEventsPerBatch {
		return nil, domainplayback.ErrInvalidTelemetryEventCount
	}

	batchHash, err := telemetryBatchPayloadHash(batch)
	if err != nil {
		return nil, err
	}

	var writeResult *domainplayback.TelemetryBatchWriteResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", telemetryReporterLockKey(batch)).Error; err != nil {
			return err
		}
		batchModel := telemetryBatchModelFromDomain(batch, batchHash)
		createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&batchModel)
		if createResult.Error != nil {
			return createResult.Error
		}
		if createResult.RowsAffected == 0 {
			existing, err := findTelemetryBatchModel(tx, batch)
			if err != nil {
				return err
			}
			if existing.PayloadHash != batchHash {
				return domainplayback.ErrTelemetryBatchConflict
			}
			writeResult = telemetryBatchWriteResult(existing, false)
			writeResult.DuplicateEventIDs = telemetryEventIDs(batch.Events)
			return nil
		}

		eventModels := make([]TelemetryEventModel, 0, len(batch.Events))
		for _, event := range batch.Events {
			eventHash, err := telemetryEventPayloadHash(batch, event)
			if err != nil {
				return err
			}
			eventModels = append(eventModels, telemetryEventModelFromDomain(batchModel.ID, batch, event, eventHash))
		}

		existingEvents, err := findTelemetryEventModels(tx, batch, eventModels)
		if err != nil {
			return err
		}
		pendingModels := make([]TelemetryEventModel, 0, len(eventModels))
		acceptedEventIDs := make([]string, 0, len(eventModels))
		duplicateEventIDs := make([]string, 0, len(existingEvents))
		for _, model := range eventModels {
			existing, exists := existingEvents[model.EventID]
			if exists {
				if existing.PayloadHash != model.PayloadHash {
					return domainplayback.ErrTelemetryEventConflict
				}
				duplicateEventIDs = append(duplicateEventIDs, model.EventID)
				continue
			}
			pendingModels = append(pendingModels, model)
			acceptedEventIDs = append(acceptedEventIDs, model.EventID)
		}
		if len(pendingModels) > 0 {
			eventCreateResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pendingModels)
			if eventCreateResult.Error != nil {
				return eventCreateResult.Error
			}
			if int(eventCreateResult.RowsAffected) != len(pendingModels) {
				return domainplayback.ErrTelemetryEventConflict
			}
		}

		acceptedCount := len(acceptedEventIDs)
		duplicateCount := len(duplicateEventIDs)
		batchModel.AcceptedCount = acceptedCount
		batchModel.DuplicateCount = duplicateCount
		if err := tx.Model(&TelemetryBatchModel{}).
			Where("id = ?", batchModel.ID).
			Updates(map[string]any{
				"accepted_count":  acceptedCount,
				"duplicate_count": duplicateCount,
			}).Error; err != nil {
			return err
		}
		writeResult = telemetryBatchWriteResult(batchModel, true)
		writeResult.AcceptedEventIDs = acceptedEventIDs
		writeResult.DuplicateEventIDs = duplicateEventIDs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return writeResult, nil
}

func (r *Repository) DeleteTelemetryBefore(ctx context.Context, cutoff time.Time, limit int) (*domainplayback.TelemetryCleanupResult, error) {
	if limit <= 0 {
		return &domainplayback.TelemetryCleanupResult{}, nil
	}
	if limit > 10_000 {
		limit = 10_000
	}

	result := &domainplayback.TelemetryCleanupResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var eventIDs []int64
		if err := tx.Model(&TelemetryEventModel{}).
			Select("id").
			Where("created_at < ?", cutoff).
			Order("created_at ASC, id ASC").
			Limit(limit).
			Find(&eventIDs).Error; err != nil {
			return err
		}
		if len(eventIDs) > 0 {
			deleted := tx.Where("id IN ?", eventIDs).Delete(&TelemetryEventModel{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.DeletedEvents = deleted.RowsAffected
		}

		var batchIDs []int64
		if err := tx.Model(&TelemetryBatchModel{}).
			Select("id").
			Where("created_at < ?", cutoff).
			Where("NOT EXISTS (?)",
				tx.Model(&TelemetryEventModel{}).
					Select("1").
					Where("playback_telemetry_event.batch_record_id = playback_telemetry_batch.id"),
			).
			Order("created_at ASC, id ASC").
			Limit(limit).
			Find(&batchIDs).Error; err != nil {
			return err
		}
		if len(batchIDs) > 0 {
			deleted := tx.Where("id IN ?", batchIDs).Delete(&TelemetryBatchModel{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.DeletedBatches = deleted.RowsAffected
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) findCurrentVideo(ctx context.Context, videoID int64) (*currentVideoModel, error) {
	var model currentVideoModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.published_at").
		Where("v.id = ? AND v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", videoID, domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &model, nil
}

func (r *Repository) findExistingQoS(ctx context.Context, userID int64, idempotencyKey string) (QoSLogModel, error) {
	var model QoSLogModel
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return model, gorm.ErrRecordNotFound
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
		Order("id DESC").
		Take(&model).
		Error
	return model, err
}

type currentVideoModel struct {
	VideoID     int64
	PublishedAt time.Time
}

func restoreConfig(model ConfigModel) *domainplayback.Config {
	return domainplayback.RestoreConfig(model.ID, model.Platform, model.NetworkType, model.PreloadCount, model.BufferMs, model.UpdatedAt)
}

func restoreQoSReport(model QoSLogModel) *domainplayback.QoSReport {
	return domainplayback.RestoreQoSReport(
		model.ID,
		model.UserID,
		model.VideoID,
		model.FirstFrameMs,
		model.StutterCount,
		model.WatchMs,
		stringValue(model.IdempotencyKey),
		model.CreatedAt,
	)
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func telemetryBatchModelFromDomain(batch *domainplayback.TelemetryBatch, payloadHash string) TelemetryBatchModel {
	return TelemetryBatchModel{
		UserID:             telemetryUserID(batch.UserID),
		AnonymousSessionID: optionalString(batch.AnonymousSessionID),
		SchemaVersion:      batch.SchemaVersion,
		BatchID:            batch.BatchID,
		PlaybackSessionID:  batch.PlaybackSessionID,
		PayloadHash:        payloadHash,
		EventCount:         len(batch.Events),
		ClientSentAt:       batch.ClientSentAt,
	}
}

func telemetryEventModelFromDomain(batchRecordID int64, batch *domainplayback.TelemetryBatch, event domainplayback.TelemetryEvent, payloadHash string) TelemetryEventModel {
	sourceType := batch.Context.SourceType
	if event.SourceType != "" {
		sourceType = event.SourceType
	}
	renditionLabel := batch.Context.RenditionLabel
	if event.RenditionLabel != "" {
		renditionLabel = event.RenditionLabel
	}
	codecFamily := batch.Context.CodecFamily
	if event.CodecFamily != "" {
		codecFamily = event.CodecFamily
	}
	cdnHost := batch.Context.CDNHost
	if event.CDNHost != "" {
		cdnHost = event.CDNHost
	}

	return TelemetryEventModel{
		BatchRecordID:         batchRecordID,
		UserID:                telemetryUserID(batch.UserID),
		AnonymousSessionID:    optionalString(batch.AnonymousSessionID),
		SchemaVersion:         batch.SchemaVersion,
		BatchID:               batch.BatchID,
		PlaybackSessionID:     batch.PlaybackSessionID,
		EventID:               event.EventID,
		PayloadHash:           payloadHash,
		EventType:             string(event.EventType),
		VideoID:               batch.Context.VideoID,
		Scene:                 batch.Context.Scene,
		RequestID:             optionalString(batch.Context.RequestID),
		OffsetMs:              event.OffsetMs,
		MediaPositionMs:       event.MediaPositionMs,
		MediaDurationMs:       event.MediaDurationMs,
		FirstFrameMs:          event.FirstFrameMs,
		IntervalDurationMs:    event.IntervalDurationMs,
		DroppedFrames:         event.DroppedFrames,
		TotalFrames:           event.TotalFrames,
		RebufferCount:         event.RebufferCount,
		RebufferDurationMs:    event.RebufferDurationMs,
		MaxRebufferDurationMs: event.MaxRebufferDurationMs,
		StartupRetryCount:     event.StartupRetryCount,
		MeasurementMethod:     optionalString(string(event.MeasurementMethod)),
		RecoveryOutcome:       optionalString(string(event.RecoveryOutcome)),
		ErrorCategory:         optionalString(string(event.ErrorCategory)),
		PlayerAdapter:         string(batch.Context.PlayerAdapter),
		SourceType:            string(sourceType),
		RenditionLabel:        renditionLabel,
		CodecFamily:           string(codecFamily),
		NetworkClass:          string(batch.Context.NetworkClass),
		SaveData:              batch.Context.SaveData,
		BrowserFamily:         string(batch.Context.BrowserFamily),
		BrowserMajor:          batch.Context.BrowserMajor,
		OSFamily:              string(batch.Context.OSFamily),
		ViewportClass:         string(batch.Context.ViewportClass),
		CDNHost:               optionalString(cdnHost),
		ClientSentAt:          batch.ClientSentAt,
	}
}

func findTelemetryBatchModel(tx *gorm.DB, batch *domainplayback.TelemetryBatch) (TelemetryBatchModel, error) {
	var model TelemetryBatchModel
	query := telemetryReporterQuery(tx, batch.UserID, batch.AnonymousSessionID)
	err := query.Where("batch_id = ?", batch.BatchID).Take(&model).Error
	return model, err
}

func findTelemetryEventModels(tx *gorm.DB, batch *domainplayback.TelemetryBatch, models []TelemetryEventModel) (map[string]TelemetryEventModel, error) {
	eventIDs := make([]string, 0, len(models))
	for _, model := range models {
		eventIDs = append(eventIDs, model.EventID)
	}
	var existing []TelemetryEventModel
	query := telemetryReporterQuery(tx, batch.UserID, batch.AnonymousSessionID)
	if err := query.Where("event_id IN ?", eventIDs).Find(&existing).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]TelemetryEventModel, len(existing))
	for _, model := range existing {
		byID[model.EventID] = model
	}
	return byID, nil
}

func telemetryReporterQuery(tx *gorm.DB, userID int64, anonymousSessionID string) *gorm.DB {
	if userID > 0 {
		return tx.Where("user_id = ?", userID)
	}
	return tx.Where("anonymous_session_id = ?", strings.TrimSpace(anonymousSessionID))
}

func telemetryBatchWriteResult(model TelemetryBatchModel, created bool) *domainplayback.TelemetryBatchWriteResult {
	return &domainplayback.TelemetryBatchWriteResult{
		BatchID:        model.BatchID,
		EventCount:     model.EventCount,
		AcceptedCount:  model.AcceptedCount,
		DuplicateCount: model.DuplicateCount,
		Created:        created,
		CreatedAt:      model.CreatedAt,
	}
}

func telemetryBatchPayloadHash(batch *domainplayback.TelemetryBatch) (string, error) {
	payload := struct {
		SchemaVersion     int
		PlaybackSessionID string
		Context           domainplayback.TelemetryContext
		Events            []domainplayback.TelemetryEvent
	}{
		SchemaVersion:     batch.SchemaVersion,
		PlaybackSessionID: batch.PlaybackSessionID,
		Context:           batch.Context,
		Events:            batch.Events,
	}
	return telemetryPayloadHash(payload)
}

func telemetryEventPayloadHash(batch *domainplayback.TelemetryBatch, event domainplayback.TelemetryEvent) (string, error) {
	payload := struct {
		SchemaVersion     int
		PlaybackSessionID string
		Context           domainplayback.TelemetryContext
		Event             domainplayback.TelemetryEvent
	}{
		SchemaVersion:     batch.SchemaVersion,
		PlaybackSessionID: batch.PlaybackSessionID,
		Context:           batch.Context,
		Event:             event,
	}
	return telemetryPayloadHash(payload)
}

func telemetryPayloadHash(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func telemetryUserID(userID int64) *int64 {
	if userID <= 0 {
		return nil
	}
	return &userID
}

func telemetryReporterLockKey(batch *domainplayback.TelemetryBatch) int64 {
	hasher := fnv.New64a()
	if batch.UserID > 0 {
		_, _ = hasher.Write([]byte("user:"))
		_, _ = hasher.Write([]byte(strconv.FormatInt(batch.UserID, 10)))
	} else {
		_, _ = hasher.Write([]byte("anonymous:"))
		_, _ = hasher.Write([]byte(batch.AnonymousSessionID))
	}
	return int64(hasher.Sum64())
}

func telemetryEventIDs(events []domainplayback.TelemetryEvent) []string {
	eventIDs := make([]string, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.EventID)
	}
	return eventIDs
}
