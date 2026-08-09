package infraembedding

import "time"

// VideoEmbeddingModel 映射 video_embedding 表，保存视频内容向量。
type VideoEmbeddingModel struct {
	VideoID       int64     `gorm:"column:video_id;primaryKey;autoIncrement:false;uniqueIndex:uk_video_embedding_video_model,priority:1"`
	Model         string    `gorm:"column:model;size:64;not null;primaryKey;uniqueIndex:uk_video_embedding_video_model,priority:2;index:idx_video_embedding_model_updated,priority:1"`
	Dimension     int       `gorm:"column:dimension;not null"`
	EmbeddingJSON string    `gorm:"column:embedding_json;type:jsonb;not null"`
	TextHash      string    `gorm:"column:text_hash;size:64;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_video_embedding_model_updated,priority:2"`
}

type SemanticJobModel struct {
	VideoID        int64      `gorm:"column:video_id;primaryKey"`
	Model          string     `gorm:"column:model;size:64;primaryKey"`
	TextHash       string     `gorm:"column:text_hash;size:64;not null"`
	Title          string     `gorm:"column:title;size:200;not null"`
	Description    string     `gorm:"column:description;size:2000;not null;default:''"`
	State          string     `gorm:"column:state;size:16;not null;index:idx_semantic_embedding_job_ready,priority:1"`
	Attempts       int        `gorm:"column:attempts;not null;default:0"`
	AvailableAt    time.Time  `gorm:"column:available_at;not null;index:idx_semantic_embedding_job_ready,priority:2"`
	LeaseOwner     string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil     *time.Time `gorm:"column:lease_until;index:idx_semantic_embedding_job_ready,priority:3"`
	LastErrorClass string     `gorm:"column:last_error_class;size:32;not null;default:''"`
	CompletedAt    *time.Time `gorm:"column:completed_at;index:idx_semantic_embedding_job_completed"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (SemanticJobModel) TableName() string {
	return "semantic_embedding_job"
}

// TableName 指定视频向量表名。
func (VideoEmbeddingModel) TableName() string {
	return "video_embedding"
}
