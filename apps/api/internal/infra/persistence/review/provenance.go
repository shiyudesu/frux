package infrareview

import (
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	"gorm.io/gorm"
)

func EnsureMachineResultProvenance(tx *gorm.DB) error {
	if tx == nil {
		return domainreview.ErrInvalidMachineSource
	}
	if err := tx.Model(&ResultModel{}).
		Where(`LOWER(BTRIM(provider)) = ? AND
			(source_kind = ? OR source_kind IS NULL OR source_kind = '')`,
			"manual-seed", domainreview.MachineSourceLegacyUnknown).
		Updates(map[string]any{
			"source_kind":  domainreview.MachineSourceTestSeed,
			"generated_at": gorm.Expr("created_at"),
		}).Error; err != nil {
		return err
	}
	if err := tx.Model(&ResultModel{}).
		Where("source_kind IS NULL OR source_kind = ''").
		Update("source_kind", domainreview.MachineSourceLegacyUnknown).Error; err != nil {
		return err
	}
	if err := tx.Model(&ResultModel{}).
		Where("source_kind = ?", domainreview.MachineSourceLegacyUnknown).
		Update("generated_at", gorm.Expr("created_at")).Error; err != nil {
		return err
	}
	if err := tx.Model(&ResultModel{}).
		Where("rollout_mode IS NULL OR rollout_mode = ''").
		Update("rollout_mode", domainreview.ModerationModeEnforce).Error; err != nil {
		return err
	}
	if err := tx.Model(&SignalModel{}).
		Where(`LOWER(BTRIM(provider)) = ? AND
			(source_kind = ? OR source_kind IS NULL OR source_kind = '')`,
			"manual-seed", domainreview.MachineSourceLegacyUnknown).
		Updates(map[string]any{
			"source_kind":  domainreview.MachineSourceTestSeed,
			"generated_at": gorm.Expr("created_at"),
		}).Error; err != nil {
		return err
	}
	if err := tx.Model(&SignalModel{}).
		Where("source_kind IS NULL OR source_kind = ''").
		Update("source_kind", domainreview.MachineSourceLegacyUnknown).Error; err != nil {
		return err
	}
	if err := tx.Model(&SignalModel{}).
		Where("source_kind = ?", domainreview.MachineSourceLegacyUnknown).
		Update("generated_at", gorm.Expr("created_at")).Error; err != nil {
		return err
	}
	return tx.Model(&DecisionModel{}).
		Where("rollout_mode IS NULL OR rollout_mode = ''").
		Update("rollout_mode", domainreview.ModerationModeEnforce).Error
}
