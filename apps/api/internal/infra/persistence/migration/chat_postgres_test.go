package migration

import (
	"context"
	"errors"
	"sync"
	"testing"

	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
	infrachat "github.com/shiyudesu/frux/internal/infra/persistence/chat"
)

func TestPostgresChatConcurrentCreationSendReadAndReconcile(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := db.AutoMigrate(
		&infrachat.ConversationModel{},
		&infrachat.ConversationMemberModel{},
		&infrachat.MessageModel{},
	); err != nil {
		t.Fatalf("migrate chat tables: %v", err)
	}
	if err := infrachat.EnsureIndexes(db); err != nil {
		t.Fatalf("ensure chat indexes: %v", err)
	}
	repository := infrachat.New(db)

	const creators = 8
	conversations := make(chan *domainchat.Conversation, creators)
	errs := make(chan error, creators)
	var createWG sync.WaitGroup
	for index := 0; index < creators; index++ {
		createWG.Add(1)
		go func() {
			defer createWG.Done()
			conversation, err := repository.CreateOrGetConversation(context.Background(), 1, 2)
			if err != nil {
				errs <- err
				return
			}
			conversations <- conversation
		}()
	}
	createWG.Wait()
	close(conversations)
	for len(errs) > 0 {
		err := <-errs
		if err != nil {
			t.Fatalf("concurrent conversation creation: %v", err)
		}
	}
	var firstID int64
	for conversation := range conversations {
		if firstID == 0 {
			firstID = conversation.ID
		}
		if conversation.ID != firstID {
			t.Fatalf("concurrent creation returned different IDs: %d and %d", firstID, conversation.ID)
		}
	}
	var memberCount int64
	if err := db.Model(&infrachat.ConversationMemberModel{}).Where("conversation_id = ?", firstID).Count(&memberCount).Error; err != nil {
		t.Fatalf("count conversation members: %v", err)
	}
	if memberCount != 2 {
		t.Fatalf("expected exactly two members, got %d", memberCount)
	}
	emptyItem, err := repository.GetConversationItemForMember(context.Background(), firstID, 1)
	if err != nil {
		t.Fatalf("read empty conversation metadata: %v", err)
	}
	if emptyItem == nil || emptyItem.Conversation == nil || emptyItem.Conversation.ID != firstID ||
		emptyItem.LastMessage != nil || emptyItem.Member == nil || emptyItem.Member.UnreadCount != 0 {
		t.Fatalf("unexpected empty conversation metadata: %#v", emptyItem)
	}

	var sendWG sync.WaitGroup
	createdCount := 0
	var sendMu sync.Mutex
	for index := 0; index < creators; index++ {
		sendWG.Add(1)
		go func() {
			defer sendWG.Done()
			_, created, err := repository.Send(context.Background(), domainchat.SendInput{
				ConversationID: firstID, SenderID: 1, Kind: domainchat.MessageKindText,
				Text: "hello", IdempotencyKey: "same-key",
			})
			if err != nil {
				errs <- err
				return
			}
			if created {
				sendMu.Lock()
				createdCount++
				sendMu.Unlock()
			}
		}()
	}
	sendWG.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent send: %v", err)
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected one created message, got %d", createdCount)
	}
	var messageCount int64
	if err := db.Model(&infrachat.MessageModel{}).Where("conversation_id = ?", firstID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("expected one idempotent message, got %d", messageCount)
	}
	committed, err := repository.FindMessageBySenderAndIdempotencyKey(context.Background(), 1, "same-key")
	if err != nil || committed == nil || committed.ID <= 0 {
		t.Fatalf("read committed idempotent message: message=%#v err=%v", committed, err)
	}
	item, err := repository.GetConversationItemForMember(context.Background(), firstID, 1)
	if err != nil {
		t.Fatalf("read populated conversation metadata: %v", err)
	}
	if item == nil || item.LastMessage == nil || item.LastMessage.ID != committed.ID {
		t.Fatalf("conversation metadata did not hydrate last message: %#v", item)
	}

	second, created, err := repository.Send(context.Background(), domainchat.SendInput{
		ConversationID: firstID, SenderID: 2, Kind: domainchat.MessageKindText,
		Text: "reply", IdempotencyKey: "reply-key",
	})
	if err != nil || !created {
		t.Fatalf("send reply: created=%v err=%v", created, err)
	}
	if _, _, err := repository.Send(context.Background(), domainchat.SendInput{
		ConversationID: firstID, SenderID: 1, Kind: domainchat.MessageKindText,
		Text: "different", IdempotencyKey: "same-key",
	}); !errors.Is(err, domainchat.ErrIdempotencyConflict) {
		t.Fatalf("expected same-key conflict, got %v", err)
	}
	unread, err := repository.CountUnread(context.Background(), 1)
	if err != nil {
		t.Fatalf("count sender unread: %v", err)
	}
	if unread != 1 {
		t.Fatalf("expected sender unread from reply, got %d", unread)
	}
	if _, err := repository.MarkRead(context.Background(), firstID, 1, second.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	unread, err = repository.CountUnread(context.Background(), 1)
	if err != nil {
		t.Fatalf("count unread after read: %v", err)
	}
	if unread != 0 {
		t.Fatalf("expected zero unread after read, got %d", unread)
	}
	if _, err := repository.ReconcileUnread(context.Background(), 1); err != nil {
		t.Fatalf("reconcile unread: %v", err)
	}
}
