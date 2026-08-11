package interfaceshttpkafkafailure

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationkafkafailure "github.com/shiyudesu/frux/internal/application/kafkafailure"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxReplayBodyBytes = 1024

type Service interface {
	List(ctx context.Context) ([]domainkafkafailure.TopicSummary, error)
	Inspect(
		ctx context.Context,
		topic string,
		partition int32,
		offset int64,
		limit int,
	) ([]domainkafkafailure.RecordDiagnostic, error)
	Replay(
		ctx context.Context,
		request applicationkafkafailure.ReplayRequest,
	) (*domainkafkafailure.ReplayResult, error)
}

type Handler struct {
	service Service
}

type replayRequest struct {
	Reason string `json:"reason"`
}

type partitionSummaryResponse struct {
	Partition           int32     `json:"partition"`
	RetainedStartOffset int64     `json:"retained_start_offset"`
	EndOffset           int64     `json:"end_offset"`
	RetainedEstimate    int64     `json:"retained_estimate"`
	EndOffsetGrowth     int64     `json:"end_offset_growth"`
	RecentIngress       int64     `json:"recent_ingress"`
	OldestRecordAt      time.Time `json:"oldest_record_at,omitempty"`
	OldestAgeSeconds    float64   `json:"oldest_age_seconds"`
}

type topicSummaryResponse struct {
	Topic            string                     `json:"topic"`
	ConsumerGroup    string                     `json:"consumer_group"`
	RetentionSeconds float64                    `json:"retention_seconds"`
	PartitionCount   int                        `json:"partition_count"`
	RetainedEstimate int64                      `json:"retained_estimate"`
	EndOffset        int64                      `json:"end_offset"`
	EndOffsetGrowth  int64                      `json:"end_offset_growth"`
	RecentIngress    int64                      `json:"recent_ingress"`
	OldestRecordAt   time.Time                  `json:"oldest_record_at,omitempty"`
	OldestAgeSeconds float64                    `json:"oldest_age_seconds"`
	Partitions       []partitionSummaryResponse `json:"partitions"`
}

type recordResponse struct {
	Topic             string    `json:"topic"`
	Partition         int32     `json:"partition"`
	Offset            int64     `json:"offset"`
	Timestamp         time.Time `json:"timestamp"`
	SourceTopic       string    `json:"source_topic"`
	SourcePartition   int32     `json:"source_partition"`
	SourceOffset      int64     `json:"source_offset"`
	ConsumerGroup     string    `json:"consumer_group"`
	EventID           string    `json:"event_id"`
	ReplayID          string    `json:"replay_id,omitempty"`
	SchemaVersion     int       `json:"schema_version"`
	FailureClass      string    `json:"failure_class"`
	Attempt           int       `json:"attempt"`
	FirstFailureAt    time.Time `json:"first_failure_at"`
	LatestFailureAt   time.Time `json:"latest_failure_at"`
	NotBefore         time.Time `json:"not_before"`
	ConsumedTopic     string    `json:"consumed_topic,omitempty"`
	ConsumedPartition int32     `json:"consumed_partition,omitempty"`
	ConsumedOffset    int64     `json:"consumed_offset,omitempty"`
	MetadataCode      string    `json:"metadata_code,omitempty"`
	Replayable        bool      `json:"replayable"`
	KeyBytes          int       `json:"key_bytes"`
	KeySHA256         string    `json:"key_sha256"`
	PayloadBytes      int       `json:"payload_bytes"`
	PayloadSHA256     string    `json:"payload_sha256"`
	ContentType       string    `json:"content_type"`
	JSONValid         bool      `json:"json_valid"`
	JSONFields        []string  `json:"json_fields"`
}

type replayResponse struct {
	Topic           string    `json:"topic"`
	Partition       int32     `json:"partition"`
	Offset          int64     `json:"offset"`
	SourceTopic     string    `json:"source_topic"`
	SourcePartition int32     `json:"source_partition"`
	SourceOffset    int64     `json:"source_offset"`
	ConsumerGroup   string    `json:"consumer_group"`
	ReplayID        string    `json:"replay_id"`
	Reason          string    `json:"reason"`
	Status          string    `json:"status"`
	CompletedAt     time.Time `json:"completed_at"`
	Duplicate       bool      `json:"duplicate"`
	Reconciled      bool      `json:"reconciled"`
}

func New(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(ctx context.Context, c *app.RequestContext) {
	if h == nil || h.service == nil {
		writeError(c, domainkafkafailure.ErrInspectionUnavailable)
		return
	}
	items, err := h.service.List(ctx)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]topicSummaryResponse, 0, len(items))
	for _, item := range items {
		summary := topicSummaryResponse{
			Topic:            item.Topic,
			ConsumerGroup:    item.ConsumerGroup,
			RetentionSeconds: item.Retention.Seconds(),
			PartitionCount:   item.PartitionCount,
			RetainedEstimate: item.RetainedEstimate,
			EndOffset:        item.EndOffset,
			EndOffsetGrowth:  item.EndOffsetGrowth,
			RecentIngress:    item.RecentIngress,
			OldestRecordAt:   item.OldestRecordAt,
			OldestAgeSeconds: item.OldestAge.Seconds(),
			Partitions:       make([]partitionSummaryResponse, 0, len(item.Partitions)),
		}
		for _, partition := range item.Partitions {
			summary.Partitions = append(summary.Partitions, partitionSummaryResponse{
				Partition:           partition.Partition,
				RetainedStartOffset: partition.RetainedStartOffset,
				EndOffset:           partition.EndOffset,
				RetainedEstimate:    partition.RetainedEstimate,
				EndOffsetGrowth:     partition.EndOffsetGrowth,
				RecentIngress:       partition.RecentIngress,
				OldestRecordAt:      partition.OldestRecordAt,
				OldestAgeSeconds:    partition.OldestAge.Seconds(),
			})
		}
		response = append(response, summary)
	}
	c.JSON(http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) Inspect(ctx context.Context, c *app.RequestContext) {
	partition, offset, limit, err := parseCoordinates(c)
	if err != nil {
		writeError(c, err)
		return
	}
	items, err := h.service.Inspect(
		ctx, c.Param("topic"), partition, offset, limit,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]recordResponse, 0, len(items))
	for _, item := range items {
		response = append(response, recordResponse{
			Topic:             item.Coordinate.Topic,
			Partition:         item.Coordinate.Partition,
			Offset:            item.Coordinate.Offset,
			Timestamp:         item.Timestamp,
			SourceTopic:       item.SourceTopic,
			SourcePartition:   item.SourcePartition,
			SourceOffset:      item.SourceOffset,
			ConsumerGroup:     item.ConsumerGroup,
			EventID:           item.EventID,
			ReplayID:          item.ReplayID,
			SchemaVersion:     item.SchemaVersion,
			FailureClass:      item.FailureClass,
			Attempt:           item.Attempt,
			FirstFailureAt:    item.FirstFailureAt,
			LatestFailureAt:   item.LatestFailureAt,
			NotBefore:         item.NotBefore,
			ConsumedTopic:     item.ConsumedTopic,
			ConsumedPartition: item.ConsumedPartition,
			ConsumedOffset:    item.ConsumedOffset,
			MetadataCode:      item.MetadataCode,
			Replayable:        item.Replayable,
			KeyBytes:          item.KeyBytes,
			KeySHA256:         item.KeySHA256,
			PayloadBytes:      item.PayloadBytes,
			PayloadSHA256:     item.PayloadSHA256,
			ContentType:       item.ContentType,
			JSONValid:         item.JSONValid,
			JSONFields:        append([]string(nil), item.JSONFields...),
		})
	}
	c.JSON(http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) Replay(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable", nil,
		)
		return
	}
	partition, err := parseInt32(c.Param("partition"))
	if err != nil {
		writeError(c, err)
		return
	}
	offset, err := parseInt64(c.Param("offset"))
	if err != nil {
		writeError(c, err)
		return
	}
	var request replayRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxReplayBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	idempotencyKey := strings.TrimSpace(string(c.GetHeader("Idempotency-Key")))
	result, err := h.service.Replay(ctx, applicationkafkafailure.ReplayRequest{
		Topic:          c.Param("topic"),
		Partition:      partition,
		Offset:         offset,
		ActorID:        principal.UserID,
		Reason:         request.Reason,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, replayResponse{
		Topic:           result.Coordinate.Topic,
		Partition:       result.Coordinate.Partition,
		Offset:          result.Coordinate.Offset,
		SourceTopic:     result.SourceTopic,
		SourcePartition: result.SourcePartition,
		SourceOffset:    result.SourceOffset,
		ConsumerGroup:   result.ConsumerGroup,
		ReplayID:        result.ReplayID,
		Reason:          string(result.Reason),
		Status:          string(result.Status),
		CompletedAt:     result.CompletedAt,
		Duplicate:       result.Duplicate,
		Reconciled:      result.Reconciled,
	})
}

func parseCoordinates(c *app.RequestContext) (int32, int64, int, error) {
	partition, err := parseInt32(string(c.Query("partition")))
	if err != nil {
		return 0, 0, 0, err
	}
	offset, err := parseInt64(string(c.Query("offset")))
	if err != nil {
		return 0, 0, 0, err
	}
	limit := 0
	rawLimit := strings.TrimSpace(string(c.Query("limit")))
	if rawLimit != "" {
		limit, err = strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, 0, domainkafkafailure.ErrInvalidLimit
		}
	}
	return partition, offset, limit, nil
}

func parseInt32(raw string) (int32, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || value < 0 {
		return 0, domainkafkafailure.ErrInvalidPartition
	}
	return int32(value), nil
}

func parseInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, domainkafkafailure.ErrInvalidOffset
	}
	return value, nil
}

func writeError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainkafkafailure.ErrInvalidTopic),
		errors.Is(err, domainkafkafailure.ErrTopicNotAllowed),
		errors.Is(err, domainkafkafailure.ErrInvalidPartition),
		errors.Is(err, domainkafkafailure.ErrInvalidOffset),
		errors.Is(err, domainkafkafailure.ErrInvalidLimit),
		errors.Is(err, domainkafkafailure.ErrInvalidActor),
		errors.Is(err, domainkafkafailure.ErrInvalidReason),
		errors.Is(err, domainkafkafailure.ErrIdempotencyKeyRequired),
		errors.Is(err, domainkafkafailure.ErrIdempotencyKeyTooLong),
		errors.Is(err, domainkafkafailure.ErrInvalidReplayID):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeKafkaDeadLetterValidationFailed,
			"invalid Kafka dead-letter request",
		)
	case errors.Is(err, domainkafkafailure.ErrRecordNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeKafkaDeadLetterRecordNotFound,
			"Kafka dead-letter record not found",
		)
	case errors.Is(err, domainkafkafailure.ErrRecordExpired):
		interfaceshttpapierror.Write(
			c, http.StatusGone,
			interfaceshttpapierror.CodeKafkaDeadLetterRecordExpired,
			"Kafka dead-letter record expired",
		)
	case errors.Is(err, domainkafkafailure.ErrInvalidProvenance):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeKafkaDeadLetterInvalidProvenance,
			"Kafka dead-letter record provenance is invalid",
		)
	case errors.Is(err, domainkafkafailure.ErrIdempotencyConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeKafkaDeadLetterReplayConflict,
			"Kafka dead-letter replay conflict",
		)
	case errors.Is(err, domainkafkafailure.ErrReplayPending),
		errors.Is(err, domainkafkafailure.ErrReplayEvidenceExpired):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeKafkaDeadLetterReplayConflict,
			"Kafka dead-letter replay outcome requires operator reconciliation",
		)
	case errors.Is(err, domainkafkafailure.ErrReplayPublicationAbsent):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeKafkaDeadLetterReplayConflict,
			"Kafka dead-letter replay publication was not found",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeKafkaDeadLetterUnavailable,
			"Kafka dead-letter recovery unavailable", err,
		)
	}
}
