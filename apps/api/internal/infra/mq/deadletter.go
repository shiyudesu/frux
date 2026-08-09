package inframq

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	applicationdeadletter "github.com/shiyudesu/frux/internal/application/deadletter"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"

	amqp "github.com/rabbitmq/amqp091-go"
)

const maxReplayHeaders = 16

type DeadLetterManager struct {
	rabbit *RabbitMQ
	config infraconfig.RabbitMQConfig
	client *http.Client
}

type managementQueue struct {
	Name                   string `json:"name"`
	Messages               int64  `json:"messages"`
	MessagesReady          int64  `json:"messages_ready"`
	MessagesUnacknowledged int64  `json:"messages_unacknowledged"`
	Consumers              int    `json:"consumers"`
	State                  string `json:"state"`
}

var ErrConsumerNotDrained = errors.New("rabbitmq consumer queue is not drained")

type managementMessage struct {
	Payload         string               `json:"payload"`
	PayloadEncoding string               `json:"payload_encoding"`
	PayloadBytes    int                  `json:"payload_bytes"`
	Exchange        string               `json:"exchange"`
	RoutingKey      string               `json:"routing_key"`
	Redelivered     bool                 `json:"redelivered"`
	Properties      managementProperties `json:"properties"`
}

type managementProperties struct {
	MessageID   string         `json:"message_id"`
	ContentType string         `json:"content_type"`
	Timestamp   int64          `json:"timestamp"`
	Headers     map[string]any `json:"headers"`
}

func NewDeadLetterManager(rabbit *RabbitMQ, cfg infraconfig.RabbitMQConfig) *DeadLetterManager {
	cfg = normalizeRabbitMQConfig(cfg)
	timeout, err := time.ParseDuration(cfg.ManagementTimeout)
	if err != nil || timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &DeadLetterManager{
		rabbit: rabbit, config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

func (m *DeadLetterManager) RunDepthObserver(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := m.ListDeadLetterQueues(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			inframetrics.ObserveMQRoutingFailure("management_api")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (m *DeadLetterManager) ListDeadLetterQueues(ctx context.Context) ([]domaindeadletter.QueueSummary, error) {
	if err := m.available(); err != nil {
		return nil, err
	}

	specs := m.rabbit.queueSpecs()
	result := make([]domaindeadletter.QueueSummary, 0, len(specs))
	for _, spec := range specs {
		var queue managementQueue
		if err := m.request(ctx, http.MethodGet, "/api/queues/%2F/"+url.PathEscape(spec.DeadQueue), nil, &queue); err != nil {
			return nil, err
		}
		summary := domaindeadletter.QueueSummary{
			Consumer: spec.Consumer, Queue: spec.DeadQueue,
			Messages: queue.Messages, MessagesReady: queue.MessagesReady,
			MessagesUnacked: queue.MessagesUnacknowledged,
			Consumers:       queue.Consumers, State: queue.State,
		}
		result = append(result, summary)
		inframetrics.ObserveMQDeadLetterDepth(spec.Consumer, queue.Messages)
	}
	return result, nil
}

func (m *DeadLetterManager) VerifyConsumerDrained(
	ctx context.Context,
	consumer string,
) error {
	if m == nil || m.rabbit == nil || m.config.ManagementURL == "" {
		return domaindeadletter.ErrInspectionFailed
	}
	queues := m.rabbit.consumerQueues(consumer)
	if len(queues) == 0 {
		return fmt.Errorf("%w: unknown consumer", ErrConsumerNotDrained)
	}
	if spec, ok := m.rabbit.queueSpec(consumer); ok &&
		m.config.DeadLetter.Enabled {
		queues = append(queues, spec.DeadQueue)
	}
	for _, queueName := range queues {
		var queue managementQueue
		if err := m.request(
			ctx,
			http.MethodGet,
			"/api/queues/%2F/"+url.PathEscape(queueName),
			nil,
			&queue,
		); err != nil {
			return err
		}
		if queue.MessagesReady != 0 || queue.MessagesUnacknowledged != 0 {
			return fmt.Errorf(
				"%w: %s ready=%d unacknowledged=%d",
				ErrConsumerNotDrained,
				queueName,
				queue.MessagesReady,
				queue.MessagesUnacknowledged,
			)
		}
	}
	return nil
}

func (m *DeadLetterManager) PreviewDeadLetterQueue(
	ctx context.Context,
	queue string,
	limit int,
) ([]domaindeadletter.MessagePreview, error) {
	spec, ok := m.deadQueueSpec(queue)
	if !ok {
		return nil, domaindeadletter.ErrInvalidQueue
	}
	if limit < 1 || limit > m.config.DeadLetter.PreviewLimit {
		return nil, domaindeadletter.ErrInvalidLimit
	}
	body := map[string]any{
		"count": limit, "ackmode": "ack_requeue_true",
		"encoding": "auto", "truncate": 65536,
	}
	var messages []managementMessage
	if err := m.request(
		ctx, http.MethodPost, "/api/queues/%2F/"+url.PathEscape(queue)+"/get",
		body, &messages,
	); err != nil {
		return nil, err
	}
	result := make([]domaindeadletter.MessagePreview, 0, len(messages))
	for _, message := range messages {
		payload, err := managementPayload(message)
		if err != nil {
			payload = nil
		}
		sum := sha256.Sum256(payload)
		fields, valid := jsonFieldNames(payload)
		originalID := headerString(message.Properties.Headers, "x-frux-original-event-id")
		if originalID == "" {
			originalID = headerString(message.Properties.Headers, "x-frux-event-id")
		}
		if originalID == "" {
			originalID = message.Properties.MessageID
		}
		preview := domaindeadletter.MessagePreview{
			MessageID: message.Properties.MessageID, OriginalEventID: originalID,
			ReplayID:    headerString(message.Properties.Headers, "x-frux-replay-id"),
			ContentType: message.Properties.ContentType,
			Exchange:    spec.Exchange, RoutingKey: spec.RoutingKey,
			PayloadBytes: message.PayloadBytes, PayloadSHA256: hex.EncodeToString(sum[:]),
			JSONValid: valid, JSONFields: fields,
			DeathCount: deathCount(message.Properties.Headers),
		}
		if message.Properties.Timestamp > 0 {
			preview.PublishedAt = time.Unix(message.Properties.Timestamp, 0).UTC()
		}
		result = append(result, preview)
	}
	return result, nil
}

func (m *DeadLetterManager) ClaimDeadLetter(
	ctx context.Context,
	queue, messageID string,
) (applicationdeadletter.ReplayClaim, error) {
	if err := m.available(); err != nil {
		return nil, err
	}
	spec, ok := m.deadQueueSpec(queue)
	if !ok {
		return nil, domaindeadletter.ErrInvalidQueue
	}
	channel, err := m.rabbit.conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, err
	}
	delivery, found, err := channel.Get(queue, false)
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	if !found {
		_ = channel.Close()
		return nil, domaindeadletter.ErrMessageNotFound
	}
	actualID := strings.TrimSpace(delivery.MessageId)
	if actualID == "" {
		actualID = headerValue(delivery.Headers, "x-frux-event-id")
	}
	if actualID != messageID {
		_ = delivery.Nack(false, true)
		_ = channel.Close()
		return nil, domaindeadletter.ErrMessageNotAtHead
	}
	originalID := headerValue(delivery.Headers, "x-frux-original-event-id")
	if originalID == "" {
		originalID = headerValue(delivery.Headers, "x-frux-event-id")
	}
	if originalID == "" {
		originalID = actualID
	}
	if originalID == "" || !validOriginalRoute(delivery.Headers, spec) {
		_ = delivery.Nack(false, true)
		_ = channel.Close()
		return nil, domaindeadletter.ErrReplayFailed
	}
	return &replayClaim{
		channel: channel, delivery: delivery, spec: spec,
		metadata: domaindeadletter.ReplayMetadata{
			Queue: queue, MessageID: actualID, OriginalEventID: originalID,
			Exchange: spec.Exchange, RoutingKey: spec.RoutingKey,
		},
		timeout: m.rabbit.replayTimeout(),
	}, nil
}

func (m *DeadLetterManager) available() error {
	if m == nil || m.rabbit == nil || m.rabbit.conn == nil || m.rabbit.conn.IsClosed() {
		return domaindeadletter.ErrInspectionFailed
	}
	if !m.config.DeadLetter.Enabled {
		return domaindeadletter.ErrInspectionFailed
	}
	return nil
}

func (m *DeadLetterManager) deadQueueSpec(queue string) (queueSpec, bool) {
	if m == nil || m.rabbit == nil {
		return queueSpec{}, false
	}
	for _, spec := range m.rabbit.queueSpecs() {
		if spec.DeadQueue == queue {
			return spec, true
		}
	}
	return queueSpec{}, false
}

func (m *DeadLetterManager) request(
	ctx context.Context,
	method, path string,
	body any,
	target any,
) error {
	if m.config.ManagementURL == "" {
		return domaindeadletter.ErrInspectionFailed
	}
	var reader io.Reader
	if body != nil {
		content, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(content)
	}
	request, err := http.NewRequestWithContext(ctx, method, m.config.ManagementURL+path, reader)
	if err != nil {
		return err
	}
	request.SetBasicAuth(m.config.ManagementUsername, m.config.ManagementPassword)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		inframetrics.ObserveMQRoutingFailure("management_api")
		return fmt.Errorf("rabbitmq management status %d", response.StatusCode)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target)
}

type replayClaim struct {
	mu       sync.Mutex
	channel  *amqp.Channel
	delivery amqp.Delivery
	spec     queueSpec
	metadata domaindeadletter.ReplayMetadata
	timeout  time.Duration
	released bool
}

func (c *replayClaim) Metadata() domaindeadletter.ReplayMetadata {
	return c.metadata
}

func (c *replayClaim) Publish(ctx context.Context, replayID string) error {
	if c == nil || c.channel == nil {
		return domaindeadletter.ErrReplayFailed
	}
	headers, err := replayHeaders(c.delivery.Headers, c.metadata.OriginalEventID, replayID)
	if err != nil {
		return err
	}
	publishCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	returns := c.channel.NotifyReturn(make(chan amqp.Return, 1))
	confirmation, err := c.channel.PublishWithDeferredConfirmWithContext(
		publishCtx, c.spec.Exchange, c.spec.RoutingKey, true, false,
		amqp.Publishing{
			Headers: headers, ContentType: c.delivery.ContentType,
			ContentEncoding: c.delivery.ContentEncoding,
			DeliveryMode:    amqp.Persistent, Priority: c.delivery.Priority,
			CorrelationId: c.delivery.CorrelationId, ReplyTo: c.delivery.ReplyTo,
			Expiration: c.delivery.Expiration, MessageId: c.metadata.OriginalEventID,
			Timestamp: time.Now().UTC(), Type: c.delivery.Type,
			UserId: c.delivery.UserId, AppId: c.delivery.AppId,
			Body: c.delivery.Body,
		},
	)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return ErrPublisherConfirmUnavailable
	}
	acknowledged, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		return err
	}
	if !acknowledged {
		return domaindeadletter.ErrReplayUnconfirmed
	}
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case returned := <-returns:
		inframetrics.ObserveMQRoutingFailure(c.spec.Consumer)
		return fmt.Errorf("%w: %s", domaindeadletter.ErrReplayUnconfirmed, returned.ReplyText)
	case <-timer.C:
		return nil
	case <-publishCtx.Done():
		return publishCtx.Err()
	}
}

func (c *replayClaim) Ack() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released {
		return nil
	}
	if err := c.delivery.Ack(false); err != nil {
		return err
	}
	c.released = true
	return c.channel.Close()
}

func (c *replayClaim) Nack() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released {
		return nil
	}
	err := c.delivery.Nack(false, true)
	c.released = true
	closeErr := c.channel.Close()
	return errors.Join(err, closeErr)
}

func replayHeaders(headers amqp.Table, originalEventID, replayID string) (amqp.Table, error) {
	result := amqp.Table{}
	count := 0
	for key, value := range headers {
		if strings.HasPrefix(strings.ToLower(key), "x-death") ||
			key == "x-first-death-exchange" || key == "x-first-death-queue" ||
			key == "x-first-death-reason" || key == "x-frux-replay-id" {
			continue
		}
		if count >= maxReplayHeaders {
			return nil, domaindeadletter.ErrReplayFailed
		}
		switch typed := value.(type) {
		case string, bool, int8, int16, int32, int64, int, time.Time:
			result[key] = typed
			count++
		case []byte:
			if len(typed) > 256 {
				return nil, domaindeadletter.ErrReplayFailed
			}
			result[key] = append([]byte(nil), typed...)
			count++
		}
	}
	result["x-frux-event-id"] = originalEventID
	result["x-frux-original-event-id"] = originalEventID
	result["x-frux-replay-id"] = replayID
	return result, nil
}

func validOriginalRoute(headers amqp.Table, spec queueSpec) bool {
	if headerValue(headers, "x-first-death-queue") != spec.SourceQueue ||
		headerValue(headers, "x-first-death-exchange") != spec.Exchange {
		return false
	}
	for _, death := range deathEntries(headers["x-death"]) {
		if tableString(death, "queue") == spec.SourceQueue &&
			tableString(death, "exchange") == spec.Exchange &&
			tableStringsContain(death, "routing-keys", spec.RoutingKey) {
			return true
		}
	}
	return false
}

func deathEntries(value any) []amqp.Table {
	switch entries := value.(type) {
	case []any:
		result := make([]amqp.Table, 0, len(entries))
		for _, entry := range entries {
			switch table := entry.(type) {
			case amqp.Table:
				result = append(result, table)
			case map[string]any:
				result = append(result, amqp.Table(table))
			}
		}
		return result
	case []amqp.Table:
		return entries
	default:
		return nil
	}
}

func tableString(table amqp.Table, key string) string {
	switch value := table[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func tableStringsContain(table amqp.Table, key, expected string) bool {
	switch values := table[key].(type) {
	case []any:
		for _, value := range values {
			if strings.TrimSpace(fmt.Sprint(value)) == expected {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if strings.TrimSpace(value) == expected {
				return true
			}
		}
	case string:
		return strings.TrimSpace(values) == expected
	}
	return false
}

func headerValue(headers amqp.Table, key string) string {
	if headers == nil {
		return ""
	}
	switch value := headers[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	case []any:
		if len(value) > 0 {
			return fmt.Sprint(value[0])
		}
	case []string:
		if len(value) > 0 {
			return strings.TrimSpace(value[0])
		}
	}
	return ""
}

func headerString(headers map[string]any, key string) string {
	if headers == nil {
		return ""
	}
	switch value := headers[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		if len(value) > 0 {
			return fmt.Sprint(value[0])
		}
	}
	return ""
}

func deathCount(headers map[string]any) int64 {
	deaths, ok := headers["x-death"].([]any)
	if !ok {
		return 0
	}
	var total int64
	for _, raw := range deaths {
		death, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch count := death["count"].(type) {
		case float64:
			total += int64(count)
		case int64:
			total += count
		}
	}
	return total
}

func managementPayload(message managementMessage) ([]byte, error) {
	if message.PayloadEncoding == "base64" {
		return base64.StdEncoding.DecodeString(message.Payload)
	}
	return []byte(message.Payload), nil
}

func jsonFieldNames(payload []byte) ([]string, bool) {
	var object map[string]json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &object) != nil {
		return nil, false
	}
	fields := make([]string, 0, min(len(object), 20))
	for key := range object {
		if len(fields) >= 20 {
			break
		}
		fields = append(fields, key)
	}
	sort.Strings(fields)
	return fields, true
}

var _ applicationdeadletter.Inspector = (*DeadLetterManager)(nil)
var _ applicationdeadletter.ReplayBroker = (*DeadLetterManager)(nil)
