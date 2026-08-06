package infravideo

import "time"

// VideoModel 映射 video 表，保存视频主体信息和发布状态。
type VideoModel struct {
	ID             int64      `gorm:"column:id;primaryKey;autoIncrement;index:idx_video_timeline,priority:3;index:idx_video_public_timeline,priority:5;index:idx_video_author_visibility_created,priority:4"`
	AuthorID       int64      `gorm:"column:author_id;not null;index:idx_video_author_status,priority:1;uniqueIndex:uk_video_author_idempotency,priority:1"`
	Title          string     `gorm:"column:title;size:128;not null"`
	Description    string     `gorm:"column:description;size:512"`
	MediaURL       string     `gorm:"column:media_url;size:512;not null"`
	CoverURL       string     `gorm:"column:cover_url;size:512;not null"`
	MediaAssetID   *int64     `gorm:"column:media_asset_id;index:idx_video_media_asset;uniqueIndex:uk_video_media_asset"`
	CoverAssetID   *int64     `gorm:"column:cover_asset_id;index:idx_video_cover_asset;uniqueIndex:uk_video_cover_asset"`
	MediaStatus    string     `gorm:"column:media_status;size:24;not null;default:legacy_ready;index:idx_video_public_timeline,priority:3"`
	MediaErrorCode string     `gorm:"column:media_error_code;size:64;not null;default:''"`
	ReviewVersion  int        `gorm:"column:review_version;not null;default:1;check:chk_video_review_version,review_version > 0"`
	Version        int        `gorm:"column:version;not null;default:1;check:chk_video_version,version > 0"`
	Status         int        `gorm:"column:status;type:smallint;not null;default:5;check:chk_video_status,status IN (1,2,3,4,5,6);index:idx_video_author_status,priority:2;index:idx_video_status_published,priority:1;index:idx_video_timeline,priority:1;index:idx_video_public_timeline,priority:1"`
	Visibility     string     `gorm:"column:visibility;size:16;not null;default:public;index:idx_video_public_timeline,priority:2;index:idx_video_author_visibility_created,priority:2"`
	PublishedAt    *time.Time `gorm:"column:published_at;index:idx_video_status_published,priority:2;index:idx_video_timeline,priority:2;index:idx_video_public_timeline,priority:4"`
	// IdempotencyKey 与 AuthorID 组成唯一索引，用于发布接口的安全重试。
	IdempotencyKey *string   `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_video_author_idempotency,priority:2"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime;index:idx_video_author_status,priority:3;index:idx_video_author_visibility_created,priority:3"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

type LocalAssetModel struct {
	AssetURL  string    `gorm:"column:asset_url;size:512;primaryKey"`
	OwnerID   int64     `gorm:"column:owner_id;not null;index:idx_local_upload_asset_owner"`
	Kind      string    `gorm:"column:kind;size:16;not null"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (LocalAssetModel) TableName() string {
	return "local_upload_asset"
}

type UserContentStatModel struct {
	UserID            int64     `gorm:"column:user_id;primaryKey"`
	PublicWorkCount   int       `gorm:"column:public_work_count;not null;default:0"`
	PrivateWorkCount  int       `gorm:"column:private_work_count;not null;default:0"`
	ReceivedLikeCount int       `gorm:"column:received_like_count;not null;default:0"`
	CollectionCount   int       `gorm:"column:collection_count;not null;default:0"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserContentStatModel) TableName() string {
	return "user_content_stat"
}

type CollectionModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement;index:idx_video_collection_owner_updated,priority:3"`
	OwnerID        int64     `gorm:"column:owner_id;not null;index:idx_video_collection_owner_updated,priority:1;uniqueIndex:uk_video_collection_owner_idempotency,priority:1"`
	Title          string    `gorm:"column:title;size:128;not null"`
	Description    string    `gorm:"column:description;size:512"`
	Visibility     string    `gorm:"column:visibility;size:16;not null;default:private"`
	Status         int       `gorm:"column:status;type:smallint;not null;default:1"`
	IdempotencyKey *string   `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_video_collection_owner_idempotency,priority:2"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_video_collection_owner_updated,priority:2"`
}

func (CollectionModel) TableName() string {
	return "video_collection"
}

type CollectionItemModel struct {
	CollectionID int64     `gorm:"column:collection_id;primaryKey;uniqueIndex:uk_video_collection_item_collection_video,priority:1;index:idx_video_collection_item_order,priority:1"`
	VideoID      int64     `gorm:"column:video_id;primaryKey;uniqueIndex:uk_video_collection_item_collection_video,priority:2"`
	Position     int       `gorm:"column:position;not null;index:idx_video_collection_item_order,priority:2"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (CollectionItemModel) TableName() string {
	return "video_collection_item"
}

type BatchOperationModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int64     `gorm:"column:user_id;not null;uniqueIndex:uk_video_batch_operation_user_key,priority:1"`
	IdempotencyKey string    `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_video_batch_operation_user_key,priority:2"`
	Fingerprint    string    `gorm:"column:fingerprint;size:64;not null"`
	Action         string    `gorm:"column:action;size:32;not null"`
	VideoIDsJSON   string    `gorm:"column:video_ids_json;type:text;not null"`
	ResultJSON     string    `gorm:"column:result_json;type:text;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

type EnforcementActionModel struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID         int64     `gorm:"column:video_id;not null;index:idx_video_enforcement_action_video_created,priority:1"`
	ActorID         int64     `gorm:"column:actor_id;not null"`
	Action          string    `gorm:"column:action;size:32;not null"`
	ReasonCode      string    `gorm:"column:reason_code;size:64;not null"`
	Note            string    `gorm:"column:note;size:4000;not null;default:''"`
	PreviousStatus  int       `gorm:"column:previous_status;type:smallint;not null"`
	NewStatus       int       `gorm:"column:new_status;type:smallint;not null"`
	PreviousVersion int       `gorm:"column:previous_version;not null"`
	NewVersion      int       `gorm:"column:new_version;not null"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;index:idx_video_enforcement_action_video_created,priority:2"`
}

func (EnforcementActionModel) TableName() string {
	return "video_enforcement_action"
}

type AdminTransitionIntentModel struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	EventID     string     `gorm:"column:event_id;size:128;not null;uniqueIndex:uk_video_admin_transition_intent_event"`
	VideoID     int64      `gorm:"column:video_id;not null"`
	State       string     `gorm:"column:state;size:16;not null;default:'pending';index:idx_video_admin_transition_intent_pending,priority:1"`
	Attempts    int        `gorm:"column:attempts;not null;default:0"`
	AvailableAt time.Time  `gorm:"column:available_at;not null;index:idx_video_admin_transition_intent_pending,priority:2"`
	LeaseOwner  string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil  *time.Time `gorm:"column:lease_until;index:idx_video_admin_transition_intent_pending,priority:3"`
	LastError   string     `gorm:"column:last_error;size:1024;not null;default:''"`
	DeliveredAt *time.Time `gorm:"column:delivered_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

func (AdminTransitionIntentModel) TableName() string {
	return "video_admin_transition_intent"
}

func (BatchOperationModel) TableName() string {
	return "video_batch_operation"
}

// TableName 指定数据库表名。
func (VideoModel) TableName() string {
	return "video"
}

// VideoStatModel 映射 video_stat 表，保存可频繁变更的互动计数。
type VideoStatModel struct {
	VideoID       int64     `gorm:"column:video_id;primaryKey"`
	LikeCount     int       `gorm:"column:like_count;not null;default:0"`
	CommentCount  int       `gorm:"column:comment_count;not null;default:0"`
	FavoriteCount int       `gorm:"column:favorite_count;not null;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName 指定统计表名。
func (VideoStatModel) TableName() string {
	return "video_stat"
}
