package interfaceshttprecommendation

import (
	"context"
	"errors"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxFeedbackBodyBytes = 4 * 1024

type Handler struct {
	service *applicationrecommendation.Service
}

func New(service *applicationrecommendation.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListCandidates(ctx context.Context, c *app.RequestContext) {
	var req candidateRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	limit := 0
	if req.Limit != nil {
		limit = *req.Limit
	}
	result, err := h.service.Recommend(ctx, applicationrecommendation.CandidateRequest{
		UserID:    req.UserID,
		Scene:     req.Scene,
		RequestID: req.RequestID,
		Cursor:    req.Cursor,
		Limit:     limit,
	})
	if err != nil {
		writeRecommendationError(c, err)
		return
	}

	c.JSON(http.StatusOK, candidateResponseFromResult(result))
}

func (h *Handler) SaveExposures(ctx context.Context, c *app.RequestContext) {
	var req exposuresRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	inputs := make([]applicationrecommendation.ExposureInput, 0, len(req.VideoIDs))
	for _, videoID := range req.VideoIDs {
		inputs = append(inputs, applicationrecommendation.ExposureInput{
			UserID:    req.UserID,
			VideoID:   videoID,
			Scene:     req.Scene,
			RequestID: req.RequestID,
		})
	}
	result, err := h.service.SaveExposures(ctx, inputs)
	if err != nil {
		writeRecommendationError(c, err)
		return
	}

	c.JSON(http.StatusCreated, exposuresResponseFromResult(result))
}

func (h *Handler) DecideExposures(ctx context.Context, c *app.RequestContext) {
	var req exposureDecisionsRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	result, err := h.service.DecideExposures(ctx, applicationrecommendation.ExposureDecisionInput{
		UserID:    req.UserID,
		Scene:     req.Scene,
		RequestID: req.RequestID,
		VideoIDs:  req.VideoIDs,
	})
	if err != nil {
		writeRecommendationError(c, err)
		return
	}

	c.JSON(http.StatusOK, exposureDecisionsResponseFromResult(result))
}

func (h *Handler) CreateFeedback(ctx context.Context, c *app.RequestContext) {
	userID, ok := recommendationUserIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	var req feedbackRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &req, maxFeedbackBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.SubmitFeedback(ctx, applicationrecommendation.FeedbackInput{
		UserID:         userID,
		VideoID:        req.VideoID,
		RequestID:      req.RequestID,
		FeedbackType:   req.FeedbackType,
		IdempotencyKey: string(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeRecommendationError(c, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, feedbackResponse{
		ID:           result.Feedback.ID,
		VideoID:      result.Feedback.VideoID,
		RequestID:    result.Feedback.RequestID,
		FeedbackType: result.Feedback.FeedbackType,
		CreatedAt:    result.Feedback.CreatedAt,
		Replayed:     result.Replayed,
	})
}

func candidateResponseFromResult(result *applicationrecommendation.CandidateResult) candidateResponse {
	items := make([]candidateItemResponse, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		items = append(items, candidateItemResponse{
			VideoID:        candidate.VideoID,
			AuthorID:       candidate.AuthorID,
			RankScore:      candidate.RankScore,
			Similarity:     candidate.Similarity,
			HotScore:       candidate.HotScore,
			FreshnessScore: candidate.FreshnessScore,
			Reason:         candidate.Reason,
			PublishedAt:    candidate.PublishedAt,
		})
	}
	return candidateResponse{
		UserID:     result.UserID,
		Scene:      result.Scene,
		RequestID:  result.RequestID,
		Candidates: items,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}
}

func exposuresResponseFromResult(result *applicationrecommendation.ExposureResult) exposuresResponse {
	items := make([]exposureItemResponse, 0, len(result.Exposures))
	for _, exposure := range result.Exposures {
		items = append(items, exposureItemResponse{
			UserID:         exposure.UserID,
			VideoID:        exposure.VideoID,
			FirstExposedAt: exposure.FirstExposedAt,
			LastExposedAt:  exposure.LastExposedAt,
			ExposureCount:  exposure.ExposureCount,
			LastScene:      exposure.LastScene,
		})
	}
	return exposuresResponse{Exposures: items}
}

func exposureDecisionsResponseFromResult(result *applicationrecommendation.ExposureDecisionResult) exposureDecisionsResponse {
	items := make([]exposureDecisionItemResponse, 0, len(result.Decisions))
	for _, decision := range result.Decisions {
		items = append(items, exposureDecisionItemResponse{
			VideoID:       decision.VideoID,
			Allowed:       decision.Allowed,
			Reason:        decision.Reason,
			LastExposedAt: decision.LastExposedAt,
		})
	}
	return exposureDecisionsResponse{
		UserID:    result.UserID,
		Scene:     result.Scene,
		RequestID: result.RequestID,
		Decisions: items,
	}
}

func writeRecommendationError(c *app.RequestContext, err error) {
	if errors.Is(err, domainrecommendation.ErrFeedbackIdempotencyConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeRecommendationFeedbackConflict, err.Error())
		return
	}
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeRecommendationValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainrecommendation.ErrVideoNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeRecommendationVideoNotFound, "video not found")
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainrecommendation.ErrInvalidUserID) ||
		errors.Is(err, domainrecommendation.ErrInvalidVideoID) ||
		errors.Is(err, domainrecommendation.ErrInvalidLimit) ||
		errors.Is(err, domainrecommendation.ErrEmptyScene) ||
		errors.Is(err, domainrecommendation.ErrEmptyRequestID) ||
		errors.Is(err, domainrecommendation.ErrSceneTooLong) ||
		errors.Is(err, domainrecommendation.ErrRequestIDTooLong) ||
		errors.Is(err, domainrecommendation.ErrSessionIDTooLong) ||
		errors.Is(err, domainrecommendation.ErrInvalidRefreshIndex) ||
		errors.Is(err, domainrecommendation.ErrTooManyRecentVideoIDs) ||
		errors.Is(err, domainrecommendation.ErrInvalidNetworkClass) ||
		errors.Is(err, domainrecommendation.ErrInvalidViewportClass) ||
		errors.Is(err, domainrecommendation.ErrTooManyPlaybackCapabilities) ||
		errors.Is(err, domainrecommendation.ErrInvalidPlaybackCapability) ||
		errors.Is(err, domainrecommendation.ErrInvalidFeedbackType) ||
		errors.Is(err, domainrecommendation.ErrFeedbackRequestMismatch) ||
		errors.Is(err, domainrecommendation.ErrIdempotencyKeyRequired) ||
		errors.Is(err, domainrecommendation.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainrecommendation.ErrInvalidCursor)
}

func recommendationUserIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}
