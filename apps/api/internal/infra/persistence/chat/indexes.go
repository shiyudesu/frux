package infrachat

import "gorm.io/gorm"

func EnsureIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_chat_conversation_pair ON chat_conversation (lower_user_id, higher_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_conversation_last_message ON chat_conversation (last_message_id DESC, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_chat_member_pair ON chat_conversation_member (conversation_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_member_user_unread ON chat_conversation_member (user_id, unread_count)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_chat_message_sender_key ON chat_message (sender_id, idempotency_key)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_message_conversation_order ON chat_message (conversation_id, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_message_video ON chat_message (video_id)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
