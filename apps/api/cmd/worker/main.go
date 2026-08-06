package main

import (
	"context"
	"database/sql"
	"log"
	"os/signal"
	"syscall"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infracache "github.com/shiyudesu/frux/internal/infra/cache"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infradatabase "github.com/shiyudesu/frux/internal/infra/database"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	inframq "github.com/shiyudesu/frux/internal/infra/mq"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
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

	recommendationRepo := infrarecommendation.New(gormDB)
	interactionRepo := infrainteraction.New(gormDB)
	reviewRepo := infrareview.New(gormDB)
	messageService := applicationmessage.New(inframessage.New(gormDB))
	commentNotificationWorker := applicationinteraction.NewCommentNotificationWorker(
		interactionRepo,
		interactionRepo,
		&commentNotificationMessageWriter{service: messageService},
	)
	if err := commentNotificationWorker.Start(ctx); err != nil {
		return err
	}
	reviewNotificationWorker := applicationreview.NewReviewNotificationWorker(
		reviewRepo,
		&reviewNotificationMessageWriter{service: messageService},
		workerReviewObserver{},
	)
	if err := reviewNotificationWorker.Start(ctx); err != nil {
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
	reviewService := applicationreview.New(
		reviewRepo,
		applicationreview.WithObserver(workerReviewObserver{}),
	)
	reviewReconciler := applicationreview.NewReconciliationWorker(reviewService)
	if err := reviewReconciler.RunOnce(ctx); err != nil {
		log.Printf("initial review reconciliation failed: %v", err)
	}
	reviewReconciler.Start(ctx)
	mediaCleanup := applicationmedia.NewCleanupService(
		mediaRepo, mediaStore, cfg.Media.Backend, cleanupDelay, cfg.Media.Processing.MaxAttempts,
	)
	applicationmedia.NewCleanupWorker(mediaCleanup, "").Start(ctx)
	reconciler := applicationmedia.NewReconciler(
		mediaRepo, mediaStore, mediaPublication, cfg.Media.Backend,
		cfg.Media.Processing.ProfileVersion, cfg.Media.Processing.MaxAttempts, cleanupDelay,
	)
	applicationmedia.NewReconciliationWorker(reconciler).Start(ctx)
	feedRepo := infrafeed.New(gormDB, infrafeed.WithMediaCatalog(mediaCatalog))
	feedPreheater := applicationvideo.NewFeedPreheater(feedRepo, feedCache)
	fanoutWorker := applicationvideo.NewFanoutWorker(feedRepo, rabbitMQ, feedCache, feedPreheater)
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

type workerReviewObserver struct{}

func (workerReviewObserver) Observe(stage, result string) {
	inframetrics.ObserveReview(stage, result)
}

func (workerReviewObserver) ObserveHumanNotification(result string) {
	inframetrics.ObserveHumanReviewNotification(result)
}

type reviewMediaReadyNotifier struct {
	publication   *applicationvideo.MediaPublicationService
	videoRepo     *infravideo.Repository
	reviewService *applicationreview.Service
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

func (n reviewMediaReadyNotifier) MediaFailed(ctx context.Context, assetID int64, errorCode string) error {
	return n.publication.MediaFailed(ctx, assetID, errorCode)
}

type commentNotificationMessageWriter struct {
	service *applicationmessage.Service
}

type reviewNotificationMessageWriter struct {
	service *applicationmessage.Service
}

func (w *reviewNotificationMessageWriter) WriteReviewNotification(
	ctx context.Context,
	notification applicationreview.ReviewNotificationDelivery,
) error {
	_, err := w.service.CreateFromEvent(
		ctx, notification.RecipientID, "SYSTEM", notification.Title, notification.Content,
		notification.EventID, notification.EventID,
	)
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
