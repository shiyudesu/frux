package inframedia

import "time"

type AssetModel struct {
	ID               int64      `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID          int64      `gorm:"column:owner_id;not null;index:idx_media_asset_owner_kind,priority:1"`
	Kind             string     `gorm:"column:kind;size:16;not null;index:idx_media_asset_owner_kind,priority:2"`
	StorageBackend   string     `gorm:"column:storage_backend;size:16;not null;uniqueIndex:uk_media_asset_backend_key,priority:1"`
	ObjectKey        string     `gorm:"column:object_key;size:1024;not null;uniqueIndex:uk_media_asset_backend_key,priority:2"`
	ContentType      string     `gorm:"column:content_type;size:128;not null"`
	SizeBytes        int64      `gorm:"column:size_bytes;not null"`
	ChecksumSHA256   string     `gorm:"column:checksum_sha256;size:64;not null;index:idx_media_asset_checksum"`
	Width            int        `gorm:"column:width;not null;default:0"`
	Height           int        `gorm:"column:height;not null;default:0"`
	DurationMS       int64      `gorm:"column:duration_ms;not null;default:0"`
	VideoCodec       string     `gorm:"column:video_codec;size:64;not null;default:''"`
	AudioCodec       string     `gorm:"column:audio_codec;size:64;not null;default:''"`
	State            string     `gorm:"column:state;size:24;not null;index:idx_media_asset_state_updated,priority:1"`
	ErrorCode        string     `gorm:"column:error_code;size:64;not null;default:''"`
	CreatedAt        time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;autoUpdateTime;index:idx_media_asset_state_updated,priority:2"`
	LastReconciledAt *time.Time `gorm:"column:last_reconciled_at;index:idx_media_asset_reconcile"`
}

func (AssetModel) TableName() string {
	return "media_asset"
}

type VariantModel struct {
	ID                 int64     `gorm:"column:id;primaryKey;autoIncrement"`
	AssetID            int64     `gorm:"column:asset_id;not null;index:idx_media_variant_asset_order,priority:1"`
	VideoID            *int64    `gorm:"column:video_id;index:idx_media_variant_video_order,priority:1"`
	ProfileVersion     string    `gorm:"column:profile_version;size:64;not null"`
	SourceType         string    `gorm:"column:source_type;size:16;not null"`
	Format             string    `gorm:"column:format;size:32;not null"`
	Codec              string    `gorm:"column:codec;size:64;not null;default:''"`
	AudioCodec         string    `gorm:"column:audio_codec;size:64;not null;default:''"`
	Width              int       `gorm:"column:width;not null;default:0"`
	Height             int       `gorm:"column:height;not null;default:0"`
	Bitrate            int       `gorm:"column:bitrate;not null;default:0"`
	Quality            string    `gorm:"column:quality;size:32;not null;default:''"`
	ObjectKey          string    `gorm:"column:object_key;size:1024;not null;uniqueIndex:uk_media_variant_object_key"`
	ExposureGeneration *string   `gorm:"column:exposure_generation;size:32;index:idx_media_variant_exposure,priority:1"`
	Role               string    `gorm:"column:role;size:24;not null"`
	SortOrder          int       `gorm:"column:sort_order;not null;default:0;index:idx_media_variant_asset_order,priority:2;index:idx_media_variant_video_order,priority:2"`
	State              string    `gorm:"column:state;size:24;not null;index:idx_media_variant_state"`
	ChecksumSHA256     string    `gorm:"column:checksum_sha256;size:64;not null"`
	SizeBytes          int64     `gorm:"column:size_bytes;not null"`
	Public             bool      `gorm:"column:public;not null;default:false;index:idx_media_variant_exposure,priority:2"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (VariantModel) TableName() string {
	return "media_variant"
}

type ProcessingProfileModel struct {
	Version    string    `gorm:"column:version;size:64;primaryKey"`
	Name       string    `gorm:"column:name;size:128;not null"`
	ConfigJSON string    `gorm:"column:config_json;type:jsonb;not null"`
	Active     bool      `gorm:"column:active;not null;default:false;index:idx_media_processing_profile_active"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProcessingProfileModel) TableName() string {
	return "media_processing_profile"
}

type ProcessingJobModel struct {
	ID                int64      `gorm:"column:id;primaryKey;autoIncrement"`
	AssetID           int64      `gorm:"column:asset_id;not null;uniqueIndex:uk_media_processing_job_asset_profile,priority:1"`
	ProfileVersion    string     `gorm:"column:profile_version;size:64;not null;uniqueIndex:uk_media_processing_job_asset_profile,priority:2"`
	State             string     `gorm:"column:state;size:24;not null;index:idx_media_processing_job_ready,priority:1"`
	Attempts          int        `gorm:"column:attempts;not null;default:0"`
	MaxAttempts       int        `gorm:"column:max_attempts;not null"`
	ErrorCode         string     `gorm:"column:error_code;size:64;not null;default:''"`
	ErrorMessage      string     `gorm:"column:error_message;size:512;not null;default:''"`
	LeaseOwner        string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil        *time.Time `gorm:"column:lease_until;index:idx_media_processing_job_lease"`
	ProcessingStep    string     `gorm:"column:processing_step;size:24;not null;default:'waiting'"`
	ProgressBPS       *int       `gorm:"column:progress_bps;check:chk_media_processing_progress_bps,progress_bps IS NULL OR (progress_bps >= 0 AND progress_bps <= 10000)"`
	ProgressUpdatedAt *time.Time `gorm:"column:progress_updated_at"`
	NextAttemptAt     time.Time  `gorm:"column:next_attempt_at;not null;index:idx_media_processing_job_ready,priority:2"`
	CompletedAt       *time.Time `gorm:"column:completed_at"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProcessingJobModel) TableName() string {
	return "media_processing_job"
}

type ProcessingRetryReceiptModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ActorID        int64     `gorm:"column:actor_id;not null;uniqueIndex:uk_media_processing_retry_actor_key,priority:1"`
	IdempotencyKey string    `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_media_processing_retry_actor_key,priority:2"`
	Fingerprint    string    `gorm:"column:fingerprint;size:64;not null"`
	JobID          int64     `gorm:"column:job_id;not null;index:idx_media_processing_retry_job"`
	ReasonCode     string    `gorm:"column:reason_code;size:64;not null"`
	Note           string    `gorm:"column:note;size:2000;not null;default:''"`
	ResultJSON     string    `gorm:"column:result_json;type:jsonb;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (ProcessingRetryReceiptModel) TableName() string {
	return "media_processing_retry_receipt"
}

type ProcessingRetryOutboxModel struct {
	EventID     string     `gorm:"column:event_id;size:128;primaryKey"`
	JobID       int64      `gorm:"column:job_id;not null;index:idx_media_processing_retry_outbox_job"`
	AssetID     int64      `gorm:"column:asset_id;not null"`
	State       string     `gorm:"column:state;size:24;not null;index:idx_media_processing_retry_outbox_ready,priority:1"`
	Attempts    int        `gorm:"column:attempts;not null;default:0"`
	AvailableAt time.Time  `gorm:"column:available_at;not null;index:idx_media_processing_retry_outbox_ready,priority:2"`
	LeaseOwner  string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil  *time.Time `gorm:"column:lease_until;index:idx_media_processing_retry_outbox_lease"`
	LastError   string     `gorm:"column:last_error;size:1024;not null;default:''"`
	DeliveredAt *time.Time `gorm:"column:delivered_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

func (ProcessingRetryOutboxModel) TableName() string {
	return "media_processing_retry_outbox"
}

type UploadSessionModel struct {
	ID                 string     `gorm:"column:id;size:64;primaryKey"`
	OwnerID            int64      `gorm:"column:owner_id;not null;uniqueIndex:uk_media_upload_session_owner_key,priority:1;index:idx_media_upload_session_owner_state,priority:1"`
	Kind               string     `gorm:"column:kind;size:16;not null"`
	StorageBackend     string     `gorm:"column:storage_backend;size:16;not null"`
	ObjectKey          string     `gorm:"column:object_key;size:1024;not null;uniqueIndex:uk_media_upload_session_object_key"`
	ContentType        string     `gorm:"column:content_type;size:128;not null"`
	SizeBytes          int64      `gorm:"column:size_bytes;not null"`
	ChecksumSHA256     string     `gorm:"column:checksum_sha256;size:64;not null"`
	State              string     `gorm:"column:state;size:24;not null;index:idx_media_upload_session_owner_state,priority:2;index:idx_media_upload_session_expiry,priority:1"`
	IdempotencyKey     *string    `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_media_upload_session_owner_key,priority:2"`
	RequestFingerprint string     `gorm:"column:request_fingerprint;size:64;not null"`
	ExpiresAt          time.Time  `gorm:"column:expires_at;not null;index:idx_media_upload_session_expiry,priority:2"`
	CompletedAssetID   *int64     `gorm:"column:completed_asset_id"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (UploadSessionModel) TableName() string {
	return "media_upload_session"
}

type CleanupTaskModel struct {
	ID             int64      `gorm:"column:id;primaryKey;autoIncrement"`
	AssetID        int64      `gorm:"column:asset_id;not null;index:idx_media_cleanup_asset"`
	StorageBackend string     `gorm:"column:storage_backend;size:16;not null;uniqueIndex:uk_media_cleanup_backend_key,priority:1"`
	ObjectKey      string     `gorm:"column:object_key;size:1024;not null;uniqueIndex:uk_media_cleanup_backend_key,priority:2"`
	State          string     `gorm:"column:state;size:24;not null;index:idx_media_cleanup_ready,priority:1"`
	Attempts       int        `gorm:"column:attempts;not null;default:0"`
	MaxAttempts    int        `gorm:"column:max_attempts;not null"`
	ErrorMessage   string     `gorm:"column:error_message;size:512;not null;default:''"`
	NotBefore      time.Time  `gorm:"column:not_before;not null;index:idx_media_cleanup_ready,priority:2"`
	LeaseOwner     string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil     *time.Time `gorm:"column:lease_until;index:idx_media_cleanup_lease"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (CleanupTaskModel) TableName() string {
	return "media_cleanup_task"
}

type VideoLifecycleTaskModel struct {
	ID                 int64      `gorm:"column:id;primaryKey;autoIncrement"`
	DedupeKey          string     `gorm:"column:dedupe_key;size:256;not null;uniqueIndex:uk_media_video_lifecycle_dedupe"`
	VideoID            int64      `gorm:"column:video_id;not null;index:idx_media_video_lifecycle_video"`
	MediaAssetID       int64      `gorm:"column:media_asset_id;not null;default:0"`
	CoverAssetID       int64      `gorm:"column:cover_asset_id;not null;default:0"`
	Action             string     `gorm:"column:action;size:16;not null"`
	RequiredStatus     int        `gorm:"column:required_status;not null;default:0"`
	RequiredVisibility string     `gorm:"column:required_visibility;size:16;not null;default:''"`
	State              string     `gorm:"column:state;size:24;not null;index:idx_media_video_lifecycle_ready,priority:1"`
	Attempts           int        `gorm:"column:attempts;not null;default:0"`
	MaxAttempts        int        `gorm:"column:max_attempts;not null"`
	ErrorCode          string     `gorm:"column:error_code;size:64;not null;default:''"`
	LeaseOwner         string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil         *time.Time `gorm:"column:lease_until;index:idx_media_video_lifecycle_lease"`
	NextAttemptAt      time.Time  `gorm:"column:next_attempt_at;not null;index:idx_media_video_lifecycle_ready,priority:2"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (VideoLifecycleTaskModel) TableName() string {
	return "media_video_lifecycle_task"
}
