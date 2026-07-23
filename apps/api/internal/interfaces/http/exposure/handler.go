package interfaceshttpexposure

import (
	applicationexposure "GCFeed/internal/application/exposure"
	domainexposure "GCFeed/internal/domain/exposure"
	interfaceshttpbinding "GCFeed/internal/interfaces/http/binding"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
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
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}

	var req createViewEventRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	result, err := h.service.RecordViewEvent(
		ctx,
		userID,
		req.VideoID,
		req.Scene,
		req.RequestID,
		req.EventType,
		req.WatchMs,
		req.Completed,
	)
	if err != nil {
		writeExposureError(c, err)
		return
	}

	c.JSON(http.StatusCreated, responseFromResult(result))
}

func responseFromResult(result *applicationexposure.RecordViewEventResult) createViewEventResponse {
	response := createViewEventResponse{
		Event: viewEventResponse{
			ID:        result.Event.ID,
			UserID:    result.Event.UserID,
			VideoID:   result.Event.VideoID,
			Scene:     result.Event.Scene,
			RequestID: result.Event.RequestID,
			EventType: result.Event.EventType,
			WatchMs:   result.Event.WatchMs,
			Completed: result.Event.Completed,
			CreatedAt: result.Event.CreatedAt,
		},
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
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if errors.Is(err, domainexposure.ErrVideoNotFound) {
		c.JSON(http.StatusNotFound, utils.H{"error": "video not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, utils.H{"error": "internal server error"})
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainexposure.ErrInvalidUserID) ||
		errors.Is(err, domainexposure.ErrInvalidVideoID) ||
		errors.Is(err, domainexposure.ErrEmptyScene) ||
		errors.Is(err, domainexposure.ErrSceneTooLong) ||
		errors.Is(err, domainexposure.ErrInvalidEventType) ||
		errors.Is(err, domainexposure.ErrRequestIDTooLong) ||
		errors.Is(err, domainexposure.ErrWatchMsNegative)
}
