package main

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type publicationReadinessStub struct {
	ready bool
	err   error
	marks int
}

func (s *publicationReadinessStub) LifecyclePublicationReady(
	context.Context, string,
) (bool, error) {
	return s.ready, s.err
}

func (s *publicationReadinessStub) MarkLifecyclePublicationReady(
	context.Context, string, time.Time,
) error {
	s.ready = true
	s.marks++
	return nil
}

type lifecycleMessageRepositoryStub struct {
	created *domainmessage.Message
}

type publicationRecoveryStub struct {
	err error
}

func (s publicationRecoveryStub) EnsurePublication(
	context.Context,
	domainmessage.LifecycleNotification,
) error {
	return s.err
}

func (r *lifecycleMessageRepositoryStub) Create(
	_ context.Context,
	message *domainmessage.Message,
	_ string,
) (*domainmessage.Message, bool, error) {
	copyMessage := *message
	copyMessage.ID = 1
	r.created = &copyMessage
	return &copyMessage, true, nil
}

func (*lifecycleMessageRepositoryStub) ListByUser(
	context.Context, int64, *domainmessage.Cursor, int,
) ([]*domainmessage.Message, error) {
	return nil, nil
}

func (*lifecycleMessageRepositoryStub) CountUnread(context.Context, int64) (int, error) {
	return 0, nil
}

func (*lifecycleMessageRepositoryStub) MarkRead(context.Context, int64, []int64) (int, error) {
	return 0, nil
}

func TestLifecycleWriterWaitsForDurablePublicationReadiness(t *testing.T) {
	notification := domainmessage.LifecycleNotification{
		EventID:     domainmessage.PublicationEventID(9, 1),
		RecipientID: 7, VideoID: 9, ReviewVersion: 1,
		Stage:      domainmessage.LifecycleStagePublished,
		Result:     domainmessage.LifecycleResultPublic,
		OccurredAt: time.Now().UTC(),
	}
	writer := &lifecycleNotificationMessageWriter{
		recovery: publicationRecoveryStub{
			err: applicationvideo.ErrLifecycleNotificationNotReady,
		},
	}
	if err := writer.WriteLifecycleNotification(
		context.Background(), notification, "视频已发布", "内容",
	); !errors.Is(err, applicationvideo.ErrLifecycleNotificationNotReady) {
		t.Fatalf("publication gate error = %v", err)
	}

	repository := &lifecycleMessageRepositoryStub{}
	writer.recovery = publicationRecoveryStub{}
	writer.service = applicationmessage.New(repository)
	if err := writer.WriteLifecycleNotification(
		context.Background(), notification, "视频已发布", "内容",
	); err != nil {
		t.Fatalf("ready publication: %v", err)
	}
	if repository.created == nil ||
		repository.created.Type != domainmessage.TypeVideoLifecycle ||
		repository.created.EventID != notification.EventID {
		t.Fatalf("created lifecycle message = %#v", repository.created)
	}
}

type workerPublishedEventPublisherStub struct {
	calls int
}

func (s *workerPublishedEventPublisherStub) PublishVideoPublished(
	context.Context, *applicationvideo.PublishedEvent,
) error {
	s.calls++
	return nil
}

func TestAdminTransitionLegacyRestoreMarksPublicationReadyOnce(t *testing.T) {
	publishedAt := time.Now().UTC()
	video := &domainvideo.Video{
		ID: 9, AuthorID: 7, ReviewVersion: 1,
		Status:      domainvideo.StatusPublished,
		Visibility:  domainvideo.VisibilityPublic,
		MediaStatus: "legacy_ready", PublishedAt: &publishedAt,
		MediaURL: "https://example.com/video.mp4",
	}
	publisher := &workerPublishedEventPublisherStub{}
	readiness := &publicationReadinessStub{}
	applier := workerVideoAdminTransitionApplier{
		publisher: publisher, publication: readiness,
	}
	if err := applier.ApplyAdminTransition(context.Background(), video); err != nil {
		t.Fatal(err)
	}
	if err := applier.ApplyAdminTransition(context.Background(), video); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || readiness.marks != 1 {
		t.Fatalf("publisher calls=%d marks=%d", publisher.calls, readiness.marks)
	}
}

func TestModerationWorkerOwnerIsUniquePerProcessStart(t *testing.T) {
	first, err := newModerationWorkerOwner()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newModerationWorkerOwner()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) > 128 || len(second) > 128 {
		t.Fatalf("owners first=%q second=%q", first, second)
	}
}
