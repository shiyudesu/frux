package infragovernance

import "time"

type RevisionModel struct {
	Key                  string     `gorm:"column:control_key;size:128;primaryKey;index:idx_governance_control_revision_created,priority:1"`
	Revision             int64      `gorm:"column:revision;primaryKey;not null"`
	ValueType            string     `gorm:"column:value_type;size:32;not null"`
	BooleanValue         bool       `gorm:"column:boolean_value;not null"`
	Reason               string     `gorm:"column:reason;size:1024;not null"`
	ExpiresAt            *time.Time `gorm:"column:expires_at;index:idx_governance_control_revision_expiry"`
	ActorID              int64      `gorm:"column:actor_id;not null"`
	RollbackFromRevision int64      `gorm:"column:rollback_from_revision;not null;default:0"`
	CreatedAt            time.Time  `gorm:"column:created_at;not null;index:idx_governance_control_revision_created,priority:2,sort:desc"`
}

func (RevisionModel) TableName() string {
	return "governance_control_revision"
}

type ActiveModel struct {
	Key       string    `gorm:"column:control_key;size:128;primaryKey"`
	Revision  int64     `gorm:"column:revision;not null;index:idx_governance_control_active_revision"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (ActiveModel) TableName() string {
	return "governance_control_active"
}
