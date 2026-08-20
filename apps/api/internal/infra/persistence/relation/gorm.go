package infrarelation

import (
	"context"
	"errors"
	"fmt"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainrelation "github.com/shiyudesu/frux/internal/domain/relation"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

type relationUserModel struct {
	UserID     int64
	Nickname   string
	AvatarURL  string
	Bio        string
	FollowedAt time.Time
}

type relationUserProfileModel struct {
	UserID    int64
	Nickname  string
	AvatarURL string
	Bio       string
}

type mutualRecipientModel struct {
	UserID     int64
	Nickname   string
	AvatarURL  string
	Bio        string
	FollowedAt time.Time
}

type followStateModel struct {
	TargetExists bool
	Following    bool
}

// New 创建关系仓储实现。
func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// SetFollow 设置关注或取关状态，并在同一事务中维护双方计数。
func (r *Repository) SetFollow(ctx context.Context, userID int64, targetUserID int64, active bool, idempotencyKey string) (*domainrelation.Follow, *domainrelation.RelationStat, *domainrelation.RelationStat, error) {
	return r.setFollow(ctx, userID, targetUserID, active, idempotencyKey, nil)
}

func (r *Repository) SetFollowWithRecommendation(ctx context.Context, userID int64, targetUserID int64, active bool, idempotencyKey string, outcome *domainrelation.RecommendationOutcomeContext) (*domainrelation.Follow, *domainrelation.RelationStat, *domainrelation.RelationStat, error) {
	if _, err := domainrelation.NewRecommendationOutcomeContext(outcomeRequestID(outcome), outcomeVideoID(outcome)); err != nil {
		return nil, nil, nil, err
	}
	return r.setFollow(ctx, userID, targetUserID, active, idempotencyKey, outcome)
}

func (r *Repository) setFollow(ctx context.Context, userID int64, targetUserID int64, active bool, idempotencyKey string, outcome *domainrelation.RecommendationOutcomeContext) (*domainrelation.Follow, *domainrelation.RelationStat, *domainrelation.RelationStat, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)

	var follow FollowModel
	var userStat RelationStatModel
	var targetStat RelationStatModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockNormalUser(tx, userID); err != nil {
			return err
		}
		if err := lockNormalUser(tx, targetUserID); err != nil {
			return err
		}

		if err := ensureStat(tx, userID); err != nil {
			return err
		}
		if err := ensureStat(tx, targetUserID); err != nil {
			return err
		}

		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND target_user_id = ?", userID, targetUserID).
			Take(&follow).
			Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		nextStatus := statusFromActive(active)
		delta := 0
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			follow = FollowModel{
				UserID:         userID,
				TargetUserID:   targetUserID,
				Status:         nextStatus,
				IdempotencyKey: idempotencyKeyPtr(idempotencyKey),
			}
			if err := tx.Create(&follow).Error; err != nil {
				return err
			}
			if active {
				delta = 1
			}
			if active {
				if err := createFollowProfileOutbox(tx, follow, outcome); err != nil {
					return err
				}
			}
		} else {
			if idempotencyKey != "" && idempotencyKeyValue(follow.IdempotencyKey) == idempotencyKey {
				if (follow.Status == domainrelation.FollowStatusActive) != active {
					return domainrelation.ErrFollowIdempotencyConflict
				}
				var err error
				userStat, err = currentStat(tx, userID)
				if err != nil {
					return err
				}
				targetStat, err = currentStat(tx, targetUserID)
				return err
			}

			previousStatus := follow.Status
			previousIdempotencyKey := idempotencyKeyValue(follow.IdempotencyKey)
			if follow.Status != nextStatus {
				if active {
					delta = 1
				} else {
					delta = -1
				}
			}
			follow.Status = nextStatus
			follow.IdempotencyKey = idempotencyKeyPtr(idempotencyKey)
			if previousStatus != nextStatus || previousIdempotencyKey != idempotencyKey {
				if err := tx.Save(&follow).Error; err != nil {
					return err
				}
				if previousStatus != nextStatus {
					if err := createFollowProfileOutbox(tx, follow, outcome); err != nil {
						return err
					}
				}
			}
		}

		var err error
		if delta != 0 {
			userStat, err = updateStat(tx, userID, delta, 0)
			if err != nil {
				return err
			}
			targetStat, err = updateStat(tx, targetUserID, 0, delta)
			return err
		}

		userStat, err = currentStat(tx, userID)
		if err != nil {
			return err
		}
		targetStat, err = currentStat(tx, targetUserID)
		return err
	})
	if err != nil {
		return nil, nil, nil, mapUserError(err)
	}

	return restoreFollow(follow), restoreStat(userStat), restoreStat(targetStat), nil
}

func createFollowProfileOutbox(tx *gorm.DB, follow FollowModel, outcome *domainrelation.RecommendationOutcomeContext) error {
	if follow.ID <= 0 || follow.UpdatedAt.IsZero() {
		return errors.New("follow projection outbox requires a persisted follow")
	}
	return tx.Create(&FollowProfileOutboxModel{
		EventID:                 fmt.Sprintf("relation:follow:%d:%d:%d", follow.ID, follow.Status, follow.UpdatedAt.UTC().UnixNano()),
		FollowID:                follow.ID,
		UserID:                  follow.UserID,
		TargetUserID:            follow.TargetUserID,
		Active:                  follow.Status == domainrelation.FollowStatusActive,
		OccurredAt:              follow.UpdatedAt.UTC(),
		RecommendationRequestID: outcomeRequestID(outcome),
		RecommendationVideoID:   outcomeVideoID(outcome),
		AvailableAt:             follow.UpdatedAt.UTC(),
	}).Error
}

func outcomeRequestID(outcome *domainrelation.RecommendationOutcomeContext) string {
	if outcome == nil {
		return ""
	}
	return outcome.RequestID
}

func outcomeVideoID(outcome *domainrelation.RecommendationOutcomeContext) int64 {
	if outcome == nil {
		return 0
	}
	return outcome.VideoID
}

// IsFollowing reads one relationship with constant work instead of scanning a following list.
func (r *Repository) IsFollowing(ctx context.Context, userID int64, targetUserID int64) (bool, error) {
	var state followStateModel
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			EXISTS (
				SELECT 1 FROM account
				WHERE id = ? AND status = ?
			) AS target_exists,
			EXISTS (
				SELECT 1 FROM user_follow
				WHERE user_id = ? AND target_user_id = ? AND status = ?
			) AS following
	`, targetUserID, domainaccount.StatusNormal, userID, targetUserID, domainrelation.FollowStatusActive).
		Scan(&state).
		Error
	if err != nil {
		return false, err
	}
	if !state.TargetExists {
		return false, domainrelation.ErrTargetUserNotFound
	}
	return state.Following, nil
}

func (r *Repository) AreMutuallyFollowing(ctx context.Context, firstUserID, secondUserID int64) (bool, error) {
	var state struct {
		FirstExists   bool
		SecondExists  bool
		FirstFollows  bool
		SecondFollows bool
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			EXISTS (SELECT 1 FROM account WHERE id = ? AND status = ? AND role = ?) AS first_exists,
			EXISTS (SELECT 1 FROM account WHERE id = ? AND status = ? AND role = ?) AS second_exists,
			EXISTS (SELECT 1 FROM user_follow WHERE user_id = ? AND target_user_id = ? AND status = ?) AS first_follows,
			EXISTS (SELECT 1 FROM user_follow WHERE user_id = ? AND target_user_id = ? AND status = ?) AS second_follows
	`, firstUserID, domainaccount.StatusNormal, domainaccount.RoleUser,
		secondUserID, domainaccount.StatusNormal, domainaccount.RoleUser,
		firstUserID, secondUserID, domainrelation.FollowStatusActive,
		secondUserID, firstUserID, domainrelation.FollowStatusActive,
	).Scan(&state).Error
	if err != nil {
		return false, err
	}
	if !state.FirstExists || !state.SecondExists {
		return false, domainrelation.ErrTargetUserNotFound
	}
	return state.FirstFollows && state.SecondFollows, nil
}

// ListFollowing 查询当前用户关注的人。
func (r *Repository) ListFollowing(ctx context.Context, userID int64, listQuery string, cursor *domainrelation.ListCursor, limit int) ([]*domainrelation.UserItem, error) {
	query := r.db.WithContext(ctx).
		Table("user_follow AS f").
		Select("a.id AS user_id, a.nickname, a.avatar_url, a.bio, f.updated_at AS followed_at").
		Joins("LEFT JOIN account AS a ON a.id = f.target_user_id").
		Where("f.user_id = ? AND f.status = ? AND a.status = ?", userID, domainrelation.FollowStatusActive, domainaccount.StatusNormal)
	if listQuery != "" {
		pattern := "%" + domainsearch.EscapeLikeLiteral(listQuery) + "%"
		query = query.Where("a.nickname ILIKE ? ESCAPE '\\'", pattern)
	}

	if cursor != nil {
		query = query.Where(
			"(f.updated_at < ? OR (f.updated_at = ? AND f.target_user_id < ?))",
			cursor.FollowedAt,
			cursor.FollowedAt,
			cursor.UserID,
		)
	}

	return scanUserItems(query.Order("f.updated_at DESC").Order("f.target_user_id DESC").Limit(limit))
}

// ListFollowers 查询关注当前用户的人。
func (r *Repository) ListFollowers(ctx context.Context, userID int64, cursor *domainrelation.ListCursor, limit int) ([]*domainrelation.UserItem, error) {
	query := r.db.WithContext(ctx).
		Table("user_follow AS f").
		Select("a.id AS user_id, a.nickname, a.avatar_url, a.bio, f.updated_at AS followed_at").
		Joins("LEFT JOIN account AS a ON a.id = f.user_id").
		Where("f.target_user_id = ? AND f.status = ? AND a.status = ?", userID, domainrelation.FollowStatusActive, domainaccount.StatusNormal)

	if cursor != nil {
		query = query.Where(
			"(f.updated_at < ? OR (f.updated_at = ? AND f.user_id < ?))",
			cursor.FollowedAt,
			cursor.FollowedAt,
			cursor.UserID,
		)
	}

	return scanUserItems(query.Order("f.updated_at DESC").Order("f.user_id DESC").Limit(limit))
}

func (r *Repository) ListMutualRecipients(ctx context.Context, userID int64, listQuery string, cursor *domainrelation.ListCursor, limit int) ([]*domainrelation.MutualRecipient, error) {
	query := r.db.WithContext(ctx).
		Table("user_follow AS outgoing").
		Select(`a.id AS user_id, a.nickname, a.avatar_url, a.bio,
			GREATEST(outgoing.updated_at, incoming.updated_at) AS followed_at`).
		Joins("JOIN user_follow AS incoming ON incoming.user_id = outgoing.target_user_id AND incoming.target_user_id = outgoing.user_id AND incoming.status = ?", domainrelation.FollowStatusActive).
		Joins("JOIN account AS a ON a.id = outgoing.target_user_id").
		Where("outgoing.user_id = ? AND outgoing.status = ? AND a.status = ? AND a.role = ?", userID, domainrelation.FollowStatusActive, domainaccount.StatusNormal, domainaccount.RoleUser)
	if listQuery != "" {
		pattern := "%" + domainsearch.EscapeLikeLiteral(listQuery) + "%"
		query = query.Where("a.nickname ILIKE ? ESCAPE '\\'", pattern)
	}
	if cursor != nil {
		query = query.Where(
			"(GREATEST(outgoing.updated_at, incoming.updated_at) < ? OR (GREATEST(outgoing.updated_at, incoming.updated_at) = ? AND outgoing.target_user_id < ?))",
			cursor.FollowedAt, cursor.FollowedAt, cursor.UserID,
		)
	}
	var models []mutualRecipientModel
	if err := query.Order("followed_at DESC").Order("outgoing.target_user_id DESC").Limit(limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*domainrelation.MutualRecipient, 0, len(models))
	for _, model := range models {
		items = append(items, &domainrelation.MutualRecipient{
			UserID: model.UserID, Nickname: strings.TrimSpace(model.Nickname),
			AvatarURL: strings.TrimSpace(model.AvatarURL), Bio: strings.TrimSpace(model.Bio),
			FollowedAt: model.FollowedAt,
		})
	}
	return items, nil
}

// GetUserProfile 读取用户展示资料，用于关注通知。
func (r *Repository) GetUserProfile(ctx context.Context, userID int64) (*domainrelation.UserProfile, error) {
	var model relationUserProfileModel
	err := r.db.WithContext(ctx).
		Table("account").
		Select("id AS user_id, nickname, avatar_url, bio").
		Where("id = ? AND status = ?", userID, domainaccount.StatusNormal).
		Take(&model).
		Error
	if err != nil {
		return nil, mapUserError(err)
	}
	return domainrelation.RestoreUserProfile(model.UserID, model.Nickname, model.AvatarURL, model.Bio), nil
}

func scanUserItems(query *gorm.DB) ([]*domainrelation.UserItem, error) {
	var models []relationUserModel
	if err := query.Scan(&models).Error; err != nil {
		return nil, err
	}

	items := make([]*domainrelation.UserItem, 0, len(models))
	for _, model := range models {
		items = append(items, domainrelation.RestoreUserItem(
			model.UserID,
			model.Nickname,
			model.AvatarURL,
			model.Bio,
			model.FollowedAt,
		))
	}
	return items, nil
}

func statusFromActive(active bool) int {
	if active {
		return domainrelation.FollowStatusActive
	}
	return domainrelation.FollowStatusCanceled
}

func lockNormalUser(tx *gorm.DB, userID int64) error {
	var user infraaccount.UserModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status = ?", userID, domainaccount.StatusNormal).
		Take(&user).
		Error
	return mapUserError(err)
}

func ensureStat(tx *gorm.DB, userID int64) error {
	stat := RelationStatModel{UserID: userID}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&stat).Error
}

func currentStat(tx *gorm.DB, userID int64) (RelationStatModel, error) {
	var stat RelationStatModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		Take(&stat).
		Error
	return stat, err
}

func updateStat(tx *gorm.DB, userID int64, followingDelta int, followerDelta int) (RelationStatModel, error) {
	stat, err := currentStat(tx, userID)
	if err != nil {
		return stat, err
	}
	stat.FollowingCount = clampCount(stat.FollowingCount + followingDelta)
	stat.FollowerCount = clampCount(stat.FollowerCount + followerDelta)
	if err := tx.Save(&stat).Error; err != nil {
		return stat, err
	}
	return stat, nil
}

func restoreFollow(model FollowModel) *domainrelation.Follow {
	return domainrelation.RestoreFollow(
		model.ID,
		model.UserID,
		model.TargetUserID,
		model.Status,
		idempotencyKeyValue(model.IdempotencyKey),
		model.CreatedAt,
		model.UpdatedAt,
	)
}

func restoreStat(model RelationStatModel) *domainrelation.RelationStat {
	return domainrelation.RestoreRelationStat(
		model.UserID,
		model.FollowingCount,
		model.FollowerCount,
		model.CreatedAt,
		model.UpdatedAt,
	)
}

func idempotencyKeyPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func idempotencyKeyValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapUserError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainrelation.ErrTargetUserNotFound
	}
	return err
}

func clampCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

var _ domainrelation.Repository = (*Repository)(nil)
var _ domainrelation.MutualFollowRepository = (*Repository)(nil)
var _ domainrelation.MutualRecipientRepository = (*Repository)(nil)
