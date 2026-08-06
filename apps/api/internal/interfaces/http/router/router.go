package interfaceshttprouter

import (
	"context"
	"database/sql"
	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	applicationadminauth "github.com/shiyudesu/frux/internal/application/adminauth"
	applicationdeadletter "github.com/shiyudesu/frux/internal/application/deadletter"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationfeed "github.com/shiyudesu/frux/internal/application/feed"
	applicationgovernance "github.com/shiyudesu/frux/internal/application/governance"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationlibrary "github.com/shiyudesu/frux/internal/application/library"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationplayback "github.com/shiyudesu/frux/internal/application/playback"
	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	applicationrelation "github.com/shiyudesu/frux/internal/application/relation"
	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	applicationsearch "github.com/shiyudesu/frux/internal/application/search"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	infracache "github.com/shiyudesu/frux/internal/infra/cache"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	inframediastore "github.com/shiyudesu/frux/internal/infra/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	inframq "github.com/shiyudesu/frux/internal/infra/mq"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	infraadminaudit "github.com/shiyudesu/frux/internal/infra/persistence/adminaudit"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	infrafeed "github.com/shiyudesu/frux/internal/infra/persistence/feed"
	infragovernance "github.com/shiyudesu/frux/internal/infra/persistence/governance"
	infrainteraction "github.com/shiyudesu/frux/internal/infra/persistence/interaction"
	infralibrary "github.com/shiyudesu/frux/internal/infra/persistence/library"
	infrapersistencemedia "github.com/shiyudesu/frux/internal/infra/persistence/media"
	inframessage "github.com/shiyudesu/frux/internal/infra/persistence/message"
	migration "github.com/shiyudesu/frux/internal/infra/persistence/migration"
	infraplayback "github.com/shiyudesu/frux/internal/infra/persistence/playback"
	infrarecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
	infrarelation "github.com/shiyudesu/frux/internal/infra/persistence/relation"
	infrareview "github.com/shiyudesu/frux/internal/infra/persistence/review"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	interfaceshttpaccount "github.com/shiyudesu/frux/internal/interfaces/http/account"
	interfaceshttpadmin "github.com/shiyudesu/frux/internal/interfaces/http/admin"
	interfaceshttpadminauth "github.com/shiyudesu/frux/internal/interfaces/http/adminauth"
	interfaceshttpdeadletter "github.com/shiyudesu/frux/internal/interfaces/http/deadletter"
	interfaceshttpexposure "github.com/shiyudesu/frux/internal/interfaces/http/exposure"
	interfaceshttpfeed "github.com/shiyudesu/frux/internal/interfaces/http/feed"
	interfaceshttpgovernance "github.com/shiyudesu/frux/internal/interfaces/http/governance"
	interfaceshttpinteraction "github.com/shiyudesu/frux/internal/interfaces/http/interaction"
	interfaceshttplibrary "github.com/shiyudesu/frux/internal/interfaces/http/library"
	interfaceshttpmessage "github.com/shiyudesu/frux/internal/interfaces/http/message"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpplayback "github.com/shiyudesu/frux/internal/interfaces/http/playback"
	interfaceshttprecommendation "github.com/shiyudesu/frux/internal/interfaces/http/recommendation"
	interfaceshttprelation "github.com/shiyudesu/frux/internal/interfaces/http/relation"
	interfaceshttpreview "github.com/shiyudesu/frux/internal/interfaces/http/review"
	interfaceshttpsearch "github.com/shiyudesu/frux/internal/interfaces/http/search"
	interfaceshttpupload "github.com/shiyudesu/frux/internal/interfaces/http/upload"
	interfaceshttpvideo "github.com/shiyudesu/frux/internal/interfaces/http/video"
	"log"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Register 负责后端依赖装配：数据库模型、仓储、Service、Handler、中间件和路由。
func Register(h *server.Hertz, cfg *infraconfig.Config, db *sql.DB) error {
	if err := validateAPIConfig(cfg); err != nil {
		return err
	}
	// database/sql 连接池交给 GORM 复用，避免维护两套数据库连接。
	gormDB, err := gorm.Open(gormpostgres.New(gormpostgres.Config{
		Conn: db,
	}), &gorm.Config{TranslateError: true})
	if err != nil {
		return err
	}

	// AutoMigrate 根据模型创建或补齐表结构，适合教学项目快速启动。
	if err := migration.AutoMigrate(gormDB); err != nil {
		return err
	}

	// JWT Manager 同时被账号服务用于签发 token，也被鉴权中间件用于校验 token。
	jwtManager, err := infrajwt.NewManager(
		cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.AdminAccessTTL,
	)
	if err != nil {
		return err
	}

	// 下面按领域模块组装依赖：Repository -> Service -> Handler。
	accountRepo := infraaccount.New(gormDB)
	accountService := applicationaccount.New(accountRepo, jwtManager, applicationaccount.WithProfileSettingRepository(accountRepo))
	accountHandler := interfaceshttpaccount.New(accountService)
	adminAuthService := applicationadminauth.New(accountRepo, jwtManager)
	adminAuthHandler := interfaceshttpadminauth.New(adminAuthService)
	adminAuditMetrics := adminAuditMetricsAdapter{}
	adminAuditRepo := infraadminaudit.New(gormDB, infraadminaudit.WithWriteObserver(adminAuditMetrics))
	adminAuditService := applicationadminaudit.New(
		adminAuditRepo,
		applicationadminaudit.WithAttemptObserver(adminAuditMetrics),
	)
	adminHandler := interfaceshttpadmin.New(interfaceshttpadmin.WithAuditQueryService(adminAuditService))
	governanceRegistry := domaingovernance.DefaultRegistry()
	governanceRepo := infragovernance.New(gormDB, governanceRegistry, adminAuditRepo)
	governanceService := applicationgovernance.New(governanceRegistry, governanceRepo)
	governanceHandler := interfaceshttpgovernance.New(governanceService)
	governanceRuntime := applicationgovernance.NewRuntime(
		governanceRegistry,
		domaingovernance.ProcessAPI,
		governanceRepo,
		applicationgovernance.WithRuntimeObserver(inframetrics.GovernanceObserver{}),
	)
	mediaRepo := infrapersistencemedia.New(gormDB)
	mediaStore, err := inframediastore.NewObjectStore(context.Background(), cfg.Media)
	if err != nil {
		return err
	}
	mediaURLResolver, err := inframediastore.NewURLResolver(cfg.Media.PublicBaseURL, mediaStore)
	if err != nil {
		return err
	}
	reviewPreviewSigner, err := inframediastore.NewLocalProtectedURLSigner(
		"/review-media", cfg.JWT.Secret, applicationreview.DefaultHumanPreviewTTL,
	)
	if err != nil {
		return err
	}
	reviewLocalStore, err := inframediastore.NewLocalStore(cfg.Media.LocalRoot)
	if err != nil {
		return err
	}
	reviewMediaHandler, err := interfaceshttpupload.NewProtectedMediaHandler(
		reviewLocalStore, cfg.Media.LocalRoot, "/review-media", reviewPreviewSigner,
	)
	if err != nil {
		return err
	}
	mediaCatalog := inframediastore.NewDeliveryCatalog(mediaRepo, mediaURLResolver, mediaStore)
	videoRepo := infravideo.New(
		gormDB,
		infravideo.WithMediaCatalog(mediaCatalog),
		infravideo.WithAdminAuditWriter(adminAuditRepo),
	)
	feedRepo := infrafeed.New(gormDB, infrafeed.WithMediaCatalog(mediaCatalog))
	recommendationRepo := infrarecommendation.New(gormDB)
	relationRepo := infrarelation.New(gormDB)
	recommendationOptions := []applicationrecommendation.Option{
		applicationrecommendation.WithPolicySelector(applicationrecommendation.NewPolicyService(recommendationRepo, nil)),
		applicationrecommendation.WithRequestLogRepository(recommendationRepo),
		applicationrecommendation.WithCandidateVisibilityFilter(recommendationRepo),
		applicationrecommendation.WithRecallProviders(
			applicationrecommendation.NewFreshContentProvider(recommendationRepo),
			applicationrecommendation.NewHotContentProvider(recommendationRepo),
			applicationrecommendation.NewContentSimilarityProvider(recommendationRepo, recommendationRepo, recommendationRepo),
			applicationrecommendation.NewFollowedAuthorProvider(followedAuthorRecallAdapter{source: relationRepo}, recommendationRepo),
			applicationrecommendation.NewSessionContinuationProvider(recommendationRepo, recommendationRepo),
		),
	}
	feedOptions := []applicationfeed.Option{}
	videoOptions := []applicationvideo.Option{}
	interactionOptions := []applicationinteraction.Option{}
	var feedCache *infracache.FeedCache
	var distributedRateLimiter applicationratelimit.DistributedLimiter
	var rabbitMQ *inframq.RabbitMQ
	if cfg.Redis.Addr != "" {
		redisClient := infracache.NewRedisClient(cfg.Redis)
		feedCache = infracache.NewFeedCache(redisClient)
		distributedRateLimiter = infracache.NewRedisRateLimiter(
			infracache.NewRateLimitRedisClient(cfg.Redis),
		)
		snapshotSigner, signerErr := applicationrecommendation.NewHMACSnapshotCursorSigner(cfg.JWT.Secret)
		if signerErr != nil {
			return signerErr
		}
		recommendationOptions = append(recommendationOptions,
			applicationrecommendation.WithSnapshotPagination(infracache.NewRecommendationSnapshotStore(redisClient), snapshotSigner),
		)
		feedOptions = append(feedOptions, applicationfeed.WithFeedCache(feedCache))
		interactionOptions = append(interactionOptions, applicationinteraction.WithHotScoreRecorder(feedCache))
		interactionOptions = append(interactionOptions, applicationinteraction.WithStatCache(feedCache))
		videoOptions = append(videoOptions, applicationvideo.WithVideoCacheInvalidator(feedCache))
	}
	rateLimitIdleTTL, err := time.ParseDuration(cfg.RateLimit.IdleTTL)
	if err != nil {
		return err
	}
	rateLimitRedisTimeout, err := time.ParseDuration(cfg.RateLimit.RedisTimeout)
	if err != nil {
		return err
	}
	rateLimitRegistry, err := applicationratelimit.DefaultRegistry(
		cfg.Playback.Telemetry.MaxBatchesPerMinute,
		rateLimitRedisTimeout,
	)
	if err != nil {
		return err
	}
	rateLimitService := applicationratelimit.NewService(
		rateLimitRegistry,
		applicationratelimit.NewLocalLimiter(cfg.RateLimit.MaxEntries, rateLimitIdleTTL),
		distributedRateLimiter,
		rateLimitControls{runtime: governanceRuntime},
		inframetrics.RateLimitObserver{},
		rateLimitIdleTTL,
	)
	recommendationService := applicationrecommendation.New(recommendationRepo, recommendationOptions...)
	recommendationHandler := interfaceshttprecommendation.New(recommendationService)
	feedOptions = append(feedOptions, applicationfeed.WithRecommender(recommendationService))
	feedService := applicationfeed.New(feedRepo, feedOptions...)
	feedHandler := interfaceshttpfeed.New(feedService)
	interactionRepo := infrainteraction.New(gormDB)
	messageRepo := inframessage.New(gormDB)
	messageService := applicationmessage.New(messageRepo)
	messageHandler := interfaceshttpmessage.New(messageService)
	playbackRepo := infraplayback.New(gormDB, infraplayback.WithMediaCatalog(mediaCatalog))
	playbackService := applicationplayback.New(
		playbackRepo,
		applicationplayback.WithTelemetryRepository(playbackRepo),
		applicationplayback.WithTelemetryObserver(playbackMetricsAdapter{}),
		applicationplayback.WithControlReader(governanceRuntime),
	)
	playbackHandler := interfaceshttpplayback.New(
		playbackService,
		interfaceshttpplayback.WithTelemetryRejectionRecorder(playbackMetricsAdapter{}.RecordTelemetryRejection),
	)
	telemetryRetention, err := time.ParseDuration(cfg.Playback.Telemetry.Retention)
	if err != nil {
		return err
	}
	telemetryCleanupInterval, err := time.ParseDuration(cfg.Playback.Telemetry.CleanupInterval)
	if err != nil {
		return err
	}
	telemetryCleaner := applicationplayback.NewTelemetryCleaner(
		playbackRepo,
		telemetryRetention,
		telemetryCleanupInterval,
		cfg.Playback.Telemetry.CleanupBatchSize,
	)
	uploadSessionTTL, err := time.ParseDuration(cfg.Media.UploadSessionTTL)
	if err != nil {
		return err
	}
	signedURLTTL, err := time.ParseDuration(cfg.Media.SignedURLTTL)
	if err != nil {
		return err
	}
	cleanupDelay, err := time.ParseDuration(cfg.Media.Processing.CleanupDelay)
	if err != nil {
		return err
	}
	mediaCleanupService := applicationmedia.NewCleanupService(
		mediaRepo, mediaStore, cfg.Media.Backend, cleanupDelay, cfg.Media.Processing.MaxAttempts,
	)
	if cfg.RabbitMQ.URL != "" {
		rabbitMQ, err = inframq.NewRabbitMQ(cfg.RabbitMQ)
		if err != nil {
			log.Printf("rabbitmq disabled: %v", err)
		} else {
			videoOptions = append(videoOptions, applicationvideo.WithPublishedEventPublisher(rabbitMQ))
			if feedCache != nil {
				interactionOptions = append(interactionOptions, applicationinteraction.WithAsyncActionPipeline(feedCache, rabbitMQ))
			}
		}
	}
	var deadLetterService *applicationdeadletter.Service
	var deadLetterManager *inframq.DeadLetterManager
	if rabbitMQ != nil {
		deadLetterManager = inframq.NewDeadLetterManager(rabbitMQ, cfg.RabbitMQ)
		deadLetterService = applicationdeadletter.New(
			deadLetterManager,
			deadLetterManager,
			adminAuditRepo,
			applicationdeadletter.WithObserver(inframetrics.DeadLetterObserver{}),
		)
	}
	deadLetterHandler := interfaceshttpdeadletter.New(deadLetterService)
	mediaOptions := []applicationmedia.Option{}
	mediaOptions = append(mediaOptions, applicationmedia.WithURLResolver(mediaURLResolver, signedURLTTL))
	mediaOptions = append(mediaOptions, applicationmedia.WithMediaAssetAuthorizer(videoRepo))
	if rabbitMQ != nil {
		mediaOptions = append(mediaOptions, applicationmedia.WithProcessingPublisher(rabbitMQ))
	}
	mediaService := applicationmedia.New(
		mediaRepo, mediaStore, cfg.Media.Backend, uploadSessionTTL,
		cfg.Media.Processing.ProfileVersion, cfg.Media.Processing.MaxAttempts, mediaOptions...,
	)
	uploadSessionHandler := interfaceshttpupload.NewSessionHandler(mediaService)
	messageWriter := NewMessageWriter(messageService)
	interactionOptions = append(interactionOptions, applicationinteraction.WithMessageWriter(messageWriter))
	relationOptions := []applicationrelation.Option{applicationrelation.WithMessageWriter(messageWriter)}
	mediaPublicationService := applicationvideo.NewMediaPublicationService(videoRepo, mediaCatalog, rabbitMQ, feedCache)
	reviewRepo := infrareview.New(gormDB, infrareview.WithAuditWriter(adminAuditRepo))
	reviewObserver := reviewMetricsAdapter{}
	var reviewPublisher applicationvideo.PublishedEventPublisher
	if rabbitMQ != nil {
		reviewPublisher = rabbitMQ
	}
	reviewService := applicationreview.New(
		reviewRepo,
		applicationreview.WithObserver(reviewObserver),
		applicationreview.WithHumanRepository(reviewRepo),
		applicationreview.WithHumanCursorSecret(cfg.JWT.Secret),
		applicationreview.WithHumanObserver(reviewObserver),
		applicationreview.WithHumanPreviewProvider(reviewPreviewProvider{
			repository: mediaRepo, resolver: mediaURLResolver, localSigner: reviewPreviewSigner,
		}),
		applicationreview.WithOutcomeApplier(reviewOutcomeApplier{
			videoReader: videoRepo, mediaPublication: mediaPublicationService,
			publisher: reviewPublisher, cacheInvalidator: feedCache,
		}),
	)
	reviewHandler := interfaceshttpreview.New(reviewService, reviewObserver)
	videoManagementOptions := []applicationvideo.ManagementOption{
		applicationvideo.WithManagementMediaCleanup(mediaCleanupService),
		applicationvideo.WithManagementMediaPublisher(mediaPublicationService),
	}
	if rabbitMQ != nil {
		videoManagementOptions = append(
			videoManagementOptions,
			applicationvideo.WithManagementPublishedPublisher(rabbitMQ),
		)
	}
	videoManagementService := applicationvideo.NewManagement(videoRepo, feedCache, videoManagementOptions...)
	videoOptions = append(videoOptions, applicationvideo.WithLocalAssetOwnership(videoManagementService))
	videoOptions = append(videoOptions, applicationvideo.WithMediaAssets(mediaRepo))
	videoOptions = append(videoOptions, applicationvideo.WithMediaDelivery(mediaCatalog))
	videoOptions = append(videoOptions, applicationvideo.WithMediaCleanup(mediaCleanupService))
	videoOptions = append(videoOptions, applicationvideo.WithReviewIntake(videoReviewIntakeAdapter{service: reviewService}))
	videoService := applicationvideo.New(videoRepo, videoOptions...)
	videoHandler := interfaceshttpvideo.New(videoService, videoManagementService)
	videoAdminService := applicationvideo.NewAdmin(
		videoRepo,
		cfg.JWT.Secret,
	)
	videoAdminHandler := interfaceshttpvideo.NewAdmin(videoAdminService)
	searchService := applicationsearch.New(videoRepo, accountRepo)
	searchHandler := interfaceshttpsearch.New(searchService)
	interactionService := applicationinteraction.New(interactionRepo, interactionOptions...)
	interactionHandler := interfaceshttpinteraction.New(interactionService)
	exposureRepo := infraexposure.New(gormDB)
	exposureService := applicationexposure.New(exposureRepo)
	exposureHandler := interfaceshttpexposure.New(exposureService)
	libraryRepo := infralibrary.New(gormDB)
	libraryService := applicationlibrary.New(
		actionIndexAdapter{source: interactionRepo},
		historyIndexAdapter{source: exposureRepo},
		libraryRepo,
		videoCatalogAdapter{source: videoRepo},
		privacyReaderAdapter{source: accountRepo},
		authorDisplayReaderAdapter{source: accountRepo},
		viewerActionReaderAdapter{source: interactionRepo},
	)
	libraryHandler := interfaceshttplibrary.New(libraryService)
	if feedCache != nil {
		relationOptions = append(relationOptions, applicationrelation.WithFollowFeedBackfiller(NewFollowFeedBackfiller(feedRepo, feedCache)))
	}
	relationService := applicationrelation.New(relationRepo, relationOptions...)
	relationHandler := interfaceshttprelation.New(relationService)
	uploadHandler := interfaceshttpupload.New(cfg.Media.LocalRoot, interfaceshttpupload.WithOwnershipRecorder(videoManagementService))
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	adminAuthMiddleware := interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager)
	optionalAuthMiddleware := interfaceshttpmiddleware.NewOptionalJWTAuth(jwtManager)
	rateLimitIdentity, err := interfaceshttpmiddleware.NewRateLimitIdentityResolver(cfg.RateLimit.TrustedProxies)
	if err != nil {
		return err
	}
	publicSearchRateLimit, err := interfaceshttpmiddleware.NewRateLimit(
		rateLimitService, applicationratelimit.PolicyPublicSearch, rateLimitIdentity,
	)
	if err != nil {
		return err
	}
	adminLoginRateLimit, err := interfaceshttpmiddleware.NewRateLimit(
		rateLimitService, applicationratelimit.PolicyAdminLogin, rateLimitIdentity,
	)
	if err != nil {
		return err
	}
	uploadSessionRateLimit, err := interfaceshttpmiddleware.NewRateLimit(
		rateLimitService, applicationratelimit.PolicyUploadSession, rateLimitIdentity,
	)
	if err != nil {
		return err
	}
	playbackTelemetryRateLimit, err := interfaceshttpmiddleware.NewRateLimit(
		rateLimitService,
		applicationratelimit.PolicyPlaybackTelemetry,
		rateLimitIdentity,
		interfaceshttpmiddleware.WithRateLimitRejectHook(func() {
			playbackMetricsAdapter{}.RecordTelemetryRejection(0)
		}),
	)
	if err != nil {
		return err
	}
	assetHandler, err := interfaceshttpupload.NewAssetHandler(cfg.Media.LocalRoot, "/uploads", videoManagementService)
	if err != nil {
		return err
	}

	h.GET("/health", HealthCheck)
	h.GET("/metrics", adaptor.HertzHandler(promhttp.Handler()))
	h.GET("/review-media/*filepath", reviewMediaHandler.Get)
	h.HEAD("/review-media/*filepath", reviewMediaHandler.Head)
	h.GET("/uploads/*filepath", optionalAuthMiddleware, assetHandler.Get)
	h.HEAD("/uploads/*filepath", optionalAuthMiddleware, assetHandler.Head)
	if localMediaStore, ok := mediaStore.(*inframediastore.LocalStore); ok {
		publicMediaHandler, err := interfaceshttpupload.NewPublicMediaHandler(
			localMediaStore,
			cfg.Media.LocalRoot,
			"/media",
			interfaceshttpupload.WithPublicMediaAuthorizer(videoRepo),
		)
		if err != nil {
			return err
		}
		h.GET("/media/*filepath", publicMediaHandler.Get)
		h.HEAD("/media/*filepath", publicMediaHandler.Head)
	}
	api := h.Group("/api")

	// RESTful 路由约定：路径表达资源，HTTP 方法表达动作。
	// 会话资源用于登录态：创建会话表示登录，删除当前会话表示登出。
	sessions := api.Group("/sessions")
	sessions.POST("", accountHandler.Login)
	sessions.DELETE("/current", accountHandler.Logout)

	// 用户资源承载注册、当前用户资料和用户作品列表。
	users := api.Group("/users")
	users.POST("", accountHandler.Register)
	users.GET("/me", authMiddleware, accountHandler.Me)
	users.PATCH("/me", authMiddleware, accountHandler.UpdateMe)
	users.GET("/me/profile-settings", authMiddleware, accountHandler.GetProfileSettings)
	users.PATCH("/me/profile-settings", authMiddleware, accountHandler.UpdateProfileSettings)
	users.GET("/me/videos", authMiddleware, videoHandler.ListMine)
	users.POST("/me/video-queries", authMiddleware, videoHandler.QueryMine)
	users.POST("/me/video-batch-actions", authMiddleware, videoHandler.BatchAction)
	users.GET("/me/video-collections", authMiddleware, videoHandler.ListMineCollections)
	users.POST("/me/video-collections", authMiddleware, videoHandler.CreateCollection)
	users.PATCH("/me/video-collections/:collectionId", authMiddleware, videoHandler.UpdateCollection)
	users.DELETE("/me/video-collections/:collectionId", authMiddleware, videoHandler.DeleteCollection)
	users.PUT("/me/video-collections/:collectionId/videos/:videoId", authMiddleware, videoHandler.AddCollectionVideo)
	users.DELETE("/me/video-collections/:collectionId/videos/:videoId", authMiddleware, videoHandler.RemoveCollectionVideo)
	users.GET("/me/liked-videos", authMiddleware, libraryHandler.ListLiked)
	users.GET("/me/favorite-videos", authMiddleware, libraryHandler.ListFavorites)
	users.GET("/me/watch-history", authMiddleware, libraryHandler.ListHistory)
	users.DELETE("/me/watch-history/:videoId", authMiddleware, libraryHandler.DeleteHistory)
	users.DELETE("/me/watch-history", authMiddleware, libraryHandler.ClearHistory)
	users.GET("/me/watch-later", authMiddleware, libraryHandler.ListWatchLater)
	users.PUT("/me/following/:targetUserId", authMiddleware, relationHandler.Follow)
	users.DELETE("/me/following/:targetUserId", authMiddleware, relationHandler.Unfollow)
	users.GET("/me/following/:targetUserId", authMiddleware, relationHandler.GetFollowState)
	users.GET("/me/following", authMiddleware, relationHandler.ListFollowing)
	users.GET("/me/followers", authMiddleware, relationHandler.ListFollowers)
	users.GET("/:userId", accountHandler.Get)
	users.GET("/:userId/videos", videoHandler.ListByAuthor)
	users.GET("/:userId/video-collections", videoHandler.ListPublicCollections)
	users.GET("/:userId/liked-videos", libraryHandler.ListPublicLiked)

	api.POST("/admin/auth/login", adminLoginRateLimit, adminAuthHandler.Login)
	admin := api.Group("/admin", adminAuthMiddleware)
	admin.GET(
		"/me",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewRead),
		adminHandler.Me,
	)
	admin.GET(
		"/audit-events",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionAuditRead,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionAuditQuery,
				domainadminaudit.TargetAuditTrail,
				"events",
			),
		),
		adminHandler.ListAuditEvents,
	)
	admin.GET(
		"/review/cases",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewRead),
		reviewHandler.ListHumanQueue,
	)
	admin.GET(
		"/review/cases/:caseId",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewRead),
		reviewHandler.GetHumanCase,
	)
	admin.GET(
		"/review/cases/:caseId/preview-access",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewRead),
		reviewHandler.GetHumanPreview,
	)
	admin.POST(
		"/review/cases/:caseId/claim",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewDecide),
		reviewHandler.ClaimHumanCase,
	)
	admin.POST(
		"/review/cases/:caseId/lease/resume",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewDecide),
		reviewHandler.ResumeHumanLease,
	)
	admin.POST(
		"/review/cases/:caseId/lease/renew",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewDecide),
		reviewHandler.RenewHumanLease,
	)
	admin.DELETE(
		"/review/cases/:caseId/lease",
		interfaceshttpmiddleware.NewRequireAdminPermission(accountRepo, domainaccount.PermissionReviewDecide),
		reviewHandler.ReleaseHumanLease,
	)
	admin.POST(
		"/review/cases/:caseId/decision",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionReviewDecide,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionReviewDecide,
				domainadminaudit.TargetReviewCase,
				"case",
			),
		),
		reviewHandler.DecideHumanCase,
	)
	admin.GET(
		"/videos",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionContentEnforce,
		),
		videoAdminHandler.Search,
	)
	admin.POST(
		"/videos/:videoId/enforcement",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionContentEnforce,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionContentEnforce,
				domainadminaudit.TargetVideo,
				"video",
			),
		),
		videoAdminHandler.TakeDown,
	)
	admin.POST(
		"/videos/:videoId/restoration",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionContentEnforce,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionContentRestore,
				domainadminaudit.TargetVideo,
				"video",
			),
		),
		videoAdminHandler.Restore,
	)
	governancePermission := func(
		targetID string,
	) app.HandlerFunc {
		return interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionGovernanceExecute,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionGovernanceExecute,
				domainadminaudit.TargetGovernanceControl,
				targetID,
			),
		)
	}
	admin.GET(
		"/governance/controls",
		governancePermission("controls"),
		governanceHandler.List,
	)
	admin.GET(
		"/governance/controls/:key/revisions",
		governancePermission("control"),
		governanceHandler.ListRevisions,
	)
	admin.PATCH(
		"/governance/controls/:key",
		governancePermission("control"),
		governanceHandler.Update,
	)
	admin.POST(
		"/governance/controls/:key/rollback",
		governancePermission("control"),
		governanceHandler.Rollback,
	)
	admin.GET(
		"/dead-letter-queues",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo, domainaccount.PermissionGovernanceExecute,
		),
		deadLetterHandler.List,
	)
	admin.GET(
		"/dead-letter-queues/:queue/messages",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo, domainaccount.PermissionGovernanceExecute,
		),
		deadLetterHandler.Preview,
	)
	admin.POST(
		"/dead-letter-messages/:messageId/replay",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			accountRepo,
			domainaccount.PermissionGovernanceExecute,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				adminAuditService,
				domainadminaudit.ActionDeadLetterReplay,
				domainadminaudit.TargetDeadLetterMessage,
				"message",
			),
		),
		deadLetterHandler.Replay,
	)

	// 视频是互动资源的父资源，点赞、收藏和评论都挂在具体视频下。
	videos := api.Group("/videos")
	videos.POST("", authMiddleware, videoHandler.Create)
	videos.GET("/:videoId", videoHandler.Get)
	videos.DELETE("/:videoId", authMiddleware, videoHandler.Delete)
	videos.PUT("/:videoId/like", authMiddleware, interactionHandler.Like)
	videos.DELETE("/:videoId/like", authMiddleware, interactionHandler.Unlike)
	videos.PUT("/:videoId/favorite", authMiddleware, interactionHandler.Favorite)
	videos.DELETE("/:videoId/favorite", authMiddleware, interactionHandler.Unfavorite)
	videos.PUT("/:videoId/watch-later", authMiddleware, libraryHandler.AddWatchLater)
	videos.DELETE("/:videoId/watch-later", authMiddleware, libraryHandler.RemoveWatchLater)
	videos.POST("/:videoId/comments", authMiddleware, interactionHandler.CreateComment)
	videos.POST("/:videoId/comments/:commentId/replies", authMiddleware, interactionHandler.CreateReply)
	videos.GET("/:videoId/comments", optionalAuthMiddleware, interactionHandler.ListComments)

	search := api.Group("/search")
	search.GET("/videos", publicSearchRateLimit, searchHandler.Videos)
	search.GET("/users", publicSearchRateLimit, searchHandler.Users)

	uploads := api.Group("/uploads", authMiddleware)
	uploads.POST("", uploadHandler.Create)
	uploadSessions := api.Group("/upload-sessions", authMiddleware)
	uploadSessions.POST("", uploadSessionRateLimit, uploadSessionHandler.Create)
	uploadSessions.POST("/:sessionId/complete", uploadSessionRateLimit, uploadSessionHandler.Complete)
	api.GET("/media-assets/:assetId/access", authMiddleware, uploadSessionHandler.Access)

	// Feed 暴露为条目集合，客户端通过游标和 limit 控制分页。
	api.GET("/feed-items", optionalAuthMiddleware, feedHandler.ListFeedItems)
	api.POST("/feed-queries", optionalAuthMiddleware, feedHandler.Query)
	api.POST("/recommendation-feedback", authMiddleware, recommendationHandler.CreateFeedback)
	api.POST("/video-view-events", authMiddleware, exposureHandler.CreateViewEvent)
	// 删除评论只需要评论自身 ID，所以放在顶层 comments 资源下。
	api.DELETE("/comments/:commentId", authMiddleware, interactionHandler.DeleteComment)
	api.GET("/comments/:commentId/replies", optionalAuthMiddleware, interactionHandler.ListReplies)
	api.GET("/comments/:commentId/thread", optionalAuthMiddleware, interactionHandler.GetThreadContext)
	api.PUT("/comments/:commentId/like", authMiddleware, interactionHandler.LikeComment)
	api.DELETE("/comments/:commentId/like", authMiddleware, interactionHandler.UnlikeComment)
	api.GET("/messages", authMiddleware, messageHandler.List)
	api.PATCH("/messages", authMiddleware, messageHandler.MarkRead)
	api.GET("/message-stats/unread", authMiddleware, messageHandler.CountUnread)
	api.GET("/playback-config", authMiddleware, playbackHandler.GetConfig)
	api.GET("/preload-videos", authMiddleware, playbackHandler.ListPreloadVideos)
	api.POST("/playback-qos-reports", authMiddleware, playbackHandler.CreateQoSReport)
	api.POST("/playback-telemetry-batches", authMiddleware, playbackTelemetryRateLimit, playbackHandler.CreateTelemetryBatch)

	if cfg.Internal.Enabled {
		internal := h.Group("/internal")
		internal.POST("/recommendation-candidates", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.ListCandidates)
		internal.POST("/exposure-decisions", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.DecideExposures)
		internal.POST("/exposures", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.SaveExposures)
		internal.POST("/messages", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), messageHandler.Create)
		internal.POST("/playback-qos-reports", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), playbackHandler.CreateInternalQoSReport)
		internal.PUT("/review/cases/:caseId/machine-results/:resultId", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), reviewHandler.PutMachineResult)
	}

	infrahttphertz.RegisterTrailingSlashRedirects(h)
	h.GET("/uploads", infrahttphertz.RedirectTo("/uploads/"))
	h.HEAD("/uploads", infrahttphertz.RedirectTo("/uploads/"))

	telemetryCleanerContext, stopTelemetryCleaner := context.WithCancel(context.Background())
	governanceContext, stopGovernance := context.WithCancel(context.Background())
	deadLetterContext, stopDeadLetter := context.WithCancel(context.Background())
	governancePollInterval, err := time.ParseDuration(cfg.Governance.PollInterval)
	if err != nil {
		return err
	}
	governancePollTimeout, err := time.ParseDuration(cfg.Governance.PollTimeout)
	if err != nil {
		return err
	}
	go func() {
		if err := governanceRuntime.Run(
			governanceContext, governancePollInterval, governancePollTimeout,
		); err != nil {
			log.Printf("governance snapshot poller stopped: %v", err)
		}
	}()
	go telemetryCleaner.Run(
		telemetryCleanerContext,
		playbackMetricsAdapter{}.RecordTelemetryCleanup,
		func(err error) {
			playbackMetricsAdapter{}.RecordTelemetryCleanupFailure()
			log.Printf("playback telemetry cleanup failed: %v", err)
		},
	)
	if deadLetterManager != nil {
		go func() {
			if err := deadLetterManager.RunDepthObserver(deadLetterContext, 15*time.Second); err != nil {
				log.Printf("dead-letter depth observer stopped: %v", err)
			}
		}()
	}
	h.Engine.OnShutdown = append(h.Engine.OnShutdown, func(context.Context) {
		stopTelemetryCleaner()
		stopGovernance()
		stopDeadLetter()
		if rabbitMQ != nil {
			_ = rabbitMQ.Close()
		}
	})

	return nil
}

func validateAPIConfig(cfg *infraconfig.Config) error {
	return infraconfig.ValidateAPIConfig(cfg)
}

type MessageWriter struct {
	service *applicationmessage.Service
}

type rateLimitControls struct {
	runtime *applicationgovernance.Runtime
}

func (c rateLimitControls) DistributedEnabled() bool {
	return c.runtime != nil && c.runtime.Bool(domaingovernance.RateLimitDistributedEnabled)
}

func (c rateLimitControls) EmergencyEnabled() bool {
	return c.runtime != nil && c.runtime.Bool(domaingovernance.RateLimitEmergencyEnabled)
}

func NewMessageWriter(service *applicationmessage.Service) *MessageWriter {
	return &MessageWriter{service: service}
}

func (w *MessageWriter) CreateFromEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string) (any, error) {
	return w.service.CreateFromEvent(ctx, userID, messageType, title, content, eventID, idempotencyKey)
}

func (w *MessageWriter) CreateFromActorEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string, actorID int64, actorNickname string, actorAvatarURL string) (any, error) {
	return w.service.CreateFromActorEvent(ctx, userID, messageType, title, content, eventID, idempotencyKey, actorID, actorNickname, actorAvatarURL)
}

func (w *MessageWriter) CreateFromTargetedActorEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string, actorID int64, actorNickname string, actorAvatarURL string, videoID int64, commentID int64, rootCommentID int64) (any, error) {
	return w.service.CreateFromTargetedActorEvent(ctx, userID, messageType, title, content, eventID, idempotencyKey, actorID, actorNickname, actorAvatarURL, videoID, commentID, rootCommentID)
}

func (w *MessageWriter) WriteCommentNotification(ctx context.Context, notification applicationinteraction.CommentNotificationDelivery) error {
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

type FollowFeedBackfiller struct {
	feedRepo interface {
		CountFollowers(ctx context.Context, authorID int64) (int, error)
		ListAuthorRecentVideos(ctx context.Context, authorID int64, limit int) ([]*domainfeed.FeedPageItem, error)
	}
	feedCache interface {
		AddInboxItems(ctx context.Context, authorID int64, userIDs []int64, item *domainfeed.FeedPageItem, maxLen int64) error
	}
}

func NewFollowFeedBackfiller(feedRepo interface {
	CountFollowers(ctx context.Context, authorID int64) (int, error)
	ListAuthorRecentVideos(ctx context.Context, authorID int64, limit int) ([]*domainfeed.FeedPageItem, error)
}, feedCache interface {
	AddInboxItems(ctx context.Context, authorID int64, userIDs []int64, item *domainfeed.FeedPageItem, maxLen int64) error
}) *FollowFeedBackfiller {
	return &FollowFeedBackfiller{feedRepo: feedRepo, feedCache: feedCache}
}

func (b *FollowFeedBackfiller) CountFollowers(ctx context.Context, authorID int64) (int, error) {
	return b.feedRepo.CountFollowers(ctx, authorID)
}

func (b *FollowFeedBackfiller) ListAuthorRecentVideos(ctx context.Context, authorID int64, limit int) ([]*domainfeed.FeedPageItem, error) {
	return b.feedRepo.ListAuthorRecentVideos(ctx, authorID, limit)
}

func (b *FollowFeedBackfiller) AddInboxItems(ctx context.Context, authorID int64, userIDs []int64, item *domainfeed.FeedPageItem, maxLen int64) error {
	return b.feedCache.AddInboxItems(ctx, authorID, userIDs, item, maxLen)
}

// HealthCheck 提供基础健康检查接口，方便本地调试和容器探活。
func HealthCheck(_ context.Context, c *app.RequestContext) {
	c.JSON(http.StatusOK, utils.H{
		"message": "All is well",
	})
}
