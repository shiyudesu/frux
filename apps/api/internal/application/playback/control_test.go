package applicationplayback

import (
	"context"
	"testing"

	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
)

type preloadControlRepository struct {
	listCalls int
}

func (r *preloadControlRepository) FindConfig(
	context.Context, string, string,
) (*domainplayback.Config, error) {
	return nil, nil
}

func (r *preloadControlRepository) ListPreloadVideos(
	context.Context, int64, int,
) ([]*domainplayback.PreloadVideo, error) {
	r.listCalls++
	return []*domainplayback.PreloadVideo{{VideoID: 1}}, nil
}

func (r *preloadControlRepository) CreateQoSReport(
	context.Context, *domainplayback.QoSReport,
) (*domainplayback.QoSReport, bool, error) {
	return nil, false, nil
}

type staticPlaybackControlReader bool

func (r staticPlaybackControlReader) Bool(domaingovernance.Key) bool {
	return bool(r)
}

func TestListPreloadVideosUsesOnlyLocalControlSnapshot(t *testing.T) {
	repository := &preloadControlRepository{}
	service := New(repository, WithControlReader(staticPlaybackControlReader(false)))
	result, err := service.ListPreloadVideos(context.Background(), 0, 3)
	if err != nil {
		t.Fatalf("list preload videos: %v", err)
	}
	if len(result.Items) != 0 || repository.listCalls != 0 {
		t.Fatalf("disabled preload result=%#v repository calls=%d", result.Items, repository.listCalls)
	}
}
