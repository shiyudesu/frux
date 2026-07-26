package domainplayback

import (
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"
)

const (
	TelemetrySchemaVersionV1 = 1

	MaxTelemetryPayloadBytes      = 64 * 1024
	MaxTelemetryEventsPerBatch    = 50
	MaxTelemetryIDLength          = 128
	MaxTelemetrySceneLength       = 32
	MaxTelemetryRequestIDLength   = 64
	MaxTelemetryRenditionLength   = 32
	MaxTelemetryCDNHostLength     = 253
	MaxTelemetryBrowserMajor      = 999
	MaxTelemetrySessionDurationMs = int64(24 * time.Hour / time.Millisecond)
	MaxTelemetryStartupDurationMs = int64(10 * time.Minute / time.Millisecond)
	MaxTelemetryFrameCount        = int64(1<<31 - 1)
	MaxTelemetryStartupRetryCount = 20
	MaxTelemetryPastSentAtSkew    = 24 * time.Hour
	MaxTelemetryFutureSentAtSkew  = 5 * time.Minute
)

type TelemetryEventType string

const (
	TelemetryEventLoadStart          TelemetryEventType = "load_start"
	TelemetryEventMetadataReady      TelemetryEventType = "metadata_ready"
	TelemetryEventFirstRenderedFrame TelemetryEventType = "first_rendered_frame"
	TelemetryEventPlaySuccess        TelemetryEventType = "play_success"
	TelemetryEventPlayFailure        TelemetryEventType = "play_failure"
	TelemetryEventRebufferStart      TelemetryEventType = "rebuffer_start"
	TelemetryEventRebufferEnd        TelemetryEventType = "rebuffer_end"
	TelemetryEventSeekStart          TelemetryEventType = "seek_start"
	TelemetryEventSeekEnd            TelemetryEventType = "seek_end"
	TelemetryEventSourceChange       TelemetryEventType = "source_change"
	TelemetryEventQualityChange      TelemetryEventType = "quality_change"
	TelemetryEventPause              TelemetryEventType = "pause"
	TelemetryEventEnd                TelemetryEventType = "end"
	TelemetryEventTerminalError      TelemetryEventType = "terminal_error"
)

type TelemetryPlayerAdapter string

const (
	TelemetryPlayerAdapterNativeMP4 TelemetryPlayerAdapter = "native_mp4"
	TelemetryPlayerAdapterDASH      TelemetryPlayerAdapter = "dash"
	TelemetryPlayerAdapterUnknown   TelemetryPlayerAdapter = "unknown"
)

type TelemetrySourceType string

const (
	TelemetrySourceMP4     TelemetrySourceType = "mp4"
	TelemetrySourceDASH    TelemetrySourceType = "dash"
	TelemetrySourceUnknown TelemetrySourceType = "unknown"
)

type TelemetryCodecFamily string

const (
	TelemetryCodecH264    TelemetryCodecFamily = "h264"
	TelemetryCodecH265    TelemetryCodecFamily = "h265"
	TelemetryCodecVP8     TelemetryCodecFamily = "vp8"
	TelemetryCodecVP9     TelemetryCodecFamily = "vp9"
	TelemetryCodecAV1     TelemetryCodecFamily = "av1"
	TelemetryCodecOther   TelemetryCodecFamily = "other"
	TelemetryCodecUnknown TelemetryCodecFamily = "unknown"
)

type TelemetryNetworkClass string

const (
	TelemetryNetworkOffline  TelemetryNetworkClass = "offline"
	TelemetryNetworkSlow2G   TelemetryNetworkClass = "slow_2g"
	TelemetryNetwork2G       TelemetryNetworkClass = "2g"
	TelemetryNetwork3G       TelemetryNetworkClass = "3g"
	TelemetryNetwork4G       TelemetryNetworkClass = "4g"
	TelemetryNetwork5G       TelemetryNetworkClass = "5g"
	TelemetryNetworkWiFi     TelemetryNetworkClass = "wifi"
	TelemetryNetworkEthernet TelemetryNetworkClass = "ethernet"
	TelemetryNetworkUnknown  TelemetryNetworkClass = "unknown"
)

type TelemetryBrowserFamily string

const (
	TelemetryBrowserChrome  TelemetryBrowserFamily = "chrome"
	TelemetryBrowserEdge    TelemetryBrowserFamily = "edge"
	TelemetryBrowserFirefox TelemetryBrowserFamily = "firefox"
	TelemetryBrowserSafari  TelemetryBrowserFamily = "safari"
	TelemetryBrowserOther   TelemetryBrowserFamily = "other"
	TelemetryBrowserUnknown TelemetryBrowserFamily = "unknown"
)

type TelemetryOSFamily string

const (
	TelemetryOSWindows  TelemetryOSFamily = "windows"
	TelemetryOSMacOS    TelemetryOSFamily = "macos"
	TelemetryOSIOS      TelemetryOSFamily = "ios"
	TelemetryOSAndroid  TelemetryOSFamily = "android"
	TelemetryOSLinux    TelemetryOSFamily = "linux"
	TelemetryOSChromeOS TelemetryOSFamily = "chromeos"
	TelemetryOSOther    TelemetryOSFamily = "other"
	TelemetryOSUnknown  TelemetryOSFamily = "unknown"
)

type TelemetryViewportClass string

const (
	TelemetryViewportSmall   TelemetryViewportClass = "small"
	TelemetryViewportMedium  TelemetryViewportClass = "medium"
	TelemetryViewportLarge   TelemetryViewportClass = "large"
	TelemetryViewportUnknown TelemetryViewportClass = "unknown"
)

type TelemetryMeasurementMethod string

const (
	TelemetryMeasurementVideoFrameCallback TelemetryMeasurementMethod = "video_frame_callback"
	TelemetryMeasurementAdvancingTime      TelemetryMeasurementMethod = "advancing_time"
	TelemetryMeasurementPlaying            TelemetryMeasurementMethod = "playing"
)

type TelemetryRecoveryOutcome string

const (
	TelemetryRecoveryResumed       TelemetryRecoveryOutcome = "resumed"
	TelemetryRecoveryPaused        TelemetryRecoveryOutcome = "paused"
	TelemetryRecoverySeeked        TelemetryRecoveryOutcome = "seeked"
	TelemetryRecoverySourceChanged TelemetryRecoveryOutcome = "source_changed"
	TelemetryRecoveryEnded         TelemetryRecoveryOutcome = "ended"
	TelemetryRecoveryFailed        TelemetryRecoveryOutcome = "failed"
)

type TelemetryErrorCategory string

const (
	TelemetryErrorAborted     TelemetryErrorCategory = "aborted"
	TelemetryErrorNetwork     TelemetryErrorCategory = "network"
	TelemetryErrorDecode      TelemetryErrorCategory = "decode"
	TelemetryErrorUnsupported TelemetryErrorCategory = "unsupported"
	TelemetryErrorAutoplay    TelemetryErrorCategory = "autoplay"
	TelemetryErrorTimeout     TelemetryErrorCategory = "timeout"
	TelemetryErrorUnknown     TelemetryErrorCategory = "unknown"
)

type TelemetryContext struct {
	VideoID        int64
	Scene          string
	RequestID      string
	PlayerAdapter  TelemetryPlayerAdapter
	SourceType     TelemetrySourceType
	RenditionLabel string
	CodecFamily    TelemetryCodecFamily
	NetworkClass   TelemetryNetworkClass
	SaveData       bool
	BrowserFamily  TelemetryBrowserFamily
	BrowserMajor   int
	OSFamily       TelemetryOSFamily
	ViewportClass  TelemetryViewportClass
	CDNHost        string
}

type NewTelemetryEventInput struct {
	EventID               string
	EventType             TelemetryEventType
	OffsetMs              int64
	MediaPositionMs       int64
	MediaDurationMs       *int64
	FirstFrameMs          *int64
	IntervalDurationMs    *int64
	DroppedFrames         *int64
	TotalFrames           *int64
	RebufferCount         *int
	RebufferDurationMs    *int64
	MaxRebufferDurationMs *int64
	StartupRetryCount     *int
	MeasurementMethod     TelemetryMeasurementMethod
	RecoveryOutcome       TelemetryRecoveryOutcome
	ErrorCategory         TelemetryErrorCategory
	SourceType            TelemetrySourceType
	RenditionLabel        string
	CodecFamily           TelemetryCodecFamily
	CDNHost               string
}

type TelemetryEvent struct {
	EventID               string
	EventType             TelemetryEventType
	OffsetMs              int64
	MediaPositionMs       int64
	MediaDurationMs       *int64
	FirstFrameMs          *int64
	IntervalDurationMs    *int64
	DroppedFrames         *int64
	TotalFrames           *int64
	RebufferCount         *int
	RebufferDurationMs    *int64
	MaxRebufferDurationMs *int64
	StartupRetryCount     *int
	MeasurementMethod     TelemetryMeasurementMethod
	RecoveryOutcome       TelemetryRecoveryOutcome
	ErrorCategory         TelemetryErrorCategory
	SourceType            TelemetrySourceType
	RenditionLabel        string
	CodecFamily           TelemetryCodecFamily
	CDNHost               string
}

type NewTelemetryBatchInput struct {
	UserID             int64
	AnonymousSessionID string
	SchemaVersion      int
	BatchID            string
	PlaybackSessionID  string
	ClientSentAt       time.Time
	Context            TelemetryContext
	Events             []NewTelemetryEventInput
}

type TelemetryBatch struct {
	UserID             int64
	AnonymousSessionID string
	SchemaVersion      int
	BatchID            string
	PlaybackSessionID  string
	ClientSentAt       time.Time
	Context            TelemetryContext
	Events             []TelemetryEvent
}

type TelemetryBatchWriteResult struct {
	BatchID           string
	EventCount        int
	AcceptedCount     int
	DuplicateCount    int
	AcceptedEventIDs  []string
	DuplicateEventIDs []string
	Created           bool
	CreatedAt         time.Time
}

type TelemetryCleanupResult struct {
	DeletedEvents  int64
	DeletedBatches int64
}

func NewTelemetryBatch(input NewTelemetryBatchInput) (*TelemetryBatch, error) {
	if input.SchemaVersion != TelemetrySchemaVersionV1 {
		return nil, ErrUnsupportedTelemetryVersion
	}
	if input.UserID < 0 {
		return nil, ErrInvalidUserID
	}

	anonymousSessionID := strings.TrimSpace(input.AnonymousSessionID)
	if input.UserID == 0 && anonymousSessionID == "" {
		return nil, ErrInvalidTelemetryReporter
	}
	if input.UserID > 0 && anonymousSessionID != "" {
		return nil, ErrInvalidTelemetryReporter
	}
	if len(anonymousSessionID) > MaxTelemetryIDLength {
		return nil, ErrTelemetryAnonymousSessionIDTooLong
	}

	batchID := strings.TrimSpace(input.BatchID)
	if batchID == "" {
		return nil, ErrEmptyTelemetryBatchID
	}
	if len(batchID) > MaxTelemetryIDLength {
		return nil, ErrTelemetryBatchIDTooLong
	}

	playbackSessionID := strings.TrimSpace(input.PlaybackSessionID)
	if playbackSessionID == "" {
		return nil, ErrEmptyTelemetrySessionID
	}
	if len(playbackSessionID) > MaxTelemetryIDLength {
		return nil, ErrTelemetrySessionIDTooLong
	}
	if input.ClientSentAt.IsZero() {
		return nil, ErrEmptyTelemetrySentAt
	}
	if len(input.Events) == 0 || len(input.Events) > MaxTelemetryEventsPerBatch {
		return nil, ErrInvalidTelemetryEventCount
	}

	context, err := normalizeTelemetryContext(input.Context)
	if err != nil {
		return nil, err
	}

	events := make([]TelemetryEvent, 0, len(input.Events))
	eventIDs := make(map[string]struct{}, len(input.Events))
	var previousOffset int64
	for index, eventInput := range input.Events {
		event, err := newTelemetryEvent(eventInput)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", index, err)
		}
		if _, exists := eventIDs[event.EventID]; exists {
			return nil, ErrDuplicateTelemetryEventID
		}
		if index > 0 && event.OffsetMs < previousOffset {
			return nil, ErrTelemetryEventsOutOfOrder
		}
		eventIDs[event.EventID] = struct{}{}
		previousOffset = event.OffsetMs
		events = append(events, *event)
	}

	return &TelemetryBatch{
		UserID:             input.UserID,
		AnonymousSessionID: anonymousSessionID,
		SchemaVersion:      input.SchemaVersion,
		BatchID:            batchID,
		PlaybackSessionID:  playbackSessionID,
		ClientSentAt:       input.ClientSentAt.UTC().Truncate(time.Microsecond),
		Context:            context,
		Events:             events,
	}, nil
}

func normalizeTelemetryContext(input TelemetryContext) (TelemetryContext, error) {
	if input.VideoID <= 0 {
		return TelemetryContext{}, ErrInvalidVideoID
	}
	rawScene := strings.TrimSpace(input.Scene)
	if len(rawScene) > MaxTelemetrySceneLength {
		return TelemetryContext{}, fmt.Errorf("%w: scene", ErrTelemetryStringTooLong)
	}
	scene := NormalizeTelemetryScene(rawScene)
	requestID := strings.TrimSpace(input.RequestID)
	if len(requestID) > MaxTelemetryRequestIDLength {
		return TelemetryContext{}, fmt.Errorf("%w: request_id", ErrTelemetryStringTooLong)
	}

	playerAdapter := NormalizeTelemetryPlayerAdapter(string(input.PlayerAdapter))
	sourceType := NormalizeTelemetrySourceType(string(input.SourceType))
	codecFamily := NormalizeTelemetryCodecFamily(string(input.CodecFamily))
	networkClass := NormalizeTelemetryNetworkClass(string(input.NetworkClass))
	browserFamily := NormalizeTelemetryBrowserFamily(string(input.BrowserFamily))
	if input.BrowserMajor < 0 || input.BrowserMajor > MaxTelemetryBrowserMajor {
		return TelemetryContext{}, fmt.Errorf("%w: browser_major", ErrInvalidTelemetryDimension)
	}
	osFamily := NormalizeTelemetryOSFamily(string(input.OSFamily))
	viewportClass := NormalizeTelemetryViewportClass(string(input.ViewportClass))
	renditionLabel := NormalizeTelemetryRenditionLabel(input.RenditionLabel)
	cdnHost, err := NormalizeTelemetryCDNHost(input.CDNHost)
	if err != nil {
		return TelemetryContext{}, err
	}

	return TelemetryContext{
		VideoID:        input.VideoID,
		Scene:          scene,
		RequestID:      requestID,
		PlayerAdapter:  playerAdapter,
		SourceType:     sourceType,
		RenditionLabel: renditionLabel,
		CodecFamily:    codecFamily,
		NetworkClass:   networkClass,
		SaveData:       input.SaveData,
		BrowserFamily:  browserFamily,
		BrowserMajor:   input.BrowserMajor,
		OSFamily:       osFamily,
		ViewportClass:  viewportClass,
		CDNHost:        cdnHost,
	}, nil
}

func newTelemetryEvent(input NewTelemetryEventInput) (*TelemetryEvent, error) {
	eventID := strings.TrimSpace(input.EventID)
	if eventID == "" {
		return nil, ErrEmptyTelemetryEventID
	}
	if len(eventID) > MaxTelemetryIDLength {
		return nil, ErrTelemetryEventIDTooLong
	}
	eventType := TelemetryEventType(normalizeTelemetryToken(string(input.EventType), ""))
	if !ValidTelemetryEventType(eventType) {
		return nil, ErrUnsupportedTelemetryEventType
	}
	if input.OffsetMs < 0 || input.OffsetMs > MaxTelemetrySessionDurationMs {
		return nil, ErrInvalidTelemetryOffset
	}
	if input.MediaPositionMs < 0 || input.MediaPositionMs > MaxTelemetrySessionDurationMs {
		return nil, ErrInvalidTelemetryPosition
	}
	if input.MediaDurationMs != nil {
		if *input.MediaDurationMs <= 0 || *input.MediaDurationMs > MaxTelemetrySessionDurationMs || input.MediaPositionMs > *input.MediaDurationMs {
			return nil, ErrInvalidTelemetryDuration
		}
	}
	if input.FirstFrameMs != nil && (*input.FirstFrameMs < 0 || *input.FirstFrameMs > MaxTelemetryStartupDurationMs) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.IntervalDurationMs != nil && (*input.IntervalDurationMs < 0 || *input.IntervalDurationMs > MaxTelemetrySessionDurationMs) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.DroppedFrames != nil && (*input.DroppedFrames < 0 || *input.DroppedFrames > MaxTelemetryFrameCount) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.TotalFrames != nil && (*input.TotalFrames < 0 || *input.TotalFrames > MaxTelemetryFrameCount) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.DroppedFrames != nil && input.TotalFrames != nil && *input.DroppedFrames > *input.TotalFrames {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.RebufferCount != nil && (*input.RebufferCount < 0 || *input.RebufferCount > 10_000) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.RebufferDurationMs != nil && (*input.RebufferDurationMs < 0 || *input.RebufferDurationMs > MaxTelemetrySessionDurationMs) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.MaxRebufferDurationMs != nil && (*input.MaxRebufferDurationMs < 0 || *input.MaxRebufferDurationMs > MaxTelemetrySessionDurationMs) {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.RebufferDurationMs != nil && input.MaxRebufferDurationMs != nil && *input.MaxRebufferDurationMs > *input.RebufferDurationMs {
		return nil, ErrInvalidTelemetryMetric
	}
	if input.StartupRetryCount != nil && (*input.StartupRetryCount < 0 || *input.StartupRetryCount > MaxTelemetryStartupRetryCount) {
		return nil, ErrInvalidTelemetryMetric
	}

	measurementMethod := TelemetryMeasurementMethod(normalizeTelemetryToken(string(input.MeasurementMethod), ""))
	if measurementMethod != "" && !ValidTelemetryMeasurementMethod(measurementMethod) {
		return nil, fmt.Errorf("%w: measurement_method", ErrInvalidTelemetryDimension)
	}
	recoveryOutcome := TelemetryRecoveryOutcome(normalizeTelemetryToken(string(input.RecoveryOutcome), ""))
	if recoveryOutcome != "" && !ValidTelemetryRecoveryOutcome(recoveryOutcome) {
		return nil, fmt.Errorf("%w: recovery_outcome", ErrInvalidTelemetryDimension)
	}
	errorCategory := TelemetryErrorCategory(normalizeTelemetryToken(string(input.ErrorCategory), ""))
	if errorCategory != "" && !ValidTelemetryErrorCategory(errorCategory) {
		return nil, fmt.Errorf("%w: error_category", ErrInvalidTelemetryDimension)
	}
	sourceType := TelemetrySourceType("")
	if strings.TrimSpace(string(input.SourceType)) != "" {
		sourceType = NormalizeTelemetrySourceType(string(input.SourceType))
	}
	codecFamily := TelemetryCodecFamily("")
	if strings.TrimSpace(string(input.CodecFamily)) != "" {
		codecFamily = NormalizeTelemetryCodecFamily(string(input.CodecFamily))
	}
	renditionLabel := ""
	if strings.TrimSpace(input.RenditionLabel) != "" {
		renditionLabel = NormalizeTelemetryRenditionLabel(input.RenditionLabel)
	}
	cdnHost, err := NormalizeTelemetryCDNHost(input.CDNHost)
	if err != nil {
		return nil, err
	}

	if err := validateTelemetryEventFields(eventType, input, measurementMethod, recoveryOutcome, errorCategory, sourceType, renditionLabel); err != nil {
		return nil, err
	}

	return &TelemetryEvent{
		EventID:               eventID,
		EventType:             eventType,
		OffsetMs:              input.OffsetMs,
		MediaPositionMs:       input.MediaPositionMs,
		MediaDurationMs:       cloneInt64(input.MediaDurationMs),
		FirstFrameMs:          cloneInt64(input.FirstFrameMs),
		IntervalDurationMs:    cloneInt64(input.IntervalDurationMs),
		DroppedFrames:         cloneInt64(input.DroppedFrames),
		TotalFrames:           cloneInt64(input.TotalFrames),
		RebufferCount:         cloneInt(input.RebufferCount),
		RebufferDurationMs:    cloneInt64(input.RebufferDurationMs),
		MaxRebufferDurationMs: cloneInt64(input.MaxRebufferDurationMs),
		StartupRetryCount:     cloneInt(input.StartupRetryCount),
		MeasurementMethod:     measurementMethod,
		RecoveryOutcome:       recoveryOutcome,
		ErrorCategory:         errorCategory,
		SourceType:            sourceType,
		RenditionLabel:        renditionLabel,
		CodecFamily:           codecFamily,
		CDNHost:               cdnHost,
	}, nil
}

func validateTelemetryEventFields(
	eventType TelemetryEventType,
	input NewTelemetryEventInput,
	measurementMethod TelemetryMeasurementMethod,
	recoveryOutcome TelemetryRecoveryOutcome,
	errorCategory TelemetryErrorCategory,
	sourceType TelemetrySourceType,
	renditionLabel string,
) error {
	if eventType == TelemetryEventFirstRenderedFrame {
		if input.FirstFrameMs == nil || measurementMethod == "" {
			return fmt.Errorf("%w: first rendered frame", ErrMissingTelemetryField)
		}
	} else if input.FirstFrameMs != nil || measurementMethod != "" {
		return fmt.Errorf("%w: first rendered frame", ErrUnexpectedTelemetryField)
	}

	switch eventType {
	case TelemetryEventRebufferEnd, TelemetryEventSeekEnd:
		if input.IntervalDurationMs == nil {
			return fmt.Errorf("%w: interval_duration_ms", ErrMissingTelemetryField)
		}
	default:
		if input.IntervalDurationMs != nil {
			return fmt.Errorf("%w: interval_duration_ms", ErrUnexpectedTelemetryField)
		}
	}

	if eventType == TelemetryEventRebufferEnd {
		if recoveryOutcome == "" {
			return fmt.Errorf("%w: recovery_outcome", ErrMissingTelemetryField)
		}
	} else if recoveryOutcome != "" {
		return fmt.Errorf("%w: recovery_outcome", ErrUnexpectedTelemetryField)
	}

	switch eventType {
	case TelemetryEventPlayFailure, TelemetryEventTerminalError:
		if errorCategory == "" {
			return fmt.Errorf("%w: error_category", ErrMissingTelemetryField)
		}
	default:
		if errorCategory != "" {
			return fmt.Errorf("%w: error_category", ErrUnexpectedTelemetryField)
		}
	}

	if eventType == TelemetryEventSourceChange && sourceType == "" {
		return fmt.Errorf("%w: source_type", ErrMissingTelemetryField)
	}
	if eventType == TelemetryEventQualityChange && renditionLabel == "" {
		return fmt.Errorf("%w: rendition_label", ErrMissingTelemetryField)
	}

	if input.DroppedFrames != nil || input.TotalFrames != nil {
		if eventType != TelemetryEventEnd && eventType != TelemetryEventTerminalError {
			return fmt.Errorf("%w: frame totals", ErrUnexpectedTelemetryField)
		}
		if input.DroppedFrames == nil || input.TotalFrames == nil {
			return fmt.Errorf("%w: frame totals", ErrMissingTelemetryField)
		}
	}

	if input.RebufferCount != nil || input.RebufferDurationMs != nil || input.MaxRebufferDurationMs != nil {
		if eventType != TelemetryEventEnd && eventType != TelemetryEventTerminalError {
			return fmt.Errorf("%w: rebuffer summary", ErrUnexpectedTelemetryField)
		}
		if input.RebufferCount == nil || input.RebufferDurationMs == nil || input.MaxRebufferDurationMs == nil {
			return fmt.Errorf("%w: rebuffer summary", ErrMissingTelemetryField)
		}
	}

	if input.StartupRetryCount != nil {
		switch eventType {
		case TelemetryEventFirstRenderedFrame, TelemetryEventPlaySuccess, TelemetryEventPlayFailure:
		default:
			return fmt.Errorf("%w: startup_retry_count", ErrUnexpectedTelemetryField)
		}
	}
	return nil
}

func ValidTelemetryEventType(value TelemetryEventType) bool {
	switch value {
	case TelemetryEventLoadStart,
		TelemetryEventMetadataReady,
		TelemetryEventFirstRenderedFrame,
		TelemetryEventPlaySuccess,
		TelemetryEventPlayFailure,
		TelemetryEventRebufferStart,
		TelemetryEventRebufferEnd,
		TelemetryEventSeekStart,
		TelemetryEventSeekEnd,
		TelemetryEventSourceChange,
		TelemetryEventQualityChange,
		TelemetryEventPause,
		TelemetryEventEnd,
		TelemetryEventTerminalError:
		return true
	default:
		return false
	}
}

func ValidTelemetryPlayerAdapter(value TelemetryPlayerAdapter) bool {
	switch value {
	case TelemetryPlayerAdapterNativeMP4, TelemetryPlayerAdapterDASH, TelemetryPlayerAdapterUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetrySourceType(value TelemetrySourceType) bool {
	switch value {
	case TelemetrySourceMP4, TelemetrySourceDASH, TelemetrySourceUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryCodecFamily(value TelemetryCodecFamily) bool {
	switch value {
	case TelemetryCodecH264, TelemetryCodecH265, TelemetryCodecVP8, TelemetryCodecVP9, TelemetryCodecAV1, TelemetryCodecOther, TelemetryCodecUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryNetworkClass(value TelemetryNetworkClass) bool {
	switch value {
	case TelemetryNetworkOffline, TelemetryNetworkSlow2G, TelemetryNetwork2G, TelemetryNetwork3G, TelemetryNetwork4G, TelemetryNetwork5G, TelemetryNetworkWiFi, TelemetryNetworkEthernet, TelemetryNetworkUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryBrowserFamily(value TelemetryBrowserFamily) bool {
	switch value {
	case TelemetryBrowserChrome, TelemetryBrowserEdge, TelemetryBrowserFirefox, TelemetryBrowserSafari, TelemetryBrowserOther, TelemetryBrowserUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryOSFamily(value TelemetryOSFamily) bool {
	switch value {
	case TelemetryOSWindows, TelemetryOSMacOS, TelemetryOSIOS, TelemetryOSAndroid, TelemetryOSLinux, TelemetryOSChromeOS, TelemetryOSOther, TelemetryOSUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryViewportClass(value TelemetryViewportClass) bool {
	switch value {
	case TelemetryViewportSmall, TelemetryViewportMedium, TelemetryViewportLarge, TelemetryViewportUnknown:
		return true
	default:
		return false
	}
}

func ValidTelemetryMeasurementMethod(value TelemetryMeasurementMethod) bool {
	switch value {
	case TelemetryMeasurementVideoFrameCallback, TelemetryMeasurementAdvancingTime, TelemetryMeasurementPlaying:
		return true
	default:
		return false
	}
}

func ValidTelemetryRecoveryOutcome(value TelemetryRecoveryOutcome) bool {
	switch value {
	case TelemetryRecoveryResumed, TelemetryRecoveryPaused, TelemetryRecoverySeeked, TelemetryRecoverySourceChanged, TelemetryRecoveryEnded, TelemetryRecoveryFailed:
		return true
	default:
		return false
	}
}

func ValidTelemetryErrorCategory(value TelemetryErrorCategory) bool {
	switch value {
	case TelemetryErrorAborted, TelemetryErrorNetwork, TelemetryErrorDecode, TelemetryErrorUnsupported, TelemetryErrorAutoplay, TelemetryErrorTimeout, TelemetryErrorUnknown:
		return true
	default:
		return false
	}
}

func NormalizeTelemetryScene(value string) string {
	switch normalizeTelemetryToken(value, "unknown") {
	case "timeline", "recommend", "following", "hot", "profile", "detail":
		return normalizeTelemetryToken(value, "unknown")
	default:
		return "unknown"
	}
}

func NormalizeTelemetryPlayerAdapter(value string) TelemetryPlayerAdapter {
	switch compactTelemetryToken(value) {
	case "nativemp4", "native", "html5", "video":
		return TelemetryPlayerAdapterNativeMP4
	case "dash", "dashjs":
		return TelemetryPlayerAdapterDASH
	default:
		return TelemetryPlayerAdapterUnknown
	}
}

func NormalizeTelemetrySourceType(value string) TelemetrySourceType {
	switch compactTelemetryToken(value) {
	case "mp4", "progressive", "progressivemp4":
		return TelemetrySourceMP4
	case "dash", "mpd", "manifest":
		return TelemetrySourceDASH
	default:
		return TelemetrySourceUnknown
	}
}

func NormalizeTelemetryCodecFamily(value string) TelemetryCodecFamily {
	token := compactTelemetryToken(value)
	switch {
	case strings.HasPrefix(token, "avc1"), strings.HasPrefix(token, "avc3"), token == "h264":
		return TelemetryCodecH264
	case strings.HasPrefix(token, "hev1"), strings.HasPrefix(token, "hvc1"), token == "h265", token == "hevc":
		return TelemetryCodecH265
	case token == "vp8", strings.HasPrefix(token, "vp08"):
		return TelemetryCodecVP8
	case token == "vp9", strings.HasPrefix(token, "vp09"):
		return TelemetryCodecVP9
	case token == "av1", strings.HasPrefix(token, "av01"):
		return TelemetryCodecAV1
	case token == "", token == "unknown":
		return TelemetryCodecUnknown
	default:
		return TelemetryCodecOther
	}
}

func NormalizeTelemetryNetworkClass(value string) TelemetryNetworkClass {
	switch compactTelemetryToken(value) {
	case "offline":
		return TelemetryNetworkOffline
	case "slow2g":
		return TelemetryNetworkSlow2G
	case "2g":
		return TelemetryNetwork2G
	case "3g":
		return TelemetryNetwork3G
	case "4g":
		return TelemetryNetwork4G
	case "5g":
		return TelemetryNetwork5G
	case "wifi", "wlan":
		return TelemetryNetworkWiFi
	case "ethernet", "wired":
		return TelemetryNetworkEthernet
	default:
		return TelemetryNetworkUnknown
	}
}

func NormalizeTelemetryBrowserFamily(value string) TelemetryBrowserFamily {
	switch compactTelemetryToken(value) {
	case "chrome", "chromium", "chromemobile":
		return TelemetryBrowserChrome
	case "edge", "edg":
		return TelemetryBrowserEdge
	case "firefox", "firefoxmobile":
		return TelemetryBrowserFirefox
	case "safari", "mobilesafari":
		return TelemetryBrowserSafari
	case "", "unknown":
		return TelemetryBrowserUnknown
	default:
		return TelemetryBrowserOther
	}
}

func NormalizeTelemetryOSFamily(value string) TelemetryOSFamily {
	switch compactTelemetryToken(value) {
	case "windows", "win":
		return TelemetryOSWindows
	case "macos", "macosx", "osx":
		return TelemetryOSMacOS
	case "ios", "iphone", "ipad":
		return TelemetryOSIOS
	case "android":
		return TelemetryOSAndroid
	case "linux":
		return TelemetryOSLinux
	case "chromeos", "cros":
		return TelemetryOSChromeOS
	case "", "unknown":
		return TelemetryOSUnknown
	default:
		return TelemetryOSOther
	}
}

func NormalizeTelemetryViewportClass(value string) TelemetryViewportClass {
	switch compactTelemetryToken(value) {
	case "small", "mobile", "compact":
		return TelemetryViewportSmall
	case "medium", "tablet":
		return TelemetryViewportMedium
	case "large", "desktop", "wide":
		return TelemetryViewportLarge
	default:
		return TelemetryViewportUnknown
	}
}

func NormalizeTelemetryRenditionLabel(value string) string {
	switch compactTelemetryToken(value) {
	case "auto":
		return "auto"
	case "source", "original":
		return "source"
	case "144p", "240p", "360p", "480p", "720p", "1080p", "1440p", "2160p":
		return compactTelemetryToken(value)
	case "", "unknown":
		return "unknown"
	default:
		return "other"
	}
}

func NormalizeTelemetryCDNHost(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" {
		return "", nil
	}
	if len(value) > MaxTelemetryCDNHostLength ||
		strings.ContainsAny(value, "/?#@:\\") ||
		strings.ContainsFunc(value, unicode.IsSpace) {
		return "", fmt.Errorf("%w: cdn_host", ErrInvalidTelemetryDimension)
	}
	if net.ParseIP(value) != nil {
		return value, nil
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%w: cdn_host", ErrInvalidTelemetryDimension)
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') &&
				character != '-' {
				return "", fmt.Errorf("%w: cdn_host", ErrInvalidTelemetryDimension)
			}
		}
	}
	return value, nil
}

func normalizeTelemetryToken(value string, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func compactTelemetryToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(value)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
