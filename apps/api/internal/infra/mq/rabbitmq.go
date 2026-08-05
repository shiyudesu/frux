package inframq

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const defaultInteractionExchange = "frux.interaction"
const defaultActionChangedQueue = "frux.interaction.action_changed"
const profileActionChangedQueue = "frux.recommendation.action_changed"
const defaultActionChangedRouting = "interaction.action_changed"
const defaultVideoExchange = "frux.video"
const defaultVideoPublishedQueue = "frux.video.published"
const defaultVideoEmbeddingQueue = "frux.video.embedding"
const defaultVideoPublishedRouting = "video.published"
const defaultExposureExchange = "frux.exposure"
const defaultViewEventRecordedQueue = "frux.exposure.view_event_recorded"
const defaultViewEventRecordedRouting = "exposure.view_event_recorded"
const defaultMediaExchange = "frux.media"
const defaultMediaProcessingQueue = "frux.media.processing"
const defaultMediaProcessingRouting = "media.processing.requested"
const viewEventConsumerRetryDelay = time.Second

var ErrEmptyRabbitMQURL = errors.New("rabbitmq url is empty")
var ErrPublisherConfirmUnavailable = errors.New("rabbitmq publisher confirm unavailable")
var ErrPublishNotAcknowledged = errors.New("rabbitmq publish not acknowledged")

type RabbitMQ struct {
	conn                     *amqp.Connection
	publishChannel           *amqp.Channel
	actionPublishChannel     *amqp.Channel
	viewEventPublishChannel  *amqp.Channel
	viewEventPublishConn     *amqp.Connection
	viewEventPublishMu       sync.Mutex
	consumerChannel          *amqp.Channel
	actionConsumerChannel    *amqp.Channel
	actionConsumerConn       *amqp.Connection
	actionConsumerMu         sync.Mutex
	viewEventConsumerChannel *amqp.Channel
	viewEventConsumerConn    *amqp.Connection
	viewEventConsumerMu      sync.Mutex
	mediaPublishChannel      *amqp.Channel
	mediaPublishMu           sync.Mutex
	mediaConsumerChannel     *amqp.Channel
	videoPublishMu           sync.Mutex
	config                   infraconfig.RabbitMQConfig
}

func NewRabbitMQ(cfg infraconfig.RabbitMQConfig) (*RabbitMQ, error) {
	cfg = normalizeRabbitMQConfig(cfg)
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, ErrEmptyRabbitMQURL
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, err
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	actionPublishChannel, err := conn.Channel()
	if err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := actionPublishChannel.Confirm(false); err != nil {
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	viewEventPublishChannel, err := conn.Channel()
	if err != nil {
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := viewEventPublishChannel.Confirm(false); err != nil {
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	consumerChannel, err := conn.Channel()
	if err != nil {
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	viewEventConsumerChannel, err := conn.Channel()
	if err != nil {
		_ = consumerChannel.Close()
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	mediaPublishChannel, err := conn.Channel()
	if err != nil {
		_ = viewEventConsumerChannel.Close()
		_ = consumerChannel.Close()
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := mediaPublishChannel.Confirm(false); err != nil {
		_ = mediaPublishChannel.Close()
		_ = viewEventConsumerChannel.Close()
		_ = consumerChannel.Close()
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	mediaConsumerChannel, err := conn.Channel()
	if err != nil {
		_ = mediaPublishChannel.Close()
		_ = viewEventConsumerChannel.Close()
		_ = consumerChannel.Close()
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := mediaConsumerChannel.Qos(1, 0, false); err != nil {
		_ = mediaConsumerChannel.Close()
		_ = mediaPublishChannel.Close()
		_ = viewEventConsumerChannel.Close()
		_ = consumerChannel.Close()
		_ = viewEventPublishChannel.Close()
		_ = actionPublishChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}

	client := &RabbitMQ{
		conn:                     conn,
		publishChannel:           channel,
		actionPublishChannel:     actionPublishChannel,
		viewEventPublishChannel:  viewEventPublishChannel,
		consumerChannel:          consumerChannel,
		viewEventConsumerChannel: viewEventConsumerChannel,
		mediaPublishChannel:      mediaPublishChannel,
		mediaConsumerChannel:     mediaConsumerChannel,
		config:                   cfg,
	}
	if err := client.ensureTopology(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (r *RabbitMQ) Close() error {
	if r == nil {
		return nil
	}
	if r.publishChannel != nil {
		_ = r.publishChannel.Close()
	}
	if r.actionPublishChannel != nil {
		_ = r.actionPublishChannel.Close()
	}
	if r.viewEventPublishChannel != nil {
		_ = r.viewEventPublishChannel.Close()
	}
	if r.viewEventPublishConn != nil {
		_ = r.viewEventPublishConn.Close()
	}
	if r.consumerChannel != nil {
		_ = r.consumerChannel.Close()
	}
	r.actionConsumerMu.Lock()
	r.resetActionConsumerLocked()
	r.actionConsumerMu.Unlock()
	if r.mediaPublishChannel != nil {
		_ = r.mediaPublishChannel.Close()
	}
	if r.mediaConsumerChannel != nil {
		_ = r.mediaConsumerChannel.Close()
	}
	r.viewEventConsumerMu.Lock()
	r.resetViewEventConsumerLocked()
	r.viewEventConsumerMu.Unlock()
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func (r *RabbitMQ) PublishMediaProcessingRequested(ctx context.Context, event *applicationmedia.ProcessingRequestedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	r.mediaPublishMu.Lock()
	defer r.mediaPublishMu.Unlock()
	confirmation, err := r.mediaPublishChannel.PublishWithDeferredConfirmWithContext(
		ctx,
		r.config.MediaExchange,
		r.config.MediaProcessingRouting,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json", DeliveryMode: amqp.Persistent,
			MessageId: event.EventID, Timestamp: time.Now().UTC(), Body: content,
		},
	)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return ErrPublisherConfirmUnavailable
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !acknowledged {
		return ErrPublishNotAcknowledged
	}
	return nil
}

func (r *RabbitMQ) PublishActionChanged(ctx context.Context, event *applicationinteraction.ActionChangedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	confirmation, err := r.actionPublishChannel.PublishWithDeferredConfirmWithContext(
		ctx,
		r.config.InteractionExchange,
		r.config.ActionChangedRouting,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    event.EventID,
			Timestamp:    time.Now(),
			Body:         content,
		},
	)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return ErrPublisherConfirmUnavailable
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !acknowledged {
		return ErrPublishNotAcknowledged
	}
	return nil
}

func (r *RabbitMQ) PublishVideoPublished(ctx context.Context, event *applicationvideo.PublishedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	r.videoPublishMu.Lock()
	defer r.videoPublishMu.Unlock()
	confirmation, err := r.publishChannel.PublishWithDeferredConfirmWithContext(
		ctx,
		r.config.VideoExchange,
		r.config.VideoPublishedRouting,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    event.EventID,
			Timestamp:    time.Now(),
			Body:         content,
		},
	)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return ErrPublisherConfirmUnavailable
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !acknowledged {
		return ErrPublishNotAcknowledged
	}
	return nil
}

func (r *RabbitMQ) PublishViewEventRecorded(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	r.viewEventPublishMu.Lock()
	defer r.viewEventPublishMu.Unlock()
	if err := r.ensureViewEventPublisher(); err != nil {
		return err
	}
	confirmation, err := r.viewEventPublishChannel.PublishWithDeferredConfirmWithContext(
		ctx,
		r.config.ExposureExchange,
		r.config.ViewEventRecordedRouting,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    event.EventID,
			Timestamp:    time.Now(),
			Body:         content,
			Headers: amqp.Table{
				"x-frux-event-id":      event.EventID,
				"x-frux-view-event-id": event.ViewEventID,
				"x-frux-user-id":       event.UserID,
			},
		},
	)
	if err != nil {
		r.resetViewEventPublisher()
		return err
	}
	if confirmation == nil {
		r.resetViewEventPublisher()
		return ErrPublisherConfirmUnavailable
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		r.resetViewEventPublisher()
		return err
	}
	if !acknowledged {
		r.resetViewEventPublisher()
		return ErrPublishNotAcknowledged
	}
	return nil
}

func (r *RabbitMQ) ensureViewEventPublisher() error {
	if r.viewEventPublishChannel != nil && !r.viewEventPublishChannel.IsClosed() {
		return nil
	}
	var conn *amqp.Connection
	if r.viewEventPublishConn != nil && !r.viewEventPublishConn.IsClosed() {
		conn = r.viewEventPublishConn
	} else if r.conn != nil && !r.conn.IsClosed() {
		conn = r.conn
	} else {
		created, err := amqp.Dial(r.config.URL)
		if err != nil {
			return err
		}
		r.viewEventPublishConn = created
		conn = created
	}
	channel, err := conn.Channel()
	if err != nil {
		if r.viewEventPublishConn != nil {
			_ = r.viewEventPublishConn.Close()
			r.viewEventPublishConn = nil
		}
		return err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		if r.viewEventPublishConn != nil {
			_ = r.viewEventPublishConn.Close()
			r.viewEventPublishConn = nil
		}
		return err
	}
	if err := channel.ExchangeDeclare(r.config.ExposureExchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		if r.viewEventPublishConn != nil {
			_ = r.viewEventPublishConn.Close()
			r.viewEventPublishConn = nil
		}
		return err
	}
	r.viewEventPublishChannel = channel
	return nil
}

func (r *RabbitMQ) resetViewEventPublisher() {
	if r.viewEventPublishChannel != nil {
		_ = r.viewEventPublishChannel.Close()
		r.viewEventPublishChannel = nil
	}
	if r.viewEventPublishConn != nil {
		_ = r.viewEventPublishConn.Close()
		r.viewEventPublishConn = nil
	}
}

func (r *RabbitMQ) ConsumeActionChanged(ctx context.Context, handler func(context.Context, *applicationinteraction.ActionChangedEvent) error) error {
	deliveries, err := r.consumeActionDeliveries(ctx)
	if err != nil {
		return err
	}

	go superviseActionDeliveries(
		ctx,
		deliveries,
		func(ctx context.Context) (<-chan amqp.Delivery, error) {
			r.actionConsumerMu.Lock()
			r.resetActionConsumerLocked()
			r.actionConsumerMu.Unlock()
			return r.consumeActionDeliveries(ctx)
		},
		func(delivery amqp.Delivery) {
			r.handleActionDelivery(ctx, delivery, handler)
		},
		viewEventConsumerRetryDelay,
	)
	return nil
}

func (r *RabbitMQ) ConsumeVideoPublished(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueue(ctx, r.config.VideoPublishedQueue, handler)
}

func (r *RabbitMQ) ConsumeVideoPublishedForEmbedding(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueue(ctx, r.config.VideoEmbeddingQueue, handler)
}

func (r *RabbitMQ) consumeVideoPublishedQueue(ctx context.Context, queue string, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	deliveries, err := r.consumerChannel.ConsumeWithContext(
		ctx,
		queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go func() {
		for delivery := range deliveries {
			start := time.Now()
			var event applicationvideo.PublishedEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_video_published_decode", time.Since(start), err)
				_ = delivery.Nack(false, false)
				continue
			}
			if err := handler(ctx, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), err)
				_ = delivery.Nack(false, true)
				continue
			}
			inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), nil)
			_ = delivery.Ack(false)
		}
	}()
	return nil
}

func (r *RabbitMQ) ConsumeViewEventRecorded(ctx context.Context, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) error {
	deliveries, err := r.consumeViewEventDeliveries(ctx)
	if err != nil {
		return err
	}

	go superviseViewEventDeliveries(
		ctx,
		deliveries,
		func(ctx context.Context) (<-chan amqp.Delivery, error) {
			r.viewEventConsumerMu.Lock()
			r.resetViewEventConsumerLocked()
			r.viewEventConsumerMu.Unlock()
			return r.consumeViewEventDeliveries(ctx)
		},
		func(delivery amqp.Delivery) {
			r.handleViewEventDelivery(ctx, delivery, handler)
		},
		viewEventConsumerRetryDelay,
	)
	return nil
}

func (r *RabbitMQ) ConsumeMediaProcessingRequested(ctx context.Context, handler func(context.Context, *applicationmedia.ProcessingRequestedEvent) error) error {
	deliveries, err := r.mediaConsumerChannel.ConsumeWithContext(
		ctx, r.config.MediaProcessingQueue, "", false, false, false, false, nil,
	)
	if err != nil {
		return err
	}
	go func() {
		for delivery := range deliveries {
			start := time.Now()
			var event applicationmedia.ProcessingRequestedEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_media_processing_decode", time.Since(start), err)
				_ = delivery.Nack(false, false)
				continue
			}
			if err := handler(ctx, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_media_processing_consume", time.Since(start), err)
				_ = delivery.Nack(false, true)
				continue
			}
			inframetrics.ObserveWorkerJob("mq_media_processing_consume", time.Since(start), nil)
			_ = delivery.Ack(false)
		}
	}()
	return nil
}

func (r *RabbitMQ) consumeViewEventDeliveries(ctx context.Context) (<-chan amqp.Delivery, error) {
	r.viewEventConsumerMu.Lock()
	defer r.viewEventConsumerMu.Unlock()
	if err := r.ensureViewEventConsumerLocked(); err != nil {
		return nil, err
	}

	deliveries, err := r.viewEventConsumerChannel.ConsumeWithContext(
		ctx,
		r.config.ViewEventRecordedQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		r.resetViewEventConsumerLocked()
		return nil, err
	}
	return deliveries, nil
}

func (r *RabbitMQ) consumeActionDeliveries(ctx context.Context) (<-chan amqp.Delivery, error) {
	r.actionConsumerMu.Lock()
	defer r.actionConsumerMu.Unlock()
	if err := r.ensureActionConsumerLocked(); err != nil {
		return nil, err
	}
	deliveries, err := r.actionConsumerChannel.ConsumeWithContext(
		ctx, r.config.ActionChangedQueue, "", false, false, false, false, nil,
	)
	if err != nil {
		r.resetActionConsumerLocked()
		return nil, err
	}
	return deliveries, nil
}

func (r *RabbitMQ) ensureActionConsumerLocked() error {
	if r.actionConsumerChannel != nil && !r.actionConsumerChannel.IsClosed() {
		return nil
	}
	var conn *amqp.Connection
	if r.actionConsumerConn != nil && !r.actionConsumerConn.IsClosed() {
		conn = r.actionConsumerConn
	} else if r.conn != nil && !r.conn.IsClosed() {
		conn = r.conn
	} else {
		created, err := amqp.Dial(r.config.URL)
		if err != nil {
			return err
		}
		r.actionConsumerConn = created
		conn = created
	}
	channel, err := conn.Channel()
	if err != nil {
		r.resetActionConsumerLocked()
		return err
	}
	if err := channel.ExchangeDeclare(r.config.InteractionExchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		r.resetActionConsumerLocked()
		return err
	}
	if _, err := channel.QueueDeclare(r.config.ActionChangedQueue, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		r.resetActionConsumerLocked()
		return err
	}
	if err := channel.QueueBind(r.config.ActionChangedQueue, r.config.ActionChangedRouting, r.config.InteractionExchange, false, nil); err != nil {
		_ = channel.Close()
		r.resetActionConsumerLocked()
		return err
	}
	r.actionConsumerChannel = channel
	return nil
}

func (r *RabbitMQ) resetActionConsumerLocked() {
	if r.actionConsumerChannel != nil {
		_ = r.actionConsumerChannel.Close()
		r.actionConsumerChannel = nil
	}
	if r.actionConsumerConn != nil {
		_ = r.actionConsumerConn.Close()
		r.actionConsumerConn = nil
	}
}

func (r *RabbitMQ) handleActionDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, *applicationinteraction.ActionChangedEvent) error) {
	start := time.Now()
	var event applicationinteraction.ActionChangedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_action_changed_decode", time.Since(start), err)
		_ = delivery.Nack(false, false)
		return
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), err)
		requeue := shouldRequeueActionDelivery(err)
		_ = delivery.Nack(false, requeue)
		if requeue {
			// A failed durable handoff is infrastructure-shaped, not a poison
			// payload. Close this consumer channel so the supervisor backs off
			// and recreates it instead of spinning on an immediate requeue.
			r.actionConsumerMu.Lock()
			r.resetActionConsumerLocked()
			r.actionConsumerMu.Unlock()
		}
		return
	}
	inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
}

// shouldRequeueActionDelivery keeps infrastructure failures visible to the
// supervised consumer while discarding malformed or business-terminal events.
// Accepted actions never reach this path for delayed profile or attribution
// work: those paths are durably handed off before the message is acknowledged.
func shouldRequeueActionDelivery(err error) bool {
	return err != nil && !applicationinteraction.IsTerminalActionEventError(err)
}

func (r *RabbitMQ) ensureViewEventConsumerLocked() error {
	if r.viewEventConsumerChannel != nil && !r.viewEventConsumerChannel.IsClosed() {
		return nil
	}
	var conn *amqp.Connection
	if r.viewEventConsumerConn != nil && !r.viewEventConsumerConn.IsClosed() {
		conn = r.viewEventConsumerConn
	} else if r.conn != nil && !r.conn.IsClosed() {
		conn = r.conn
	} else {
		created, err := amqp.Dial(r.config.URL)
		if err != nil {
			return err
		}
		r.viewEventConsumerConn = created
		conn = created
	}
	channel, err := conn.Channel()
	if err != nil {
		r.resetViewEventConsumerLocked()
		return err
	}
	if err := channel.ExchangeDeclare(r.config.ExposureExchange, "topic", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		r.resetViewEventConsumerLocked()
		return err
	}
	if _, err := channel.QueueDeclare(r.config.ViewEventRecordedQueue, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		r.resetViewEventConsumerLocked()
		return err
	}
	if err := channel.QueueBind(
		r.config.ViewEventRecordedQueue,
		r.config.ViewEventRecordedRouting,
		r.config.ExposureExchange,
		false,
		nil,
	); err != nil {
		_ = channel.Close()
		r.resetViewEventConsumerLocked()
		return err
	}
	r.viewEventConsumerChannel = channel
	return nil
}

func (r *RabbitMQ) resetViewEventConsumerLocked() {
	if r.viewEventConsumerChannel != nil {
		_ = r.viewEventConsumerChannel.Close()
		r.viewEventConsumerChannel = nil
	}
	if r.viewEventConsumerConn != nil {
		_ = r.viewEventConsumerConn.Close()
		r.viewEventConsumerConn = nil
	}
}

func (r *RabbitMQ) handleViewEventDelivery(ctx context.Context, delivery amqp.Delivery, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) {
	start := time.Now()
	var event applicationexposure.ViewEventRecordedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_view_event_decode", time.Since(start), err)
		_ = delivery.Nack(false, false)
		return
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), err)
		_ = delivery.Nack(false, true)
		return
	}
	inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
}

func superviseViewEventDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery),
	retryDelay time.Duration,
) {
	superviseDeliveries(ctx, deliveries, reconsume, handle, retryDelay, "mq_view_event_reconsume")
}

func superviseDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery),
	retryDelay time.Duration,
	job string,
) {
	for {
		for {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-deliveries:
				if !ok {
					goto reconnect
				}
				handle(delivery)
			}
		}

	reconnect:
		for {
			timer := time.NewTimer(retryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			next, err := reconsume(ctx)
			inframetrics.ObserveWorkerJob(job, 0, err)
			if err != nil {
				continue
			}
			deliveries = next
			break
		}
	}
}

func superviseActionDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery),
	retryDelay time.Duration,
) {
	superviseDeliveries(ctx, deliveries, reconsume, handle, retryDelay, "mq_action_changed_reconsume")
}

func (r *RabbitMQ) ensureTopology() error {
	if err := r.publishChannel.ExchangeDeclare(
		r.config.InteractionExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		r.config.ActionChangedQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.QueueBind(
		r.config.ActionChangedQueue,
		r.config.ActionChangedRouting,
		r.config.InteractionExchange,
		false,
		nil,
	); err != nil {
		return err
	}
	// Action profile projection is leased from the accepted-action outbox.
	// Retire the legacy duplicate binding so unavailable embeddings cannot
	// make a second RabbitMQ consumer immediately requeue the same action.
	if _, err := r.publishChannel.QueueDeclare(profileActionChangedQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if err := r.publishChannel.QueueUnbind(profileActionChangedQueue, r.config.ActionChangedRouting, r.config.InteractionExchange, nil); err != nil {
		return err
	}
	if err := r.publishChannel.ExchangeDeclare(
		r.config.VideoExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		r.config.VideoPublishedQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.QueueBind(
		r.config.VideoPublishedQueue,
		r.config.VideoPublishedRouting,
		r.config.VideoExchange,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		r.config.VideoEmbeddingQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.QueueBind(
		r.config.VideoEmbeddingQueue,
		r.config.VideoPublishedRouting,
		r.config.VideoExchange,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.ExchangeDeclare(
		r.config.ExposureExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		r.config.ViewEventRecordedQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.QueueBind(
		r.config.ViewEventRecordedQueue,
		r.config.ViewEventRecordedRouting,
		r.config.ExposureExchange,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.publishChannel.ExchangeDeclare(
		r.config.MediaExchange, "topic", true, false, false, false, nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		r.config.MediaProcessingQueue, true, false, false, false, nil,
	); err != nil {
		return err
	}
	return r.publishChannel.QueueBind(
		r.config.MediaProcessingQueue, r.config.MediaProcessingRouting,
		r.config.MediaExchange, false, nil,
	)
}

func normalizeRabbitMQConfig(cfg infraconfig.RabbitMQConfig) infraconfig.RabbitMQConfig {
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.InteractionExchange = strings.TrimSpace(cfg.InteractionExchange)
	cfg.ActionChangedQueue = strings.TrimSpace(cfg.ActionChangedQueue)
	cfg.ActionChangedRouting = strings.TrimSpace(cfg.ActionChangedRouting)
	if cfg.InteractionExchange == "" {
		cfg.InteractionExchange = defaultInteractionExchange
	}
	if cfg.ActionChangedQueue == "" {
		cfg.ActionChangedQueue = defaultActionChangedQueue
	}
	if cfg.ActionChangedRouting == "" {
		cfg.ActionChangedRouting = defaultActionChangedRouting
	}
	if cfg.VideoExchange == "" {
		cfg.VideoExchange = defaultVideoExchange
	}
	if cfg.VideoPublishedQueue == "" {
		cfg.VideoPublishedQueue = defaultVideoPublishedQueue
	}
	cfg.VideoEmbeddingQueue = strings.TrimSpace(cfg.VideoEmbeddingQueue)
	if cfg.VideoEmbeddingQueue == "" {
		cfg.VideoEmbeddingQueue = defaultVideoEmbeddingQueue
	}
	if cfg.VideoPublishedRouting == "" {
		cfg.VideoPublishedRouting = defaultVideoPublishedRouting
	}
	cfg.ExposureExchange = strings.TrimSpace(cfg.ExposureExchange)
	cfg.ViewEventRecordedQueue = strings.TrimSpace(cfg.ViewEventRecordedQueue)
	cfg.ViewEventRecordedRouting = strings.TrimSpace(cfg.ViewEventRecordedRouting)
	if cfg.ExposureExchange == "" {
		cfg.ExposureExchange = defaultExposureExchange
	}
	if cfg.ViewEventRecordedQueue == "" {
		cfg.ViewEventRecordedQueue = defaultViewEventRecordedQueue
	}
	if cfg.ViewEventRecordedRouting == "" {
		cfg.ViewEventRecordedRouting = defaultViewEventRecordedRouting
	}
	cfg.MediaExchange = strings.TrimSpace(cfg.MediaExchange)
	cfg.MediaProcessingQueue = strings.TrimSpace(cfg.MediaProcessingQueue)
	cfg.MediaProcessingRouting = strings.TrimSpace(cfg.MediaProcessingRouting)
	if cfg.MediaExchange == "" {
		cfg.MediaExchange = defaultMediaExchange
	}
	if cfg.MediaProcessingQueue == "" {
		cfg.MediaProcessingQueue = defaultMediaProcessingQueue
	}
	if cfg.MediaProcessingRouting == "" {
		cfg.MediaProcessingRouting = defaultMediaProcessingRouting
	}
	return cfg
}
