package inframq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyConsumerDrainedChecksReadyAndUnacknowledged(t *testing.T) {
	state := managementQueue{}
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(state)
	}))
	defer server.Close()

	cfg := testRabbitMQConfig()
	cfg.ManagementURL = server.URL
	cfg.ManagementUsername = "guest"
	cfg.ManagementPassword = "guest"
	rabbit := &RabbitMQ{config: normalizeRabbitMQConfig(cfg)}
	manager := NewDeadLetterManager(rabbit, cfg)

	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); err != nil {
		t.Fatalf("drained queue: %v", err)
	}

	state.MessagesReady = 1
	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); !errors.Is(err, ErrConsumerNotDrained) {
		t.Fatalf("ready backlog error = %v", err)
	}

	state.MessagesReady = 0
	state.MessagesUnacknowledged = 1
	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); !errors.Is(err, ErrConsumerNotDrained) {
		t.Fatalf("unacknowledged backlog error = %v", err)
	}
}
