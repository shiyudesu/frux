package interfaceshttpexposure

import (
	"context"
	"errors"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service *applicationexposure.Service
}

// New 注入曝光应用服务。
func New(service *applicationexposure.Service) *Handler {
	return &Handler{service: service}
}

// CreateViewEvent 处理视频曝光和观看行为上报。
func (h *Handler) CreateViewEvent(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	var req createViewEventRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	occurredAt := time.Time{}
	if req.OccurredAt != nil {
		occurredAt = *req.OccurredAt
	}
	result, err := h.service.RecordViewEvent(ctx, domainexposure.NewViewEventInput{
		UserID: userID, VideoID: req.VideoID, Scene: req.Scene, RequestID: req.RequestID,
		EventType: req.EventType, EventID: req.EventID, PlaybackSessionID: req.PlaybackSessionID,
		Sequence: req.Sequence, OccurredAt: occurredAt, PositionMs: req.PositionMs,
		WatchMs: req.WatchMs, DurationMs: req.DurationMs, Completed: req.Completed,
	})
	if err != nil {
		writeExposureError(c, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, responseFromResult(result))
}

func responseFromResult(result *applicationexposure.RecordViewEventResult) createViewEventResponse {
	response := createViewEventResponse{
		Event: viewEventResponse{
			ID: result.Event.ID, UserID: result.Event.UserID, VideoID: result.Event.VideoID,
			Scene: result.Event.Scene, RequestID: result.Event.RequestID, EventType: result.Event.EventType,
			EventID: result.Event.EventID, PlaybackSessionID: result.Event.PlaybackSessionID,
			Sequence: result.Event.Sequence, OccurredAt: result.Event.OccurredAt,
			PositionMs: result.Event.PositionMs, WatchMs: result.Event.WatchMs,
			DurationMs: result.Event.DurationMs, Completed: result.Event.Completed,
			CreatedAt: result.Event.CreatedAt,
		},
		Replayed: result.Replayed,
	}
	if result.Exposure != nil {
		response.Exposure = &exposureResponse{
			UserID:         result.Exposure.UserID,
			VideoID:        result.Exposure.VideoID,
			FirstExposedAt: result.Exposure.FirstExposedAt,
			LastExposedAt:  result.Exposure.LastExposedAt,
			ExposureCount:  result.Exposure.ExposureCount,
			LastScene:      result.Exposure.LastScene,
		}
	}
	return response
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func writeExposureError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeExposureValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainexposure.ErrVideoNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeExposureVideoNotFound, "video not found")
		return
	}
	if errors.Is(err, domainexposure.ErrEventIDConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeExposureEventConflict, err.Error())
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainexposure.ErrInvalidUserID) ||
		errors.Is(err, domainexposure.ErrInvalidVideoID) ||
		errors.Is(err, domainexposure.ErrEmptyScene) ||
		errors.Is(err, domainexposure.ErrSceneTooLong) ||
		errors.Is(err, domainexposure.ErrInvalidEventType) ||
		errors.Is(err, domainexposure.ErrRequestIDTooLong) ||
		errors.Is(err, domainexposure.ErrEmptyEventID) ||
		errors.Is(err, domainexposure.ErrEventIDTooLong) ||
		errors.Is(err, domainexposure.ErrEmptyPlaybackSessionID) ||
		errors.Is(err, domainexposure.ErrPlaybackSessionIDTooLong) ||
		errors.Is(err, domainexposure.ErrInvalidSequence) ||
		errors.Is(err, domainexposure.ErrEmptyOccurredAt) ||
		errors.Is(err, domainexposure.ErrOccurredAtOutOfRange) ||
		errors.Is(err, domainexposure.ErrPositionMsNegative) ||
		errors.Is(err, domainexposure.ErrWatchMsNegative) ||
		errors.Is(err, domainexposure.ErrInvalidDurationMs)
}
