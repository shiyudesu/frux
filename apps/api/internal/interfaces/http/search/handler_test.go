package interfaceshttpsearch

import (
	"context"
	"encoding/json"
	"errors"
	applicationsearch "github.com/shiyudesu/frux/internal/application/search"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type handlerVideoIndexStub struct {
	items []*domainsearch.VideoIndexItem
	err   error
}

func (s handlerVideoIndexStub) SearchVideos(context.Context, string, *domainsearch.VideoCursor, int) ([]*domainsearch.VideoIndexItem, error) {
	return s.items, s.err
}

type handlerUserIndexStub struct {
	items []*domainsearch.UserIndexItem
	err   error
}

type unavailableSemanticQueryStub struct{}

func (unavailableSemanticQueryStub) EmbedPublicQuery(context.Context, string) (*domainembedding.MultimodalQueryVector, error) {
	return nil, errors.New("provider unavailable")
}

type handlerSemanticIndexStub struct{}

func (handlerSemanticIndexStub) ExactMultimodalSearch(context.Context, domainembedding.MultimodalContractIdentity, []float64, []int64, int) ([]domainembedding.MultimodalExactCandidate, error) {
	return nil, nil
}

type handlerVideoLoaderStub struct{}

func (handlerVideoLoaderStub) BatchGetReadable(context.Context, int64, []int64, bool) (map[int64]*domainvideo.Video, error) {
	return map[int64]*domainvideo.Video{}, nil
}

func (s handlerUserIndexStub) SearchUsers(context.Context, string, *domainsearch.UserCursor, int) ([]*domainsearch.UserIndexItem, error) {
	return s.items, s.err
}

func TestHandlersReturnTypedPagesAndMapErrors(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	service := applicationsearch.New(
		handlerVideoIndexStub{items: []*domainsearch.VideoIndexItem{{
			ID: 11, AuthorID: 7, Title: "cat", CoverURL: "https://example.com/cover.jpg",
			Status: 2, Visibility: "public", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
			MediaStatus: "ready",
			Relevance:   domainsearch.VideoRelevanceExactTitle,
		}}},
		handlerUserIndexStub{items: []*domainsearch.UserIndexItem{{
			ID: 7, Nickname: "Creator", UpdatedAt: now,
			Relevance: domainsearch.UserRelevanceExactNickname,
		}}},
	)
	handler := New(service)
	router := server.New()
	router.GET("/api/search/videos", handler.Videos)
	router.GET("/api/search/users", handler.Users)

	videoResponse := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat&limit=1", nil)
	if videoResponse.Code != http.StatusOK {
		t.Fatalf("video status=%d body=%s", videoResponse.Code, videoResponse.Body.String())
	}
	var videoPage videoPageResponse
	if err := json.Unmarshal(videoResponse.Body.Bytes(), &videoPage); err != nil {
		t.Fatal(err)
	}
	if len(videoPage.Items) != 1 || videoPage.Items[0].ID != 11 || videoPage.Items[0].AuthorID != 7 {
		t.Fatalf("unexpected video response: %+v", videoPage)
	}
	if videoPage.Items[0].Status != 2 || videoPage.Items[0].Visibility != "public" ||
		videoPage.Items[0].MediaStatus != "ready" || videoPage.Items[0].CreatedAt.IsZero() {
		t.Fatalf("video response did not reuse public Video fields: %+v", videoPage.Items[0])
	}

	userResponse := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/users?q=creator", nil)
	if userResponse.Code != http.StatusOK {
		t.Fatalf("user status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var userPage userPageResponse
	if err := json.Unmarshal(userResponse.Body.Bytes(), &userPage); err != nil {
		t.Fatal(err)
	}
	if len(userPage.Items) != 1 || userPage.Items[0].ID != 7 || userPage.Items[0].Nickname != "Creator" {
		t.Fatalf("unexpected user response: %+v", userPage)
	}
	if strings.Contains(userResponse.Body.String(), `"account"`) {
		t.Fatalf("user response leaked account field: %s", userResponse.Body.String())
	}

	badLimit := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat&limit=invalid", nil)
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status=%d body=%s", badLimit.Code, badLimit.Body.String())
	}
	var badLimitBody struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(badLimit.Body.Bytes(), &badLimitBody); err != nil {
		t.Fatalf("decode bad limit response: %v", err)
	}
	if badLimitBody.Code != interfaceshttpapierror.CodeSearchParametersInvalid || badLimitBody.Error != "搜索参数已失效，请重新搜索" {
		t.Fatalf("unexpected bad limit body: %+v", badLimitBody)
	}
}

func TestHandlerHidesInfrastructureErrors(t *testing.T) {
	service := applicationsearch.New(
		handlerVideoIndexStub{err: errors.New("database details")},
		handlerUserIndexStub{},
	)
	handler := New(service)
	router := server.New()
	router.GET("/api/search/videos", handler.Videos)
	response := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected infrastructure error status: %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode infrastructure error: %v", err)
	}
	if body.Code != interfaceshttpapierror.CodeSearchServiceUnavailable || body.Error != "搜索服务暂时不可用，请稍后重试" {
		t.Fatalf("unexpected infrastructure error response: %+v raw=%s", body, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "database details") {
		t.Fatalf("response leaked infrastructure detail: %s", response.Body.String())
	}
}

func TestHandlerMapsUnavailableHybridContinuationToRetryableResponse(t *testing.T) {
	now := time.Now().UTC()
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	config, err := applicationsearch.NewHybridVideoSearchConfig(
		contract, domainembedding.MultimodalHybridMergeVersionV1,
		domainsearch.MaxLimit+1, 10, 10, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := applicationsearch.New(
		handlerVideoIndexStub{}, handlerUserIndexStub{},
		applicationsearch.WithHybridVideoSearch(
			unavailableSemanticQueryStub{}, handlerSemanticIndexStub{}, handlerVideoLoaderStub{}, config,
		),
	)
	cursor := applicationsearch.EncodeHybridVideoCursor("cat", &applicationsearch.HybridVideoCursor{
		Mode:           applicationsearch.VideoRetrievalModeHybrid,
		RankingVersion: domainembedding.MultimodalHybridMergeVersionV1,
		ContractKey:    contract.Key(), HybridScore: 1, PublishedAt: now,
		VideoID: 1, ExpiresAt: now.Add(time.Minute),
	})
	handler := New(service)
	router := server.New()
	router.GET("/api/search/videos", handler.Videos)
	response := ut.PerformRequest(
		router.Engine, http.MethodGet,
		"/api/search/videos?q=cat&cursor="+cursor, nil,
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), interfaceshttpapierror.CodeSearchServiceUnavailable) ||
		strings.Contains(response.Body.String(), "provider unavailable") {
		t.Fatalf("unexpected retryable response: %s", response.Body.String())
	}
}
