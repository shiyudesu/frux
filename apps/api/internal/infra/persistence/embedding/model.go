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

// TableName 指定视频向量表名。
func (VideoEmbeddingModel) TableName() string {
	return "video_embedding"
}

type MultimodalContractColumns struct {
	ProviderAlias            string `gorm:"column:provider_alias;size:64;not null"`
	ModelAlias               string `gorm:"column:model_alias;size:64;not null"`
	RevisionAlias            string `gorm:"column:revision_alias;size:64;not null"`
	Dimension                int    `gorm:"column:dimension;not null;check:chk_multimodal_dimension,dimension >= 32 AND dimension <= 8192"`
	TextCanonicalizer        string `gorm:"column:text_canonicalizer;size:64;not null"`
	FrameSamplingPolicy      string `gorm:"column:frame_sampling_policy;size:64;not null"`
	ImagePreprocessingPolicy string `gorm:"column:image_preprocessing_policy;size:64;not null"`
	FusionPolicy             string `gorm:"column:fusion_policy;size:64;not null"`
}

type MultimodalEmbeddingJobModel struct {
	ID                        int64  `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID                   int64  `gorm:"column:video_id;not null;uniqueIndex:uk_multimodal_job_video_contract,priority:1"`
	ContractKey               string `gorm:"column:contract_key;size:64;not null;uniqueIndex:uk_multimodal_job_video_contract,priority:2"`
	MultimodalContractColumns `gorm:"embedded"`
	SourceHash                string     `gorm:"column:source_hash;size:64;not null;index:idx_multimodal_job_source"`
	State                     string     `gorm:"column:state;size:16;not null;index:idx_multimodal_job_claim,priority:1;index:idx_multimodal_job_terminal,priority:1;check:chk_multimodal_job_state,state IN ('pending','leased','retry','succeeded','terminal')"`
	Attempts                  int        `gorm:"column:attempts;not null;default:0;check:chk_multimodal_job_attempts,attempts >= 0 AND attempts <= 10"`
	MaxAttempts               int        `gorm:"column:max_attempts;not null;check:chk_multimodal_job_max_attempts,max_attempts >= 1 AND max_attempts <= 10"`
	ClaimToken                string     `gorm:"column:claim_token;size:128;not null;default:''"`
	LeaseUntil                *time.Time `gorm:"column:lease_until;index:idx_multimodal_job_lease"`
	NextAttemptAt             time.Time  `gorm:"column:next_attempt_at;not null;index:idx_multimodal_job_claim,priority:2"`
	FailureCode               string     `gorm:"column:failure_code;size:64;not null;default:''"`
	CompletedAt               *time.Time `gorm:"column:completed_at"`
	CreatedAt                 time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at;not null;autoUpdateTime;index:idx_multimodal_job_terminal,priority:2"`
}

func (MultimodalEmbeddingJobModel) TableName() string {
	return "multimodal_embedding_job"
}

type MultimodalJobOperationModel struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	JobID        int64     `gorm:"column:job_id;not null;uniqueIndex:uk_multimodal_job_operation,priority:1;index:idx_multimodal_job_operation_created,priority:1"`
	OperationKey string    `gorm:"column:operation_key;size:128;not null;uniqueIndex:uk_multimodal_job_operation,priority:2"`
	Operation    string    `gorm:"column:operation;size:32;not null;check:chk_multimodal_job_operation,operation IN ('manual_requeue')"`
	CreatedAt    time.Time `gorm:"column:created_at;not null;autoCreateTime;index:idx_multimodal_job_operation_created,priority:2"`
}

func (MultimodalJobOperationModel) TableName() string {
	return "multimodal_job_operation"
}

type MultimodalVectorFactModel struct {
	ID                        int64  `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID                   int64  `gorm:"column:video_id;not null;uniqueIndex:uk_multimodal_fact_video_contract,priority:1"`
	ContractKey               string `gorm:"column:contract_key;size:64;not null;uniqueIndex:uk_multimodal_fact_video_contract,priority:2;index:idx_multimodal_fact_contract_updated,priority:1;index:idx_multimodal_fact_contract_source,priority:1"`
	MultimodalContractColumns `gorm:"embedded"`
	SourceHash                string    `gorm:"column:source_hash;size:64;not null;index:idx_multimodal_fact_contract_source,priority:2"`
	VectorDigest              string    `gorm:"column:vector_digest;size:64;not null"`
	EmbeddingJSON             string    `gorm:"column:embedding_json;type:jsonb;not null"`
	CreatedAt                 time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt                 time.Time `gorm:"column:updated_at;not null;autoUpdateTime;index:idx_multimodal_fact_contract_updated,priority:2"`
}

func (MultimodalVectorFactModel) TableName() string {
	return "multimodal_vector_fact"
}

type MultimodalProjectionModel struct {
	VideoID                   int64  `gorm:"column:video_id;primaryKey;autoIncrement:false"`
	ContractKey               string `gorm:"column:contract_key;size:64;primaryKey;index:idx_multimodal_projection_contract_published,priority:1;index:idx_multimodal_projection_contract_source,priority:1"`
	MultimodalContractColumns `gorm:"embedded"`
	SourceHash                string    `gorm:"column:source_hash;size:64;not null;index:idx_multimodal_projection_contract_source,priority:2"`
	VectorDigest              string    `gorm:"column:vector_digest;size:64;not null"`
	EmbeddingJSON             string    `gorm:"column:embedding_json;type:jsonb;not null"`
	PublishedAt               time.Time `gorm:"column:published_at;not null;index:idx_multimodal_projection_contract_published,priority:2,sort:desc"`
	UpdatedAt                 time.Time `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (MultimodalProjectionModel) TableName() string {
	return "multimodal_projection"
}
