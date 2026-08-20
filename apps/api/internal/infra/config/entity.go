package infraconfig

// Config 是应用启动配置的根结构，对应 configs/config.yaml。
type Config struct {
	Port       int              `yaml:"port"`
	JWT        JWTConfig        `yaml:"jwt"`
	Security   SecurityConfig   `yaml:"security"`
	Internal   InternalConfig   `yaml:"internal"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	Kafka      KafkaConfig      `yaml:"kafka"`
	Media      MediaConfig      `yaml:"media"`
	Moderation ModerationConfig `yaml:"moderation"`
	Multimodal MultimodalConfig `yaml:"multimodal"`
	Playback   PlaybackConfig   `yaml:"playback"`
	Governance GovernanceConfig `yaml:"governance"`
	RateLimit  RateLimitConfig  `yaml:"rate_limit"`
}

type MultimodalConfig struct {
	Enabled               bool                     `yaml:"enabled"`
	VideoJobsEnabled      bool                     `yaml:"video_jobs_enabled"`
	QueryEmbeddingEnabled bool                     `yaml:"query_embedding_enabled"`
	HybridSearchEnabled   bool                     `yaml:"hybrid_search_enabled"`
	SimilarVideosEnabled  bool                     `yaml:"similar_videos_enabled"`
	MaxVideoTextRunes     int                      `yaml:"max_video_text_runes"`
	Contract              MultimodalContractConfig `yaml:"contract"`
	Provider              MultimodalProviderConfig `yaml:"provider"`
	Jobs                  MultimodalJobConfig      `yaml:"jobs"`
	Images                MultimodalImageConfig    `yaml:"images"`
	Query                 MultimodalQueryConfig    `yaml:"query"`
	Exact                 MultimodalExactConfig    `yaml:"exact"`
	Hybrid                MultimodalHybridConfig   `yaml:"hybrid"`
}

type MultimodalContractConfig struct {
	ProviderAlias            string `yaml:"provider_alias"`
	ModelAlias               string `yaml:"model_alias"`
	RevisionAlias            string `yaml:"revision_alias"`
	Dimension                int    `yaml:"dimension"`
	TextCanonicalizer        string `yaml:"text_canonicalizer"`
	FrameSamplingPolicy      string `yaml:"frame_sampling_policy"`
	ImagePreprocessingPolicy string `yaml:"image_preprocessing_policy"`
	FusionPolicy             string `yaml:"fusion_policy"`
}

type MultimodalProviderConfig struct {
	Endpoint           string `yaml:"endpoint"`
	HMACSecret         string `yaml:"hmac_secret"`
	ProtocolVersion    string `yaml:"protocol_version"`
	AllowInsecureLocal bool   `yaml:"allow_insecure_local"`
	StartupTimeout     string `yaml:"startup_timeout"`
	Deadline           string `yaml:"deadline"`
	AdmissionLimit     int    `yaml:"admission_limit"`
	MaxRequestBytes    int64  `yaml:"max_request_bytes"`
	MaxResponseBytes   int64  `yaml:"max_response_bytes"`
}

type MultimodalJobConfig struct {
	MaxAttempts       int    `yaml:"max_attempts"`
	LeaseTTL          string `yaml:"lease_ttl"`
	HeartbeatInterval string `yaml:"heartbeat_interval"`
	PollInterval      string `yaml:"poll_interval"`
	RetryBase         string `yaml:"retry_base"`
	RetryMax          string `yaml:"retry_max"`
	ShutdownTimeout   string `yaml:"shutdown_timeout"`
	TerminalRetention string `yaml:"terminal_retention"`
}

type MultimodalImageConfig struct {
	MaxCount         int      `yaml:"max_count"`
	MaxBytesEach     int      `yaml:"max_bytes_each"`
	MaxTotalBytes    int      `yaml:"max_total_bytes"`
	MaxPixelsEach    int64    `yaml:"max_pixels_each"`
	AllowedMIMETypes []string `yaml:"allowed_mime_types"`
}

type MultimodalQueryConfig struct {
	MaxRunes     int    `yaml:"max_runes"`
	CacheTTL     string `yaml:"cache_ttl"`
	CacheEntries int    `yaml:"cache_entries"`
}

type MultimodalExactConfig struct {
	MaxLimit int    `yaml:"max_limit"`
	Deadline string `yaml:"deadline"`
}

type MultimodalHybridConfig struct {
	Version             string `yaml:"version"`
	FallbackMode        string `yaml:"fallback_mode"`
	PoolLimit           int    `yaml:"pool_limit"`
	LexicalReservation  int    `yaml:"lexical_reservation"`
	SemanticReservation int    `yaml:"semantic_reservation"`
	CursorTTL           string `yaml:"cursor_ttl"`
}

type SecurityConfig struct {
	HMACSecret string `yaml:"hmac_secret"`
}

type KafkaConfig struct {
	Enabled                bool                            `yaml:"enabled"`
	Environment            string                          `yaml:"environment"`
	Brokers                []string                        `yaml:"brokers"`
	ClientID               string                          `yaml:"client_id"`
	TopicPrefix            string                          `yaml:"topic_prefix"`
	AllowLocalProvisioning bool                            `yaml:"allow_local_provisioning"`
	Authentication         KafkaAuthenticationConfig       `yaml:"authentication"`
	TLS                    KafkaTLSConfig                  `yaml:"tls"`
	Timeouts               KafkaTimeoutConfig              `yaml:"timeouts"`
	Consumer               KafkaConsumerConfig             `yaml:"consumer"`
	ProductionValidation   KafkaProductionValidationConfig `yaml:"production_validation"`
}

type KafkaAuthenticationConfig struct {
	Mechanism string `yaml:"mechanism"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type KafkaTLSConfig struct {
	Enabled            bool   `yaml:"enabled"`
	CAFile             string `yaml:"ca_file"`
	CertificateFile    string `yaml:"certificate_file"`
	PrivateKeyFile     string `yaml:"private_key_file"`
	ServerName         string `yaml:"server_name"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type KafkaTimeoutConfig struct {
	Dial     string `yaml:"dial"`
	Request  string `yaml:"request"`
	Produce  string `yaml:"produce"`
	Admin    string `yaml:"admin"`
	Shutdown string `yaml:"shutdown"`
}

type KafkaConsumerConfig struct {
	MaxPollRecords       int    `yaml:"max_poll_records"`
	MaxPollBytes         int    `yaml:"max_poll_bytes"`
	PartitionConcurrency int    `yaml:"partition_concurrency"`
	DrainTimeout         string `yaml:"drain_timeout"`
	AssignmentTimeout    string `yaml:"assignment_timeout"`
}

type KafkaProductionValidationConfig struct {
	ReplicationFactor     int  `yaml:"replication_factor"`
	MinInSyncReplicas     int  `yaml:"min_in_sync_replicas"`
	RequireAuthentication bool `yaml:"require_authentication"`
	RequireTLS            bool `yaml:"require_tls"`
}

type ModerationConfig struct {
	Mode                  string `yaml:"mode"`
	ProviderConfigVersion int    `yaml:"provider_config_version"`
	InputProfileVersion   string `yaml:"input_profile_version"`
	Endpoint              string `yaml:"endpoint"`
	HMACSecret            string `yaml:"hmac_secret"`
	AllowInsecureLocal    bool   `yaml:"allow_insecure_local"`
	Timeout               string `yaml:"timeout"`
	WorkerConcurrency     int    `yaml:"worker_concurrency"`
	MaxAttempts           int    `yaml:"max_attempts"`
	LeaseTTL              string `yaml:"lease_ttl"`
	PollInterval          string `yaml:"poll_interval"`
	SampleURLTTL          string `yaml:"sample_url_ttl"`
	SampleRetention       string `yaml:"sample_retention"`
	SamplePresignEndpoint string `yaml:"sample_presign_endpoint"`
}

type GovernanceConfig struct {
	PollInterval string `yaml:"poll_interval"`
	PollTimeout  string `yaml:"poll_timeout"`
}

type RateLimitConfig struct {
	MaxEntries     int      `yaml:"max_entries"`
	IdleTTL        string   `yaml:"idle_ttl"`
	RedisTimeout   string   `yaml:"redis_timeout"`
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type MediaConfig struct {
	Backend          string                `yaml:"backend"`
	LocalRoot        string                `yaml:"local_root"`
	PublicBaseURL    string                `yaml:"public_base_url"`
	SignedURLTTL     string                `yaml:"signed_url_ttl"`
	UploadSessionTTL string                `yaml:"upload_session_ttl"`
	Processing       MediaProcessingConfig `yaml:"processing"`
	S3               S3Config              `yaml:"s3"`
}

type S3Config struct {
	Endpoint           string `yaml:"endpoint"`
	PresignEndpoint    string `yaml:"presign_endpoint"`
	Region             string `yaml:"region"`
	Bucket             string `yaml:"bucket"`
	AccessKey          string `yaml:"access_key"`
	SecretKey          string `yaml:"secret_key"`
	UsePathStyle       bool   `yaml:"use_path_style"`
	AutoCreateBucket   bool   `yaml:"auto_create_bucket"`
	RequirePublicHTTPS bool   `yaml:"require_public_https"`
}

type MediaProcessingConfig struct {
	ProfileVersion       string `yaml:"profile_version"`
	MaxAttempts          int    `yaml:"max_attempts"`
	WorkerConcurrency    int    `yaml:"worker_concurrency"`
	LeaseTTL             string `yaml:"lease_ttl"`
	MaxDuration          string `yaml:"max_duration"`
	CommandTimeout       string `yaml:"command_timeout"`
	FFmpegPreset         string `yaml:"ffmpeg_preset"`
	CleanupDelay         string `yaml:"cleanup_delay"`
	DisableOrphanCleanup bool   `yaml:"disable_orphan_cleanup"`
}

type PlaybackConfig struct {
	Telemetry PlaybackTelemetryConfig `yaml:"telemetry"`
}

type PlaybackTelemetryConfig struct {
	Retention           string `yaml:"retention"`
	CleanupInterval     string `yaml:"cleanup_interval"`
	CleanupBatchSize    int    `yaml:"cleanup_batch_size"`
	MaxBatchesPerMinute int    `yaml:"max_batches_per_minute"`
}

type JWTKeyConfig struct {
	ID     string `yaml:"id"`
	Secret string `yaml:"secret"`
}

type JWTKeyRingConfig struct {
	ActiveKeyID string         `yaml:"active_key_id"`
	Keys        []JWTKeyConfig `yaml:"keys"`
}

// JWTConfig 保存隔离的 JWT 签名密钥环、访问 token 有效期和迁移窗口。
type JWTConfig struct {
	Secret            string           `yaml:"secret"`
	Issuer            string           `yaml:"issuer"`
	AccessTTL         string           `yaml:"access_ttl"`
	AdminAccessTTL    string           `yaml:"admin_access_ttl"`
	ClockLeeway       string           `yaml:"clock_leeway"`
	Consumer          JWTKeyRingConfig `yaml:"consumer"`
	Admin             JWTKeyRingConfig `yaml:"admin"`
	LegacySecret      string           `yaml:"legacy_secret"`
	LegacyIssuedUntil string           `yaml:"legacy_issued_until"`
	LegacyAcceptUntil string           `yaml:"legacy_accept_until"`
}

// InternalConfig 保存内部接口服务鉴权配置。
type InternalConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// DatabaseConfig 保存 PostgreSQL 连接参数。
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	SSLMode  string `yaml:"ssl_mode"`
	TimeZone string `yaml:"time_zone"`
}

// RedisConfig 保存 Redis 连接参数，用于 Feed 读缓存。
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}
