package interfaceshttpmiddleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
)

const ContextAdminPrincipalKey = "admin_principal"

const (
	deniedAuditTimeout              = 250 * time.Millisecond
	deniedAuditMaxConcurrent        = 16
	deniedAuditMaxGlobalPerWindow   = 100
	deniedAuditMaxPerActorPerWindow = 10
	deniedAuditLimiterCapacity      = 1024
	deniedAuditLimiterWindow        = time.Minute
)

var deniedAuditSlots = make(chan struct{}, deniedAuditMaxConcurrent)
var deniedAuditGlobal = &deniedGlobalLimiter{}

type DeniedAttemptRecorder interface {
	RecordDeniedAttempt(ctx context.Context, input applicationadminaudit.BuildInput)
	RecordDeniedAttemptDropped()
}

type AdminPermissionOption func(*adminPermissionConfig)

type adminPermissionConfig struct {
	recorder   DeniedAttemptRecorder
	action     domainadminaudit.Action
	targetType domainadminaudit.TargetType
	targetID   string
	limiter    *deniedAttemptLimiter
}

type deniedAttemptEntry struct {
	windowStart time.Time
	count       int
}

type deniedAttemptLimiter struct {
	mu      sync.Mutex
	entries map[int64]deniedAttemptEntry
}

type deniedGlobalLimiter struct {
	mu          sync.Mutex
	windowStart time.Time
	count       int
}

func WithDeniedAttemptAudit(
	recorder DeniedAttemptRecorder,
	action domainadminaudit.Action,
	targetType domainadminaudit.TargetType,
	targetID string,
) AdminPermissionOption {
	return func(config *adminPermissionConfig) {
		config.recorder = recorder
		config.action = action
		config.targetType = targetType
		config.targetID = strings.TrimSpace(targetID)
		config.limiter = &deniedAttemptLimiter{entries: make(map[int64]deniedAttemptEntry)}
	}
}

func NewRequireAdminPermission(
	reader domainaccount.AdminPrincipalReader,
	required domainaccount.AdminPermission,
	options ...AdminPermissionOption,
) app.HandlerFunc {
	config := adminPermissionConfig{}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	return func(ctx context.Context, c *app.RequestContext) {
		userID, ok := authenticatedUserID(c)
		if !ok {
			interfaceshttpapierror.AbortInvalidAccessToken(c)
			return
		}
		if reader == nil {
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}

		principal, err := reader.FindAdminPrincipalByID(ctx, userID)
		if err != nil {
			if errors.Is(err, domainaccount.ErrUserNotFound) {
				recordDeniedAttempt(c, config, userID, required, "account_missing")
				abortAdminPermissionDenied(c)
				return
			}
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}
		if principal == nil {
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}
		authVersion, ok := authenticationVersion(c)
		if !ok || authVersion != principal.AuthVersion {
			recordDeniedAttempt(c, config, userID, required, "credential_version_changed")
			interfaceshttpapierror.AbortInvalidAdminAccessToken(c)
			return
		}
		if !principal.HasPermission(required) {
			reasonCode := "permission_denied"
			if !principal.Active() {
				reasonCode = "inactive_account"
			}
			recordDeniedAttempt(c, config, userID, required, reasonCode)
			abortAdminPermissionDenied(c)
			return
		}

		c.Set(ContextAdminPrincipalKey, principal)
		c.Next(ctx)
	}
}

func authenticationVersion(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(ContextAuthVersionKey)
	if !exists {
		return 0, false
	}
	version, ok := value.(int64)
	return version, ok && version > 0
}

func recordDeniedAttempt(
	c *app.RequestContext,
	config adminPermissionConfig,
	userID int64,
	permission domainaccount.AdminPermission,
	reasonCode string,
) {
	if config.recorder == nil || !domainadminaudit.ValidAction(config.action) ||
		!domainadminaudit.ValidTargetType(config.targetType) || config.targetID == "" {
		return
	}
	if config.limiter == nil || !config.limiter.allow(userID, time.Now().UTC()) {
		config.recorder.RecordDeniedAttemptDropped()
		return
	}
	if !deniedAuditGlobal.allow(time.Now().UTC()) {
		config.recorder.RecordDeniedAttemptDropped()
		return
	}
	route := c.FullPath()
	if route == "" {
		route = string(c.Path())
	}
	input := applicationadminaudit.BuildInput{
		ActorID: userID, Permission: permission, Action: config.action,
		TargetType: config.targetType, TargetID: config.targetID,
		RequestID: adminRequestID(),
		Detail: map[string]string{
			"http_method": string(c.Method()),
			"reason_code": reasonCode,
			"route":       route,
		},
	}
	select {
	case deniedAuditSlots <- struct{}{}:
	default:
		config.recorder.RecordDeniedAttemptDropped()
		return
	}
	go func(recorder DeniedAttemptRecorder, input applicationadminaudit.BuildInput) {
		defer func() { <-deniedAuditSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), deniedAuditTimeout)
		defer cancel()
		recorder.RecordDeniedAttempt(ctx, input)
	}(config.recorder, input)
}

func (l *deniedAttemptLimiter) allow(actorID int64, now time.Time) bool {
	if l == nil || actorID <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[actorID]
	if exists && now.Sub(entry.windowStart) >= deniedAuditLimiterWindow {
		entry = deniedAttemptEntry{}
		exists = false
	}
	if !exists {
		if len(l.entries) >= deniedAuditLimiterCapacity {
			for id, candidate := range l.entries {
				if now.Sub(candidate.windowStart) >= deniedAuditLimiterWindow {
					delete(l.entries, id)
				}
			}
			if len(l.entries) >= deniedAuditLimiterCapacity {
				return false
			}
		}
		entry = deniedAttemptEntry{windowStart: now}
	}
	if entry.count >= deniedAuditMaxPerActorPerWindow {
		l.entries[actorID] = entry
		return false
	}
	entry.count++
	l.entries[actorID] = entry
	return true
}

func (l *deniedGlobalLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= deniedAuditLimiterWindow {
		l.windowStart = now
		l.count = 0
	}
	if l.count >= deniedAuditMaxGlobalPerWindow {
		return false
	}
	l.count++
	return true
}

func adminRequestID() string {
	return domainadminaudit.NewRequestID()
}

func AdminPrincipalFromContext(c *app.RequestContext) (*domainaccount.AdminPrincipal, bool) {
	value, exists := c.Get(ContextAdminPrincipalKey)
	if !exists {
		return nil, false
	}
	principal, ok := value.(*domainaccount.AdminPrincipal)
	return principal, ok && principal != nil
}

func authenticatedUserID(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func abortAdminPermissionDenied(c *app.RequestContext) {
	interfaceshttpapierror.Abort(
		c,
		http.StatusForbidden,
		interfaceshttpapierror.CodeAdminPermissionDenied,
		"admin permission denied",
	)
}
