package applicationmessage

import (
	"context"
	"errors"
	"strings"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

var ErrInvalidAccountLifecycle = errors.New("account lifecycle message is invalid")

func (s *Service) CreateAccountLifecycle(
	ctx context.Context,
	notification domainaccount.AccountLifecycleNotification,
) (*CreateResult, error) {
	validated, err := domainaccount.NewAccountLifecycleNotification(
		notification.RecipientID,
		notification.Operation,
		notification.ReasonCode,
		notification.AuthVersion,
		notification.OccurredAt,
	)
	if err != nil || validated.EventID != strings.TrimSpace(notification.EventID) {
		return nil, ErrInvalidAccountLifecycle
	}
	title, content, err := AccountLifecycleMessageContent(
		validated.Operation, validated.ReasonCode,
	)
	if err != nil {
		return nil, err
	}
	return s.CreateFromEvent(
		ctx, validated.RecipientID, domainmessage.TypeSystem,
		title, content, validated.EventID, validated.EventID,
	)
}

func AccountLifecycleMessageContent(
	operation domainaccount.AccountManagementOperation,
	reasonCode string,
) (string, string, error) {
	reasonCode = strings.TrimSpace(reasonCode)
	if !domainaccount.ValidAccountManagementReason(operation, reasonCode) {
		return "", "", ErrInvalidAccountLifecycle
	}
	reason, ok := accountLifecycleReasonLabel(operation, reasonCode)
	if !ok {
		return "", "", ErrInvalidAccountLifecycle
	}
	switch operation {
	case domainaccount.AccountOperationFreeze:
		return "账号已被冻结", "你的账号已被冻结，原因：" + reason +
			"。如有疑问，请联系管理员。", nil
	case domainaccount.AccountOperationUnfreeze:
		return "账号已解冻", "你的账号已解冻，原因：" + reason +
			"。你可以重新登录 Frux。", nil
	default:
		return "", "", ErrInvalidAccountLifecycle
	}
}

func IsTerminalAccountLifecycleError(err error) bool {
	return errors.Is(err, ErrInvalidAccountLifecycle) ||
		errors.Is(err, domainaccount.ErrInvalidAccountNotification) ||
		errors.Is(err, domainmessage.ErrInvalidUserID) ||
		errors.Is(err, domainmessage.ErrInvalidMessageType) ||
		errors.Is(err, domainmessage.ErrEmptyTitle) ||
		errors.Is(err, domainmessage.ErrTitleTooLong) ||
		errors.Is(err, domainmessage.ErrEmptyContent) ||
		errors.Is(err, domainmessage.ErrContentTooLong) ||
		errors.Is(err, domainmessage.ErrEventIDTooLong) ||
		errors.Is(err, domainmessage.ErrIdempotencyKeyTooLong)
}

func accountLifecycleReasonLabel(
	operation domainaccount.AccountManagementOperation,
	reasonCode string,
) (string, bool) {
	switch operation {
	case domainaccount.AccountOperationFreeze:
		switch reasonCode {
		case domainaccount.AccountReasonPolicyViolation:
			return "违反平台规则", true
		case domainaccount.AccountReasonAbuse:
			return "存在滥用行为", true
		case domainaccount.AccountReasonSecurityRisk:
			return "存在账号安全风险", true
		}
	case domainaccount.AccountOperationUnfreeze:
		switch reasonCode {
		case domainaccount.AccountReasonAppealApproved:
			return "申诉已通过", true
		case domainaccount.AccountReasonIssueResolved:
			return "相关问题已解决", true
		case domainaccount.AccountReasonManualCorrection:
			return "已完成人工纠正", true
		}
	}
	return "", false
}
