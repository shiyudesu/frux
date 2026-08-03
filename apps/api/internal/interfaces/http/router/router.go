package interfaceshttprouter

import (
	applicationaccount "GCFeed/internal/application/account"
	applicationexposure "GCFeed/internal/application/exposure"
	applicationfeed "GCFeed/internal/application/feed"
	applicationinteraction "GCFeed/internal/application/interaction"
	applicationlibrary "GCFeed/internal/application/library"
	applicationmedia "GCFeed/internal/application/media"
	applicationmessage "GCFeed/internal/application/message"
	applicationplayback "GCFeed/internal/application/playback"
	applicationrecommendation "GCFeed/internal/application/recommendation"
	applicationrelation "GCFeed/internal/application/relation"
	applicationvideo "GCFeed/internal/application/video"
	domainfeed "GCFeed/internal/domain/feed"
	infracache "GCFeed/internal/infra/cache"
	infraconfig "GCFeed/internal/infra/config"
	infrahttphertz "GCFeed/internal/infra/httphertz"
	infrajwt "GCFeed/internal/infra/jwt"
	inframediastore "GCFeed/internal/infra/media"
	inframq "GCFeed/internal/infra/mq"
	infraaccount "GCFeed/internal/infra/persistence/account"
	infraexposure "GCFeed/internal/infra/persistence/exposure"
	infrafeed "GCFeed/internal/infra/persistence/feed"
	infrainteraction "GCFeed/internal/infra/persistence/interaction"
	infralibrary "GCFeed/internal/infra/persistence/library"
	infrapersistencemedia "GCFeed/internal/infra/persistence/media"
	inframessage "GCFeed/internal/infra/persistence/message"
	migration "GCFeed/internal/infra/persistence/migration"
	infraplayback "GCFeed/internal/infra/persistence/playback"
	infrarecommendation "GCFeed/internal/infra/persistence/recommendation"
	infrarelation "GCFeed/internal/infra/persistence/relation"
	infravideo "GCFeed/internal/infra/persistence/video"
	interfaceshttpaccount "GCFeed/internal/interfaces/http/account"
	interfaceshttpexposure "GCFeed/internal/interfaces/http/exposure"
	interfaceshttpfeed "GCFeed/internal/interfaces/http/feed"
	interfaceshttpinteraction "GCFeed/internal/interfaces/http/interaction"
	interfaceshttplibrary "GCFeed/internal/interfaces/http/library"
	interfaceshttpmessage "GCFeed/internal/interfaces/http/message"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttpplayback "GCFeed/internal/interfaces/http/playback"
	interfaceshttprecommendation "GCFeed/internal/interfaces/http/recommendation"
	interfaceshttprelation "GCFeed/internal/interfaces/http/relation"
	interfaceshttpupload "GCFeed/internal/interfaces/http/upload"
	interfaceshttpvideo "GCFeed/internal/interfaces/http/video"
	"context"
	"database/sql"
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
	jwtManager, err := infrajwt.NewManager(cfg.JWT.Secret, cfg.JWT.AccessTTL)
	if err != nil {
		return err
	}

	// 下面按领域模块组装依赖：Repository -> Service -> Handler。
	accountRepo := infraaccount.New(gormDB)
	accountService := applicationaccount.New(accountRepo, jwtManager, applicationaccount.WithProfileSettingRepository(accountRepo))
	accountHandler := interfaceshttpaccount.New(accountService)
	mediaRepo := infrapersistencemedia.New(gormDB)
	mediaStore, err := inframediastore.NewObjectStore(context.Background(), cfg.Media)
	if err != nil {
		return err
	}
	mediaURLResolver, err := inframediastore.NewURLResolver(cfg.Media.PublicBaseURL, mediaStore)
	if err != nil {
		return err
	}
	mediaCatalog := inframediastore.NewDeliveryCatalog(mediaRepo, mediaURLResolver, mediaStore)
	videoRepo := infravideo.New(gormDB, infravideo.WithMediaCatalog(mediaCatalog))
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
	var rabbitMQ *inframq.RabbitMQ
	if cfg.Redis.Addr != "" {
		redisClient := infracache.NewRedisClient(cfg.Redis)
		feedCache = infracache.NewFeedCache(redisClient)
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
	)
	playbackHandler := interfaceshttpplayback.New(
		playbackService,
		interfaceshttpplayback.WithTelemetryRateLimit(cfg.Playback.Telemetry.MaxBatchesPerMinute),
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
	videoManagementService := applicationvideo.NewManagement(
		videoRepo, feedCache,
		applicationvideo.WithManagementMediaCleanup(mediaCleanupService),
		applicationvideo.WithManagementMediaPublisher(mediaPublicationService),
	)
	videoOptions = append(videoOptions, applicationvideo.WithLocalAssetOwnership(videoManagementService))
	videoOptions = append(videoOptions, applicationvideo.WithMediaAssets(mediaRepo))
	videoOptions = append(videoOptions, applicationvideo.WithMediaDelivery(mediaCatalog))
	videoOptions = append(videoOptions, applicationvideo.WithMediaCleanup(mediaCleanupService))
	videoService := applicationvideo.New(videoRepo, videoOptions...)
	videoHandler := interfaceshttpvideo.New(videoService, videoManagementService)
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
	)
	libraryHandler := interfaceshttplibrary.New(libraryService)
	if feedCache != nil {
		relationOptions = append(relationOptions, applicationrelation.WithFollowFeedBackfiller(NewFollowFeedBackfiller(feedRepo, feedCache)))
	}
	relationService := applicationrelation.New(relationRepo, relationOptions...)
	relationHandler := interfaceshttprelation.New(relationService)
	uploadHandler := interfaceshttpupload.New(cfg.Media.LocalRoot, interfaceshttpupload.WithOwnershipRecorder(videoManagementService))
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	optionalAuthMiddleware := interfaceshttpmiddleware.NewOptionalJWTAuth(jwtManager)
	assetHandler, err := interfaceshttpupload.NewAssetHandler(cfg.Media.LocalRoot, "/uploads", videoManagementService)
	if err != nil {
		return err
	}

	h.GET("/health", HealthCheck)
	h.GET("/metrics", adaptor.HertzHandler(promhttp.Handler()))
	h.GET("/uploads/*filepath", optionalAuthMiddleware, assetHandler.Get)
	h.HEAD("/uploads/*filepath", optionalAuthMiddleware, assetHandler.Head)
	if localMediaStore, ok := mediaStore.(*inframediastore.LocalStore); ok {
		publicMediaHandler, err := interfaceshttpupload.NewPublicMediaHandler(localMediaStore, cfg.Media.LocalRoot, "/media")
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

	uploads := api.Group("/uploads", authMiddleware)
	uploads.POST("", uploadHandler.Create)
	uploadSessions := api.Group("/upload-sessions", authMiddleware)
	uploadSessions.POST("", uploadSessionHandler.Create)
	uploadSessions.POST("/:sessionId/complete", uploadSessionHandler.Complete)
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
	api.POST("/playback-telemetry-batches", authMiddleware, playbackHandler.CreateTelemetryBatch)

	if cfg.Internal.Enabled {
		internal := h.Group("/internal")
		internal.POST("/recommendation-candidates", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.ListCandidates)
		internal.POST("/exposure-decisions", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.DecideExposures)
		internal.POST("/exposures", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), recommendationHandler.SaveExposures)
		internal.POST("/messages", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), messageHandler.Create)
		internal.POST("/playback-qos-reports", interfaceshttpmiddleware.NewInternalTokenAuth(cfg.Internal.Token), playbackHandler.CreateInternalQoSReport)
	}

	infrahttphertz.RegisterTrailingSlashRedirects(h)
	h.GET("/uploads", infrahttphertz.RedirectTo("/uploads/"))
	h.HEAD("/uploads", infrahttphertz.RedirectTo("/uploads/"))

	telemetryCleanerContext, stopTelemetryCleaner := context.WithCancel(context.Background())
	go telemetryCleaner.Run(
		telemetryCleanerContext,
		playbackMetricsAdapter{}.RecordTelemetryCleanup,
		func(err error) {
			playbackMetricsAdapter{}.RecordTelemetryCleanupFailure()
			log.Printf("playback telemetry cleanup failed: %v", err)
		},
	)
	h.Engine.OnShutdown = append(h.Engine.OnShutdown, func(context.Context) {
		stopTelemetryCleaner()
	})

	return nil
}

func validateAPIConfig(cfg *infraconfig.Config) error {
	return infraconfig.ValidateAPIConfig(cfg)
}

type MessageWriter struct {
	service *applicationmessage.Service
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
