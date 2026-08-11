package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
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
	infrabehaviorstream "github.com/shiyudesu/frux/internal/infra/behaviorstream"
	infracache "github.com/shiyudesu/frux/internal/infra/cache"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infradatabase "github.com/shiyudesu/frux/internal/infra/database"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	inframoderation "github.com/shiyudesu/frux/internal/infra/moderation"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
	infragovernance "github.com/shiyudesu/frux/internal/infra/persistence/governance"
	infrainteraction "github.com/shiyudesu/frux/internal/infra/persistence/interaction"
	infrakafkafailure "github.com/shiyudesu/frux/internal/infra/persistence/kafkafailure"
	infrapersistencemedia "github.com/shiyudesu/frux/internal/infra/persistence/media"
	inframessage "github.com/shiyudesu/frux/internal/infra/persistence/message"
	migration "github.com/shiyudesu/frux/internal/infra/persistence/migration"
	infrarecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
	infrarelation "github.com/shiyudesu/frux/internal/infra/persistence/relation"
	infrareview "github.com/shiyudesu/frux/internal/infra/persistence/review"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	infravideostream "github.com/shiyudesu/frux/internal/infra/videostream"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const configPath = "./configs/config.yaml"

func main() {
	if err := run(); err != nil {
		log.Printf("frux worker failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := infraconfig.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.Kafka.Enabled || len(cfg.Kafka.Brokers) == 0 {
		return errors.New("Kafka brokers are required for worker")
	}
	if cfg.Redis.Addr == "" {
		return errors.New("redis addr is required for worker")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	kafkaBackbone, err := infrakafka.StartSupervised(
		ctx, cfg.Kafka, inframetrics.KafkaObserver{}, inframetrics.KafkaObserver{},
	)
	if err != nil {
		return fmt.Errorf("init Kafka backbone: %w", err)
	}
	defer func() {
		if err := kafkaBackbone.Close(context.Background()); err != nil {
			log.Printf("close kafka backbone failed: %v", err)
		}
	}()

	sqlDB, err := infradatabase.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	defer closeSQL(sqlDB)

	gormDB, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		return fmt.Errorf("init GORM: %w", err)
	}
	if err := migration.AutoMigrate(gormDB); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	go kafkaBackbone.RunHealthObserver(ctx, 15*time.Second, 2*time.Second)
	go func() {
		if err := inframetrics.RunServer(ctx, ":9091"); err != nil {
			log.Printf("metrics server failed: %v", err)
		}
	}()

	runtimeFailures := make(chan error, 16)
	if err := startWorkers(ctx, cfg, gormDB, kafkaBackbone, runtimeFailures); err != nil {
		return fmt.Errorf("start workers: %w", err)
	}
	log.Println("frux worker is running")
	select {
	case <-ctx.Done():
		log.Println("frux worker stopped")
		return nil
	case err := <-runtimeFailures:
		stop()
		return fmt.Errorf("fatal Kafka consumer failure: %w", err)
	}
}

func startWorkers(
	ctx context.Context,
	cfg *infraconfig.Config,
	gormDB *gorm.DB,
	kafkaBackbone *infrakafka.Backbone,
	runtimeFailures chan<- error,
) error {
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
		nil,
		applicationinteraction.WithRecommendationOutcomeRecorder(recommendationRepo),
		applicationinteraction.WithActionConsumerObserver(inframetrics.BehaviorObserver{}),
	)
	if err := actionWorker.Start(ctx); err != nil {
		return err
	}
	retryOffsetStore := infrakafkafailure.NewRetryOffsetInitializationStore(gormDB)
	kafkaStarter := func(
		starterContext context.Context,
		backbone *infrakafka.Backbone,
		kafkaConfig infraconfig.KafkaConfig,
		group infrakafka.ConsumerGroupID,
		handler applicationeventstream.Handler,
		failures chan<- error,
	) error {
		return startKafkaConsumer(
			starterContext,
			backbone,
			kafkaConfig,
			group,
			handler,
			failures,
			retryOffsetStore,
		)
	}

	exposureRepo := infraexposure.New(gormDB)
	viewPublisher, err := infrabehaviorstream.NewViewPublisher(
		kafkaBackbone.Publisher(),
		inframetrics.BehaviorObserver{},
	)
	if err != nil {
		return err
	}
	outboxDispatcher := applicationexposure.NewOutboxDispatcher(
		exposureRepo,
		viewPublisher,
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
	behaviorWorker := applicationrecommendation.NewBehaviorEventWorker(
		recommendationRepo,
		nil,
		applicationrecommendation.WithBehaviorConsumerObserver(inframetrics.BehaviorObserver{}),
	)
	if err := behaviorWorker.Start(ctx); err != nil {
		return err
	}
	behaviorConsumers := orderedBehaviorKafkaConsumers(
		behaviorKafkaConsumer{
			activeGroup:   infrakafka.GroupConsumeViewActive,
			activeHandler: infrabehaviorstream.NewViewHandler(behaviorWorker),
		},
		behaviorKafkaConsumer{
			activeGroup:   infrakafka.GroupPersistActionActive,
			activeHandler: infrabehaviorstream.NewActionHandler(actionWorker),
		},
	)
	if err := superviseBehaviorKafkaConsumers(
		ctx,
		kafkaBackbone,
		cfg.Kafka,
		behaviorConsumers,
		runtimeFailures,
		kafkaStarter,
	); err != nil {
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
	durablePublicationPublisher := applicationvideo.NewDurablePublicationPublisher(videoRepo)
	publicationTransportPublisher, err := infravideostream.NewVideoPublisher(
		kafkaBackbone.Publisher(),
		inframetrics.VideoWorkflowObserver{},
	)
	if err != nil {
		return err
	}
	publicationDispatcher := applicationvideo.NewPublicationOutboxDispatcher(
		videoRepo,
		publicationTransportPublisher,
		inframetrics.VideoWorkflowObserver{},
	)
	if err := publicationDispatcher.Start(ctx); err != nil {
		return err
	}
	mediaPublication := applicationvideo.NewMediaPublicationService(
		videoRepo, mediaCatalog, durablePublicationPublisher, feedCache,
	)
	publicationRecovery := applicationvideo.NewPublicationRecoveryService(
		videoRepo, mediaPublication, durablePublicationPublisher,
	)
	reviewService := applicationreview.New(
		reviewRepo,
		applicationreview.WithObserver(workerReviewObserver{}),
		applicationreview.WithOutcomeApplier(workerReviewOutcomeApplier{
			videoReader: videoRepo, mediaPublication: mediaPublication,
			publisher: durablePublicationPublisher, cacheInvalidator: feedCache,
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
			publisher:        durablePublicationPublisher,
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
	lifecycleOwner, err := newWorkerOwner("media-lifecycle")
	if err != nil {
		return err
	}
	applicationmedia.NewVideoLifecycleWorker(
		applicationmedia.NewVideoLifecycleService(
			mediaRepo,
			workerVideoLifecycleReader{repository: videoRepo},
			workerVideoLifecycleDelivery{
				protector: mediaCatalog,
				publisher: mediaPublication,
			},
			mediaCleanup,
		),
		lifecycleOwner,
	).Start(ctx)
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
		feedRepo, nil, feedCache, feedPreheater,
		applicationvideo.WithFanoutControlReader(governanceRuntime),
	)
	if err := fanoutWorker.Start(ctx); err != nil {
		return err
	}

	embeddingRepo := infraembedding.New(gormDB)
	embeddingService := applicationembedding.New(embeddingRepo, nil)
	embeddingWorker := applicationembedding.NewVideoEmbeddingWorker(
		embeddingService, nil,
	)
	if err := embeddingWorker.Start(ctx); err != nil {
		return err
	}

	mediaWorker := applicationmedia.NewMediaProcessingWorker(
		mediaRepo, mediaProcessor, nil, leaseTTL, cfg.Media.Processing.WorkerConcurrency,
		applicationmedia.WithMediaStateNotifier(reviewMediaReadyNotifier{
			publication: mediaPublication, videoRepo: videoRepo, reviewService: reviewService,
		}),
	)
	if err := mediaWorker.Start(ctx); err != nil {
		return err
	}
	videoConsumers := []behaviorKafkaConsumer{
		{
			activeGroup:   infrakafka.GroupFeedVideoPublishedActive,
			activeHandler: infravideostream.NewFanoutHandler(fanoutWorker),
		},
		{
			activeGroup:   infrakafka.GroupEmbeddingVideoPublishedActive,
			activeHandler: infravideostream.NewEmbeddingHandler(embeddingWorker),
		},
		{
			activeGroup:   infrakafka.GroupMediaProcessingActive,
			activeHandler: infravideostream.NewMediaWakeupHandler(mediaWorker),
		},
	}
	return superviseBehaviorKafkaConsumers(
		ctx,
		kafkaBackbone,
		cfg.Kafka,
		videoConsumers,
		runtimeFailures,
		kafkaStarter,
	)
}

func superviseBehaviorKafkaConsumers(
	ctx context.Context,
	backbone *infrakafka.Backbone,
	cfg infraconfig.KafkaConfig,
	consumers []behaviorKafkaConsumer,
	runtimeFailures chan<- error,
	starter kafkaConsumerStarter,
) error {
	for _, consumer := range consumers {
		if consumer.activeGroup == "" || consumer.activeHandler == nil {
			return fmt.Errorf(
				"%w: active consumer is incomplete",
				infrakafka.ErrConsumerConfiguration,
			)
		}
	}
	go func() {
		backoff := 100 * time.Millisecond
		for ctx.Err() == nil {
			attemptCtx, cancelAttempt := context.WithCancel(ctx)
			err := startBehaviorKafkaConsumers(
				attemptCtx, backbone, cfg, consumers, runtimeFailures, starter,
			)
			if err == nil {
				return
			}
			cancelAttempt()
			inframetrics.ObserveWorkerJob("kafka_consumer_startup", 0, err)
			if !infrakafka.RetryableConsumerError(err) {
				if runtimeFailures != nil {
					select {
					case runtimeFailures <- err:
					default:
					}
				}
				return
			}
			log.Printf("Kafka consumer startup retrying: %v", err)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
	}()
	return nil
}

type behaviorKafkaConsumer struct {
	activeGroup   infrakafka.ConsumerGroupID
	activeHandler applicationeventstream.Handler
}

type kafkaConsumerStarter func(
	context.Context,
	*infrakafka.Backbone,
	infraconfig.KafkaConfig,
	infrakafka.ConsumerGroupID,
	applicationeventstream.Handler,
	chan<- error,
) error

func orderedBehaviorKafkaConsumers(
	view behaviorKafkaConsumer,
	action behaviorKafkaConsumer,
) []behaviorKafkaConsumer {
	return []behaviorKafkaConsumer{view, action}
}

func startBehaviorKafkaConsumers(
	ctx context.Context,
	backbone *infrakafka.Backbone,
	cfg infraconfig.KafkaConfig,
	consumers []behaviorKafkaConsumer,
	runtimeFailures chan<- error,
	starter kafkaConsumerStarter,
) error {
	for _, consumer := range consumers {
		if err := starter(
			ctx,
			backbone,
			cfg,
			consumer.activeGroup,
			consumer.activeHandler,
			runtimeFailures,
		); err != nil {
			return err
		}
	}
	return nil
}

func startKafkaConsumer(
	ctx context.Context,
	backbone *infrakafka.Backbone,
	cfg infraconfig.KafkaConfig,
	group infrakafka.ConsumerGroupID,
	handler applicationeventstream.Handler,
	runtimeFailures chan<- error,
	retryOffsetStore infrakafka.RetryOffsetInitializationStore,
) error {
	observer := inframetrics.KafkaObserver{}
	if err := startKafkaConsumerInstance(
		ctx,
		cfg,
		group,
		infrakafka.ConsumerStageSource,
		runtimeFailures,
		func(sessionContext context.Context) (*infrakafka.Consumer, error) {
			return infrakafka.NewConsumer(
				sessionContext,
				cfg,
				group,
				handler,
				observer,
				infrakafka.WithRetryOffsetInitializationStore(retryOffsetStore),
			)
		},
		observer,
	); err != nil {
		return err
	}
	recovery, err := infrakafka.Recovery(group)
	if err != nil {
		return err
	}
	if recovery.Policy != infrakafka.RecoveryRetryTopics {
		return nil
	}
	for _, tier := range recovery.RetryTiers {
		tier := tier
		if err := startKafkaConsumerInstance(
			ctx,
			cfg,
			group,
			infrakafka.ConsumerStage("retry_"+tier.Label),
			runtimeFailures,
			func(sessionContext context.Context) (*infrakafka.Consumer, error) {
				return infrakafka.NewRetryTierConsumer(
					sessionContext,
					cfg,
					group,
					tier.Tier,
					handler,
					observer,
					infrakafka.WithRetryOffsetInitializationStore(retryOffsetStore),
				)
			},
			observer,
		); err != nil {
			return err
		}
	}
	return nil
}

func startKafkaConsumerInstance(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	group infrakafka.ConsumerGroupID,
	stage infrakafka.ConsumerStage,
	runtimeFailures chan<- error,
	newConsumer infrakafka.ConsumerFactory,
	observer inframetrics.KafkaObserver,
) error {
	assignmentTimeout, err := time.ParseDuration(cfg.Consumer.AssignmentTimeout)
	if err != nil || assignmentTimeout <= 0 {
		return fmt.Errorf("%w: invalid assignment timeout", infrakafka.ErrConsumerConfiguration)
	}
	consumerCtx, cancelConsumer := context.WithCancel(ctx)
	ready := make(chan struct{})
	supervisorDone := make(chan error, 1)
	supervisor := infrakafka.Supervisor{
		Group: group, Stage: stage, Observer: observer, Ready: ready,
		NewConsumer: newConsumer,
	}
	go func() {
		err := supervisor.Run(consumerCtx)
		supervisorDone <- err
		if err != nil {
			log.Printf("kafka consumer %s stopped: %v", group, err)
			if runtimeFailures != nil {
				select {
				case runtimeFailures <- fmt.Errorf("kafka consumer %s: %w", group, err):
				default:
				}
			}
		}
	}()
	return waitForKafkaConsumerStartup(
		ctx,
		cancelConsumer,
		group,
		ready,
		supervisorDone,
		assignmentTimeout,
	)
}

func waitForKafkaConsumerStartup(
	ctx context.Context,
	cancel context.CancelFunc,
	group infrakafka.ConsumerGroupID,
	ready <-chan struct{},
	supervisorDone <-chan error,
	timeout time.Duration,
) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case err := <-supervisorDone:
		cancel()
		if err == nil {
			return fmt.Errorf("%w: %s exited before assignment", infrakafka.ErrConsumerSession, group)
		}
		return err
	case <-timer.C:
		cancel()
		return fmt.Errorf("%w: %s after %s", infrakafka.ErrConsumerStartupTimeout, group, timeout)
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
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
		if a.publisher != nil {
			return a.publisher.PublishVideoPublished(ctx, event)
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

type workerVideoLifecycleReader struct {
	repository interface {
		FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
	}
}

func (r workerVideoLifecycleReader) ReadVideoLifecycleState(
	ctx context.Context,
	videoID int64,
) (applicationmedia.VideoLifecycleState, error) {
	video, err := r.repository.FindByIDAnyStatus(ctx, videoID)
	if errors.Is(err, domainvideo.ErrVideoNotFound) {
		return applicationmedia.VideoLifecycleState{}, nil
	}
	if err != nil {
		return applicationmedia.VideoLifecycleState{}, err
	}
	return applicationmedia.VideoLifecycleState{
		Exists: true, Status: video.Status, Visibility: video.Visibility,
		PublicEligible: video.Status == domainvideo.StatusPublished &&
			video.Visibility == domainvideo.VisibilityPublic,
	}, nil
}

type workerVideoLifecycleDelivery struct {
	protector interface {
		ProtectVideo(context.Context, int64, int64, int64) error
	}
	publisher interface {
		MediaReady(context.Context, int64) error
	}
}

func (d workerVideoLifecycleDelivery) ProtectVideo(
	ctx context.Context,
	videoID, mediaAssetID, coverAssetID int64,
) error {
	return d.protector.ProtectVideo(ctx, videoID, mediaAssetID, coverAssetID)
}

func (d workerVideoLifecycleDelivery) RestoreVideo(
	ctx context.Context,
	_ int64,
	mediaAssetID int64,
	_ int64,
) error {
	if mediaAssetID <= 0 {
		return nil
	}
	return d.publisher.MediaReady(ctx, mediaAssetID)
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
		if a.publisher != nil {
			return a.publisher.PublishVideoPublished(ctx, event)
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
