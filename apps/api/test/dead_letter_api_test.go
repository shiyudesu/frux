package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	applicationdeadletter "github.com/shiyudesu/frux/internal/application/deadletter"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpdeadletter "github.com/shiyudesu/frux/internal/interfaces/http/deadletter"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type apiDeadLetterInspector struct{}

func (apiDeadLetterInspector) ListDeadLetterQueues(context.Context) ([]domaindeadletter.QueueSummary, error) {
	return []domaindeadletter.QueueSummary{{
		Consumer: "action_changed", Queue: "frux.interaction.action_changed.dlq.q2",
		Messages: 2, MessagesReady: 2, State: "running",
	}}, nil
}

func (apiDeadLetterInspector) PreviewDeadLetterQueue(
	_ context.Context,
	queue string,
	_ int,
) ([]domaindeadletter.MessagePreview, error) {
	return []domaindeadletter.MessagePreview{{
		MessageID: "action-1", OriginalEventID: "action-1",
		Exchange: "frux.interaction", RoutingKey: "interaction.action_changed",
		PayloadBytes: 42, PayloadSHA256: "abc", JSONValid: true,
		JSONFields: []string{"event_id"},
	}}, nil
}

type apiDeadLetterBroker struct {
	mu     sync.Mutex
	claims map[string]*apiDeadLetterClaim
}

func (b *apiDeadLetterBroker) ClaimDeadLetter(
	_ context.Context,
	_ string,
	messageID string,
) (applicationdeadletter.ReplayClaim, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	claim := b.claims[messageID]
	if claim == nil || claim.consumed {
		return nil, domaindeadletter.ErrMessageNotFound
	}
	claim.consumed = true
	return claim, nil
}

type apiDeadLetterClaim struct {
	metadata   domaindeadletter.ReplayMetadata
	publishErr error
	consumed   bool
	acked      bool
}

func (c *apiDeadLetterClaim) Metadata() domaindeadletter.ReplayMetadata { return c.metadata }
func (c *apiDeadLetterClaim) Publish(context.Context, string) error     { return c.publishErr }
func (c *apiDeadLetterClaim) Ack() error {
	c.acked = true
	return nil
}
func (c *apiDeadLetterClaim) Nack() error {
	c.consumed = false
	return nil
}

type apiDeadLetterAudit struct {
	mu    sync.Mutex
	facts []*domainadminaudit.Fact
}

func (a *apiDeadLetterAudit) Append(_ context.Context, fact *domainadminaudit.Fact) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.facts = append(a.facts, fact)
	return nil
}

func TestDeadLetterAdminAPIFlow(t *testing.T) {
	queue := "frux.interaction.action_changed.dlq.q2"
	broker := &apiDeadLetterBroker{claims: map[string]*apiDeadLetterClaim{
		"action-1": {metadata: domaindeadletter.ReplayMetadata{
			Queue: queue, MessageID: "action-1", OriginalEventID: "action-1",
		}},
		"action-timeout": {
			metadata: domaindeadletter.ReplayMetadata{
				Queue: queue, MessageID: "action-timeout", OriginalEventID: "action-timeout",
			},
			publishErr: context.DeadlineExceeded,
		},
	}}
	audit := &apiDeadLetterAudit{}
	service := applicationdeadletter.New(apiDeadLetterInspector{}, broker, audit,
		applicationdeadletter.WithReplayIDGenerator(func() string {
			return "replay-0123456789abcdef0123456789abcdef"
		}),
	)
	handler := interfaceshttpdeadletter.New(service)
	jwtManager, err := infrajwt.NewManager("dead-letter-test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	principals := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		10: domainaccount.RestoreAdminPrincipal(10, domainaccount.StatusNormal, domainaccount.RoleOperator),
		11: domainaccount.RestoreAdminPrincipal(11, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	}}
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group("/api/admin", interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager))
	requireGovernance := interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionGovernanceExecute,
	)
	admin.GET("/dead-letter-queues", requireGovernance, handler.List)
	admin.GET("/dead-letter-queues/:queue/messages", requireGovernance, handler.Preview)
	admin.POST("/dead-letter-messages/:messageId/replay", requireGovernance, handler.Replay)

	operatorToken := signAdminAuthorizationToken(t, jwtManager, 10, domainaccount.RoleUser)
	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 11, domainaccount.RoleAdmin)

	forbidden := performDeadLetterRequest(
		router, http.MethodGet, "/api/admin/dead-letter-queues", reviewerToken, nil,
	)
	requireAdminAuthorizationError(
		t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
	)
	if bytes.Contains(forbidden.Body.Bytes(), []byte(queue)) {
		t.Fatal("forbidden dead-letter response leaked queue metadata")
	}

	list := performDeadLetterRequest(
		router, http.MethodGet, "/api/admin/dead-letter-queues", operatorToken, nil,
	)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(queue)) {
		t.Fatalf("unexpected dead-letter list status=%d body=%s", list.Code, list.Body.String())
	}
	preview := performDeadLetterRequest(
		router, http.MethodGet,
		"/api/admin/dead-letter-queues/"+queue+"/messages?limit=1",
		operatorToken, nil,
	)
	if preview.Code != http.StatusOK ||
		!bytes.Contains(preview.Body.Bytes(), []byte(`"original_event_id":"action-1"`)) ||
		bytes.Contains(preview.Body.Bytes(), []byte(`"payload"`)) {
		t.Fatalf("unexpected redacted preview status=%d body=%s", preview.Code, preview.Body.String())
	}

	confirmed := performDeadLetterRequest(
		router, http.MethodPost, "/api/admin/dead-letter-messages/action-1/replay",
		operatorToken, map[string]any{"queue": queue, "reason": "operator_retry"},
	)
	if confirmed.Code != http.StatusOK ||
		!bytes.Contains(confirmed.Body.Bytes(), []byte(`"original_event_id":"action-1"`)) ||
		!broker.claims["action-1"].acked {
		t.Fatalf("unexpected confirmed replay status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}

	duplicate := performDeadLetterRequest(
		router, http.MethodPost, "/api/admin/dead-letter-messages/action-1/replay",
		operatorToken, map[string]any{"queue": queue, "reason": "duplicate_retry"},
	)
	if duplicate.Code != http.StatusNotFound {
		t.Fatalf("duplicate replay status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	timeout := performDeadLetterRequest(
		router, http.MethodPost, "/api/admin/dead-letter-messages/action-timeout/replay",
		operatorToken, map[string]any{"queue": queue, "reason": "operator_retry"},
	)
	if timeout.Code != http.StatusServiceUnavailable || broker.claims["action-timeout"].acked {
		t.Fatalf("timeout replay status=%d body=%s", timeout.Code, timeout.Body.String())
	}
	if broker.claims["action-timeout"].consumed {
		t.Fatal("timeout replay did not leave the dead-letter message available")
	}

	audit.mu.Lock()
	defer audit.mu.Unlock()
	if len(audit.facts) != 3 ||
		audit.facts[0].Outcome() != domainadminaudit.OutcomeSuccess ||
		audit.facts[1].Outcome() != domainadminaudit.OutcomeFailure ||
		audit.facts[2].Outcome() != domainadminaudit.OutcomeFailure {
		t.Fatalf("unexpected replay audit outcomes: %#v", audit.facts)
	}
}

func performDeadLetterRequest(
	router *server.Hertz,
	method, path, token string,
	body map[string]any,
) *ut.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	headers := []ut.Header{{Key: "Authorization", Value: "Bearer " + token}}
	var requestBody *ut.Body
	if body != nil {
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
		requestBody = &ut.Body{Body: bytes.NewReader(payload), Len: len(payload)}
	}
	return ut.PerformRequest(router.Engine, method, path, requestBody, headers...)
}
