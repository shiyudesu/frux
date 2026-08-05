package interfaceshttpplayback

import (
	"context"
	"errors"
	applicationplayback "github.com/shiyudesu/frux/internal/application/playback"
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service                  *applicationplayback.Service
	telemetryLimiter         *telemetryRateLimiter
	recordTelemetryRejection func(int)
}

type Option func(*Handler)

// New 注入播放优化应用服务。
func New(service *applicationplayback.Service, options ...Option) *Handler {
	handler := &Handler{
		service:          service,
		telemetryLimiter: newTelemetryRateLimiter(defaultTelemetryBatchesPerMinute, time.Minute, defaultTelemetryRateLimitEntries),
	}
	for _, option := range options {
		option(handler)
	}
	return handler
}

func WithTelemetryRateLimit(batchesPerMinute int) Option {
	return func(handler *Handler) {
		handler.telemetryLimiter = newTelemetryRateLimiter(batchesPerMinute, time.Minute, defaultTelemetryRateLimitEntries)
	}
}

func WithTelemetryRejectionRecorder(record func(int)) Option {
	return func(handler *Handler) {
		handler.recordTelemetryRejection = record
	}
}

// GetConfig 查询当前客户端播放配置。
func (h *Handler) GetConfig(ctx context.Context, c *app.RequestContext) {
	result, err := h.service.GetConfig(ctx, c.Query("platform"), c.Query("network_type"))
	if err != nil {
		writePlaybackError(c, err)
		return
	}
	c.JSON(http.StatusOK, configResponseFromResult(result))
}

// ListPreloadVideos 保留兼容客户端使用的发布时间顺序补充接口。
func (h *Handler) ListPreloadVideos(ctx context.Context, c *app.RequestContext) {
	currentVideoID, err := parseOptionalInt64(c.Query("current_video_id"))
	if err != nil {
		writePlaybackError(c, domainplayback.ErrInvalidVideoID)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writePlaybackError(c, err)
		return
	}

	result, err := h.service.ListPreloadVideos(ctx, currentVideoID, limit)
	if err != nil {
		writePlaybackError(c, err)
		return
	}
	c.JSON(http.StatusOK, preloadResponseFromResult(result))
}

// CreateQoSReport 处理 Web 客户端播放质量上报。
func (h *Handler) CreateQoSReport(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	h.createQoSReport(ctx, c, userID)
}

// CreateInternalQoSReport 处理服务间播放质量上报。
func (h *Handler) CreateInternalQoSReport(ctx context.Context, c *app.RequestContext) {
	var req createQoSReportRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	h.createQoSReportWithRequest(ctx, c, req.UserID, req)
}

func (h *Handler) CreateTelemetryBatch(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	if h.telemetryLimiter != nil && !h.telemetryLimiter.Allow(userID) {
		h.recordRejectedTelemetry(0)
		interfaceshttpapierror.Write(c, http.StatusTooManyRequests, interfaceshttpapierror.CodePlaybackTelemetryRateLimited, "telemetry rate limit exceeded")
		return
	}

	var req createTelemetryBatchRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &req, domainplayback.MaxTelemetryPayloadBytes); err != nil {
		h.recordRejectedTelemetry(0)
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.CreateTelemetryBatch(ctx, telemetryBatchInputFromRequest(userID, req))
	if err != nil {
		writePlaybackError(c, err)
		return
	}

	status := http.StatusCreated
	if !result.Summary.Created {
		status = http.StatusOK
	}
	c.JSON(status, telemetryBatchResponseFromResult(result))
}

func (h *Handler) recordRejectedTelemetry(eventCount int) {
	if h.recordTelemetryRejection != nil {
		h.recordTelemetryRejection(eventCount)
	}
}

func (h *Handler) createQoSReport(ctx context.Context, c *app.RequestContext, userID int64) {
	var req createQoSReportRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	h.createQoSReportWithRequest(ctx, c, userID, req)
}

func (h *Handler) createQoSReportWithRequest(ctx context.Context, c *app.RequestContext, userID int64, req createQoSReportRequest) {
	result, err := h.service.CreateQoSReport(
		ctx,
		userID,
		req.VideoID,
		req.FirstFrameMs,
		req.StutterCount,
		req.WatchMs,
		string(c.GetHeader("Idempotency-Key")),
	)
	if err != nil {
		writePlaybackError(c, err)
		return
	}

	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, qosResponseFromResult(result))
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func parseOptionalInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, domainplayback.ErrInvalidVideoID
	}
	return value, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domainplayback.ErrInvalidLimit
	}
	return limit, nil
}

func configResponseFromResult(result *applicationplayback.ConfigResult) playbackConfigResponse {
	config := result.Config
	return playbackConfigResponse{
		ID:           config.ID,
		Platform:     config.Platform,
		NetworkType:  config.NetworkType,
		PreloadCount: config.PreloadCount,
		BufferMs:     config.BufferMs,
		UpdatedAt:    config.UpdatedAt,
	}
}

func preloadResponseFromResult(result *applicationplayback.PreloadResult) preloadVideosResponse {
	items := make([]preloadVideoResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, preloadVideoResponse{
			VideoID:         item.VideoID,
			MediaURL:        item.MediaURL,
			CoverURL:        item.CoverURL,
			MediaStatus:     item.MediaStatus,
			PlaybackSources: item.PlaybackSources,
		})
	}
	return preloadVideosResponse{Items: items}
}

func qosResponseFromResult(result *applicationplayback.QoSReportResult) qosReportResponse {
	report := result.Report
	return qosReportResponse{
		ID:           report.ID,
		UserID:       report.UserID,
		VideoID:      report.VideoID,
		FirstFrameMs: report.FirstFrameMs,
		StutterCount: report.StutterCount,
		WatchMs:      report.WatchMs,
		CreatedAt:    report.CreatedAt,
	}
}

func telemetryBatchInputFromRequest(userID int64, req createTelemetryBatchRequest) domainplayback.NewTelemetryBatchInput {
	events := make([]domainplayback.NewTelemetryEventInput, 0, len(req.Events))
	for _, event := range req.Events {
		events = append(events, domainplayback.NewTelemetryEventInput{
			EventID:               event.EventID,
			EventType:             domainplayback.TelemetryEventType(event.EventType),
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
			MeasurementMethod:     domainplayback.TelemetryMeasurementMethod(event.MeasurementMethod),
			RecoveryOutcome:       domainplayback.TelemetryRecoveryOutcome(event.RecoveryOutcome),
			ErrorCategory:         domainplayback.TelemetryErrorCategory(event.ErrorCategory),
			SourceType:            domainplayback.TelemetrySourceType(event.SourceType),
			RenditionLabel:        event.RenditionLabel,
			CodecFamily:           domainplayback.TelemetryCodecFamily(event.CodecFamily),
			CDNHost:               event.CDNHost,
		})
	}
	return domainplayback.NewTelemetryBatchInput{
		UserID:            userID,
		SchemaVersion:     req.SchemaVersion,
		BatchID:           req.BatchID,
		PlaybackSessionID: req.PlaybackSessionID,
		ClientSentAt:      req.ClientSentAt,
		Context: domainplayback.TelemetryContext{
			VideoID:        req.Context.VideoID,
			Scene:          req.Context.Scene,
			RequestID:      req.Context.RequestID,
			PlayerAdapter:  domainplayback.TelemetryPlayerAdapter(req.Context.PlayerAdapter),
			SourceType:     domainplayback.TelemetrySourceType(req.Context.SourceType),
			RenditionLabel: req.Context.RenditionLabel,
			CodecFamily:    domainplayback.TelemetryCodecFamily(req.Context.CodecFamily),
			NetworkClass:   domainplayback.TelemetryNetworkClass(req.Context.NetworkClass),
			SaveData:       req.Context.SaveData,
			BrowserFamily:  domainplayback.TelemetryBrowserFamily(req.Context.BrowserFamily),
			BrowserMajor:   req.Context.BrowserMajor,
			OSFamily:       domainplayback.TelemetryOSFamily(req.Context.OSFamily),
			ViewportClass:  domainplayback.TelemetryViewportClass(req.Context.ViewportClass),
			CDNHost:        req.Context.CDNHost,
		},
		Events: events,
	}
}

func telemetryBatchResponseFromResult(result *applicationplayback.TelemetryBatchResult) telemetryBatchResponse {
	summary := result.Summary
	return telemetryBatchResponse{
		BatchID:        summary.BatchID,
		EventCount:     summary.EventCount,
		AcceptedCount:  summary.AcceptedCount,
		DuplicateCount: summary.DuplicateCount,
		CreatedAt:      summary.CreatedAt,
	}
}

func writePlaybackError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodePlaybackValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainplayback.ErrTelemetryBatchConflict) ||
		errors.Is(err, domainplayback.ErrTelemetryEventConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodePlaybackTelemetryConflict, err.Error())
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainplayback.ErrInvalidUserID) ||
		errors.Is(err, domainplayback.ErrInvalidVideoID) ||
		errors.Is(err, domainplayback.ErrInvalidPlatform) ||
		errors.Is(err, domainplayback.ErrInvalidNetworkType) ||
		errors.Is(err, domainplayback.ErrInvalidLimit) ||
		errors.Is(err, domainplayback.ErrInvalidFirstFrameMs) ||
		errors.Is(err, domainplayback.ErrInvalidStutterCount) ||
		errors.Is(err, domainplayback.ErrInvalidWatchMs) ||
		errors.Is(err, domainplayback.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainplayback.ErrUnsupportedTelemetryVersion) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryReporter) ||
		errors.Is(err, domainplayback.ErrEmptyTelemetryBatchID) ||
		errors.Is(err, domainplayback.ErrTelemetryBatchIDTooLong) ||
		errors.Is(err, domainplayback.ErrEmptyTelemetrySessionID) ||
		errors.Is(err, domainplayback.ErrTelemetrySessionIDTooLong) ||
		errors.Is(err, domainplayback.ErrTelemetryAnonymousSessionIDTooLong) ||
		errors.Is(err, domainplayback.ErrEmptyTelemetrySentAt) ||
		errors.Is(err, domainplayback.ErrTelemetrySentAtOutOfRange) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryContext) ||
		errors.Is(err, domainplayback.ErrTelemetryStringTooLong) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryEventCount) ||
		errors.Is(err, domainplayback.ErrEmptyTelemetryEventID) ||
		errors.Is(err, domainplayback.ErrTelemetryEventIDTooLong) ||
		errors.Is(err, domainplayback.ErrDuplicateTelemetryEventID) ||
		errors.Is(err, domainplayback.ErrUnsupportedTelemetryEventType) ||
		errors.Is(err, domainplayback.ErrTelemetryEventsOutOfOrder) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryOffset) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryPosition) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryDuration) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryMetric) ||
		errors.Is(err, domainplayback.ErrInvalidTelemetryDimension) ||
		errors.Is(err, domainplayback.ErrMissingTelemetryField) ||
		errors.Is(err, domainplayback.ErrUnexpectedTelemetryField)
}
