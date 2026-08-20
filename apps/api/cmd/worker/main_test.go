package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"github.com/twmb/franz-go/pkg/kerr"
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

func TestAccountNotificationWriterCreatesSystemMessageAndClassifiesInvalid(t *testing.T) {
	now := time.Now().UTC()
	notification, err := domainaccount.NewAccountLifecycleNotification(
		7, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, 2, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := &lifecycleMessageRepositoryStub{}
	writer := &accountNotificationMessageWriter{
		service: applicationmessage.New(repository),
	}
	if err := writer.WriteAccountLifecycle(
		context.Background(), *notification,
	); err != nil {
		t.Fatal(err)
	}
	if repository.created == nil ||
		repository.created.Type != domainmessage.TypeSystem ||
		repository.created.Title != "账号已被冻结" ||
		repository.created.EventID != notification.EventID {
		t.Fatalf("created account message = %#v", repository.created)
	}
	forged := *notification
	forged.ReasonCode = domainaccount.AccountReasonAppealApproved
	if err := writer.WriteAccountLifecycle(
		context.Background(), forged,
	); !errors.Is(err, applicationaccount.ErrTerminalAccountNotification) {
		t.Fatalf("invalid notification error = %v", err)
	}
}

type workerPublishedEventPublisherStub struct {
	calls int
}

type workerMediaPublicationStub struct {
	protectCalls int
}

func (*workerMediaPublicationStub) MediaReady(context.Context, int64) error {
	return nil
}

func (s *workerMediaPublicationStub) ProtectVideo(
	context.Context,
	int64,
	int64,
	int64,
) error {
	s.protectCalls++
	return nil
}

func (s *workerPublishedEventPublisherStub) PublishVideoPublished(
	context.Context, *applicationvideo.PublishedEvent,
) error {
	s.calls++
	return nil
}

type workerHandlerStub struct{}

func (workerHandlerStub) Handle(
	context.Context,
	applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	return applicationeventstream.OutcomeDurableSuccess, nil
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

func TestAdminTransitionLegacyOfflineIntentStillProtectsMedia(t *testing.T) {
	media := &workerMediaPublicationStub{}
	applier := workerVideoAdminTransitionApplier{mediaPublication: media}
	err := applier.ApplyAdminTransition(
		context.Background(),
		&domainvideo.Video{
			ID: 10, Status: domainvideo.StatusOffline,
			MediaAssetID: 21, CoverAssetID: 22,
		},
	)
	if err != nil || media.protectCalls != 1 {
		t.Fatalf("protect calls=%d err=%v", media.protectCalls, err)
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

type workerMultimodalProviderStub struct {
	contract domainembedding.MultimodalContractIdentity
}

func (s *workerMultimodalProviderStub) Contract() domainembedding.MultimodalContractIdentity {
	return s.contract
}

func (*workerMultimodalProviderStub) EmbedVideoContent(
	context.Context,
	applicationembedding.MultimodalVideoEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	return nil, errors.New("unused")
}

func (*workerMultimodalProviderStub) EmbedQueryText(
	context.Context,
	applicationembedding.MultimodalQueryEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	return nil, errors.New("unused")
}

type workerMultimodalRepositoryStub struct {
	claims  atomic.Int32
	claimed chan struct{}
	err     error
}

func (*workerMultimodalRepositoryStub) HandoffMultimodalJob(
	context.Context,
	*domainembedding.MultimodalEmbeddingJob,
) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error) {
	return nil, false, false, nil
}

func (s *workerMultimodalRepositoryStub) ClaimMultimodalJobs(
	context.Context,
	string,
	time.Duration,
	int,
) ([]*domainembedding.MultimodalEmbeddingJob, error) {
	if s.claims.Add(1) == 1 && s.claimed != nil {
		close(s.claimed)
	}
	return nil, s.err
}

func (*workerMultimodalRepositoryStub) HeartbeatMultimodalJob(context.Context, int64, string, time.Duration) (bool, error) {
	return false, nil
}

func (*workerMultimodalRepositoryStub) RetryMultimodalJob(context.Context, int64, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (*workerMultimodalRepositoryStub) CompleteMultimodalJob(context.Context, int64, string, *domainembedding.MultimodalVectorFact) (bool, error) {
	return false, nil
}

func (*workerMultimodalRepositoryStub) TerminalMultimodalJob(context.Context, int64, string, string) (bool, error) {
	return false, nil
}

func (*workerMultimodalRepositoryStub) ListMultimodalReconciliationVideoIDs(context.Context, domainembedding.MultimodalContractIdentity, int64, int) ([]int64, error) {
	return nil, nil
}

func (*workerMultimodalRepositoryStub) FindMultimodalVectorFact(context.Context, int64, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error) {
	return nil, domainembedding.ErrMultimodalVectorFactNotFound
}

func (*workerMultimodalRepositoryStub) UpsertMultimodalProjection(context.Context, *domainembedding.MultimodalProjection) (bool, error) {
	return false, nil
}

func (*workerMultimodalRepositoryStub) DeleteMultimodalProjection(context.Context, int64, string) (bool, error) {
	return false, nil
}

type workerMultimodalVideoReaderStub struct{}

func (*workerMultimodalVideoReaderStub) FindByIDAnyStatus(context.Context, int64) (*domainvideo.Video, error) {
	return nil, domainvideo.ErrVideoNotFound
}

type workerMultimodalAssetReaderStub struct{}

func (*workerMultimodalAssetReaderStub) FindAssetByID(context.Context, int64) (*domainmedia.MediaAsset, error) {
	return nil, domainmedia.ErrMediaAssetNotFound
}

func TestMultimodalJobRuntimeDoesNotConstructProviderWhenDisabled(t *testing.T) {
	calls := 0
	err := startMultimodalJobRuntimeWithProvider(
		context.Background(), &infraconfig.Config{}, nil, nil, nil, nil, nil,
		func(context.Context, infraconfig.MultimodalConfig, string) (workerReadyMultimodalProvider, error) {
			calls++
			return nil, errors.New("must not be called")
		},
	)
	if err != nil || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestMultimodalJobRuntimeWaitsForReadinessBeforeClaiming(t *testing.T) {
	cfg, contract := workerMultimodalRuntimeConfig(t)
	repository := &workerMultimodalRepositoryStub{
		claimed: make(chan struct{}), err: errors.New("stop after first claim"),
	}
	readinessStarted := make(chan struct{})
	releaseReadiness := make(chan struct{})
	runtimeFailures := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan error, 1)
	go func() {
		started <- startMultimodalJobRuntimeWithProvider(
			ctx, cfg, repository, &workerMultimodalVideoReaderStub{},
			&workerMultimodalAssetReaderStub{}, nil, runtimeFailures,
			func(context.Context, infraconfig.MultimodalConfig, string) (workerReadyMultimodalProvider, error) {
				close(readinessStarted)
				<-releaseReadiness
				return &workerMultimodalProviderStub{contract: contract}, nil
			},
		)
	}()
	<-readinessStarted
	if repository.claims.Load() != 0 {
		t.Fatal("job was claimed before readiness completed")
	}
	close(releaseReadiness)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	select {
	case <-repository.claimed:
	case <-time.After(time.Second):
		t.Fatal("multimodal worker did not claim after readiness")
	}
	select {
	case err := <-runtimeFailures:
		if err == nil || !strings.Contains(err.Error(), "multimodal job worker") {
			t.Fatalf("runtime error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("multimodal worker failure was not supervised")
	}
}

func TestMultimodalJobRuntimeStopsBeforeClaimOnReadinessFailure(t *testing.T) {
	cfg, _ := workerMultimodalRuntimeConfig(t)
	repository := &workerMultimodalRepositoryStub{}
	want := errors.New("provider unavailable")
	err := startMultimodalJobRuntimeWithProvider(
		context.Background(), cfg, repository, &workerMultimodalVideoReaderStub{},
		&workerMultimodalAssetReaderStub{}, nil, nil,
		func(context.Context, infraconfig.MultimodalConfig, string) (workerReadyMultimodalProvider, error) {
			return nil, want
		},
	)
	if !errors.Is(err, want) || repository.claims.Load() != 0 {
		t.Fatalf("claims=%d err=%v", repository.claims.Load(), err)
	}
}

func workerMultimodalRuntimeConfig(t testing.TB) (*infraconfig.Config, domainembedding.MultimodalContractIdentity) {
	t.Helper()
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
	return &infraconfig.Config{Multimodal: infraconfig.MultimodalConfig{
		Enabled: true, VideoJobsEnabled: true, MaxVideoTextRunes: 2048,
		Contract: infraconfig.MultimodalContractConfig{
			ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias,
			RevisionAlias: contract.RevisionAlias, Dimension: contract.Dimension,
			TextCanonicalizer:        contract.TextCanonicalizer,
			FrameSamplingPolicy:      contract.FrameSamplingPolicy,
			ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy,
			FusionPolicy:             contract.FusionPolicy,
		},
		Provider: infraconfig.MultimodalProviderConfig{Deadline: "1s", AdmissionLimit: 1},
		Jobs: infraconfig.MultimodalJobConfig{
			MaxAttempts: 3, LeaseTTL: "5s", HeartbeatInterval: "1s", PollInterval: "100ms",
			RetryBase: "1s", RetryMax: "10s", ShutdownTimeout: "1s",
		},
		Images: infraconfig.MultimodalImageConfig{
			MaxCount: 2, MaxBytesEach: 64 << 10, MaxTotalBytes: 128 << 10,
			MaxPixelsEach: 10_000, AllowedMIMETypes: []string{"image/jpeg"},
		},
	}}, contract
}

func TestBehaviorKafkaConsumersStartViewBeforeAction(t *testing.T) {
	started := make([]infrakafka.ConsumerGroupID, 0, 2)
	starter := func(
		_ context.Context,
		_ *infrakafka.Backbone,
		_ infraconfig.KafkaConfig,
		group infrakafka.ConsumerGroupID,
		_ applicationeventstream.Handler,
		_ chan<- error,
	) error {
		started = append(started, group)
		return nil
	}
	err := startBehaviorKafkaConsumers(
		context.Background(),
		nil,
		infraconfig.KafkaConfig{},
		orderedBehaviorKafkaConsumers(
			behaviorKafkaConsumer{
				activeGroup:   infrakafka.GroupConsumeViewActive,
				activeHandler: workerHandlerStub{},
			},
			behaviorKafkaConsumer{
				activeGroup:   infrakafka.GroupPersistActionActive,
				activeHandler: workerHandlerStub{},
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
	if len(started) != len(want) ||
		started[0] != want[0] ||
		started[1] != want[1] {
		t.Fatalf("startup order=%v want=%v", started, want)
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
					activeGroup:   infrakafka.GroupConsumeViewActive,
					activeHandler: workerHandlerStub{},
				},
				behaviorKafkaConsumer{
					activeGroup:   infrakafka.GroupPersistActionActive,
					activeHandler: workerHandlerStub{},
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
		infraconfig.KafkaConfig{},
		[]behaviorKafkaConsumer{{
			activeGroup:   infrakafka.GroupConsumeViewActive,
			activeHandler: workerHandlerStub{},
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

func TestKafkaConsumerSupervisorReportsFatalStartupFailure(t *testing.T) {
	fatal := make(chan error, 1)
	starter := func(
		context.Context,
		*infrakafka.Backbone,
		infraconfig.KafkaConfig,
		infrakafka.ConsumerGroupID,
		applicationeventstream.Handler,
		chan<- error,
	) error {
		return fmt.Errorf("%w: retained source offset missing", infrakafka.ErrConsumerDataLoss)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := superviseBehaviorKafkaConsumers(
		ctx,
		nil,
		infraconfig.KafkaConfig{},
		[]behaviorKafkaConsumer{{
			activeGroup:   infrakafka.GroupConsumeViewActive,
			activeHandler: workerHandlerStub{},
		}},
		fatal,
		starter,
	); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-fatal:
		if !errors.Is(err, infrakafka.ErrConsumerDataLoss) {
			t.Fatalf("fatal error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fatal startup failure was not reported")
	}
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
