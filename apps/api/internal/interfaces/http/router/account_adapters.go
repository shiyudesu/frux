package interfaceshttprouter

import (
	"context"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

type accountCommentModeratorReader struct {
	reader domainaccount.AdminPrincipalReader
}

func (r accountCommentModeratorReader) IsCommentModerator(
	ctx context.Context,
	userID int64,
) (bool, error) {
	principal, err := r.reader.FindAdminPrincipalByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return principal.Active() && principal.Role == domainaccount.RoleAdmin, nil
}
