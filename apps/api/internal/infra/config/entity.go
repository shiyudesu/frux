package infraconfig

// Config 是应用启动配置的根结构，对应 configs/config.yaml。
type Config struct {
	Port     int            `yaml:"port"`
	JWT      JWTConfig      `yaml:"jwt"`
	Internal InternalConfig `yaml:"internal"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	RabbitMQ RabbitMQConfig `yaml:"rabbitmq"`
	Media    MediaConfig    `yaml:"media"`
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
	Endpoint         string `yaml:"endpoint"`
	PresignEndpoint  string `yaml:"presign_endpoint"`
	Region           string `yaml:"region"`
	Bucket           string `yaml:"bucket"`
	AccessKey        string `yaml:"access_key"`
	SecretKey        string `yaml:"secret_key"`
	UsePathStyle     bool   `yaml:"use_path_style"`
	AutoCreateBucket bool   `yaml:"auto_create_bucket"`
}

type MediaProcessingConfig struct {
	ProfileVersion    string `yaml:"profile_version"`
	MaxAttempts       int    `yaml:"max_attempts"`
	WorkerConcurrency int    `yaml:"worker_concurrency"`
	LeaseTTL          string `yaml:"lease_ttl"`
	CleanupDelay      string `yaml:"cleanup_delay"`
}

// JWTConfig 保存 JWT 签名密钥和访问 token 有效期。
type JWTConfig struct {
	Secret    string `yaml:"secret"`
	AccessTTL string `yaml:"access_ttl"`
}

// InternalConfig 保存内部接口服务鉴权配置。
type InternalConfig struct {
	Token string `yaml:"token"`
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

// RabbitMQConfig 保存 RabbitMQ 连接和队列配置。
type RabbitMQConfig struct {
	URL                      string `yaml:"url"`
	InteractionExchange      string `yaml:"interaction_exchange"`
	ActionChangedQueue       string `yaml:"action_changed_queue"`
	ActionChangedRouting     string `yaml:"action_changed_routing"`
	VideoExchange            string `yaml:"video_exchange"`
	VideoPublishedQueue      string `yaml:"video_published_queue"`
	VideoEmbeddingQueue      string `yaml:"video_embedding_queue"`
	VideoPublishedRouting    string `yaml:"video_published_routing"`
	ExposureExchange         string `yaml:"exposure_exchange"`
	ViewEventRecordedQueue   string `yaml:"view_event_recorded_queue"`
	ViewEventRecordedRouting string `yaml:"view_event_recorded_routing"`
	MediaExchange            string `yaml:"media_exchange"`
	MediaProcessingQueue     string `yaml:"media_processing_queue"`
	MediaProcessingRouting   string `yaml:"media_processing_routing"`
}
