package infrakafkafailure

import "time"

type ReplayAttemptModel struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	IdempotencyKeyFingerprint string `gorm:"column:idempotency_key_fingerprint;size:71;not null;uniqueIndex:uk_kafka_failure_replay_actor_key,priority:2"`
	RequestFingerprint        string `gorm:"column:request_fingerprint;size:64;not null"`

	DLQTopic     string `gorm:"column:dlq_topic;size:249;not null;index:idx_kafka_failure_replay_coordinate,priority:1"`
	DLQPartition int32  `gorm:"column:dlq_partition;not null;index:idx_kafka_failure_replay_coordinate,priority:2"`
	DLQOffset    int64  `gorm:"column:dlq_offset;not null;index:idx_kafka_failure_replay_coordinate,priority:3"`

	SourceTopic     string `gorm:"column:source_topic;size:249;not null;default:''"`
	SourcePartition int32  `gorm:"column:source_partition;not null;default:0"`
	SourceOffset    int64  `gorm:"column:source_offset;not null;default:0"`
	ConsumerGroup   string `gorm:"column:consumer_group;size:128;not null;default:''"`
	OriginalEventID string `gorm:"column:original_event_id;size:256;not null;default:''"`

	ActorID     int64  `gorm:"column:actor_id;not null;uniqueIndex:uk_kafka_failure_replay_actor_key,priority:1;index:idx_kafka_failure_replay_actor_created,priority:1"`
	ReplayID    string `gorm:"column:replay_id;size:71;not null;uniqueIndex:uk_kafka_failure_replay_id"`
	Reason      string `gorm:"column:reason;size:64;not null"`
	Status      string `gorm:"column:status;size:16;not null;index:idx_kafka_failure_replay_status_created,priority:1"`
	FailureCode string `gorm:"column:failure_code;size:64;not null;default:''"`

	RequestedAt time.Time  `gorm:"column:requested_at;not null"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null;autoCreateTime;index:idx_kafka_failure_replay_actor_created,priority:2,sort:desc;index:idx_kafka_failure_replay_status_created,priority:2,sort:desc"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ReplayAttemptModel) TableName() string {
	return "kafka_failure_replay_attempt"
}

type RetryGroupInitializationModel struct {
	Identity string `gorm:"column:identity;size:64;primaryKey"`

	Environment   string `gorm:"column:environment;size:64;not null"`
	TopicPrefix   string `gorm:"column:topic_prefix;size:128;not null"`
	ConsumerGroup string `gorm:"column:consumer_group;size:249;not null"`
	Topic         string `gorm:"column:topic;size:249;not null"`
	Version       string `gorm:"column:version;size:16;not null"`
	State         string `gorm:"column:state;size:16;not null;index:idx_kafka_retry_group_initialization_state"`

	CompletedAt *time.Time `gorm:"column:completed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (RetryGroupInitializationModel) TableName() string {
	return "kafka_retry_group_initialization"
}

type RetryGroupInitializationPartitionModel struct {
	Identity  string `gorm:"column:identity;size:64;primaryKey"`
	Partition int32  `gorm:"column:partition;primaryKey"`

	InitialOffset int64      `gorm:"column:initial_offset;not null"`
	Committed     bool       `gorm:"column:committed;not null;default:false"`
	CommittedAt   *time.Time `gorm:"column:committed_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (RetryGroupInitializationPartitionModel) TableName() string {
	return "kafka_retry_group_initialization_partition"
}
