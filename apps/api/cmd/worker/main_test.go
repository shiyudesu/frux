package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"github.com/twmb/franz-go/pkg/kerr"
)

type semanticSamplerStub struct {
	mutex    sync.Mutex
	backlog  int
	coverage int
	cleanup  int
	sampled  chan struct{}
}

func (s *semanticSamplerStub) SemanticBacklog(
	context.Context,
) ([]domainembedding.SemanticBacklog, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.backlog++
	return []domainembedding.SemanticBacklog{{
		State: domainembedding.SemanticJobPending, Count: 1,
	}}, nil
}

func (s *semanticSamplerStub) SemanticCoverage(context.Context) (int64, int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.coverage++
	return 1, 2, nil
}

func (s *semanticSamplerStub) CleanupSemanticJobs(
	context.Context,
	time.Time,
	int,
) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanup++
	if s.cleanup == 2 {
		close(s.sampled)
	}
	return 0, nil
}

func TestSemanticSamplerRunsAndStopsWithWorkerContext(t *testing.T) {
	repository := &semanticSamplerStub{sampled: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sampleSemanticBacklog(ctx, repository, 5*time.Millisecond)
	}()
	select {
	case <-repository.sampled:
	case <-time.After(time.Second):
		t.Fatal("semantic sampler did not repeat")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("semantic sampler did not stop on shutdown")
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.backlog < 2 ||
		repository.coverage != repository.backlog ||
		repository.cleanup != repository.backlog {
		t.Fatalf(
			"sampler calls backlog=%d coverage=%d cleanup=%d",
			repository.backlog,
			repository.coverage,
			repository.cleanup,
		)
	}
}

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

type workerParityStub struct{}

func (workerParityStub) Compare(
	context.Context, applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	return applicationeventstream.ParityMatch, nil
}

func TestAdminTransitionLegacyRestoreAlwaysRepairsDurableHandoff(t *testing.T) {
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
	if publisher.calls != 2 || readiness.marks != 0 {
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

func TestBehaviorKafkaConsumersStartViewBeforeAction(t *testing.T) {
	for _, mode := range []infrakafka.ConsumerMode{
		infrakafka.ConsumerModeKafka,
		infrakafka.ConsumerModeKafkaShadow,
	} {
		t.Run(string(mode), func(t *testing.T) {
			started := make([]infrakafka.ConsumerGroupID, 0, 2)
			starter := func(
				_ context.Context,
				_ *infrakafka.Backbone,
				_ infraconfig.KafkaConfig,
				group infrakafka.ConsumerGroupID,
				_ string,
				_ applicationeventstream.Handler,
				_ chan<- error,
			) error {
				started = append(started, group)
				return nil
			}
			err := startBehaviorKafkaConsumers(
				context.Background(),
				nil,
				infraconfig.KafkaConfig{ShadowDeployment: "test"},
				orderedBehaviorKafkaConsumers(
					behaviorKafkaConsumer{
						migration:   infrakafka.StreamMigration{Consumer: mode},
						activeGroup: infrakafka.GroupConsumeViewActive,
						shadowGroup: infrakafka.GroupConsumeViewShadow,
						parity:      workerParityStub{},
					},
					behaviorKafkaConsumer{
						migration:   infrakafka.StreamMigration{Consumer: mode},
						activeGroup: infrakafka.GroupPersistActionActive,
						shadowGroup: infrakafka.GroupPersistActionShadow,
						parity:      workerParityStub{},
					},
				),
				nil,
				starter,
			)
			if err != nil {
				t.Fatal(err)
			}
			want := []infrakafka.ConsumerGroupID{
				infrakafka.GroupConsumeViewActive,
				infrakafka.GroupPersistActionActive,
			}
			if mode == infrakafka.ConsumerModeKafkaShadow {
				want = []infrakafka.ConsumerGroupID{
					infrakafka.GroupConsumeViewShadow,
					infrakafka.GroupPersistActionShadow,
				}
			}
			if len(started) != len(want) ||
				started[0] != want[0] ||
				started[1] != want[1] {
				t.Fatalf("startup order=%v want=%v", started, want)
			}
		})
	}
}

func TestBehaviorKafkaConsumersWaitForViewReadinessBeforeAction(t *testing.T) {
	viewRelease := make(chan struct{})
	viewStarted := make(chan struct{})
	actionStarted := make(chan struct{})
	starter := func(
		_ context.Context,
		_ *infrakafka.Backbone,
		_ infraconfig.KafkaConfig,
		group infrakafka.ConsumerGroupID,
		_ string,
		_ applicationeventstream.Handler,
		_ chan<- error,
	) error {
		switch group {
		case infrakafka.GroupConsumeViewActive:
			close(viewStarted)
			<-viewRelease
		case infrakafka.GroupPersistActionActive:
			close(actionStarted)
		}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- startBehaviorKafkaConsumers(
			context.Background(),
			nil,
			infraconfig.KafkaConfig{},
			orderedBehaviorKafkaConsumers(
				behaviorKafkaConsumer{
					migration:   infrakafka.StreamMigration{Consumer: infrakafka.ConsumerModeKafka},
					activeGroup: infrakafka.GroupConsumeViewActive,
					parity:      workerParityStub{},
				},
				behaviorKafkaConsumer{
					migration:   infrakafka.StreamMigration{Consumer: infrakafka.ConsumerModeKafka},
					activeGroup: infrakafka.GroupPersistActionActive,
					parity:      workerParityStub{},
				},
			),
			nil,
			starter,
		)
	}()
	<-viewStarted
	select {
	case <-actionStarted:
		t.Fatal("action startup began before view readiness")
	default:
	}
	close(viewRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("behavior consumer startup did not finish")
	}
}

func TestKafkaConsumerSupervisorDoesNotBlockUnrelatedWorkerStartup(t *testing.T) {
	starterEntered := make(chan struct{})
	release := make(chan struct{})
	starter := func(
		_ context.Context,
		_ *infrakafka.Backbone,
		_ infraconfig.KafkaConfig,
		_ infrakafka.ConsumerGroupID,
		_ string,
		_ applicationeventstream.Handler,
		_ chan<- error,
	) error {
		close(starterEntered)
		<-release
		return errors.New("broker unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	if err := superviseBehaviorKafkaConsumers(
		ctx,
		nil,
		nil,
		nil,
		infraconfig.KafkaConfig{},
		[]behaviorKafkaConsumer{{
			migration: infrakafka.StreamMigration{
				Consumer: infrakafka.ConsumerModeKafka,
			},
			activeGroup: infrakafka.GroupConsumeViewActive,
			parity:      workerParityStub{},
		}},
		nil,
		starter,
	); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("supervisor blocked worker composition: %v", elapsed)
	}
	select {
	case <-starterEntered:
	case <-time.After(time.Second):
		t.Fatal("transport supervisor did not attempt consumer startup")
	}
	close(release)
}

func TestWaitForKafkaConsumerStartup(t *testing.T) {
	t.Run("assignment ready", func(t *testing.T) {
		consumerCtx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{})
		close(ready)
		err := waitForKafkaConsumerStartup(
			context.Background(),
			cancel,
			infrakafka.GroupConsumeViewActive,
			ready,
			make(chan error),
			time.Second,
		)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-consumerCtx.Done():
			t.Fatal("successful startup canceled consumer")
		default:
		}
		cancel()
	})

	t.Run("timeout cancels consumer", func(t *testing.T) {
		consumerCtx, cancel := context.WithCancel(context.Background())
		err := waitForKafkaConsumerStartup(
			context.Background(),
			cancel,
			infrakafka.GroupConsumeViewActive,
			make(chan struct{}),
			make(chan error),
			time.Millisecond,
		)
		if !errors.Is(err, infrakafka.ErrConsumerStartupTimeout) {
			t.Fatalf("error=%v", err)
		}
		select {
		case <-consumerCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("timed out startup did not cancel consumer")
		}
	})

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "fatal init", err: infrakafka.ErrConsumerConfiguration},
		{name: "fatal runtime", err: kerr.GroupAuthorizationFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			consumerCtx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			done <- test.err
			err := waitForKafkaConsumerStartup(
				context.Background(),
				cancel,
				infrakafka.GroupConsumeViewActive,
				make(chan struct{}),
				done,
				time.Second,
			)
			if !errors.Is(err, test.err) {
				t.Fatalf("error=%v", err)
			}
			select {
			case <-consumerCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("fatal startup did not cancel consumer")
			}
		})
	}
}
