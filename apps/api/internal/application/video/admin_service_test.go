package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type adminVideoRepositoryStub struct {
	items []*domainvideo.Video
}

func (r *adminVideoRepositoryStub) ListAdminVideos(
	_ context.Context,
	query domainvideo.AdminVideoQuery,
) ([]*domainvideo.Video, error) {
	items := r.items
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (r *adminVideoRepositoryStub) CommitAdminTransition(
	_ context.Context,
	command domainvideo.AdminTransitionCommand,
	_ *domainadminaudit.Fact,
) (*domainvideo.AdminTransitionResult, error) {
	video := r.items[0]
	if video.Version != command.ExpectedVersion {
		return nil, domainvideo.ErrVideoVersionConflict
	}
	previous := video.Status
	if err := video.ApplyLifecycleTransition(command.Transition, command.OccurredAt); err != nil {
		return nil, err
	}
	video.Version++
	return &domainvideo.AdminTransitionResult{
		Video: video, PreviousStatus: previous,
	}, nil
}

func TestAdminVideoSearchCursorBindsFilters(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminVideoRepositoryStub{items: []*domainvideo.Video{
		{ID: 3, Status: domainvideo.StatusRejected, Version: 1, CreatedAt: now},
		{ID: 2, Status: domainvideo.StatusRejected, Version: 1, CreatedAt: now.Add(-time.Second)},
	}}
	service := NewAdmin(repository, "cursor-secret")
	page, err := service.Search(context.Background(), AdminVideoSearchRequest{
		Status: domainvideo.StatusRejected, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor == "" || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}
	_, err = service.Search(context.Background(), AdminVideoSearchRequest{
		Status: domainvideo.StatusPublished, Limit: 1, Cursor: page.NextCursor,
	})
	if !errors.Is(err, domainvideo.ErrInvalidCursor) {
		t.Fatalf("changed-filter error = %v", err)
	}
}

func TestAdminVideoTransitionsCheckVersionAndReason(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminVideoRepositoryStub{items: []*domainvideo.Video{{
		ID: 7, Status: domainvideo.StatusPublished, Version: 4,
		PublishedAt: &now, CreatedAt: now,
	}}}
	service := NewAdmin(repository, "cursor-secret", WithAdminClock(func() time.Time { return now }))
	result, err := service.TakeDown(context.Background(), AdminEnforcementRequest{
		VideoID: 7, ActorID: 9, ExpectedVersion: 4,
		ReasonCode: domainvideo.EnforcementReasonPolicy, Note: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Video.Status != domainvideo.StatusOffline || result.Video.Version != 5 {
		t.Fatalf("result = %#v", result.Video)
	}
	_, err = service.Restore(context.Background(), AdminEnforcementRequest{
		VideoID: 7, ActorID: 9, ExpectedVersion: 4,
		ReasonCode: domainvideo.RestorationReasonAllowed,
	})
	if !errors.Is(err, domainvideo.ErrVideoVersionConflict) {
		t.Fatalf("stale restore error = %v", err)
	}
	_, err = service.Restore(context.Background(), AdminEnforcementRequest{
		VideoID: 7, ActorID: 9, ExpectedVersion: 5, ReasonCode: "unknown",
	})
	if !errors.Is(err, domainvideo.ErrInvalidEnforcementReason) {
		t.Fatalf("reason error = %v", err)
	}
}
