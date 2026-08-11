package infrakafka

import (
	"context"
	"errors"
	"sort"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

const (
	replayEvidenceClockSkew           = 5 * time.Minute
	maxReplayEvidenceRecords          = 10_000
	defaultReplaySettlementInterval   = 250 * time.Millisecond
	defaultReplaySettlementWindow     = 10 * time.Second
	defaultReplayUncertaintyWindow    = 10 * time.Second
	requiredStableReplayEvidenceScans = 2
)

type RecoveryAdapter struct {
	backbone           *Backbone
	prefix             string
	uncertaintyWindow  time.Duration
	settlementWindow   time.Duration
	settlementInterval time.Duration
	sleep              func(context.Context, time.Duration) error
}

type replayPublishUncertainError struct{}

func (*replayPublishUncertainError) Error() string {
	return domainkafkafailure.ErrReplayPublishUncertain.Error()
}

func (*replayPublishUncertainError) Unwrap() error {
	return domainkafkafailure.ErrReplayPublishUncertain
}

func (*replayPublishUncertainError) MayHaveAcknowledged() bool {
	return true
}

func NewRecoveryAdapter(backbone *Backbone, cfg infraconfig.KafkaConfig) *RecoveryAdapter {
	uncertaintyWindow, err := time.ParseDuration(cfg.Timeouts.Produce)
	if err != nil || uncertaintyWindow <= 0 {
		uncertaintyWindow = defaultReplayUncertaintyWindow
	}
	settlementWindow, err := time.ParseDuration(cfg.Timeouts.Admin)
	if err != nil || settlementWindow <= 0 {
		settlementWindow = defaultReplaySettlementWindow
	}
	return &RecoveryAdapter{
		backbone: backbone, prefix: cfg.TopicPrefix,
		uncertaintyWindow:  uncertaintyWindow,
		settlementWindow:   settlementWindow,
		settlementInterval: defaultReplaySettlementInterval,
		sleep:              waitReplayEvidence,
	}
}

func (a *RecoveryAdapter) RouteForDLQ(
	topic string,
) (domainkafkafailure.RecoveryRoute, error) {
	recovery, err := DLQTopicAllowed(a.prefix, topic)
	if err != nil || recovery.Policy != RecoveryRetryTopics {
		return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
	}
	source, err := TopicName(a.prefix, recovery.SourceTopic)
	if err != nil {
		return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
	}
	replayTopic := source
	replayTier := 0
	if recovery.ReplayDestination == ReplayToFirstRetry {
		first, ok := recovery.RetryTier(1)
		if !ok {
			return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
		}
		replayTopic, err = TopicName(a.prefix, first.Topic)
		if err != nil {
			return domainkafkafailure.RecoveryRoute{}, domainkafkafailure.ErrTopicNotAllowed
		}
		replayTier = first.Tier
	}
	return domainkafkafailure.RecoveryRoute{
		DLQTopic:      topic,
		ConsumerGroup: string(recovery.Group),
		SourceTopic:   source,
		ReplayTopic:   replayTopic,
		ReplayTier:    replayTier,
		MaxAttempt:    len(recovery.RetryTiers) + 1,
		Retention:     recovery.DLQRetention,
	}, nil
}

func (a *RecoveryAdapter) List(
	ctx context.Context,
) ([]domainkafkafailure.TopicSummary, error) {
	inspector := a.inspector()
	if inspector == nil {
		return nil, domainkafkafailure.ErrInspectionUnavailable
	}
	summaries, err := inspector.ListDLQTopics(ctx)
	if err != nil {
		return nil, mapInspectionError(err)
	}
	result := make([]domainkafkafailure.TopicSummary, 0, len(summaries))
	for _, summary := range summaries {
		item := domainkafkafailure.TopicSummary{
			Topic:            summary.Topic,
			ConsumerGroup:    string(summary.ConsumerGroup),
			Retention:        summary.Retention,
			PartitionCount:   summary.PartitionCount,
			RetainedEstimate: summary.RetainedEstimate,
			EndOffset:        summary.EndOffset,
			EndOffsetGrowth:  summary.EndOffsetGrowth,
			RecentIngress:    summary.RecentIngress,
			OldestRecordAt:   summary.OldestRecordAt,
			OldestAge:        summary.OldestAge,
			Partitions:       make([]domainkafkafailure.PartitionSummary, 0, len(summary.Partitions)),
		}
		for _, partition := range summary.Partitions {
			item.Partitions = append(item.Partitions, domainkafkafailure.PartitionSummary{
				Partition:           partition.Partition,
				RetainedStartOffset: partition.RetainedStartOffset,
				EndOffset:           partition.EndOffset,
				RetainedEstimate:    partition.RetainedEstimate,
				EndOffsetGrowth:     partition.EndOffsetGrowth,
				RecentIngress:       partition.RecentIngress,
				OldestRecordAt:      partition.OldestRecordAt,
				OldestAge:           partition.OldestAge,
			})
		}
		result = append(result, item)
	}
	return result, nil
}

func (a *RecoveryAdapter) Inspect(
	ctx context.Context,
	topic string,
	partition int32,
	offset int64,
	limit int,
) ([]domainkafkafailure.RecordDiagnostic, error) {
	inspector := a.inspector()
	if inspector == nil {
		return nil, domainkafkafailure.ErrInspectionUnavailable
	}
	records, err := inspector.ReadDLQRecords(ctx, topic, partition, offset, limit)
	if err != nil {
		return nil, mapInspectionError(err)
	}
	result := make([]domainkafkafailure.RecordDiagnostic, 0, len(records))
	for _, record := range records {
		result = append(result, domainkafkafailure.RecordDiagnostic{
			Coordinate: domainkafkafailure.Coordinate{
				Topic: record.Topic, Partition: record.Partition, Offset: record.Offset,
			},
			Timestamp:         record.Timestamp,
			SourceTopic:       record.SourceTopic,
			SourcePartition:   record.SourcePartition,
			SourceOffset:      record.SourceOffset,
			ConsumerGroup:     string(record.ConsumerGroup),
			EventID:           record.EventID,
			ReplayID:          record.ReplayID,
			SchemaVersion:     record.SchemaVersion,
			FailureClass:      string(record.FailureClass),
			Attempt:           record.Attempt,
			FirstFailureAt:    record.FirstFailureAt,
			LatestFailureAt:   record.LatestFailureAt,
			NotBefore:         record.NotBefore,
			ConsumedTopic:     record.ConsumedTopic,
			ConsumedPartition: record.ConsumedPartition,
			ConsumedOffset:    record.ConsumedOffset,
			MetadataCode:      string(record.MetadataCode),
			Replayable:        record.Replayable,
			KeyBytes:          record.KeyBytes,
			KeySHA256:         record.KeySHA256,
			PayloadBytes:      record.PayloadBytes,
			PayloadSHA256:     record.PayloadSHA256,
			ContentType:       record.ContentType,
			JSONValid:         record.JSONValid,
			JSONFields:        append([]string(nil), record.JSONFields...),
		})
	}
	return result, nil
}

func (a *RecoveryAdapter) Fetch(
	ctx context.Context,
	coordinate domainkafkafailure.Coordinate,
) (domainkafkafailure.RetainedRecord, error) {
	inspector := a.inspector()
	if inspector == nil {
		return domainkafkafailure.RetainedRecord{}, domainkafkafailure.ErrInspectionUnavailable
	}
	record, metadata, err := inspector.FetchDLQRecord(
		ctx, coordinate.Topic, coordinate.Partition, coordinate.Offset,
	)
	if err != nil {
		return domainkafkafailure.RetainedRecord{}, mapInspectionError(err)
	}
	return domainkafkafailure.RetainedRecord{
		Coordinate: coordinate,
		Timestamp:  record.Timestamp,
		Key:        append([]byte(nil), record.Key...),
		Value:      append([]byte(nil), record.Value...),
		Metadata: domainkafkafailure.RecoveryMetadata{
			SourceTopic:       metadata.SourceTopic,
			SourcePartition:   metadata.SourcePartition,
			SourceOffset:      metadata.SourceOffset,
			EventID:           metadata.EventID,
			SchemaVersion:     metadata.SchemaVersion,
			ConsumerGroup:     string(metadata.ConsumerGroup),
			Attempt:           metadata.Attempt,
			Tier:              metadata.Tier,
			FailureClass:      string(metadata.FailureClass),
			FirstFailureAt:    metadata.FirstFailureAt,
			LatestFailureAt:   metadata.LatestFailureAt,
			NotBefore:         metadata.NotBefore,
			PayloadSHA256:     metadata.PayloadSHA256,
			ReplayID:          metadata.ReplayID,
			ConsumedTopic:     metadata.ConsumedTopic,
			ConsumedPartition: metadata.ConsumedPartition,
			ConsumedOffset:    metadata.ConsumedOffset,
			KeySHA256:         metadata.KeySHA256,
			MetadataCode:      string(metadata.MetadataCode),
			NonReplayable:     metadata.NonReplayable,
		},
	}, nil
}

func (a *RecoveryAdapter) Validate(
	sourceTopic string,
	key, value []byte,
) (string, int, error) {
	for _, recovery := range Recoveries() {
		registered, err := TopicName(a.prefix, recovery.SourceTopic)
		if err != nil || registered != sourceTopic {
			continue
		}
		decoded, err := DecodeEvent(recovery.SourceTopic, key, value, time.Now().UTC())
		if err != nil {
			return "", 0, domainkafkafailure.ErrInvalidProvenance
		}
		return decoded.Envelope.EventID, decoded.Envelope.SchemaVersion, nil
	}
	return "", 0, domainkafkafailure.ErrInvalidProvenance
}

func (a *RecoveryAdapter) PublishReplay(
	ctx context.Context,
	route domainkafkafailure.RecoveryRoute,
	record domainkafkafailure.RetainedRecord,
	replayID string,
) error {
	expected, err := a.RouteForDLQ(route.DLQTopic)
	if err != nil || expected != route {
		return domainkafkafailure.ErrInvalidProvenance
	}

	recovery, err := DLQTopicAllowed(a.prefix, route.DLQTopic)
	if err != nil {
		return domainkafkafailure.ErrInvalidProvenance
	}
	destination := recovery.SourceTopic
	replayStartedAt := time.Now().UTC()
	metadata := RecoveryMetadata{
		SourceTopic:     record.Metadata.SourceTopic,
		SourcePartition: record.Metadata.SourcePartition,
		SourceOffset:    record.Metadata.SourceOffset,
		EventID:         record.Metadata.EventID,
		SchemaVersion:   record.Metadata.SchemaVersion,
		ConsumerGroup:   ConsumerGroupID(record.Metadata.ConsumerGroup),
		Attempt:         1,
		Tier:            route.ReplayTier,
		FailureClass:    FailureClass(record.Metadata.FailureClass),
		FirstFailureAt:  replayStartedAt,
		LatestFailureAt: replayStartedAt,
		NotBefore:       replayStartedAt,
		PayloadSHA256:   record.Metadata.PayloadSHA256,
		ReplayID:        replayID,
	}
	if route.ReplayTier > 0 {
		tier, ok := recovery.RetryTier(route.ReplayTier)
		if !ok {
			return domainkafkafailure.ErrInvalidProvenance
		}
		destination = tier.Topic
		metadata.NotBefore = replayStartedAt.Add(tier.Delay)
	}
	headers, err := EncodeRecoveryHeaders(
		a.prefix, destination, metadata, record.Key, record.Value,
	)
	if err != nil {
		return domainkafkafailure.ErrInvalidProvenance
	}
	publisher := a.publisher()
	if publisher == nil {
		return domainkafkafailure.ErrReplayPublishUnavailable
	}
	err = publisher.PublishRecovery(
		ctx, destination, record.Key, record.Value, headers,
	)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrProduceUncertain),
		applicationeventstream.MayHaveTransportAcknowledgement(err):
		return &replayPublishUncertainError{}
	case errors.Is(err, context.DeadlineExceeded):
		return domainkafkafailure.ErrReplayPublishTimeout
	case errors.Is(err, ErrProduceFailed):
		return domainkafkafailure.ErrReplayPublishRejected
	default:
		return domainkafkafailure.ErrReplayPublishUnavailable
	}
}

func (a *RecoveryAdapter) VerifyReplay(
	ctx context.Context,
	route domainkafkafailure.RecoveryRoute,
	replayID string,
	requestedAt time.Time,
) (domainkafkafailure.ReplayEvidence, error) {
	expected, err := a.RouteForDLQ(route.DLQTopic)
	if err != nil || expected != route || requestedAt.IsZero() {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	recovery, err := DLQTopicAllowed(a.prefix, route.DLQTopic)
	if err != nil {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	destinationID := recovery.SourceTopic
	if route.ReplayTier > 0 {
		tier, ok := recovery.RetryTier(route.ReplayTier)
		if !ok {
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		destinationID = tier.Topic
	}
	destinationTopic, err := TopicName(a.prefix, destinationID)
	if err != nil || destinationTopic != route.ReplayTopic {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	destinationSpec, err := Topic(destinationID)
	if err != nil || destinationSpec.Retention <= 0 {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	inspector := a.inspector()
	if inspector == nil || inspector.backend == nil {
		return domainkafkafailure.ReplayEvidence{},
			domainkafkafailure.ErrReplayEvidenceUnavailable
	}
	uncertaintyWindow := a.uncertaintyWindow
	if uncertaintyWindow <= 0 {
		uncertaintyWindow = defaultReplayUncertaintyWindow
	}
	settlementWindow := a.settlementWindow
	if settlementWindow <= 0 {
		settlementWindow = defaultReplaySettlementWindow
	}
	settlementInterval := a.settlementInterval
	if settlementInterval <= 0 {
		settlementInterval = defaultReplaySettlementInterval
	}
	sleep := a.sleep
	if sleep == nil {
		sleep = waitReplayEvidence
	}
	uncertaintyEnds := requestedAt.UTC().Add(uncertaintyWindow)
	settlementStarts := inspector.currentTime()
	if uncertaintyEnds.After(settlementStarts) {
		settlementStarts = uncertaintyEnds
	}
	settlementEnds := settlementStarts.Add(settlementWindow)
	var previous map[int32]dlqPartitionOffsets
	previousEligible := false
	stableScans := 0

	for {
		if err := ctx.Err(); err != nil {
			return domainkafkafailure.ReplayEvidence{}, err
		}
		scanContext, cancel := inspector.operationContext(ctx)
		offsets, scanErr := inspector.backend.PartitionOffsets(
			scanContext,
			[]string{destinationTopic},
			requestedAt.UTC().Add(-replayEvidenceClockSkew),
		)
		if scanErr != nil {
			cancel()
			if ctx.Err() != nil {
				return domainkafkafailure.ReplayEvidence{}, ctx.Err()
			}
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		topicOffsets, found := offsets[destinationTopic]
		if !found || len(topicOffsets) == 0 {
			cancel()
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		evidence, malformed, scanErr := a.scanReplayEvidence(
			scanContext,
			inspector.backend,
			recovery,
			route,
			destinationID,
			destinationTopic,
			topicOffsets,
			replayID,
			requestedAt,
		)
		cancel()
		if scanErr != nil {
			if ctx.Err() != nil {
				return domainkafkafailure.ReplayEvidence{}, ctx.Err()
			}
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		if evidence.Status == domainkafkafailure.ReplayEvidenceFound {
			return evidence, nil
		}
		if malformed {
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}

		now := inspector.currentTime()
		if !requestedAt.UTC().Add(destinationSpec.Retention).
			Add(replayEvidenceClockSkew).After(now) {
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceExpired
		}
		eligible := !now.Before(uncertaintyEnds)
		if eligible && previousEligible &&
			equalReplayEvidenceBounds(previous, topicOffsets) {
			stableScans++
		} else if eligible {
			stableScans = 1
		} else {
			stableScans = 0
		}
		if stableScans >= requiredStableReplayEvidenceScans {
			return domainkafkafailure.ReplayEvidence{
				Status:           domainkafkafailure.ReplayEvidenceAbsent,
				DestinationTopic: destinationTopic,
				ReplayID:         replayID,
			}, nil
		}
		previous = cloneReplayEvidenceBounds(topicOffsets)
		previousEligible = eligible
		if !now.Before(settlementEnds) {
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		wait := settlementInterval
		if remaining := settlementEnds.Sub(now); wait > remaining {
			wait = remaining
		}
		if err := sleep(ctx, wait); err != nil {
			if ctx.Err() != nil {
				return domainkafkafailure.ReplayEvidence{}, ctx.Err()
			}
			return domainkafkafailure.ReplayEvidence{},
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
	}
}

func (a *RecoveryAdapter) scanReplayEvidence(
	ctx context.Context,
	backend dlqInspectionBackend,
	recovery RecoverySpec,
	route domainkafkafailure.RecoveryRoute,
	destinationID TopicID,
	destinationTopic string,
	topicOffsets map[int32]dlqPartitionOffsets,
	replayID string,
	requestedAt time.Time,
) (domainkafkafailure.ReplayEvidence, bool, error) {
	partitions := make([]int, 0, len(topicOffsets))
	for partition := range topicOffsets {
		partitions = append(partitions, int(partition))
	}
	sort.Ints(partitions)
	scanned := 0
	malformedEvidence := false
	for _, value := range partitions {
		partition := int32(value)
		bounds := topicOffsets[partition]
		if partition < 0 || bounds.Start < 0 || bounds.End < bounds.Start ||
			bounds.RecentStart < bounds.Start || bounds.RecentStart > bounds.End {
			return domainkafkafailure.ReplayEvidence{}, false,
				domainkafkafailure.ErrReplayEvidenceUnavailable
		}
		for next := bounds.RecentStart; next < bounds.End; {
			remaining := bounds.End - next
			limit := MaxDLQReadLimit
			if remaining < int64(limit) {
				limit = int(remaining)
			}
			if scanned+limit > maxReplayEvidenceRecords {
				return domainkafkafailure.ReplayEvidence{}, false,
					domainkafkafailure.ErrReplayEvidenceUnavailable
			}
			records, err := backend.ReadRecords(ctx, []dlqReadRange{{
				Topic: destinationTopic, Partition: partition,
				Start: next, End: bounds.End, Limit: limit,
			}})
			if err != nil {
				return domainkafkafailure.ReplayEvidence{}, false, err
			}
			if len(records) == 0 {
				next = bounds.End
				break
			}
			if len(records) > limit || scanned+len(records) > maxReplayEvidenceRecords {
				return domainkafkafailure.ReplayEvidence{}, false,
					domainkafkafailure.ErrReplayEvidenceUnavailable
			}
			scanned += len(records)
			lastOffset := next
			for _, record := range records {
				if record.Topic != destinationTopic ||
					record.Partition != partition ||
					record.Offset < next || record.Offset >= bounds.End {
					return domainkafkafailure.ReplayEvidence{}, false,
						domainkafkafailure.ErrReplayEvidenceUnavailable
				}
				if record.Offset >= lastOffset {
					lastOffset = record.Offset + 1
				}
				candidateReplayID, present, headerErr := replayEvidenceID(record.Headers)
				if headerErr != nil {
					malformedEvidence = true
					continue
				}
				if !present {
					continue
				}
				metadata, decodeErr := DecodeRecoveryHeaders(
					a.prefix, destinationID,
					record.Headers, record.Key, record.Value,
				)
				if decodeErr != nil {
					malformedEvidence = true
					continue
				}
				if candidateReplayID != replayID {
					continue
				}
				if record.Timestamp.UTC().Before(
					requestedAt.UTC().Add(-replayEvidenceClockSkew),
				) {
					malformedEvidence = true
					continue
				}
				if metadata.ConsumerGroup != recovery.Group ||
					metadata.Tier != route.ReplayTier ||
					metadata.SourceTopic != route.SourceTopic {
					malformedEvidence = true
					continue
				}
				return domainkafkafailure.ReplayEvidence{
					Status:           domainkafkafailure.ReplayEvidenceFound,
					DestinationTopic: destinationTopic,
					ReplayID:         metadata.ReplayID,
					SourceTopic:      metadata.SourceTopic,
					SourcePartition:  metadata.SourcePartition,
					SourceOffset:     metadata.SourceOffset,
					ConsumerGroup:    string(metadata.ConsumerGroup),
					EventID:          metadata.EventID,
					SchemaVersion:    metadata.SchemaVersion,
					PayloadSHA256:    metadata.PayloadSHA256,
					KeySHA256:        domainkafkafailure.PayloadSHA256(record.Key),
					RecordedAt:       record.Timestamp.UTC(),
				}, malformedEvidence, nil
			}
			if lastOffset <= next {
				return domainkafkafailure.ReplayEvidence{}, false,
					domainkafkafailure.ErrReplayEvidenceUnavailable
			}
			next = lastOffset
		}
	}
	return domainkafkafailure.ReplayEvidence{}, malformedEvidence, nil
}

func cloneReplayEvidenceBounds(
	source map[int32]dlqPartitionOffsets,
) map[int32]dlqPartitionOffsets {
	result := make(map[int32]dlqPartitionOffsets, len(source))
	for partition, bounds := range source {
		result[partition] = bounds
	}
	return result
}

func equalReplayEvidenceBounds(
	left map[int32]dlqPartitionOffsets,
	right map[int32]dlqPartitionOffsets,
) bool {
	if len(left) != len(right) {
		return false
	}
	for partition, bounds := range left {
		other, found := right[partition]
		if !found || other != bounds {
			return false
		}
	}
	return true
}

func waitReplayEvidence(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func replayEvidenceID(
	headers []applicationeventstream.Header,
) (string, bool, error) {
	if len(headers) > MaxRecoveryHeaders {
		return "", false, ErrRecoveryMetadata
	}
	totalBytes := 0
	var encoded []byte
	for _, header := range headers {
		totalBytes += len(header.Key) + len(header.Value)
		if totalBytes > MaxRecoveryTotalHeaderBytes {
			return "", false, ErrRecoveryMetadata
		}
		if header.Key != RecoveryHeaderKey {
			continue
		}
		if encoded != nil || len(header.Value) == 0 ||
			len(header.Value) > MaxRecoveryHeaderBytes {
			return "", false, ErrRecoveryMetadata
		}
		encoded = header.Value
	}
	if encoded == nil {
		return "", false, nil
	}
	var metadata RecoveryMetadata
	if err := decodeStrict(encoded, &metadata); err != nil {
		return "", false, ErrRecoveryMetadata
	}
	return metadata.ReplayID, true, nil
}

func (a *RecoveryAdapter) inspector() *DLQInspector {
	if a == nil || a.backbone == nil {
		return nil
	}
	return a.backbone.DLQInspector()
}

func (a *RecoveryAdapter) publisher() *Publisher {
	if a == nil || a.backbone == nil {
		return nil
	}
	return a.backbone.Publisher()
}

func mapInspectionError(err error) error {
	switch {
	case errors.Is(err, ErrDLQTopicNotAllowed):
		return domainkafkafailure.ErrTopicNotAllowed
	case errors.Is(err, ErrDLQInvalidPartition):
		return domainkafkafailure.ErrInvalidPartition
	case errors.Is(err, ErrDLQInvalidOffset):
		return domainkafkafailure.ErrInvalidOffset
	case errors.Is(err, ErrDLQInvalidLimit):
		return domainkafkafailure.ErrInvalidLimit
	case errors.Is(err, ErrDLQOffsetExpired):
		return domainkafkafailure.ErrRecordExpired
	case errors.Is(err, ErrDLQOffsetUnavailable):
		return domainkafkafailure.ErrRecordNotFound
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ErrDLQRecordInvalid):
		return domainkafkafailure.ErrInvalidProvenance
	default:
		return domainkafkafailure.ErrInspectionUnavailable
	}
}
