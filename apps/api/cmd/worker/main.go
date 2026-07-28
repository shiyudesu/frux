package main

import (
	"context"
	"database/sql"
	"log"
	"os/signal"
	"syscall"
	"time"

	applicationembedding "GCFeed/internal/application/embedding"
	applicationexposure "GCFeed/internal/application/exposure"
	applicationinteraction "GCFeed/internal/application/interaction"
	applicationmedia "GCFeed/internal/application/media"
	applicationrecommendation "GCFeed/internal/application/recommendation"
	applicationvideo "GCFeed/internal/application/video"
	domainmedia "GCFeed/internal/domain/media"
	infracache "GCFeed/internal/infra/cache"
	infraconfig "GCFeed/internal/infra/config"
	infradatabase "GCFeed/internal/infra/database"
	inframedia "GCFeed/internal/infra/media"
	inframetrics "GCFeed/internal/infra/metrics"
	inframq "GCFeed/internal/infra/mq"
	infraembedding "GCFeed/internal/infra/persistence/embedding"
	infraexposure "GCFeed/internal/infra/persistence/exposure"
	infrafeed "GCFeed/internal/infra/persistence/feed"
	infrainteraction "GCFeed/internal/infra/persistence/interaction"
	infrapersistencemedia "GCFeed/internal/infra/persistence/media"
	migration "GCFeed/internal/infra/persistence/migration"
	infrarecommendation "GCFeed/internal/infra/persistence/recommendation"
	infrarelation "GCFeed/internal/infra/persistence/relation"
	infravideo "GCFeed/internal/infra/persistence/video"

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
	log.Println("gcfeed worker is running")
	<-ctx.Done()
	log.Println("gcfeed worker stopped")
}

func startWorkers(ctx context.Context, cfg *infraconfig.Config, gormDB *gorm.DB, rabbitMQ *inframq.RabbitMQ) error {
	redisClient := infracache.NewRedisClient(cfg.Redis)
	feedCache := infracache.NewFeedCache(redisClient)

	recommendationRepo := infrarecommendation.New(gormDB)
	interactionRepo := infrainteraction.New(gormDB)
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
		Name:       "GCFeed baseline H.264/AAC and DASH profile",
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
		applicationmedia.WithMediaStateNotifier(mediaPublication),
	)
	return mediaWorker.Start(ctx)
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
