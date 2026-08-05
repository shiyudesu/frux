package interfaceshttpupload

import (
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

type CreateUploadSessionRequest struct {
	Kind           string `json:"kind"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type uploadRequestResponse struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type uploadSessionResponse struct {
	Mode             string                 `json:"mode"`
	ID               string                 `json:"id,omitempty"`
	Kind             string                 `json:"kind,omitempty"`
	State            string                 `json:"state,omitempty"`
	ObjectKey        string                 `json:"object_key,omitempty"`
	ExpiresAt        *time.Time             `json:"expires_at,omitempty"`
	Upload           *uploadRequestResponse `json:"upload,omitempty"`
	CompletedAssetID int64                  `json:"completed_asset_id,omitempty"`
	Replayed         bool                   `json:"replayed,omitempty"`
}

type completedAssetResponse struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	StorageBackend string `json:"storage_backend"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type completeUploadSessionResponse struct {
	SessionID string                 `json:"session_id"`
	State     string                 `json:"state"`
	Asset     completedAssetResponse `json:"asset"`
	Replayed  bool                   `json:"replayed,omitempty"`
}

type protectedAssetAccessResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SessionHandler struct {
	service *applicationmedia.Service
}

func NewSessionHandler(service *applicationmedia.Service) *SessionHandler {
	return &SessionHandler{service: service}
}

func (h *SessionHandler) Create(ctx context.Context, c *app.RequestContext) {
	userID, ok := uploadUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}
	var request CreateUploadSessionRequest
	if err := interfaceshttpbinding.BindJSON(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}
	result, err := h.service.CreateUploadSession(ctx, applicationmedia.CreateUploadSessionInput{
		OwnerID: userID, Kind: request.Kind, Filename: request.Filename,
		ContentType: request.ContentType, SizeBytes: request.SizeBytes,
		ChecksumSHA256: request.ChecksumSHA256,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	})
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	response := uploadSessionResponse{
		Mode: result.Mode, CompletedAssetID: result.CompletedAssetID, Replayed: result.Replayed,
	}
	if result.Session != nil {
		response.ID = result.Session.ID
		response.Kind = result.Session.Kind
		response.State = result.Session.State
		response.ObjectKey = result.Session.ObjectKey
		response.ExpiresAt = &result.Session.ExpiresAt
	}
	if result.UploadRequest != nil {
		response.Upload = &uploadRequestResponse{
			URL: result.UploadRequest.URL, Method: result.UploadRequest.Method,
			Headers: result.UploadRequest.Headers, ExpiresAt: result.UploadRequest.ExpiresAt,
		}
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, response)
}

func (h *SessionHandler) Complete(ctx context.Context, c *app.RequestContext) {
	userID, ok := uploadUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}
	result, err := h.service.CompleteUploadSession(ctx, userID, c.Param("sessionId"))
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, completeUploadSessionResponse{
		SessionID: result.Session.ID, State: result.Session.State, Replayed: result.Replayed,
		Asset: completedAssetResponse{
			ID: result.Asset.ID, Kind: result.Asset.Kind, State: result.Asset.State,
			StorageBackend: result.Asset.StorageBackend, ContentType: result.Asset.ContentType,
			SizeBytes: result.Asset.SizeBytes, ChecksumSHA256: result.Asset.ChecksumSHA256,
		},
	})
}

func (h *SessionHandler) Access(ctx context.Context, c *app.RequestContext) {
	userID, ok := uploadUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}
	assetID, err := strconv.ParseInt(strings.TrimSpace(c.Param("assetId")), 10, 64)
	if err != nil || assetID <= 0 {
		writeUploadSessionError(c, domainmedia.ErrInvalidAssetID)
		return
	}
	access, err := h.service.GetProtectedAssetAccess(ctx, userID, assetID)
	if err != nil {
		writeUploadSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, protectedAssetAccessResponse{URL: access.URL, ExpiresAt: access.ExpiresAt})
}

func writeUploadSessionError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainmedia.ErrInvalidOwnerID),
		errors.Is(err, domainmedia.ErrInvalidAssetID),
		errors.Is(err, domainmedia.ErrInvalidAssetKind),
		errors.Is(err, domainmedia.ErrInvalidObjectKey),
		errors.Is(err, domainmedia.ErrInvalidContentType),
		errors.Is(err, domainmedia.ErrInvalidSize),
		errors.Is(err, domainmedia.ErrInvalidChecksum),
		errors.Is(err, domainmedia.ErrInvalidUploadSession):
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
	case errors.Is(err, domainmedia.ErrUploadSessionNotFound):
		c.JSON(http.StatusNotFound, utils.H{"error": err.Error()})
	case errors.Is(err, domainmedia.ErrMediaAssetNotFound):
		c.JSON(http.StatusNotFound, utils.H{"error": err.Error()})
	case errors.Is(err, domainmedia.ErrMediaAssetPermissionDenied):
		c.JSON(http.StatusForbidden, utils.H{"error": err.Error()})
	case errors.Is(err, domainmedia.ErrUploadSessionExpired),
		errors.Is(err, domainmedia.ErrUploadSessionConflict),
		errors.Is(err, domainmedia.ErrUploadSessionCompleted),
		errors.Is(err, applicationmedia.ErrUploadObjectMismatch):
		c.JSON(http.StatusConflict, utils.H{"error": err.Error()})
	case errors.Is(err, applicationmedia.ErrDirectUploadUnavailable),
		errors.Is(err, applicationmedia.ErrDispatchProcessingFailed):
		c.JSON(http.StatusServiceUnavailable, utils.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, utils.H{"error": "media upload failed"})
	}
}
