package infrareview

import (
	"encoding/json"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureInitialPolicy(db *gorm.DB) error {
	policy, err := domainreview.InitialPolicy(time.Now().UTC())
	if err != nil {
		return err
	}
	configJSON, err := json.Marshal(policy.Config)
	if err != nil {
		return err
	}
	model := PolicyModel{
		Version: policy.Version, Enabled: policy.Enabled, ConfigJSON: string(configJSON),
		CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "version"}}, DoNothing: true,
	}).Create(&model).Error
}

func EnsurePolicyIndexes(db *gorm.DB) error {
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uk_review_policy_single_enabled
		ON review_policy ((enabled))
		WHERE enabled = TRUE
	`).Error
}
