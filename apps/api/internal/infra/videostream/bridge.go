package infravideostream

import (
	"context"
	"errors"
	"fmt"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

type RabbitVideoPublisher interface {
	PublishVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
}

type RabbitMediaPublisher interface {
	PublishMediaProcessingRequested(context.Context, *applicationmedia.ProcessingRequestedEvent) error
}

type KafkaPublisher interface {
	Publish(
		context.Context,
		infrakafka.TopicID,
		[]byte,
		infrakafka.EventMetadata,
		any,
	) (infrakafka.ProduceMetadata, error)
}

type Observer interface {
	ObserveVideoWorkflowPublication(workflow, role, transport, result string)
}

type VideoPublisher struct {
	mode     infrakafka.ProducerMode
	rabbit   RabbitVideoPublisher
	kafka    KafkaPublisher
	observer Observer
	now      func() time.Time
}

func NewVideoPublisher(
	mode infrakafka.ProducerMode,
	rabbit RabbitVideoPublisher,
	kafka KafkaPublisher,
	observer Observer,
) (*VideoPublisher, error) {
	if err := validateTransports(mode, rabbit != nil, kafka != nil); err != nil {
		return nil, err
	}
	return &VideoPublisher{
		mode: mode, rabbit: rabbit, kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *VideoPublisher) PublishVideoPublished(
	ctx context.Context,
	event *applicationvideo.PublishedEvent,
) error {
	if event == nil {
		return nil
	}
	return p.publishByMode(ctx, "publication", func(ctx context.Context, transport string) error {
		switch transport {
		case "rabbit":
			return p.rabbit.PublishVideoPublished(ctx, event)
		case "kafka":
			key, err := infrakafka.EncodeKey(
				infrakafka.KeyKindVideoID,
				infrakafka.VideoKey{VideoID: event.VideoID},
			)
			if err != nil {
				return err
			}
			_, err = p.kafka.Publish(ctx, infrakafka.TopicVideoPublished, key, infrakafka.EventMetadata{
				EventID: event.EventID, Type: infrakafka.EventTypeVideoPublished,
				SchemaVersion: 1, OccurredAt: event.OccurredAt, ProducedAt: p.now(),
				Producer: infrakafka.ProducerVideoWorker,
			}, videoPayload(event))
			return err
		default:
			return errors.New("video publication transport unavailable")
		}
	})
}

func (p *VideoPublisher) publishByMode(
	ctx context.Context,
	workflow string,
	publish func(context.Context, string) error,
) error {
	primary, mirror := transports(p.mode)
	if mirror == "" {
		err := publish(ctx, primary)
		p.observe(workflow, "primary", primary, err)
		return err
	}
	primaryErr, mirrorErr := publishConcurrently(
		ctx,
		func(ctx context.Context) error { return publish(ctx, primary) },
		func(ctx context.Context) error { return publish(ctx, mirror) },
	)
	p.observe(workflow, "primary", primary, primaryErr)
	p.observe(workflow, "mirror", mirror, mirrorErr)
	if combined := errors.Join(primaryErr, mirrorErr); combined != nil {
		return &PublicationError{
			primaryTransport: primary, primaryErr: primaryErr,
			mirrorTransport: mirror, mirrorErr: mirrorErr,
		}
	}
	return nil
}

func (p *VideoPublisher) observe(workflow, role, transport string, err error) {
	if p != nil && p.observer != nil {
		p.observer.ObserveVideoWorkflowPublication(
			workflow, role, transport, publicationResult(err),
		)
	}
}

type MediaPublisher struct {
	mode     infrakafka.ProducerMode
	rabbit   RabbitMediaPublisher
	kafka    KafkaPublisher
	observer Observer
	now      func() time.Time
}

func NewMediaPublisher(
	mode infrakafka.ProducerMode,
	rabbit RabbitMediaPublisher,
	kafka KafkaPublisher,
	observer Observer,
) (*MediaPublisher, error) {
	if err := validateTransports(mode, rabbit != nil, kafka != nil); err != nil {
		return nil, err
	}
	return &MediaPublisher{
		mode: mode, rabbit: rabbit, kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *MediaPublisher) PublishMediaProcessingRequested(
	ctx context.Context,
	event *applicationmedia.ProcessingRequestedEvent,
) error {
	if event == nil {
		return nil
	}
	videoPublisher := &VideoPublisher{
		mode: p.mode, observer: p.observer, now: p.now,
	}
	return videoPublisher.publishByMode(ctx, "media_wakeup", func(ctx context.Context, transport string) error {
		switch transport {
		case "rabbit":
			return p.rabbit.PublishMediaProcessingRequested(ctx, event)
		case "kafka":
			key, err := infrakafka.EncodeKey(
				infrakafka.KeyKindAssetID,
				infrakafka.AssetKey{AssetID: event.AssetID},
			)
			if err != nil {
				return err
			}
			_, err = p.kafka.Publish(
				ctx,
				infrakafka.TopicMediaProcessingRequested,
				key,
				infrakafka.EventMetadata{
					EventID: event.EventID, Type: infrakafka.EventTypeMediaProcessingRequested,
					SchemaVersion: 1, OccurredAt: event.OccurredAt, ProducedAt: p.now(),
					Producer: infrakafka.ProducerMediaAPI,
				},
				infrakafka.MediaProcessingRequestedPayload{
					EventID: event.EventID, AssetID: event.AssetID,
					ProfileVersion: event.ProfileVersion, OccurredAt: event.OccurredAt,
				},
			)
			return err
		default:
			return errors.New("media wakeup transport unavailable")
		}
	})
}

type PublicationError struct {
	primaryTransport string
	primaryErr       error
	mirrorTransport  string
	mirrorErr        error
}

func (e *PublicationError) Error() string { return errors.Join(e.primaryErr, e.mirrorErr).Error() }
func (e *PublicationError) Unwrap() []error {
	return []error{e.primaryErr, e.mirrorErr}
}
func (e *PublicationError) TransportAcknowledged(transport string) bool {
	switch transport {
	case e.primaryTransport:
		return e.primaryErr == nil
	case e.mirrorTransport:
		return e.mirrorErr == nil
	default:
		return false
	}
}
func (e *PublicationError) PrimaryTransportAcknowledged() bool { return e.primaryErr == nil }
func (e *PublicationError) AnyTransportAcknowledged() bool {
	return e.primaryErr == nil || e.mirrorErr == nil
}
func (e *PublicationError) PrimaryTransportMayBeAcknowledged() bool {
	return e.primaryErr == nil || applicationeventstream.MayHaveTransportAcknowledgement(e.primaryErr)
}
func (e *PublicationError) AnyTransportMayBeAcknowledged() bool {
	return e.primaryErr == nil || e.mirrorErr == nil ||
		applicationeventstream.MayHaveTransportAcknowledgement(e.primaryErr) ||
		applicationeventstream.MayHaveTransportAcknowledgement(e.mirrorErr)
}

type FanoutHandler struct {
	worker interface {
		HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
	}
}

func NewFanoutHandler(worker interface {
	HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
}) *FanoutHandler {
	return &FanoutHandler{worker: worker}
}

func (h *FanoutHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	if err := h.worker.HandleVideoPublished(ctx, publishedEvent(payload)); err != nil {
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

type EmbeddingHandler struct {
	intake interface {
		HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
	}
}

func NewEmbeddingHandler(intake interface {
	HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
}) *EmbeddingHandler {
	return &EmbeddingHandler{intake: intake}
}

func (h *EmbeddingHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || h == nil || h.intake == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	if err := h.intake.HandleVideoPublished(ctx, publishedEvent(payload)); err != nil {
		if errors.Is(err, domainembedding.ErrInvalidSemanticText) {
			return applicationeventstream.OutcomeTerminal, err
		}
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

type MediaWakeupHandler struct {
	worker interface {
		SignalRequested(context.Context, *applicationmedia.ProcessingRequestedEvent) error
	}
}

type FanoutParityChecker struct {
	Reader interface {
		CountFollowers(context.Context, int64) (int, error)
		ListFollowerIDs(context.Context, int64, int64, int) ([]int64, error)
	}
	Index interface {
		HasFollowingFanout(
			context.Context, int64, []int64, *domainfeed.FeedPageItem, bool,
		) (bool, error)
	}
}

func (c FanoutParityChecker) Compare(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || c.Reader == nil || c.Index == nil {
		return applicationeventstream.ParityMismatch, nil
	}
	count, err := c.Reader.CountFollowers(ctx, payload.AuthorID)
	if err != nil {
		return applicationeventstream.ParityPending, err
	}
	item := &domainfeed.FeedPageItem{
		VideoID: payload.VideoID, AuthorID: payload.AuthorID, PublishedAt: payload.PublishedAt,
	}
	if count >= domainfeed.BigCreatorFollowerThreshold {
		present, err := c.Index.HasFollowingFanout(
			ctx, payload.AuthorID, nil, item, true,
		)
		if err != nil {
			return applicationeventstream.ParityPending, err
		}
		if !present {
			return applicationeventstream.ParityPending, nil
		}
		return applicationeventstream.ParityMatch, nil
	}
	followers := make([]int64, 0, count)
	cursor := int64(0)
	for len(followers) < count {
		batch, err := c.Reader.ListFollowerIDs(ctx, payload.AuthorID, cursor, 500)
		if err != nil {
			return applicationeventstream.ParityPending, err
		}
		if len(batch) == 0 {
			break
		}
		followers = append(followers, batch...)
		cursor = batch[len(batch)-1]
	}
	if len(followers) != count {
		return applicationeventstream.ParityPending, nil
	}
	present, err := c.Index.HasFollowingFanout(
		ctx, payload.AuthorID, followers, item, false,
	)
	if err != nil {
		return applicationeventstream.ParityPending, err
	}
	if !present {
		return applicationeventstream.ParityPending, nil
	}
	return applicationeventstream.ParityMatch, nil
}

type EmbeddingParityChecker struct {
	Reader interface {
		PublicationIntakeParity(
			context.Context, int64, string, string,
		) (bool, bool, error)
	}
}

func (c EmbeddingParityChecker) Compare(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || c.Reader == nil {
		return applicationeventstream.ParityMismatch, nil
	}
	present, matches, err := c.Reader.PublicationIntakeParity(
		ctx, payload.VideoID, payload.Title, payload.Description,
	)
	if err != nil {
		return applicationeventstream.ParityPending, err
	}
	if !present {
		return applicationeventstream.ParityPending, nil
	}
	if !matches {
		return applicationeventstream.ParityMismatch, nil
	}
	return applicationeventstream.ParityMatch, nil
}

type MediaWakeupParityChecker struct {
	Reader interface {
		FindProcessingJobByAsset(
			context.Context, int64,
		) (*domainmedia.MediaProcessingJob, error)
	}
}

func (c MediaWakeupParityChecker) Compare(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	payload, ok := event.Payload.(*infrakafka.MediaProcessingRequestedPayload)
	if !ok || c.Reader == nil {
		return applicationeventstream.ParityMismatch, nil
	}
	job, err := c.Reader.FindProcessingJobByAsset(ctx, payload.AssetID)
	if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
		return applicationeventstream.ParityPending, nil
	}
	if err != nil {
		return applicationeventstream.ParityPending, err
	}
	if job == nil {
		return applicationeventstream.ParityPending, nil
	}
	if job.ProfileVersion != payload.ProfileVersion {
		return applicationeventstream.ParityMismatch, nil
	}
	return applicationeventstream.ParityMatch, nil
}

func NewMediaWakeupHandler(worker interface {
	SignalRequested(context.Context, *applicationmedia.ProcessingRequestedEvent) error
}) *MediaWakeupHandler {
	return &MediaWakeupHandler{worker: worker}
}

func (h *MediaWakeupHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.MediaProcessingRequestedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	err := h.worker.SignalRequested(ctx, &applicationmedia.ProcessingRequestedEvent{
		EventID: payload.EventID, AssetID: payload.AssetID,
		ProfileVersion: payload.ProfileVersion, OccurredAt: payload.OccurredAt,
	})
	if err != nil {
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

func videoPayload(event *applicationvideo.PublishedEvent) infrakafka.VideoPublishedPayload {
	return infrakafka.VideoPublishedPayload{
		EventID: event.EventID, VideoID: event.VideoID, AuthorID: event.AuthorID,
		Title: event.Title, Description: event.Description,
		MediaURL: event.MediaURL, CoverURL: event.CoverURL,
		PublishedAt: event.PublishedAt, OccurredAt: event.OccurredAt,
	}
}

func publishedEvent(payload *infrakafka.VideoPublishedPayload) *applicationvideo.PublishedEvent {
	return &applicationvideo.PublishedEvent{
		EventID: payload.EventID, VideoID: payload.VideoID, AuthorID: payload.AuthorID,
		Title: payload.Title, Description: payload.Description,
		MediaURL: payload.MediaURL, CoverURL: payload.CoverURL,
		PublishedAt: payload.PublishedAt, OccurredAt: payload.OccurredAt,
	}
}

func validateTransports(mode infrakafka.ProducerMode, rabbit, kafka bool) error {
	primary, mirror := transports(mode)
	if primary == "" || primary == "rabbit" && !rabbit || primary == "kafka" && !kafka ||
		mirror == "rabbit" && !rabbit || mirror == "kafka" && !kafka {
		return fmt.Errorf("%w: video workflow publisher transport", infrakafka.ErrUnknownRegistryValue)
	}
	return nil
}

func transports(mode infrakafka.ProducerMode) (string, string) {
	switch mode {
	case infrakafka.ProducerModeRabbit:
		return "rabbit", ""
	case infrakafka.ProducerModeRabbitWithKafkaMirror:
		return "rabbit", "kafka"
	case infrakafka.ProducerModeKafkaWithRabbitMirror:
		return "kafka", "rabbit"
	case infrakafka.ProducerModeKafka:
		return "kafka", ""
	default:
		return "", ""
	}
}

func publishConcurrently(
	ctx context.Context,
	primary func(context.Context) error,
	mirror func(context.Context) error,
) (error, error) {
	results := make(chan struct {
		primary bool
		err     error
	}, 2)
	go func() {
		results <- struct {
			primary bool
			err     error
		}{primary: true, err: primary(ctx)}
	}()
	go func() {
		results <- struct {
			primary bool
			err     error
		}{err: mirror(ctx)}
	}()
	var primaryErr, mirrorErr error
	for range 2 {
		result := <-results
		if result.primary {
			primaryErr = result.err
		} else {
			mirrorErr = result.err
		}
	}
	return primaryErr, mirrorErr
}

func publicationResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case applicationeventstream.MayHaveTransportAcknowledgement(err):
		return "uncertain"
	default:
		return "failure"
	}
}
