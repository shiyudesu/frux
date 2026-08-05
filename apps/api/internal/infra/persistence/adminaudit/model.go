package infraadminaudit

import "time"

type EventModel struct {
	ID                 int64     `gorm:"column:id;primaryKey;autoIncrement;index:idx_admin_audit_event_created_id,priority:2,sort:desc"`
	ActorID            int64     `gorm:"column:actor_id;not null;index:idx_admin_audit_event_actor_created,priority:1"`
	Permission         string    `gorm:"column:permission;size:64;not null"`
	Action             string    `gorm:"column:action;size:64;not null;index:idx_admin_audit_event_action_created,priority:1"`
	TargetType         string    `gorm:"column:target_type;size:64;not null;index:idx_admin_audit_event_target_created,priority:1"`
	TargetID           string    `gorm:"column:target_id;size:128;not null;index:idx_admin_audit_event_target_created,priority:2"`
	Outcome            string    `gorm:"column:outcome;size:16;not null;index:idx_admin_audit_event_outcome_created,priority:1"`
	RequestID          string    `gorm:"column:request_id;size:128;not null;index:idx_admin_audit_event_request"`
	IdempotencyKeyHash string    `gorm:"column:idempotency_key_hash;size:71;not null;default:''"`
	DetailJSON         string    `gorm:"column:detail_json;type:jsonb;not null;default:'{}'"`
	CreatedAt          time.Time `gorm:"column:created_at;not null;index:idx_admin_audit_event_created_id,priority:1,sort:desc;index:idx_admin_audit_event_actor_created,priority:2,sort:desc;index:idx_admin_audit_event_action_created,priority:2,sort:desc;index:idx_admin_audit_event_target_created,priority:3,sort:desc;index:idx_admin_audit_event_outcome_created,priority:2,sort:desc"`
}

func (EventModel) TableName() string {
	return "admin_audit_event"
}
