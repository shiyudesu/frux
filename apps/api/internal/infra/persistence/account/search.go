package infraaccount

import (
	"context"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	"time"

	"gorm.io/gorm"
)

const userSearchRelevanceSQL = `
	CASE
		WHEN LOWER(a.nickname) = LOWER(?) THEN 1
		WHEN a.nickname ILIKE ? ESCAPE '\' THEN 2
		WHEN a.nickname ILIKE ? ESCAPE '\' THEN 3
		ELSE 3
	END
`

type userSearchModel struct {
	ID        int64
	Nickname  string
	AvatarURL string
	Bio       string
	UpdatedAt time.Time
	Relevance int
}

func (r *Repository) SearchUsers(ctx context.Context, query string, cursor *domainsearch.UserCursor, limit int) ([]*domainsearch.UserIndexItem, error) {
	var models []userSearchModel
	if err := buildUserSearchQuery(r.db.WithContext(ctx), query, cursor, limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*domainsearch.UserIndexItem, 0, len(models))
	for _, model := range models {
		items = append(items, &domainsearch.UserIndexItem{
			ID: model.ID, Nickname: model.Nickname,
			AvatarURL: model.AvatarURL, Bio: model.Bio,
			UpdatedAt: model.UpdatedAt, Relevance: model.Relevance,
		})
	}
	return items, nil
}

func buildUserSearchQuery(db *gorm.DB, query string, cursor *domainsearch.UserCursor, limit int) *gorm.DB {
	escaped := domainsearch.EscapeLikeLiteral(query)
	prefixPattern := escaped + "%"
	containsPattern := "%" + escaped + "%"
	base := db.
		Table("account AS a").
		Select(
			`a.id, a.nickname, a.avatar_url, a.bio, a.updated_at, `+
				userSearchRelevanceSQL+` AS relevance`,
			query,
			prefixPattern,
			containsPattern,
		).
		Where("a.status = ?", domainaccount.StatusNormal).
		Where("a.nickname ILIKE ? ESCAPE '\\'", containsPattern)

	ranked := db.Table("(?) AS ranked_users", base)
	if cursor != nil {
		ranked = ranked.Where(
			`(
				relevance > ?
				OR (relevance = ? AND updated_at < ?)
				OR (relevance = ? AND updated_at = ? AND id < ?)
			)`,
			cursor.Relevance,
			cursor.Relevance, cursor.UpdatedAt,
			cursor.Relevance, cursor.UpdatedAt, cursor.UserID,
		)
	}
	return ranked.
		Order("relevance ASC").
		Order("updated_at DESC").
		Order("id DESC").
		Limit(limit)
}
