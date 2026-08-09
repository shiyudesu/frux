package inframq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyConsumerDrainedChecksReadyAndUnacknowledged(t *testing.T) {
	sourceState := managementQueue{}
	dlqState := managementQueue{}
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		response.Header().Set("Content-Type", "application/json")
		state := sourceState
		if strings.Contains(request.RequestURI, ".dlq.") {
			state = dlqState
		}
		_ = json.NewEncoder(response).Encode(state)
	}))
	defer server.Close()

	cfg := testRabbitMQConfig()
	cfg.ManagementURL = server.URL
	cfg.ManagementUsername = "guest"
	cfg.ManagementPassword = "guest"
	cfg.DeadLetter.Enabled = true
	rabbit := &RabbitMQ{config: normalizeRabbitMQConfig(cfg)}
	manager := NewDeadLetterManager(rabbit, cfg)

	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); err != nil {
		t.Fatalf("drained queue: %v", err)
	}

	sourceState = managementQueue{MessagesReady: 1}
	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); !errors.Is(err, ErrConsumerNotDrained) {
		t.Fatalf("ready backlog error = %v", err)
	}

	sourceState = managementQueue{MessagesUnacknowledged: 1}
	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); !errors.Is(err, ErrConsumerNotDrained) {
		t.Fatalf("unacknowledged backlog error = %v", err)
	}
	sourceState = managementQueue{}
	dlqState = managementQueue{MessagesReady: 1}
	if err := manager.VerifyConsumerDrained(
		context.Background(),
		ConsumerActionChanged,
	); !errors.Is(err, ErrConsumerNotDrained) {
		t.Fatalf("DLQ backlog error = %v", err)
	}
}
