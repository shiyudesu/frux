package infrarecommendation

import (
	"encoding/json"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsureInitialPolicies inserts only missing bootstrap versions. It is safe to
// call from both API and worker startup: existing operator rows are never read,
// updated, enabled, or disabled by this function.
func EnsureInitialPolicies(db *gorm.DB) error {
	policies, err := domainrecommendation.InitialRecommendationPolicies(time.Now().UTC())
	if err != nil {
		return err
	}
	for _, policy := range policies {
		configJSON, err := json.Marshal(policy.Config)
		if err != nil {
			return err
		}
		model := PolicyModel{
			Scene: policy.Scene, Version: policy.Version, Enabled: policy.Enabled, ConfigJSON: string(configJSON),
			CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
		}
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "scene"}, {Name: "version"}},
			DoNothing: true,
		}).Create(&model).Error; err != nil {
			return err
		}
	}
	return nil
}
