package inframq

import (
	"context"
	"errors"
	"sync"
	"time"

	applicationdeadletter "github.com/shiyudesu/frux/internal/application/deadletter"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type SupervisedRabbitMQ struct {
	config infraconfig.RabbitMQConfig
	mu     sync.Mutex
	client *RabbitMQ
	closed bool
}

func NewSupervisedRabbitMQ(
	cfg infraconfig.RabbitMQConfig,
) (*SupervisedRabbitMQ, error) {
	cfg = normalizeRabbitMQConfig(cfg)
	if cfg.URL == "" {
		return nil, ErrEmptyRabbitMQURL
	}
	inframetrics.ObserveRabbitMQTransport(false)
	return &SupervisedRabbitMQ{config: cfg}, nil
}

func (s *SupervisedRabbitMQ) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	client := s.client
	s.client = nil
	s.mu.Unlock()
	inframetrics.ObserveRabbitMQTransport(false)
	if client != nil {
		return client.Close()
	}
	return nil
}

func (s *SupervisedRabbitMQ) connection(ctx context.Context) (*RabbitMQ, error) {
	if s == nil {
		return nil, errors.New("rabbitmq supervisor is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	if s.client != nil && s.client.conn != nil && !s.client.conn.IsClosed() {
		return s.client, nil
	}
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	client, err := NewRabbitMQContext(ctx, s.config)
	if err != nil {
		inframetrics.ObserveRabbitMQReconnectFailure()
		return nil, err
	}
	s.client = client
	inframetrics.ObserveRabbitMQTransport(true)
	return client, nil
}

func (s *SupervisedRabbitMQ) invalidate(client *RabbitMQ) {
	s.mu.Lock()
	if s.client == client {
		s.client = nil
	}
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	inframetrics.ObserveRabbitMQTransport(false)
}

func (s *SupervisedRabbitMQ) superviseConsumer(
	ctx context.Context,
	job string,
	start func(context.Context, *RabbitMQ) error,
) error {
	if s == nil || start == nil {
		return errors.New("rabbitmq consumer supervisor is unavailable")
	}
	go func() {
		backoff := 100 * time.Millisecond
		for ctx.Err() == nil {
			attemptCtx, cancelAttempt := context.WithCancel(ctx)
			client, err := s.connection(attemptCtx)
			if err == nil {
				err = start(attemptCtx, client)
			}
			if err == nil {
				backoff = 100 * time.Millisecond
				ticker := time.NewTicker(time.Second)
				for ctx.Err() == nil && client.conn != nil && !client.conn.IsClosed() {
					<-ticker.C
				}
				ticker.Stop()
				if ctx.Err() != nil {
					cancelAttempt()
					return
				}
				err = errors.New("rabbitmq connection closed")
			}
			cancelAttempt()
			inframetrics.ObserveWorkerJob(job, 0, err)
			s.invalidate(client)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if backoff < maxConsumerRetryDelay {
				backoff *= 2
				if backoff > maxConsumerRetryDelay {
					backoff = maxConsumerRetryDelay
				}
			}
		}
	}()
	return nil
}

func (s *SupervisedRabbitMQ) publish(
	ctx context.Context,
	publish func(*RabbitMQ) error,
) error {
	client, err := s.connection(ctx)
	if err != nil {
		return err
	}
	if err := publish(client); err != nil {
		s.invalidate(client)
		return err
	}
	return nil
}

func (s *SupervisedRabbitMQ) PublishActionChanged(
	ctx context.Context,
	event *applicationinteraction.ActionChangedEvent,
) error {
	return s.publish(ctx, func(client *RabbitMQ) error {
		return client.PublishActionChanged(ctx, event)
	})
}

func (s *SupervisedRabbitMQ) PublishViewEventRecorded(
	ctx context.Context,
	event *applicationexposure.ViewEventRecordedEvent,
) error {
	return s.publish(ctx, func(client *RabbitMQ) error {
		return client.PublishViewEventRecorded(ctx, event)
	})
}

func (s *SupervisedRabbitMQ) PublishVideoPublished(
	ctx context.Context,
	event *applicationvideo.PublishedEvent,
) error {
	return s.publish(ctx, func(client *RabbitMQ) error {
		return client.PublishVideoPublished(ctx, event)
	})
}

func (s *SupervisedRabbitMQ) PublishMediaProcessingRequested(
	ctx context.Context,
	event *applicationmedia.ProcessingRequestedEvent,
) error {
	return s.publish(ctx, func(client *RabbitMQ) error {
		return client.PublishMediaProcessingRequested(ctx, event)
	})
}

func (s *SupervisedRabbitMQ) ConsumeActionChanged(
	ctx context.Context,
	handler func(context.Context, *applicationinteraction.ActionChangedEvent) error,
) error {
	return s.superviseConsumer(ctx, "rabbit_action_supervisor", func(sessionCtx context.Context, client *RabbitMQ) error {
		return client.ConsumeActionChanged(sessionCtx, handler)
	})
}

func (s *SupervisedRabbitMQ) ConsumeViewEventRecorded(
	ctx context.Context,
	handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error,
) error {
	return s.superviseConsumer(ctx, "rabbit_view_supervisor", func(sessionCtx context.Context, client *RabbitMQ) error {
		return client.ConsumeViewEventRecorded(sessionCtx, handler)
	})
}

func (s *SupervisedRabbitMQ) ConsumeVideoPublished(
	ctx context.Context,
	handler func(context.Context, *applicationvideo.PublishedEvent) error,
) error {
	return s.superviseConsumer(ctx, "rabbit_feed_supervisor", func(sessionCtx context.Context, client *RabbitMQ) error {
		return client.ConsumeVideoPublished(sessionCtx, handler)
	})
}

func (s *SupervisedRabbitMQ) ConsumeVideoPublishedForEmbedding(
	ctx context.Context,
	handler func(context.Context, *applicationvideo.PublishedEvent) error,
) error {
	return s.superviseConsumer(ctx, "rabbit_embedding_supervisor", func(sessionCtx context.Context, client *RabbitMQ) error {
		return client.ConsumeVideoPublishedForEmbedding(sessionCtx, handler)
	})
}

func (s *SupervisedRabbitMQ) ConsumeMediaProcessingRequested(
	ctx context.Context,
	handler func(context.Context, *applicationmedia.ProcessingRequestedEvent) error,
) error {
	return s.superviseConsumer(ctx, "rabbit_media_supervisor", func(sessionCtx context.Context, client *RabbitMQ) error {
		return client.ConsumeMediaProcessingRequested(sessionCtx, handler)
	})
}

func (s *SupervisedRabbitMQ) VerifyConsumerDrained(
	ctx context.Context,
	consumer string,
) error {
	client, err := s.connection(ctx)
	if err != nil {
		return err
	}
	err = NewDeadLetterManager(client, s.config).VerifyConsumerDrained(ctx, consumer)
	if errors.Is(err, domaindeadletter.ErrInspectionFailed) {
		s.invalidate(client)
	}
	return err
}

func (s *SupervisedRabbitMQ) ListDeadLetterQueues(
	ctx context.Context,
) ([]domaindeadletter.QueueSummary, error) {
	client, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	return NewDeadLetterManager(client, s.config).ListDeadLetterQueues(ctx)
}

func (s *SupervisedRabbitMQ) PreviewDeadLetterQueue(
	ctx context.Context,
	queue string,
	limit int,
) ([]domaindeadletter.MessagePreview, error) {
	client, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	return NewDeadLetterManager(client, s.config).
		PreviewDeadLetterQueue(ctx, queue, limit)
}

func (s *SupervisedRabbitMQ) ClaimDeadLetter(
	ctx context.Context,
	queue string,
	messageID string,
) (applicationdeadletter.ReplayClaim, error) {
	client, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	return NewDeadLetterManager(client, s.config).
		ClaimDeadLetter(ctx, queue, messageID)
}

func (s *SupervisedRabbitMQ) RunDepthObserver(
	ctx context.Context,
	interval time.Duration,
) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.ListDeadLetterQueues(ctx); err != nil && ctx.Err() == nil {
			inframetrics.ObserveMQRoutingFailure("management_api")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
