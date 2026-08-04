package infraaccount

import (
	domainaccount "GCFeed/internal/domain/account"
	domainsearch "GCFeed/internal/domain/search"
	"context"
	"time"

	"gorm.io/gorm"
)

const userSearchRelevanceSQL = `
	CASE
		WHEN LOWER(a.account) = LOWER(?) THEN 1
		WHEN a.account ILIKE ? ESCAPE '\' THEN 2
		WHEN a.nickname ILIKE ? ESCAPE '\' THEN 3
		WHEN a.account ILIKE ? ESCAPE '\' THEN 4
		WHEN a.nickname ILIKE ? ESCAPE '\' THEN 5
		ELSE 5
	END
`

type userSearchModel struct {
	ID        int64
	Account   string
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
			ID: model.ID, Account: model.Account, Nickname: model.Nickname,
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
			`a.id, a.account, a.nickname, a.avatar_url, a.bio, a.updated_at, `+
				userSearchRelevanceSQL+` AS relevance`,
			query,
			prefixPattern,
			prefixPattern,
			containsPattern,
			containsPattern,
		).
		Where("a.status = ?", domainaccount.StatusNormal).
		Where(
			"(a.account ILIKE ? ESCAPE '\\' OR a.nickname ILIKE ? ESCAPE '\\')",
			containsPattern,
			containsPattern,
		)

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
