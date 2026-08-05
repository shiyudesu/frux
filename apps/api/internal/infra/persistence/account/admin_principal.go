package infraaccount

import (
	"context"
	"errors"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"gorm.io/gorm"
)

type adminPrincipalModel struct {
	ID     int64
	Status int
	Role   string
}

func (r *Repository) FindAdminPrincipalByID(ctx context.Context, userID int64) (*domainaccount.AdminPrincipal, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}

	var model adminPrincipalModel
	err := r.db.WithContext(ctx).
		Table("account").
		Select("id, status, role").
		Where("id = ?", userID).
		Take(&model).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainaccount.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return domainaccount.RestoreAdminPrincipal(model.ID, model.Status, model.Role), nil
}
