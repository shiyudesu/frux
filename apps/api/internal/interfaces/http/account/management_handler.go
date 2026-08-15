package interfaceshttpaccount

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxAccountManagementBodyBytes = 4 << 10

type ManagementHandler struct {
	service *applicationaccount.ManagementService
}

type managedAccountResponse struct {
	ID                 int64     `json:"id"`
	Account            string    `json:"account"`
	Nickname           string    `json:"nickname"`
	AvatarURL          string    `json:"avatar_url"`
	Bio                string    `json:"bio"`
	Gender             int       `json:"gender"`
	Status             int       `json:"status"`
	StatusName         string    `json:"status_name"`
	Version            int64     `json:"version"`
	FollowingCount     int       `json:"following_count"`
	FollowerCount      int       `json:"follower_count"`
	PublicWorkCount    int       `json:"public_work_count"`
	PrivateWorkCount   int       `json:"private_work_count"`
	ReceivedLikeCount  int       `json:"received_like_count"`
	ActiveSessionCount int       `json:"active_session_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type managedAccountListResponse struct {
	Items      []managedAccountResponse `json:"items"`
	NextCursor string                   `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
}

type manageAccountRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	ReasonCode      string `json:"reason_code"`
}

type manageAccountResponse struct {
	UserID              int64     `json:"user_id"`
	Operation           string    `json:"operation"`
	Status              int       `json:"status"`
	StatusName          string    `json:"status_name"`
	Version             int64     `json:"version"`
	RevokedSessionCount int64     `json:"revoked_session_count"`
	OccurredAt          time.Time `json:"occurred_at"`
	Replayed            bool      `json:"replayed"`
	AuditCommitted      bool      `json:"audit_committed"`
}

func NewManagementHandler(service *applicationaccount.ManagementService) *ManagementHandler {
	return &ManagementHandler{service: service}
}

func (h *ManagementHandler) List(ctx context.Context, c *app.RequestContext) {
	request, err := parseManagedAccountListRequest(c)
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	page, err := h.service.List(ctx, request)
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	response := managedAccountListResponse{
		Items:      make([]managedAccountResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor, HasMore: page.HasMore,
	}
	for _, account := range page.Items {
		response.Items = append(response.Items, managedAccountResponseFromDomain(account))
	}
	c.JSON(http.StatusOK, response)
}

func (h *ManagementHandler) Get(ctx context.Context, c *app.RequestContext) {
	userID, err := parsePositiveUserID(c.Param("userId"))
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	account, err := h.service.Get(ctx, userID)
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	c.JSON(http.StatusOK, managedAccountResponseFromDomain(account))
}

func (h *ManagementHandler) Freeze(ctx context.Context, c *app.RequestContext) {
	h.manage(ctx, c, domainaccount.AccountOperationFreeze)
}

func (h *ManagementHandler) Unfreeze(ctx context.Context, c *app.RequestContext) {
	h.manage(ctx, c, domainaccount.AccountOperationUnfreeze)
}

func (h *ManagementHandler) RevokeSessions(ctx context.Context, c *app.RequestContext) {
	h.manage(ctx, c, domainaccount.AccountOperationRevokeSessions)
}

func (h *ManagementHandler) manage(
	ctx context.Context,
	c *app.RequestContext,
	operation domainaccount.AccountManagementOperation,
) {
	userID, err := parsePositiveUserID(c.Param("userId"))
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(
			c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
			"admin permission denied",
		)
		return
	}
	var request manageAccountRequest
	if err := interfaceshttpbinding.BindStrictJSON(
		c, &request, maxAccountManagementBodyBytes,
	); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	input := applicationaccount.ManageAccountRequest{
		ActorID: principal.UserID, UserID: userID,
		ExpectedVersion: request.ExpectedVersion, ReasonCode: request.ReasonCode,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	}
	var result *domainaccount.AccountManagementResult
	switch operation {
	case domainaccount.AccountOperationFreeze:
		result, err = h.service.Freeze(ctx, input)
	case domainaccount.AccountOperationUnfreeze:
		result, err = h.service.Unfreeze(ctx, input)
	default:
		result, err = h.service.RevokeSessions(ctx, input)
	}
	if err != nil {
		writeManagedAccountError(c, err)
		return
	}
	c.JSON(http.StatusOK, manageAccountResponse{
		UserID: result.UserID, Operation: string(result.Operation),
		Status: result.Status, StatusName: managedAccountStatusName(result.Status),
		Version: result.Version, RevokedSessionCount: result.RevokedSessionCount,
		OccurredAt: result.OccurredAt, Replayed: result.Replayed, AuditCommitted: true,
	})
}

func parseManagedAccountListRequest(
	c *app.RequestContext,
) (applicationaccount.ManagedAccountListRequest, error) {
	userID, err := parseOptionalManagedAccountID(c.Query("user_id"))
	if err != nil {
		return applicationaccount.ManagedAccountListRequest{}, err
	}
	status, err := parseManagedAccountStatus(c.Query("status"))
	if err != nil {
		return applicationaccount.ManagedAccountListRequest{}, err
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return applicationaccount.ManagedAccountListRequest{}, domainaccount.ErrInvalidManagedAccountLimit
		}
	}
	return applicationaccount.ManagedAccountListRequest{
		UserID: userID, Search: c.Query("query"), Status: status,
		Cursor: c.Query("cursor"), Limit: limit,
	}, nil
}

func parseOptionalManagedAccountID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return parsePositiveUserID(raw)
}

func parseManagedAccountStatus(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return 0, nil
	case "normal":
		return domainaccount.StatusNormal, nil
	case "frozen":
		return domainaccount.StatusFrozen, nil
	case "cancelled":
		return domainaccount.StatusCancelled, nil
	default:
		return 0, domainaccount.ErrInvalidManagedAccountStatus
	}
}

func managedAccountResponseFromDomain(
	account *domainaccount.ManagedAccount,
) managedAccountResponse {
	return managedAccountResponse{
		ID: account.ID, Account: account.Account, Nickname: account.Nickname,
		AvatarURL: account.AvatarURL, Bio: account.Bio, Gender: account.Gender,
		Status: account.Status, StatusName: managedAccountStatusName(account.Status),
		Version: account.Version, FollowingCount: account.FollowingCount,
		FollowerCount: account.FollowerCount, PublicWorkCount: account.PublicWorkCount,
		PrivateWorkCount: account.PrivateWorkCount, ReceivedLikeCount: account.ReceivedLikeCount,
		ActiveSessionCount: account.ActiveSessionCount,
		CreatedAt:          account.CreatedAt, UpdatedAt: account.UpdatedAt,
	}
}

func managedAccountStatusName(status int) string {
	switch status {
	case domainaccount.StatusNormal:
		return "normal"
	case domainaccount.StatusFrozen:
		return "frozen"
	case domainaccount.StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

func writeManagedAccountError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrInvalidManagedAccountCursor):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminUserAccountCursorInvalid,
			"invalid managed account cursor",
		)
	case errors.Is(err, domainaccount.ErrInvalidUserID),
		errors.Is(err, domainaccount.ErrInvalidManagedAccountQuery),
		errors.Is(err, domainaccount.ErrManagedAccountQueryTooLong),
		errors.Is(err, domainaccount.ErrInvalidManagedAccountStatus),
		errors.Is(err, domainaccount.ErrInvalidManagedAccountLimit),
		errors.Is(err, domainaccount.ErrInvalidAuthVersion),
		errors.Is(err, domainaccount.ErrInvalidAccountManagementOperation),
		errors.Is(err, domainaccount.ErrInvalidAccountManagementReason),
		errors.Is(err, domainaccount.ErrAccountManagementIdempotencyKeyRequired),
		errors.Is(err, domainaccount.ErrAccountManagementIdempotencyKeyTooLong):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminUserAccountValidationFailed,
			"invalid managed account request",
		)
	case errors.Is(err, domainaccount.ErrManagedAccountNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeAdminUserAccountNotFound,
			"managed account not found",
		)
	case errors.Is(err, domainaccount.ErrAccountManagementVersionConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAdminUserAccountVersionConflict,
			"managed account version conflict",
		)
	case errors.Is(err, domainaccount.ErrInvalidAccountManagementTransition):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAdminUserAccountStateConflict,
			"managed account state conflict",
		)
	case errors.Is(err, domainaccount.ErrAccountManagementIdempotencyConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAdminUserAccountIdempotencyConflict,
			"managed account idempotency conflict",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminUserAccountUnavailable,
			"managed account unavailable", err,
		)
	}
}
