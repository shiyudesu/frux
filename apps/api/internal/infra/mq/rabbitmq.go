package inframq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"net"
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

type UncertainPublishError struct {
	cause error
}

func (e *UncertainPublishError) Error() string {
	if e == nil || e.cause == nil {
		return "rabbitmq publish result uncertain"
	}
	return fmt.Sprintf("rabbitmq publish result uncertain: %v", e.cause)
}

func (e *UncertainPublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (*UncertainPublishError) MayHaveAcknowledged() bool {
	return true
}

type RabbitMQ struct {
	conn                     *amqp.Connection
	publishChannel           *amqp.Channel
	actionPublishChannel     *amqp.Channel
	actionPublishConn        *amqp.Connection
	actionPublishMu          sync.Mutex
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
	return newRabbitMQ(cfg, amqp.Dial)
}

func NewRabbitMQContext(
	ctx context.Context,
	cfg infraconfig.RabbitMQConfig,
) (*RabbitMQ, error) {
	dialer := &net.Dialer{}
	return newRabbitMQ(cfg, func(url string) (*amqp.Connection, error) {
		return amqp.DialConfig(url, amqp.Config{
			Dial: func(network, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, address)
			},
		})
	})
}

func newRabbitMQ(
	cfg infraconfig.RabbitMQConfig,
	dial func(string) (*amqp.Connection, error),
) (*RabbitMQ, error) {
	cfg = normalizeRabbitMQConfig(cfg)
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, ErrEmptyRabbitMQURL
	}

	conn, err := dial(cfg.URL)
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
	r.actionPublishMu.Lock()
	r.resetActionPublisher()
	r.actionPublishMu.Unlock()
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
		return &UncertainPublishError{cause: ErrPublisherConfirmUnavailable}
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return &UncertainPublishError{cause: err}
	}
	if !acknowledged {
		return publisherNackError(r.mediaPublishChannel)
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
	r.actionPublishMu.Lock()
	defer r.actionPublishMu.Unlock()
	if err := r.ensureActionPublisher(); err != nil {
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
		r.resetActionPublisher()
		return &UncertainPublishError{cause: err}
	}
	if confirmation == nil {
		return &UncertainPublishError{cause: ErrPublisherConfirmUnavailable}
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		r.resetActionPublisher()
		return &UncertainPublishError{cause: err}
	}
	if !acknowledged {
		result := publisherNackError(r.actionPublishChannel)
		if _, uncertain := result.(*UncertainPublishError); uncertain {
			r.resetActionPublisher()
		}
		return result
	}
	return nil
}

func (r *RabbitMQ) ensureActionPublisher() error {
	if r.actionPublishChannel != nil && !r.actionPublishChannel.IsClosed() {
		return nil
	}
	connection := r.actionPublishConn
	if connection == nil || connection.IsClosed() {
		connection = r.conn
	}
	if connection == nil || connection.IsClosed() {
		created, err := amqp.Dial(r.config.URL)
		if err != nil {
			return err
		}
		r.actionPublishConn = created
		connection = created
	}
	channel, err := connection.Channel()
	if err != nil {
		r.resetActionPublisher()
		return err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		r.resetActionPublisher()
		return err
	}
	if err := channel.ExchangeDeclare(
		r.config.InteractionExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = channel.Close()
		r.resetActionPublisher()
		return err
	}
	r.actionPublishChannel = channel
	return nil
}

func (r *RabbitMQ) resetActionPublisher() {
	if r.actionPublishChannel != nil {
		_ = r.actionPublishChannel.Close()
		r.actionPublishChannel = nil
	}
	if r.actionPublishConn != nil {
		_ = r.actionPublishConn.Close()
		r.actionPublishConn = nil
	}
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
		return &UncertainPublishError{cause: ErrPublisherConfirmUnavailable}
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return &UncertainPublishError{cause: err}
	}
	if !acknowledged {
		return publisherNackError(r.publishChannel)
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
		return &UncertainPublishError{cause: ErrPublisherConfirmUnavailable}
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		r.resetViewEventPublisher()
		return &UncertainPublishError{cause: err}
	}
	if !acknowledged {
		channelClosed := r.viewEventPublishChannel == nil ||
			r.viewEventPublishChannel.IsClosed()
		r.resetViewEventPublisher()
		if channelClosed {
			return &UncertainPublishError{cause: ErrPublishNotAcknowledged}
		}
		return ErrPublishNotAcknowledged
	}
	return nil
}

func publisherNackError(channel *amqp.Channel) error {
	if channel == nil || channel.IsClosed() {
		return &UncertainPublishError{cause: ErrPublishNotAcknowledged}
	}
	return ErrPublishNotAcknowledged
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
	for _, queue := range r.consumerQueues(ConsumerActionChanged) {
		if r.isProtectedQueue(queue) {
			if err := r.consumeProtectedQueue(
				ctx, ConsumerActionChanged, queue,
				func(delivery amqp.Delivery) bool {
					return r.handleActionDelivery(ctx, queue, delivery, handler)
				},
			); err != nil {
				return err
			}
			continue
		}
		if err := r.consumeLegacyActionQueue(ctx, queue, handler); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) consumeLegacyActionQueue(
	ctx context.Context,
	queue string,
	handler func(context.Context, *applicationinteraction.ActionChangedEvent) error,
) error {
	deliveries, err := r.consumeActionDeliveries(ctx, queue)
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
			return r.consumeActionDeliveries(ctx, queue)
		},
		func(delivery amqp.Delivery) bool {
			reconnect := r.handleActionDelivery(ctx, queue, delivery, handler)
			if reconnect {
				r.actionConsumerMu.Lock()
				r.resetActionConsumerLocked()
				r.actionConsumerMu.Unlock()
			}
			return reconnect
		},
		viewEventConsumerRetryDelay,
	)
	return nil
}

func (r *RabbitMQ) ConsumeVideoPublished(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueues(ctx, ConsumerVideoPublished, handler)
}

func (r *RabbitMQ) ConsumeVideoPublishedForEmbedding(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueues(ctx, ConsumerVideoEmbedding, handler)
}

func (r *RabbitMQ) consumeVideoPublishedQueues(ctx context.Context, consumer string, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	for _, queue := range r.consumerQueues(consumer) {
		if r.isProtectedQueue(queue) {
			if err := r.consumeProtectedQueue(
				ctx, consumer, queue,
				func(delivery amqp.Delivery) bool {
					return r.handleVideoPublishedDelivery(ctx, consumer, queue, delivery, handler)
				},
			); err != nil {
				return err
			}
			continue
		}
		if err := r.consumeVideoPublishedQueue(ctx, consumer, queue, handler); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) consumeVideoPublishedQueue(ctx context.Context, consumer, queue string, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeProtectedQueue(
		ctx, consumer, queue,
		func(delivery amqp.Delivery) bool {
			return r.handleVideoPublishedDelivery(ctx, consumer, queue, delivery, handler)
		},
	)
}

func (r *RabbitMQ) handleVideoPublishedDelivery(
	ctx context.Context,
	consumer, queue string,
	delivery amqp.Delivery,
	handler func(context.Context, *applicationvideo.PublishedEvent) error,
) bool {
	start := time.Now()
	var event applicationvideo.PublishedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_video_published_decode", time.Since(start), err)
		r.rejectDelivery(consumer, queue, delivery, false)
		return false
	}
	if event.EventID == "" || event.VideoID <= 0 || event.AuthorID <= 0 || event.PublishedAt.IsZero() {
		err := errors.New("invalid video published event")
		inframetrics.ObserveWorkerJob("mq_video_published_decode", time.Since(start), err)
		r.rejectDelivery(consumer, queue, delivery, false)
		return false
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), err)
		retry := shouldRetryConsumerError(err)
		r.rejectDelivery(consumer, queue, delivery, retry)
		return shouldReconnectProtectedDelivery(queue, retry, r)
	}
	inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
	return false
}

func (r *RabbitMQ) ConsumeViewEventRecorded(ctx context.Context, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) error {
	for _, queue := range r.consumerQueues(ConsumerViewEventRecorded) {
		if r.isProtectedQueue(queue) {
			if err := r.consumeProtectedQueue(
				ctx, ConsumerViewEventRecorded, queue,
				func(delivery amqp.Delivery) bool {
					return r.handleViewEventDelivery(ctx, queue, delivery, handler)
				},
			); err != nil {
				return err
			}
			continue
		}
		if err := r.consumeLegacyViewEventQueue(ctx, queue, handler); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) consumeLegacyViewEventQueue(
	ctx context.Context,
	queue string,
	handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error,
) error {
	deliveries, err := r.consumeViewEventDeliveries(ctx, queue)
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
			return r.consumeViewEventDeliveries(ctx, queue)
		},
		func(delivery amqp.Delivery) bool {
			reconnect := r.handleViewEventDelivery(ctx, queue, delivery, handler)
			if reconnect {
				r.viewEventConsumerMu.Lock()
				r.resetViewEventConsumerLocked()
				r.viewEventConsumerMu.Unlock()
			}
			return reconnect
		},
		viewEventConsumerRetryDelay,
	)
	return nil
}

func (r *RabbitMQ) ConsumeMediaProcessingRequested(ctx context.Context, handler func(context.Context, *applicationmedia.ProcessingRequestedEvent) error) error {
	for _, queue := range r.consumerQueues(ConsumerMediaProcessing) {
		if r.isProtectedQueue(queue) {
			if err := r.consumeProtectedQueue(
				ctx, ConsumerMediaProcessing, queue,
				func(delivery amqp.Delivery) bool {
					return r.handleMediaProcessingDelivery(ctx, queue, delivery, handler)
				},
			); err != nil {
				return err
			}
			continue
		}
		if err := r.consumeProtectedQueue(
			ctx, ConsumerMediaProcessing, queue,
			func(delivery amqp.Delivery) bool {
				return r.handleMediaProcessingDelivery(ctx, queue, delivery, handler)
			},
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) handleMediaProcessingDelivery(
	ctx context.Context,
	queue string,
	delivery amqp.Delivery,
	handler func(context.Context, *applicationmedia.ProcessingRequestedEvent) error,
) bool {
	start := time.Now()
	var event applicationmedia.ProcessingRequestedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_media_processing_decode", time.Since(start), err)
		r.rejectDelivery(ConsumerMediaProcessing, queue, delivery, false)
		return false
	}
	if event.EventID == "" || event.AssetID <= 0 || strings.TrimSpace(event.ProfileVersion) == "" {
		err := errors.New("invalid media processing event")
		inframetrics.ObserveWorkerJob("mq_media_processing_decode", time.Since(start), err)
		r.rejectDelivery(ConsumerMediaProcessing, queue, delivery, false)
		return false
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_media_processing_consume", time.Since(start), err)
		retry := shouldRetryConsumerError(err)
		r.rejectDelivery(ConsumerMediaProcessing, queue, delivery, retry)
		return shouldReconnectProtectedDelivery(queue, retry, r)
	}
	inframetrics.ObserveWorkerJob("mq_media_processing_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
	return false
}

func (r *RabbitMQ) consumeViewEventDeliveries(ctx context.Context, queue string) (<-chan amqp.Delivery, error) {
	r.viewEventConsumerMu.Lock()
	defer r.viewEventConsumerMu.Unlock()
	if err := r.ensureViewEventConsumerLocked(); err != nil {
		return nil, err
	}

	deliveries, err := r.viewEventConsumerChannel.ConsumeWithContext(
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
		r.resetViewEventConsumerLocked()
		return nil, err
	}
	return deliveries, nil
}

func (r *RabbitMQ) consumeActionDeliveries(ctx context.Context, queue string) (<-chan amqp.Delivery, error) {
	r.actionConsumerMu.Lock()
	defer r.actionConsumerMu.Unlock()
	if err := r.ensureActionConsumerLocked(); err != nil {
		return nil, err
	}
	deliveries, err := r.actionConsumerChannel.ConsumeWithContext(
		ctx, queue, "", false, false, false, false, nil,
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

func (r *RabbitMQ) handleActionDelivery(ctx context.Context, queue string, delivery amqp.Delivery, handler func(context.Context, *applicationinteraction.ActionChangedEvent) error) bool {
	start := time.Now()
	var event applicationinteraction.ActionChangedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_action_changed_decode", time.Since(start), err)
		r.rejectDelivery(ConsumerActionChanged, queue, delivery, false)
		return false
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), err)
		requeue := shouldRequeueActionDelivery(err)
		r.rejectDelivery(ConsumerActionChanged, queue, delivery, requeue)
		return requeue
	}
	inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
	return false
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

func (r *RabbitMQ) handleViewEventDelivery(ctx context.Context, queue string, delivery amqp.Delivery, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) bool {
	start := time.Now()
	var event applicationexposure.ViewEventRecordedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_view_event_decode", time.Since(start), err)
		r.rejectDelivery(ConsumerViewEventRecorded, queue, delivery, false)
		return false
	}
	if event.EventID == "" || event.UserID <= 0 || event.VideoID <= 0 || event.EventType == "" {
		err := errors.New("invalid view event")
		inframetrics.ObserveWorkerJob("mq_view_event_decode", time.Since(start), err)
		r.rejectDelivery(ConsumerViewEventRecorded, queue, delivery, false)
		return false
	}
	if err := handler(ctx, &event); err != nil {
		inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), err)
		retry := shouldRetryConsumerError(err)
		r.rejectDelivery(ConsumerViewEventRecorded, queue, delivery, retry)
		return retry
	}
	inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), nil)
	_ = delivery.Ack(false)
	return false
}

func superviseViewEventDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery) bool,
	retryDelay time.Duration,
) {
	superviseDeliveries(ctx, deliveries, reconsume, handle, retryDelay, "mq_view_event_reconsume")
}

func superviseDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery) bool,
	retryDelay time.Duration,
	job string,
) {
	delay := retryDelay
	for {
		for {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-deliveries:
				if !ok {
					goto reconnect
				}
				if handle(delivery) {
					goto reconnect
				}
			}
		}

	reconnect:
		for {
			if !waitConsumerRetry(ctx, delay) {
				return
			}
			next, err := reconsume(ctx)
			observeConsumerReconnect(job, err)
			if err != nil {
				delay = boundedConsumerRetryDelay(delay)
				continue
			}
			deliveries = next
			delay = retryDelay
			break
		}
	}
}

func superviseActionDeliveries(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	reconsume func(context.Context) (<-chan amqp.Delivery, error),
	handle func(amqp.Delivery) bool,
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
	if r.shouldBindLegacyQueue(ConsumerActionChanged) {
		if err := r.publishChannel.QueueBind(
			r.config.ActionChangedQueue,
			r.config.ActionChangedRouting,
			r.config.InteractionExchange,
			false,
			nil,
		); err != nil {
			return err
		}
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
	if r.shouldBindLegacyQueue(ConsumerVideoPublished) {
		if err := r.publishChannel.QueueBind(
			r.config.VideoPublishedQueue,
			r.config.VideoPublishedRouting,
			r.config.VideoExchange,
			false,
			nil,
		); err != nil {
			return err
		}
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
	if r.shouldBindLegacyQueue(ConsumerVideoEmbedding) {
		if err := r.publishChannel.QueueBind(
			r.config.VideoEmbeddingQueue,
			r.config.VideoPublishedRouting,
			r.config.VideoExchange,
			false,
			nil,
		); err != nil {
			return err
		}
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
	if r.shouldBindLegacyQueue(ConsumerViewEventRecorded) {
		if err := r.publishChannel.QueueBind(
			r.config.ViewEventRecordedQueue,
			r.config.ViewEventRecordedRouting,
			r.config.ExposureExchange,
			false,
			nil,
		); err != nil {
			return err
		}
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
	if r.shouldBindLegacyQueue(ConsumerMediaProcessing) {
		if err := r.publishChannel.QueueBind(
			r.config.MediaProcessingQueue, r.config.MediaProcessingRouting,
			r.config.MediaExchange, false, nil,
		); err != nil {
			return err
		}
	}
	return r.ensureDeadLetterTopology()
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
	normalizeDeadLetterConfig(&cfg)
	return cfg
}
