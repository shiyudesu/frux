package infraacceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type APIClient struct {
	http *HTTPClient
	base string
}

type CreatedAsset struct {
	ID   int64
	Kind string
}

type CreatedVideo struct {
	ID           int64
	MediaAssetID int64
	CoverAssetID int64
}

type ReviewLease struct {
	LeaseToken string
	Version    int
}

type SimilarResult struct {
	Available bool
	VideoIDs  []int64
}

func NewAPIClient(client *HTTPClient, baseEndpoint string) (*APIClient, error) {
	baseEndpoint = strings.TrimRight(strings.TrimSpace(baseEndpoint), "/")
	if client == nil || validateEndpoint(baseEndpoint) != nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	return &APIClient{http: client, base: baseEndpoint}, nil
}

func (c *APIClient) Login(ctx context.Context, admin bool, account, password string) (string, error) {
	path := "/api/sessions"
	if admin {
		path = "/api/admin/auth/login"
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	err := c.http.JSON(ctx, http.MethodPost, c.base+path, "", map[string]string{
		"account": account, "password": password,
	}, &response)
	if err != nil || strings.TrimSpace(response.AccessToken) == "" {
		if err != nil {
			return "", err
		}
		return "", &HTTPError{Code: HTTPFailureDecode}
	}
	return response.AccessToken, nil
}

func (c *APIClient) UploadFixture(
	ctx context.Context,
	bearer string,
	kind string,
	path string,
	idempotencyKey string,
) (CreatedAsset, error) {
	file, err := os.Open(path)
	if err != nil {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureRequest}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureRequest}
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureRead}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureRead}
	}
	contentType := fixtureContentType(path)
	var session struct {
		Mode   string `json:"mode"`
		ID     string `json:"id"`
		Upload struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
	}
	err = c.http.JSONHeaders(
		ctx, http.MethodPost, c.base+"/api/upload-sessions", bearer,
		map[string]string{"Idempotency-Key": idempotencyKey},
		map[string]any{
			"kind": kind, "filename": filepath.Base(path), "content_type": contentType,
			"size_bytes": info.Size(), "checksum_sha256": hex.EncodeToString(hasher.Sum(nil)),
		},
		&session,
	)
	if err != nil {
		return CreatedAsset{}, err
	}
	if session.Mode != "direct" || session.ID == "" || session.Upload.URL == "" ||
		strings.ToUpper(session.Upload.Method) != http.MethodPut {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureDecode}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, session.Upload.URL, file)
	if err != nil {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureRequest}
	}
	for name, value := range session.Upload.Headers {
		request.Header.Set(name, value)
	}
	if _, err := c.http.do(request); err != nil {
		return CreatedAsset{}, err
	}
	var completed struct {
		Asset struct {
			ID   int64  `json:"id"`
			Kind string `json:"kind"`
		} `json:"asset"`
	}
	if err := c.http.JSON(
		ctx, http.MethodPost, c.base+"/api/upload-sessions/"+url.PathEscape(session.ID)+"/complete",
		bearer, nil, &completed,
	); err != nil {
		return CreatedAsset{}, err
	}
	if completed.Asset.ID <= 0 || completed.Asset.Kind != kind {
		return CreatedAsset{}, &HTTPError{Code: HTTPFailureDecode}
	}
	return CreatedAsset{ID: completed.Asset.ID, Kind: completed.Asset.Kind}, nil
}

func (c *APIClient) CreateVideo(
	ctx context.Context,
	bearer string,
	mediaAssetID int64,
	coverAssetID int64,
	title string,
	description string,
	idempotencyKey string,
) (CreatedVideo, error) {
	var response struct {
		ID           int64 `json:"id"`
		MediaAssetID int64 `json:"media_asset_id"`
		CoverAssetID int64 `json:"cover_asset_id"`
	}
	err := c.http.JSONHeaders(
		ctx, http.MethodPost, c.base+"/api/videos", bearer,
		map[string]string{"Idempotency-Key": idempotencyKey},
		map[string]any{
			"title": title, "description": description,
			"media_asset_id": mediaAssetID, "cover_asset_id": coverAssetID,
		},
		&response,
	)
	if err != nil {
		return CreatedVideo{}, err
	}
	if response.ID <= 0 || response.MediaAssetID != mediaAssetID || response.CoverAssetID != coverAssetID {
		return CreatedVideo{}, &HTTPError{Code: HTTPFailureDecode}
	}
	return CreatedVideo(response), nil
}

func (c *APIClient) ClaimReview(ctx context.Context, bearer string, caseID int64, version int) (ReviewLease, error) {
	var response struct {
		LeaseToken string `json:"lease_token"`
		Case       struct {
			Version int `json:"version"`
		} `json:"case"`
	}
	err := c.http.JSON(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/admin/review/cases/%d/claim", c.base, caseID), bearer,
		map[string]int{"expected_case_version": version}, &response,
	)
	if err != nil {
		return ReviewLease{}, err
	}
	if response.LeaseToken == "" || response.Case.Version <= version {
		return ReviewLease{}, &HTTPError{Code: HTTPFailureDecode}
	}
	return ReviewLease{LeaseToken: response.LeaseToken, Version: response.Case.Version}, nil
}

func (c *APIClient) ApproveReview(
	ctx context.Context, bearer string, caseID int64, reviewVersion int,
	lease ReviewLease, idempotencyKey string,
) error {
	return c.http.JSONHeaders(
		ctx, http.MethodPost, fmt.Sprintf("%s/api/admin/review/cases/%d/decision", c.base, caseID), bearer,
		map[string]string{"Idempotency-Key": idempotencyKey},
		map[string]any{
			"lease_token": lease.LeaseToken, "expected_case_version": lease.Version,
			"review_version": reviewVersion, "outcome": "approve",
			"reason_code": "content_compliant", "note": "multimodal technical acceptance",
		}, nil,
	)
}

func (c *APIClient) Similar(ctx context.Context, sourceVideoID int64) (SimilarResult, error) {
	var response struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		SemanticAvailable bool `json:"semantic_available"`
	}
	err := c.http.JSON(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/videos/%d/similar?limit=20", c.base, sourceVideoID), "", nil, &response,
	)
	if err != nil {
		return SimilarResult{}, err
	}
	ids := make([]int64, 0, len(response.Items))
	for _, item := range response.Items {
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	return SimilarResult{Available: response.SemanticAvailable, VideoIDs: ids}, nil
}

func (c *APIClient) Hybrid(ctx context.Context, query string) ([]int64, error) {
	endpoint := c.base + "/api/search/videos?q=" + url.QueryEscape(query) + "&limit=20"
	var response struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := c.http.JSON(ctx, http.MethodGet, endpoint, "", nil, &response); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(response.Items))
	for _, item := range response.Items {
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

func (c *APIClient) DeleteVideo(ctx context.Context, bearer string, videoID int64) error {
	return c.http.JSON(ctx, http.MethodDelete,
		c.base+"/api/videos/"+strconv.FormatInt(videoID, 10), bearer, nil, nil,
	)
}

func fixtureContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		if value := mime.TypeByExtension(filepath.Ext(path)); value != "" {
			return value
		}
		return "application/octet-stream"
	}
}
