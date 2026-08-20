package infrachat

import (
	"context"
	"errors"
	"time"

	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateOrGetConversation(ctx context.Context, firstUserID, secondUserID int64) (*domainchat.Conversation, error) {
	lower, higher, err := domainchat.CanonicalPair(firstUserID, secondUserID)
	if err != nil {
		return nil, err
	}

	var conversation ConversationModel
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation = ConversationModel{LowerUserID: lower, HigherUserID: higher}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&conversation)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("lower_user_id = ? AND higher_user_id = ?", lower, higher).
				Take(&conversation).Error; err != nil {
				return err
			}
		}
		members := []ConversationMemberModel{
			{ConversationID: conversation.ID, UserID: lower},
			{ConversationID: conversation.ID, UserID: higher},
		}
		for _, member := range members {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapChatPersistenceError(err)
	}
	return restoreConversation(conversation), nil
}

func (r *Repository) FindConversationByPair(ctx context.Context, lowerUserID, higherUserID int64) (*domainchat.Conversation, error) {
	lowerUserID, higherUserID, err := domainchat.CanonicalPair(lowerUserID, higherUserID)
	if err != nil {
		return nil, err
	}
	var conversation ConversationModel
	err = r.db.WithContext(ctx).
		Where("lower_user_id = ? AND higher_user_id = ?", lowerUserID, higherUserID).
		Take(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainchat.ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return restoreConversation(conversation), nil
}

func (r *Repository) FindConversationsByPairs(ctx context.Context, userID int64, otherUserIDs []int64) (map[int64]*domainchat.Conversation, error) {
	result := make(map[int64]*domainchat.Conversation, len(otherUserIDs))
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if len(otherUserIDs) == 0 {
		return result, nil
	}
	unique := make([]int64, 0, len(otherUserIDs))
	seen := make(map[int64]struct{}, len(otherUserIDs))
	for _, otherID := range otherUserIDs {
		if otherID <= 0 || otherID == userID {
			continue
		}
		if _, exists := seen[otherID]; exists {
			continue
		}
		seen[otherID] = struct{}{}
		unique = append(unique, otherID)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var models []ConversationModel
	if err := r.db.WithContext(ctx).
		Where("(lower_user_id = ? AND higher_user_id IN ?) OR (higher_user_id = ? AND lower_user_id IN ?)",
			userID, unique, userID, unique).
		Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		otherID := model.LowerUserID
		if otherID == userID {
			otherID = model.HigherUserID
		}
		result[otherID] = restoreConversation(model)
	}
	return result, nil
}

func (r *Repository) GetConversationForMember(ctx context.Context, conversationID, userID int64) (*domainchat.Conversation, error) {
	if conversationID <= 0 {
		return nil, domainchat.ErrInvalidConversationID
	}
	var conversation ConversationModel
	err := r.db.WithContext(ctx).
		Table("chat_conversation AS c").
		Joins("JOIN chat_conversation_member AS m ON m.conversation_id = c.id").
		Where("c.id = ? AND m.user_id = ?", conversationID, userID).
		Take(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainchat.ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return restoreConversation(conversation), nil
}

func (r *Repository) GetConversationItemForMember(ctx context.Context, conversationID, userID int64) (*domainchat.ConversationItem, error) {
	if conversationID <= 0 {
		return nil, domainchat.ErrInvalidConversationID
	}
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	type row struct {
		ID                   int64      `gorm:"column:id"`
		LowerUserID          int64      `gorm:"column:lower_user_id"`
		HigherUserID         int64      `gorm:"column:higher_user_id"`
		LastMessageID        *int64     `gorm:"column:last_message_id"`
		LastMessageAt        *time.Time `gorm:"column:last_message_at"`
		CreatedAt            time.Time  `gorm:"column:created_at"`
		UpdatedAt            time.Time  `gorm:"column:updated_at"`
		MemberConversationID int64      `gorm:"column:member_conversation_id"`
		MemberUserID         int64      `gorm:"column:member_user_id"`
		LastReadMessageID    int64      `gorm:"column:member_last_read_message_id"`
		LastReadAt           *time.Time `gorm:"column:member_last_read_at"`
		UnreadCount          int        `gorm:"column:member_unread_count"`
		MutedAt              *time.Time `gorm:"column:member_muted_at"`
		HiddenAt             *time.Time `gorm:"column:member_hidden_at"`
		CounterpartID        int64      `gorm:"column:counterpart_id"`
	}
	var item row
	err := r.db.WithContext(ctx).
		Table("chat_conversation AS c").
		Select(`c.id, c.lower_user_id, c.higher_user_id, c.last_message_id, c.last_message_at,
			c.created_at, c.updated_at, m.conversation_id AS member_conversation_id,
			m.user_id AS member_user_id, m.last_read_message_id AS member_last_read_message_id,
			m.last_read_at AS member_last_read_at, m.unread_count AS member_unread_count,
			m.muted_at AS member_muted_at, m.hidden_at AS member_hidden_at,
			CASE WHEN c.lower_user_id = ? THEN c.higher_user_id ELSE c.lower_user_id END AS counterpart_id`, userID).
		Joins("JOIN chat_conversation_member AS m ON m.conversation_id = c.id AND m.user_id = ?", userID).
		Where("c.id = ?", conversationID).
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainchat.ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	var lastMessageIDs []int64
	if item.LastMessageID != nil {
		lastMessageIDs = append(lastMessageIDs, *item.LastMessageID)
	}
	messages, err := r.findMessagesByIDs(ctx, lastMessageIDs)
	if err != nil {
		return nil, err
	}
	member := domainchat.RestoreMember(
		item.MemberConversationID, item.MemberUserID, item.LastReadMessageID,
		item.LastReadAt, item.UnreadCount, item.MutedAt, item.HiddenAt,
		time.Time{}, time.Time{},
	)
	lastMessage := messageFromMap(messages, item.LastMessageID)
	if lastMessage != nil && lastMessage.ConversationID != item.ID {
		lastMessage = nil
	}
	return &domainchat.ConversationItem{
		Conversation: domainchat.RestoreConversation(
			item.ID, item.LowerUserID, item.HigherUserID, int64Value(item.LastMessageID),
			item.LastMessageAt, item.CreatedAt, item.UpdatedAt,
		),
		Member:      member,
		Counterpart: domainchat.UnavailableParticipant(item.CounterpartID),
		LastMessage: lastMessage,
	}, nil
}

func (r *Repository) ListConversations(ctx context.Context, userID int64, cursor *domainchat.ConversationCursor, limit int) ([]*domainchat.ConversationItem, error) {
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	type row struct {
		ID                   int64      `gorm:"column:id"`
		LowerUserID          int64      `gorm:"column:lower_user_id"`
		HigherUserID         int64      `gorm:"column:higher_user_id"`
		LastMessageID        *int64     `gorm:"column:last_message_id"`
		LastMessageAt        *time.Time `gorm:"column:last_message_at"`
		CreatedAt            time.Time  `gorm:"column:created_at"`
		UpdatedAt            time.Time  `gorm:"column:updated_at"`
		MemberConversationID int64      `gorm:"column:member_conversation_id"`
		MemberUserID         int64      `gorm:"column:member_user_id"`
		LastReadMessageID    int64      `gorm:"column:member_last_read_message_id"`
		LastReadAt           *time.Time `gorm:"column:member_last_read_at"`
		UnreadCount          int        `gorm:"column:member_unread_count"`
		CounterpartID        int64      `gorm:"column:counterpart_id"`
	}
	query := r.db.WithContext(ctx).
		Table("chat_conversation AS c").
		Select(`c.*, m.conversation_id AS member_conversation_id, m.user_id AS member_user_id,
			m.last_read_message_id AS member_last_read_message_id, m.last_read_at AS member_last_read_at,
			m.unread_count AS member_unread_count,
			CASE WHEN c.lower_user_id = ? THEN c.higher_user_id ELSE c.lower_user_id END AS counterpart_id`, userID).
		Joins("JOIN chat_conversation_member AS m ON m.conversation_id = c.id AND m.user_id = ?", userID).
		Where("c.last_message_id IS NOT NULL")
	if cursor != nil {
		query = query.Where(
			"(c.last_message_id < ? OR (c.last_message_id = ? AND c.id < ?))",
			cursor.LastMessageID, cursor.LastMessageID, cursor.ConversationID,
		)
	}
	var rows []row
	if err := query.Order("c.last_message_id DESC").Order("c.id DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}

	lastIDs := make([]int64, 0, len(rows))
	for _, item := range rows {
		if item.LastMessageID != nil {
			lastIDs = append(lastIDs, *item.LastMessageID)
		}
	}
	messages, err := r.findMessagesByIDs(ctx, lastIDs)
	if err != nil {
		return nil, err
	}
	items := make([]*domainchat.ConversationItem, 0, len(rows))
	for _, item := range rows {
		member := domainchat.RestoreMember(
			item.MemberConversationID, item.MemberUserID, item.LastReadMessageID,
			item.LastReadAt, item.UnreadCount, nil, nil, time.Time{}, time.Time{},
		)
		var lastMessage *domainchat.Message
		if item.LastMessageID != nil {
			lastMessage = messages[*item.LastMessageID]
		}
		items = append(items, &domainchat.ConversationItem{
			Conversation: domainchat.RestoreConversation(
				item.ID, item.LowerUserID, item.HigherUserID, int64Value(item.LastMessageID),
				item.LastMessageAt, item.CreatedAt, item.UpdatedAt,
			),
			Member:      member,
			Counterpart: domainchat.RestoreParticipant(item.CounterpartID, "", "", "", false),
			LastMessage: lastMessage,
		})
	}
	return items, nil
}

func (r *Repository) ListMessages(ctx context.Context, conversationID, userID int64, cursor *domainchat.HistoryCursor, limit int) ([]*domainchat.Message, error) {
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if _, err := r.GetConversationForMember(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	query := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID)
	if cursor != nil {
		if cursor.ConversationID != conversationID {
			return nil, domainchat.ErrInvalidCursor
		}
		query = query.Where("id < ?", cursor.MessageID)
	}
	var models []MessageModel
	if err := query.Order("id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	messages := make([]*domainchat.Message, 0, len(models))
	for _, model := range models {
		messages = append(messages, restoreMessage(model))
	}
	return messages, nil
}

func (r *Repository) ListMessagesAfter(ctx context.Context, conversationID, userID, afterMessageID int64, limit int) ([]*domainchat.Message, error) {
	if afterMessageID <= 0 {
		return nil, domainchat.ErrInvalidMessageID
	}
	if _, err := r.GetConversationForMember(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	var models []MessageModel
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND id > ?", conversationID, afterMessageID).
		Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	messages := make([]*domainchat.Message, 0, len(models))
	for _, model := range models {
		messages = append(messages, restoreMessage(model))
	}
	return messages, nil
}

func (r *Repository) FindMessageBySenderAndIdempotencyKey(ctx context.Context, senderID int64, idempotencyKey string) (*domainchat.Message, error) {
	if senderID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if idempotencyKey == "" {
		return nil, domainchat.ErrIdempotencyKeyRequired
	}
	var model MessageModel
	err := r.db.WithContext(ctx).
		Where("sender_id = ? AND idempotency_key = ?", senderID, idempotencyKey).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainchat.ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	return restoreMessage(model), nil
}

func (r *Repository) Send(ctx context.Context, input domainchat.SendInput) (*domainchat.Message, bool, error) {
	if err := validateSendInput(input); err != nil {
		return nil, false, err
	}
	var result *domainchat.Message
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation ConversationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", input.ConversationID).Take(&conversation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainchat.ErrConversationNotFound
			}
			return err
		}
		var members []ConversationMemberModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conversation_id = ?", input.ConversationID).
			Order("user_id ASC").Find(&members).Error; err != nil {
			return err
		}
		var senderMember *ConversationMemberModel
		for index := range members {
			if members[index].UserID == input.SenderID {
				senderMember = &members[index]
				break
			}
		}
		if senderMember == nil {
			return domainchat.ErrNotMember
		}

		var existing MessageModel
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("sender_id = ? AND idempotency_key = ?", input.SenderID, input.IdempotencyKey).
			Take(&existing).Error
		if findErr == nil {
			if !samePayload(existing, input) {
				return domainchat.ErrIdempotencyConflict
			}
			result = restoreMessage(existing)
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		model := MessageModel{
			ConversationID: input.ConversationID,
			SenderID:       input.SenderID,
			Kind:           string(input.Kind),
			Text:           input.Text,
			IdempotencyKey: input.IdempotencyKey,
			CreatedAt:      time.Now().UTC(),
		}
		if input.VideoID > 0 {
			videoID := input.VideoID
			model.VideoID = &videoID
		}
		if err := tx.Create(&model).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) {
				if lookupErr := tx.Where("sender_id = ? AND idempotency_key = ?", input.SenderID, input.IdempotencyKey).Take(&existing).Error; lookupErr == nil {
					if !samePayload(existing, input) {
						return domainchat.ErrIdempotencyConflict
					}
					result = restoreMessage(existing)
					return nil
				}
			}
			return err
		}
		lastMessageID := model.ID
		lastMessageAt := model.CreatedAt
		if err := tx.Model(&ConversationModel{}).Where("id = ?", input.ConversationID).Updates(ConversationModel{
			LastMessageID: &lastMessageID, LastMessageAt: &lastMessageAt, UpdatedAt: model.CreatedAt,
		}).Error; err != nil {
			return err
		}
		for index := range members {
			if members[index].UserID == input.SenderID {
				continue
			}
			if err := tx.Model(&ConversationMemberModel{}).
				Where("conversation_id = ? AND user_id = ?", input.ConversationID, members[index].UserID).
				UpdateColumn("unread_count", gorm.Expr("unread_count + ?", 1)).Error; err != nil {
				return err
			}
		}
		result = restoreMessage(model)
		created = true
		return nil
	})
	if err != nil {
		return nil, false, mapChatPersistenceError(err)
	}
	return result, created, nil
}

func (r *Repository) MarkRead(ctx context.Context, conversationID, userID, throughMessageID int64) (*domainchat.Member, error) {
	if conversationID <= 0 {
		return nil, domainchat.ErrInvalidConversationID
	}
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if throughMessageID <= 0 {
		return nil, domainchat.ErrInvalidMessageID
	}
	var member ConversationMemberModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conversation_id = ? AND user_id = ?", conversationID, userID).
			Take(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainchat.ErrConversationNotFound
			}
			return err
		}
		var message MessageModel
		if err := tx.Where("id = ? AND conversation_id = ?", throughMessageID, conversationID).Take(&message).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainchat.ErrMessageNotFound
			}
			return err
		}
		boundary := member.LastReadMessageID
		if throughMessageID > boundary {
			boundary = throughMessageID
			now := time.Now().UTC()
			member.LastReadMessageID = boundary
			member.LastReadAt = &now
		}
		var unread int64
		if err := tx.Model(&MessageModel{}).
			Where("conversation_id = ? AND sender_id <> ? AND id > ?", conversationID, userID, boundary).
			Count(&unread).Error; err != nil {
			return err
		}
		member.UnreadCount = int(unread)
		return tx.Save(&member).Error
	})
	if err != nil {
		return nil, mapChatPersistenceError(err)
	}
	return restoreMember(member), nil
}

func (r *Repository) CountUnread(ctx context.Context, userID int64) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ConversationMemberModel{}).
		Where("user_id = ?", userID).Select("COALESCE(SUM(unread_count), 0)").Scan(&count).Error
	return int(count), err
}

func (r *Repository) ReconcileUnread(ctx context.Context, userID int64) (int, error) {
	var total int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var members []ConversationMemberModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).Find(&members).Error; err != nil {
			return err
		}
		for index := range members {
			var unread int64
			if err := tx.Model(&MessageModel{}).
				Where("conversation_id = ? AND sender_id <> ? AND id > ?", members[index].ConversationID, userID, members[index].LastReadMessageID).
				Count(&unread).Error; err != nil {
				return err
			}
			members[index].UnreadCount = int(unread)
			total += int(unread)
			if err := tx.Model(&ConversationMemberModel{}).
				Where("conversation_id = ? AND user_id = ?", members[index].ConversationID, userID).
				Update("unread_count", int(unread)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return total, mapChatPersistenceError(err)
}

func (r *Repository) findMessagesByIDs(ctx context.Context, ids []int64) (map[int64]*domainchat.Message, error) {
	result := make(map[int64]*domainchat.Message, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var models []MessageModel
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.ID] = restoreMessage(model)
	}
	return result, nil
}

func restoreConversation(model ConversationModel) *domainchat.Conversation {
	return domainchat.RestoreConversation(model.ID, model.LowerUserID, model.HigherUserID, int64Value(model.LastMessageID), model.LastMessageAt, model.CreatedAt, model.UpdatedAt)
}

func restoreMember(model ConversationMemberModel) *domainchat.Member {
	return domainchat.RestoreMember(model.ConversationID, model.UserID, model.LastReadMessageID, model.LastReadAt, model.UnreadCount, model.MutedAt, model.HiddenAt, model.CreatedAt, model.UpdatedAt)
}

func restoreMessage(model MessageModel) *domainchat.Message {
	return domainchat.RestoreMessage(model.ID, model.ConversationID, model.SenderID, domainchat.MessageKind(model.Kind), model.Text, int64Value(model.VideoID), model.IdempotencyKey, model.RevokedAt, model.CreatedAt)
}

func samePayload(model MessageModel, input domainchat.SendInput) bool {
	return model.ConversationID == input.ConversationID &&
		model.Kind == string(input.Kind) &&
		model.Text == input.Text &&
		int64Value(model.VideoID) == input.VideoID
}

func validateSendInput(input domainchat.SendInput) error {
	if input.ConversationID <= 0 {
		return domainchat.ErrInvalidConversationID
	}
	if input.SenderID <= 0 {
		return domainchat.ErrInvalidUserID
	}
	if input.IdempotencyKey == "" {
		return domainchat.ErrIdempotencyKeyRequired
	}
	if len(input.IdempotencyKey) > domainchat.MaxIdempotencyKey {
		return domainchat.ErrIdempotencyKeyTooLong
	}
	switch input.Kind {
	case domainchat.MessageKindText:
		if input.VideoID != 0 {
			return domainchat.ErrInvalidMessageShape
		}
		if _, err := domainchat.NewTextMessage(input.ConversationID, input.SenderID, input.Text, input.IdempotencyKey, time.Time{}); err != nil {
			return err
		}
	case domainchat.MessageKindVideo:
		if input.Text != "" {
			return domainchat.ErrInvalidMessageShape
		}
		if _, err := domainchat.NewVideoMessage(input.ConversationID, input.SenderID, input.VideoID, input.IdempotencyKey, time.Time{}); err != nil {
			return err
		}
	default:
		return domainchat.ErrInvalidMessageShape
	}
	return nil
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func messageFromMap(messages map[int64]*domainchat.Message, id *int64) *domainchat.Message {
	if id == nil {
		return nil
	}
	return messages[*id]
}

func mapChatPersistenceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainchat.ErrConversationNotFound
	}
	if errors.Is(err, domainchat.ErrConversationNotFound) ||
		errors.Is(err, domainchat.ErrNotMember) ||
		errors.Is(err, domainchat.ErrMessageNotFound) ||
		errors.Is(err, domainchat.ErrIdempotencyConflict) {
		return err
	}
	return err
}

var _ domainchat.Repository = (*Repository)(nil)
var _ domainchat.IncrementalMessageRepository = (*Repository)(nil)
