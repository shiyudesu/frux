package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationgovernance "github.com/shiyudesu/frux/internal/application/governance"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infracache "github.com/shiyudesu/frux/internal/infra/cache"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infradatabase "github.com/shiyudesu/frux/internal/infra/database"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	inframoderation "github.com/shiyudesu/frux/internal/infra/moderation"
	inframq "github.com/shiyudesu/frux/internal/infra/mq"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
	infragovernance "github.com/shiyudesu/frux/internal/infra/persistence/governance"
	infrainteraction "github.com/shiyudesu/frux/internal/infra/persistence/interaction"
	infrapersistencemedia "github.com/shiyudesu/frux/internal/infra/persistence/media"
	inframessage "github.com/shiyudesu/frux/internal/infra/persistence/message"
	migration "github.com/shiyudesu/frux/internal/infra/persistence/migration"
	infrarecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
	infrarelation "github.com/shiyudesu/frux/internal/infra/persistence/relation"
	infrareview "github.com/shiyudesu/frux/internal/infra/persistence/review"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const configPath = "./configs/config.yaml"

func main() {
	cfg, err := infraconfig.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	if cfg.RabbitMQ.URL == "" {
		log.Fatal("rabbitmq url is required for worker")
	}
	if cfg.Redis.Addr == "" {
		log.Fatal("redis addr is required for worker")
	}
	kafkaBackbone, err := infrakafka.Start(
		context.Background(), cfg.Kafka, inframetrics.KafkaObserver{}, inframetrics.KafkaObserver{},
	)
	if err != nil {
		log.Fatalf("init kafka backbone failed: %v", err)
	}
	defer func() {
		if err := kafkaBackbone.Close(context.Background()); err != nil {
			log.Printf("close kafka backbone failed: %v", err)
		}
	}()

	sqlDB, err := infradatabase.New(cfg.Database)
	if err != nil {
		log.Fatalf("init database failed: %v", err)
	}
	defer closeSQL(sqlDB)

	gormDB, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("init gorm failed: %v", err)
	}
	if err := migration.AutoMigrate(gormDB); err != nil {
		log.Fatalf("auto migrate failed: %v", err)
	}

	rabbitMQ, err := inframq.NewRabbitMQ(cfg.RabbitMQ)
	if err != nil {
		log.Fatalf("init rabbitmq failed: %v", err)
	}
	defer closeRabbitMQ(rabbitMQ)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go kafkaBackbone.RunHealthObserver(ctx, 15*time.Second, 2*time.Second)
	go func() {
		if err := inframetrics.RunServer(ctx, ":9091"); err != nil {
			log.Printf("metrics server failed: %v", err)
		}
	}()

	if err := startWorkers(ctx, cfg, gormDB, rabbitMQ); err != nil {
		log.Fatalf("start workers failed: %v", err)
	}
	log.Println("frux worker is running")
	<-ctx.Done()
	log.Println("frux worker stopped")
}

func startWorkers(ctx context.Context, cfg *infraconfig.Config, gormDB *gorm.DB, rabbitMQ *inframq.RabbitMQ) error {
	redisClient := infracache.NewRedisClient(cfg.Redis)
	feedCache := infracache.NewFeedCache(redisClient)
	governancePollInterval, err := time.ParseDuration(cfg.Governance.PollInterval)
	if err != nil {
		return err
	}
	governancePollTimeout, err := time.ParseDuration(cfg.Governance.PollTimeout)
	if err != nil {
		return err
	}
	governanceRegistry := domaingovernance.DefaultRegistry()
	governanceRepo := infragovernance.New(gormDB, governanceRegistry, nil)
	governanceRuntime := applicationgovernance.NewRuntime(
		governanceRegistry,
		domaingovernance.ProcessWorker,
		governanceRepo,
		applicationgovernance.WithRuntimeObserver(inframetrics.GovernanceObserver{}),
	)
	go func() {
		if err := governanceRuntime.Run(
			ctx, governancePollInterval, governancePollTimeout,
		); err != nil {
			log.Printf("governance snapshot poller stopped: %v", err)
		}
	}()

	recommendationRepo := infrarecommendation.New(gormDB)
	interactionRepo := infrainteraction.New(gormDB)
	moderationJobConfig := domainreview.ModerationJobConfig{
		Mode:                  cfg.Moderation.Mode,
		ProviderConfigVersion: cfg.Moderation.ProviderConfigVersion,
		InputProfileVersion:   cfg.Moderation.InputProfileVersion,
		MaxAttempts:           cfg.Moderation.MaxAttempts,
	}
	reviewRepo := infrareview.New(
		gormDB,
		infrareview.WithModerationJobConfig(moderationJobConfig),
	)
	messageService := applicationmessage.New(inframessage.New(gormDB))
	commentNotificationWorker := applicationinteraction.NewCommentNotificationWorker(
		interactionRepo,
		interactionRepo,
		&commentNotificationMessageWriter{service: messageService},
	)
	if err := commentNotificationWorker.Start(ctx); err != nil {
		return err
	}
	actionWorker := applicationinteraction.NewActionWorker(
		interactionRepo,
		rabbitMQ,
		applicationinteraction.WithRecommendationOutcomeRecorder(recommendationRepo),
	)
	if err := actionWorker.Start(ctx); err != nil {
		return err
	}

	exposureRepo := infraexposure.New(gormDB)
	outboxDispatcher := applicationexposure.NewOutboxDispatcher(
		exposureRepo,
		rabbitMQ,
		applicationexposure.WithOutboxObserver(func(stats applicationexposure.OutboxStats, err error) {
			inframetrics.ObserveViewEventOutbox(stats.Pending, stats.OldestPending, time.Now().UTC(), err)
		}),
	)
	if err := outboxDispatcher.Start(ctx); err != nil {
		return err
	}

	if err := applicationrecommendation.NewRequestLogCleanupWorker(recommendationRepo, recommendationRepo).Start(ctx); err != nil {
		return err
	}
	behaviorWorker := applicationrecommendation.NewBehaviorEventWorker(recommendationRepo, rabbitMQ)
	if err := behaviorWorker.Start(ctx); err != nil {
		return err
	}
	profileOutboxWorker := applicationrecommendation.NewProfileOutboxWorker(
		applicationrecommendation.NewProfileProjector(recommendationRepo),
		recommendationRepo,
		infrarelation.New(gormDB),
		applicationrecommendation.WithProfileOutboxOutcomeRepository(recommendationRepo),
		applicationrecommendation.WithActionProfileOutboxStore(interactionRepo),
		applicationrecommendation.WithBehaviorProfileOutboxStore(recommendationRepo),
	)
	if err := profileOutboxWorker.Start(ctx); err != nil {
		return err
	}

	embeddingRepo := infraembedding.New(gormDB)
	embeddingService := applicationembedding.New(embeddingRepo, nil)
	embeddingWorker := applicationembedding.NewVideoEmbeddingWorker(embeddingService, rabbitMQ)
	if err := embeddingWorker.Start(ctx); err != nil {
		return err
	}

	mediaStore, err := inframedia.NewObjectStore(ctx, cfg.Media)
	if err != nil {
		return err
	}
	leaseTTL, err := time.ParseDuration(cfg.Media.Processing.LeaseTTL)
	if err != nil {
		return err
	}
	cleanupDelay, err := time.ParseDuration(cfg.Media.Processing.CleanupDelay)
	if err != nil {
		return err
	}
	mediaRepo := infrapersistencemedia.New(gormDB)
	if err := mediaRepo.UpsertProcessingProfile(ctx, &domainmedia.ProcessingProfile{
		Version:    cfg.Media.Processing.ProfileVersion,
		Name:       "Frux baseline H.264/AAC and DASH profile",
		ConfigJSON: `{"baseline_max_height":720,"renditions":[480,720,1080],"dash_segment_seconds":4}`,
		Active:     true,
	}); err != nil {
		return err
	}
	mediaProcessor := inframedia.NewFFmpegProcessor(mediaStore)
	mediaURLResolver, err := inframedia.NewURLResolver(cfg.Media.PublicBaseURL, mediaStore)
	if err != nil {
		return err
	}
	mediaCatalog := inframedia.NewDeliveryCatalog(mediaRepo, mediaURLResolver, mediaStore)
	videoRepo := infravideo.New(gormDB, infravideo.WithMediaCatalog(mediaCatalog))
	mediaPublication := applicationvideo.NewMediaPublicationService(videoRepo, mediaCatalog, rabbitMQ, feedCache)
	publicationRecovery := applicationvideo.NewPublicationRecoveryService(
		videoRepo, mediaPublication, rabbitMQ,
	)
	reviewService := applicationreview.New(
		reviewRepo,
		applicationreview.WithObserver(workerReviewObserver{}),
		applicationreview.WithOutcomeApplier(workerReviewOutcomeApplier{
			videoReader: videoRepo, mediaPublication: mediaPublication,
			publisher: rabbitMQ, cacheInvalidator: feedCache,
		}),
	)
	lifecycleWriter := &lifecycleNotificationMessageWriter{
		service: messageService, recovery: publicationRecovery,
	}
	reviewNotificationWorker := applicationreview.NewReviewNotificationWorker(
		reviewRepo,
		&reviewNotificationMessageWriter{
			service: messageService, lifecycle: lifecycleWriter,
		},
		workerReviewObserver{},
	)
	if err := reviewNotificationWorker.Start(ctx); err != nil {
		return err
	}
	lifecycleNotificationWorker := applicationvideo.NewLifecycleNotificationWorker(
		videoRepo,
		lifecycleWriter,
		inframetrics.VideoLifecycleNotificationObserver{},
	)
	if err := lifecycleNotificationWorker.Start(ctx); err != nil {
		return err
	}
	adminIntentWorker := applicationvideo.NewAdminTransitionIntentWorker(
		videoRepo,
		videoRepo,
		feedCache,
		workerVideoAdminTransitionApplier{
			mediaPublication: mediaPublication,
			publisher:        rabbitMQ,
			publication:      videoRepo,
		},
	)
	if err := adminIntentWorker.Start(ctx); err != nil {
		return err
	}
	reviewReconciler := applicationreview.NewReconciliationWorker(reviewService)
	if err := reviewReconciler.RunOnce(ctx); err != nil {
		log.Printf("initial review reconciliation failed: %v", err)
	}
	reviewReconciler.Start(ctx)
	mediaCleanup := applicationmedia.NewCleanupService(
		mediaRepo, mediaStore, cfg.Media.Backend, cleanupDelay, cfg.Media.Processing.MaxAttempts,
	)
	cleanupOwner, err := newWorkerOwner("cleanup")
	if err != nil {
		return err
	}
	applicationmedia.NewCleanupWorker(mediaCleanup, cleanupOwner).Start(ctx)
	if err := startModerationWorker(
		ctx, cfg, reviewRepo, reviewService, mediaStore, mediaURLResolver, mediaCleanup,
	); err != nil {
		return err
	}
	reconciler := applicationmedia.NewReconciler(
		mediaRepo, mediaStore, mediaPublication, cfg.Media.Backend,
		cfg.Media.Processing.ProfileVersion, cfg.Media.Processing.MaxAttempts, cleanupDelay,
	)
	applicationmedia.NewReconciliationWorker(reconciler).Start(ctx)
	feedRepo := infrafeed.New(gormDB, infrafeed.WithMediaCatalog(mediaCatalog))
	feedPreheater := applicationvideo.NewFeedPreheater(feedRepo, feedCache)
	fanoutWorker := applicationvideo.NewFanoutWorker(
		feedRepo, rabbitMQ, feedCache, feedPreheater,
		applicationvideo.WithFanoutControlReader(governanceRuntime),
	)
	if err := fanoutWorker.Start(ctx); err != nil {
		return err
	}

	mediaWorker := applicationmedia.NewMediaProcessingWorker(
		mediaRepo, mediaProcessor, rabbitMQ, leaseTTL, cfg.Media.Processing.WorkerConcurrency,
		applicationmedia.WithMediaStateNotifier(reviewMediaReadyNotifier{
			publication: mediaPublication, videoRepo: videoRepo, reviewService: reviewService,
		}),
	)
	return mediaWorker.Start(ctx)
}

func startModerationWorker(
	ctx context.Context,
	cfg *infraconfig.Config,
	repository *infrareview.Repository,
	reviewService *applicationreview.Service,
	mediaStore domainmedia.MediaObjectStore,
	defaultResolver domainmedia.MediaURLResolver,
	cleanup applicationreview.ModerationSampleCleanup,
) error {
	leaseTTL, err := time.ParseDuration(cfg.Moderation.LeaseTTL)
	if err != nil {
		return err
	}
	pollInterval, err := time.ParseDuration(cfg.Moderation.PollInterval)
	if err != nil {
		return err
	}
	sampleURLTTL, err := time.ParseDuration(cfg.Moderation.SampleURLTTL)
	if err != nil {
		return err
	}
	sampleRetention, err := time.ParseDuration(cfg.Moderation.SampleRetention)
	if err != nil {
		return err
	}
	jobConfig := domainreview.ModerationJobConfig{
		Mode:                  cfg.Moderation.Mode,
		ProviderConfigVersion: cfg.Moderation.ProviderConfigVersion,
		InputProfileVersion:   cfg.Moderation.InputProfileVersion,
		MaxAttempts:           cfg.Moderation.MaxAttempts,
	}
	var preparer applicationreview.ModerationInputPreparer
	var provider applicationreview.ModerationProvider
	if cfg.Moderation.Mode != domainreview.ModerationModeDisabled {
		timeout, err := time.ParseDuration(cfg.Moderation.Timeout)
		if err != nil {
			return err
		}
		gatewayOptions := []inframoderation.GatewayOption{}
		if cfg.Moderation.AllowInsecureLocal {
			gatewayOptions = append(gatewayOptions, inframoderation.WithInsecureLocalGateway())
		}
		provider, err = inframoderation.NewHTTPGateway(
			cfg.Moderation.Endpoint, cfg.Moderation.HMACSecret, timeout, gatewayOptions...,
		)
		if err != nil {
			return err
		}
		resolver := defaultResolver
		if cfg.Media.Backend == domainmedia.StorageBackendS3 {
			moderationMediaConfig := cfg.Media
			moderationMediaConfig.S3.PresignEndpoint = cfg.Moderation.SamplePresignEndpoint
			moderationStore, err := inframedia.NewObjectStore(ctx, moderationMediaConfig)
			if err != nil {
				return err
			}
			resolver, err = inframedia.NewURLResolver(
				cfg.Moderation.SamplePresignEndpoint, moderationStore,
			)
			if err != nil {
				return err
			}
		} else if cfg.Media.Backend == domainmedia.StorageBackendLocal {
			signer, err := inframedia.NewLocalProtectedURLSigner(
				"/moderation-media", cfg.JWT.Secret, sampleURLTTL,
			)
			if err != nil {
				return err
			}
			resolver, err = inframedia.NewLocalModerationURLResolver(
				fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), signer,
			)
			if err != nil {
				return err
			}
		}
		preparer = inframedia.NewModerationInputPreparer(
			mediaStore, resolver, cleanup, sampleRetention,
		)
	}
	worker, err := applicationreview.NewModerationWorker(
		repository, preparer, provider, reviewService, cleanup,
		inframetrics.ModerationObserver{},
		applicationreview.ModerationWorkerConfig{
			JobConfig: jobConfig, LeaseTTL: leaseTTL, PollInterval: pollInterval,
			SampleURLTTL: sampleURLTTL, SampleRetention: sampleRetention,
			Concurrency: cfg.Moderation.WorkerConcurrency,
		},
	)
	if err != nil {
		return err
	}
	if err := worker.Reconcile(ctx, 500); err != nil {
		log.Printf("initial moderation reconciliation failed: %v", err)
	}
	owner, err := newModerationWorkerOwner()
	if err != nil {
		return err
	}
	go worker.Run(ctx, owner)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := worker.Reconcile(ctx, 500); err != nil {
					log.Printf("moderation reconciliation failed: %v", err)
				}
			}
		}
	}()
	return nil
}

func newModerationWorkerOwner() (string, error) {
	return newWorkerOwner("moderation")
}

func newWorkerOwner(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(random[:]), nil
}

type workerReviewObserver struct{}

func (workerReviewObserver) Observe(stage, result string) {
	inframetrics.ObserveReview(stage, result)
}

type workerReviewOutcomeApplier struct {
	videoReader interface {
		FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
	}
	mediaPublication interface {
		MediaReady(ctx context.Context, assetID int64) error
		ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	}
	publisher        applicationvideo.PublishedEventPublisher
	cacheInvalidator applicationvideo.VideoCacheInvalidator
}

func (a workerReviewOutcomeApplier) ApplyReviewOutcome(
	ctx context.Context,
	result *domainreview.ProcessingResult,
) error {
	if result == nil || result.Decision == nil || result.Case == nil {
		return nil
	}
	if a.cacheInvalidator != nil {
		_ = a.cacheInvalidator.InvalidateVideo(ctx, result.Case.VideoID)
	}
	switch result.Decision.Outcome {
	case domainreview.OutcomeApprove:
		video, err := a.videoReader.FindByIDAnyStatus(ctx, result.Case.VideoID)
		if err != nil {
			return err
		}
		if video == nil ||
			(!domainmedia.IsPublicReadyStatus(video.MediaStatus) &&
				video.MediaErrorCode != "publication_event_failed") {
			return nil
		}
		if result.MediaAssetID > 0 {
			return a.mediaPublication.MediaReady(ctx, result.MediaAssetID)
		}
		event := applicationvideo.NewPublishedEvent(video)
		if event == nil {
			return nil
		}
		if tracker, ok := a.videoReader.(workerReviewPublicationTracker); ok {
			ready, err := tracker.LifecyclePublicationReady(ctx, event.EventID)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
		}
		if a.publisher != nil {
			if err := a.publisher.PublishVideoPublished(ctx, event); err != nil {
				return err
			}
		}
		if tracker, ok := a.videoReader.(workerReviewPublicationTracker); ok {
			return tracker.MarkLifecyclePublicationReady(
				ctx, domainmessage.PublicationEventID(result.Case.VideoID, result.Case.ReviewVersion),
				time.Now().UTC(),
			)
		}
	case domainreview.OutcomeReject:
		if result.MediaAssetID > 0 {
			return a.mediaPublication.ProtectVideo(
				ctx, result.Case.VideoID, result.MediaAssetID, result.CoverAssetID,
			)
		}
	}
	return nil
}

type workerReviewPublicationTracker interface {
	LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
	MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
}

func (workerReviewObserver) ObserveHumanNotification(result string) {
	inframetrics.ObserveHumanReviewNotification(result)
}

type reviewMediaReadyNotifier struct {
	publication   *applicationvideo.MediaPublicationService
	videoRepo     *infravideo.Repository
	reviewService *applicationreview.Service
}

type workerVideoAdminTransitionApplier struct {
	mediaPublication interface {
		MediaReady(ctx context.Context, assetID int64) error
		ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	}
	publisher   applicationvideo.PublishedEventPublisher
	publication interface {
		LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
		MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
	}
}

func (a workerVideoAdminTransitionApplier) ApplyAdminTransition(
	ctx context.Context,
	video *domainvideo.Video,
) error {
	if video == nil {
		return nil
	}
	if video.Status == domainvideo.StatusOffline {
		if video.MediaAssetID > 0 {
			return a.mediaPublication.ProtectVideo(
				ctx, video.ID, video.MediaAssetID, video.CoverAssetID,
			)
		}
		return nil
	}
	if video.Status != domainvideo.StatusPublished {
		return nil
	}
	if video.MediaAssetID > 0 {
		return a.mediaPublication.MediaReady(ctx, video.MediaAssetID)
	}
	if event := applicationvideo.NewPublishedEvent(video); event != nil {
		if a.publication != nil {
			ready, err := a.publication.LifecyclePublicationReady(ctx, event.EventID)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
		}
		if a.publisher != nil {
			if err := a.publisher.PublishVideoPublished(ctx, event); err != nil {
				return err
			}
		}
		if a.publication != nil {
			return a.publication.MarkLifecyclePublicationReady(
				ctx, event.EventID, time.Now().UTC(),
			)
		}
	}
	return nil
}

func (n reviewMediaReadyNotifier) MediaReady(ctx context.Context, assetID int64) error {
	if err := n.publication.MediaReady(ctx, assetID); err != nil {
		return err
	}
	videos, err := n.videoRepo.ListByMediaAssetID(ctx, assetID)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if video != nil && video.Status == domainvideo.StatusPendingReview {
			if _, _, err := n.reviewService.EnsureCase(ctx, video.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n reviewMediaReadyNotifier) MediaFailed(ctx context.Context, assetID int64, profileVersion, errorCode string) error {
	return n.publication.MediaFailed(ctx, assetID, profileVersion, errorCode)
}

func (n reviewMediaReadyNotifier) MediaRepairing(ctx context.Context, assetID int64, errorCode string) error {
	return n.publication.MediaRepairing(ctx, assetID, errorCode)
}

type commentNotificationMessageWriter struct {
	service *applicationmessage.Service
}

type reviewNotificationMessageWriter struct {
	service   *applicationmessage.Service
	lifecycle *lifecycleNotificationMessageWriter
}

type lifecycleNotificationMessageWriter struct {
	service  *applicationmessage.Service
	recovery interface {
		EnsurePublication(
			ctx context.Context,
			notification domainmessage.LifecycleNotification,
		) error
	}
}

func (w *reviewNotificationMessageWriter) WriteReviewNotification(
	ctx context.Context,
	notification applicationreview.ReviewNotificationDelivery,
) error {
	if notification.MessageType == domainmessage.TypeVideoLifecycle {
		if w.lifecycle == nil {
			return applicationvideo.ErrLifecycleNotificationNotReady
		}
		return w.lifecycle.WriteLifecycleNotification(
			ctx, domainmessage.LifecycleNotification{
				EventID: notification.EventID, RecipientID: notification.RecipientID,
				VideoID: notification.VideoID, ReviewVersion: notification.ReviewVersion,
				Stage: notification.Stage, Result: notification.Result,
				ReasonCode: notification.ReasonCode, OccurredAt: notification.OccurredAt,
			}, notification.Title, notification.Content,
		)
	}
	_, err := w.service.CreateFromEvent(
		ctx, notification.RecipientID, "SYSTEM", notification.Title, notification.Content,
		notification.EventID, notification.EventID,
	)
	return err
}

func (w *lifecycleNotificationMessageWriter) WriteLifecycleNotification(
	ctx context.Context,
	notification domainmessage.LifecycleNotification,
	title string,
	content string,
) error {
	if notification.Stage == domainmessage.LifecycleStagePublished {
		if w.recovery == nil {
			return applicationvideo.ErrLifecycleNotificationNotReady
		}
		if err := w.recovery.EnsurePublication(ctx, notification); err != nil {
			return err
		}
	}
	_, err := w.service.CreateLifecycle(ctx, notification, title, content)
	return err
}

func (w *commentNotificationMessageWriter) WriteCommentNotification(ctx context.Context, notification applicationinteraction.CommentNotificationDelivery) error {
	_, err := w.service.CreateFromTargetedActorEvent(
		ctx,
		notification.RecipientID,
		notification.MessageType,
		notification.Title,
		notification.Content,
		notification.EventID,
		notification.EventID,
		notification.ActorID,
		notification.ActorNickname,
		notification.ActorAvatarURL,
		notification.VideoID,
		notification.CommentID,
		notification.RootCommentID,
	)
	return err
}

func closeSQL(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

func closeRabbitMQ(rabbitMQ *inframq.RabbitMQ) {
	if rabbitMQ != nil {
		_ = rabbitMQ.Close()
	}
}
