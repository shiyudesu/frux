package applicationmessage

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

type accountLifecycleMessageRepositoryStub struct {
	message *domainmessage.Message
}

func (r *accountLifecycleMessageRepositoryStub) Create(
	_ context.Context,
	message *domainmessage.Message,
	_ string,
) (*domainmessage.Message, bool, error) {
	r.message = message
	return message, true, nil
}

func (*accountLifecycleMessageRepositoryStub) ListByUser(
	context.Context, int64, *domainmessage.Cursor, int,
) ([]*domainmessage.Message, error) {
	return nil, nil
}

func (*accountLifecycleMessageRepositoryStub) CountUnread(context.Context, int64) (int, error) {
	return 0, nil
}

func (*accountLifecycleMessageRepositoryStub) MarkRead(context.Context, int64, []int64) (int, error) {
	return 0, nil
}

func TestCreateAccountLifecycleUsesSafeRegisteredCopy(t *testing.T) {
	now := time.Now().UTC()
	notification, err := domainaccount.NewAccountLifecycleNotification(
		7, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonSecurityRisk, 2, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := &accountLifecycleMessageRepositoryStub{}
	service := New(repository)
	if _, err := service.CreateAccountLifecycle(
		context.Background(), *notification,
	); err != nil {
		t.Fatal(err)
	}
	if repository.message == nil ||
		repository.message.Type != domainmessage.TypeSystem ||
		repository.message.Title != "账号已被冻结" ||
		repository.message.Content != "你的账号已被冻结，原因：存在账号安全风险。如有疑问，请联系管理员。" ||
		repository.message.EventID != notification.EventID {
		t.Fatalf("message = %+v", repository.message)
	}
}

func TestAccountLifecycleContentRejectsUnsupportedPayload(t *testing.T) {
	if _, _, err := AccountLifecycleMessageContent(
		domainaccount.AccountOperationRevokeSessions,
		domainaccount.AccountReasonSecurityResponse,
	); !errors.Is(err, ErrInvalidAccountLifecycle) {
		t.Fatalf("operation error = %v", err)
	}
	if _, _, err := AccountLifecycleMessageContent(
		domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAppealApproved,
	); !errors.Is(err, ErrInvalidAccountLifecycle) {
		t.Fatalf("reason error = %v", err)
	}
}
