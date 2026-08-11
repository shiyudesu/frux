package infrakafka

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	MaxDLQReadLimit        = 100
	MaxDLQJSONFields       = 20
	MaxDLQJSONFieldLength  = 64
	defaultDLQRecentWindow = 15 * time.Minute
)

var (
	ErrDLQInspectionFailed  = errors.New("kafka DLQ inspection failed")
	ErrDLQTopicNotAllowed   = errors.New("kafka DLQ topic is not allowed")
	ErrDLQInvalidPartition  = errors.New("invalid kafka DLQ partition")
	ErrDLQInvalidOffset     = errors.New("invalid kafka DLQ offset")
	ErrDLQOffsetExpired     = errors.New("kafka DLQ offset is no longer retained")
	ErrDLQOffsetUnavailable = errors.New("kafka DLQ offset is unavailable")
	ErrDLQInvalidLimit      = errors.New("invalid kafka DLQ read limit")
	ErrDLQRecordInvalid     = errors.New("invalid kafka DLQ recovery record")
)

type DLQTopicSummary struct {
	Topic            string                `json:"topic"`
	ConsumerGroup    ConsumerGroupID       `json:"consumer_group"`
	Retention        time.Duration         `json:"retention"`
	PartitionCount   int                   `json:"partition_count"`
	RetainedEstimate int64                 `json:"retained_estimate"`
	EndOffset        int64                 `json:"end_offset"`
	EndOffsetGrowth  int64                 `json:"end_offset_growth"`
	RecentIngress    int64                 `json:"recent_ingress"`
	OldestRecordAt   time.Time             `json:"oldest_record_at,omitempty"`
	OldestAge        time.Duration         `json:"oldest_age"`
	Partitions       []DLQPartitionSummary `json:"partitions"`
}

type DLQPartitionSummary struct {
	Partition           int32         `json:"partition"`
	RetainedStartOffset int64         `json:"retained_start_offset"`
	EndOffset           int64         `json:"end_offset"`
	RetainedEstimate    int64         `json:"retained_estimate"`
	EndOffsetGrowth     int64         `json:"end_offset_growth"`
	RecentIngress       int64         `json:"recent_ingress"`
	OldestRecordAt      time.Time     `json:"oldest_record_at,omitempty"`
	OldestAge           time.Duration `json:"oldest_age"`
}

type DLQRecordDiagnostic struct {
	Topic             string               `json:"topic"`
	Partition         int32                `json:"partition"`
	Offset            int64                `json:"offset"`
	Timestamp         time.Time            `json:"timestamp"`
	SourceTopic       string               `json:"source_topic"`
	SourcePartition   int32                `json:"source_partition"`
	SourceOffset      int64                `json:"source_offset"`
	ConsumerGroup     ConsumerGroupID      `json:"consumer_group"`
	EventID           string               `json:"event_id"`
	ReplayID          string               `json:"replay_id,omitempty"`
	SchemaVersion     int                  `json:"schema_version"`
	FailureClass      FailureClass         `json:"failure_class"`
	Attempt           int                  `json:"attempt"`
	FirstFailureAt    time.Time            `json:"first_failure_at"`
	LatestFailureAt   time.Time            `json:"latest_failure_at"`
	NotBefore         time.Time            `json:"not_before"`
	ConsumedTopic     string               `json:"consumed_topic,omitempty"`
	ConsumedPartition int32                `json:"consumed_partition,omitempty"`
	ConsumedOffset    int64                `json:"consumed_offset,omitempty"`
	MetadataCode      RecoveryMetadataCode `json:"metadata_code,omitempty"`
	Replayable        bool                 `json:"replayable"`
	KeyBytes          int                  `json:"key_bytes"`
	KeySHA256         string               `json:"key_sha256"`
	PayloadBytes      int                  `json:"payload_bytes"`
	PayloadSHA256     string               `json:"payload_sha256"`
	ContentType       string               `json:"content_type"`
	JSONValid         bool                 `json:"json_valid"`
	JSONFields        []string             `json:"json_fields"`
}

type dlqPartitionOffsets struct {
	Start       int64
	End         int64
	RecentStart int64
}

type dlqReadRange struct {
	Topic     string
	Partition int32
	Start     int64
	End       int64
	Limit     int
}

type dlqInspectionBackend interface {
	PartitionOffsets(
		ctx context.Context,
		topics []string,
		recentSince time.Time,
	) (map[string]map[int32]dlqPartitionOffsets, error)
	ReadRecords(ctx context.Context, ranges []dlqReadRange) ([]brokerRecord, error)
}

type franzDLQInspectionBackend struct {
	admin  *kadm.Client
	config infraconfig.KafkaConfig
}

type DLQInspector struct {
	backend      dlqInspectionBackend
	prefix       string
	timeout      time.Duration
	recentWindow time.Duration
	now          func() time.Time
}

func NewDLQInspector(client *Client, cfg infraconfig.KafkaConfig) *DLQInspector {
	if client == nil || client.kgoClient == nil {
		return nil
	}
	timeout := client.adminTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &DLQInspector{
		backend: &franzDLQInspectionBackend{
			admin: kadm.NewClient(client.kgoClient), config: cfg,
		},
		prefix: cfg.TopicPrefix, timeout: timeout,
		recentWindow: defaultDLQRecentWindow, now: time.Now,
	}
}

func (i *DLQInspector) ListDLQTopics(ctx context.Context) ([]DLQTopicSummary, error) {
	if i == nil || i.backend == nil {
		return nil, ErrDLQInspectionFailed
	}
	registrations, err := registeredDLQTopics(i.prefix)
	if err != nil {
		return nil, ErrDLQInspectionFailed
	}
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.name)
	}
	now := i.currentTime()
	operationContext, cancel := i.operationContext(ctx)
	defer cancel()
	offsets, err := i.backend.PartitionOffsets(
		operationContext,
		names,
		now.Add(-i.recentWindow),
	)
	if err != nil {
		return nil, dlqInspectionError(ctx, err)
	}

	ranges := make([]dlqReadRange, 0)
	for _, registration := range registrations {
		for partition, partitionOffsets := range offsets[registration.name] {
			if partitionOffsets.Start < partitionOffsets.End {
				ranges = append(ranges, dlqReadRange{
					Topic: registration.name, Partition: partition,
					Start: partitionOffsets.Start, End: partitionOffsets.End, Limit: 1,
				})
			}
		}
	}
	oldestRecords, err := i.backend.ReadRecords(operationContext, ranges)
	if err != nil {
		return nil, dlqInspectionError(ctx, err)
	}
	oldestByPartition := make(map[dlqCoordinate]brokerRecord, len(oldestRecords))
	for _, record := range oldestRecords {
		coordinate := dlqCoordinate{topic: record.Topic, partition: record.Partition}
		existing, found := oldestByPartition[coordinate]
		if !found || record.Offset < existing.Offset {
			oldestByPartition[coordinate] = record
		}
	}

	summaries := make([]DLQTopicSummary, 0, len(registrations))
	for _, registration := range registrations {
		partitionOffsets, found := offsets[registration.name]
		if !found || len(partitionOffsets) == 0 {
			return nil, ErrDLQInspectionFailed
		}
		partitions := make([]int, 0, len(partitionOffsets))
		for partition := range partitionOffsets {
			if partition < 0 {
				return nil, ErrDLQInspectionFailed
			}
			partitions = append(partitions, int(partition))
		}
		sort.Ints(partitions)
		summary := DLQTopicSummary{
			Topic: registration.name, ConsumerGroup: registration.spec.Group,
			Retention:      registration.spec.DLQRetention,
			PartitionCount: len(partitions),
			Partitions:     make([]DLQPartitionSummary, 0, len(partitions)),
		}
		for _, partitionValue := range partitions {
			partition := int32(partitionValue)
			item := partitionOffsets[partition]
			if item.Start < 0 || item.End < item.Start ||
				item.RecentStart < item.Start || item.RecentStart > item.End {
				return nil, ErrDLQInspectionFailed
			}
			retained := item.End - item.Start
			recent := item.End - item.RecentStart
			partitionSummary := DLQPartitionSummary{
				Partition: partition, RetainedStartOffset: item.Start,
				EndOffset: item.End, RetainedEstimate: retained,
				EndOffsetGrowth: recent, RecentIngress: recent,
			}
			if retained > 0 {
				oldest, exists := oldestByPartition[dlqCoordinate{
					topic: registration.name, partition: partition,
				}]
				if !exists || oldest.Offset != item.Start || oldest.Timestamp.IsZero() {
					return nil, ErrDLQOffsetUnavailable
				}
				partitionSummary.OldestRecordAt = oldest.Timestamp.UTC()
				partitionSummary.OldestAge = nonNegativeDuration(
					now.Sub(partitionSummary.OldestRecordAt),
				)
				if summary.OldestRecordAt.IsZero() ||
					partitionSummary.OldestRecordAt.Before(summary.OldestRecordAt) {
					summary.OldestRecordAt = partitionSummary.OldestRecordAt
				}
			}
			summary.RetainedEstimate += retained
			summary.EndOffset += item.End
			summary.EndOffsetGrowth += recent
			summary.RecentIngress += recent
			summary.Partitions = append(summary.Partitions, partitionSummary)
		}
		if !summary.OldestRecordAt.IsZero() {
			summary.OldestAge = nonNegativeDuration(now.Sub(summary.OldestRecordAt))
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (i *DLQInspector) ReadDLQRecords(
	ctx context.Context,
	topic string,
	partition int32,
	startingOffset int64,
	limit int,
) ([]DLQRecordDiagnostic, error) {
	if i == nil || i.backend == nil {
		return nil, ErrDLQInspectionFailed
	}
	topic = strings.TrimSpace(topic)
	recovery, err := DLQTopicAllowed(i.prefix, topic)
	if err != nil {
		return nil, ErrDLQTopicNotAllowed
	}
	if partition < 0 {
		return nil, ErrDLQInvalidPartition
	}
	if startingOffset < 0 {
		return nil, ErrDLQInvalidOffset
	}
	if limit < 1 || limit > MaxDLQReadLimit {
		return nil, ErrDLQInvalidLimit
	}
	operationContext, cancel := i.operationContext(ctx)
	defer cancel()
	offsets, err := i.backend.PartitionOffsets(
		operationContext,
		[]string{topic},
		i.currentTime().Add(-i.recentWindow),
	)
	if err != nil {
		return nil, dlqInspectionError(ctx, err)
	}
	partitions, found := offsets[topic]
	if !found {
		return nil, ErrDLQInspectionFailed
	}
	partitionOffsets, found := partitions[partition]
	if !found {
		return nil, ErrDLQInvalidPartition
	}
	if startingOffset < partitionOffsets.Start {
		return nil, ErrDLQOffsetExpired
	}
	if startingOffset >= partitionOffsets.End {
		return nil, ErrDLQOffsetUnavailable
	}
	available := partitionOffsets.End - startingOffset
	readLimit := limit
	if int64(readLimit) > available {
		readLimit = int(available)
	}
	records, err := i.backend.ReadRecords(operationContext, []dlqReadRange{{
		Topic: topic, Partition: partition, Start: startingOffset,
		End: partitionOffsets.End, Limit: readLimit,
	}})
	if err != nil {
		return nil, dlqInspectionError(ctx, err)
	}
	sort.Slice(records, func(left, right int) bool {
		return records[left].Offset < records[right].Offset
	})
	if len(records) == 0 || records[0].Topic != topic ||
		records[0].Partition != partition || records[0].Offset != startingOffset {
		return nil, ErrDLQOffsetUnavailable
	}
	result := make([]DLQRecordDiagnostic, 0, min(len(records), readLimit))
	for _, record := range records {
		if len(result) >= readLimit {
			break
		}
		if record.Topic != topic || record.Partition != partition ||
			record.Offset < startingOffset || record.Offset >= partitionOffsets.End {
			return nil, ErrDLQInspectionFailed
		}
		diagnostic, err := diagnoseDLQRecord(i.prefix, recovery, record)
		if err != nil {
			return nil, err
		}
		result = append(result, diagnostic)
	}
	return result, nil
}

func (i *DLQInspector) FetchDLQRecord(
	ctx context.Context,
	topic string,
	partition int32,
	offset int64,
) (brokerRecord, RecoveryMetadata, error) {
	if i == nil || i.backend == nil {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQInspectionFailed
	}
	topic = strings.TrimSpace(topic)
	recovery, err := DLQTopicAllowed(i.prefix, topic)
	if err != nil {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQTopicNotAllowed
	}
	if partition < 0 {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQInvalidPartition
	}
	if offset < 0 {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQInvalidOffset
	}
	operationContext, cancel := i.operationContext(ctx)
	defer cancel()
	offsets, err := i.backend.PartitionOffsets(
		operationContext,
		[]string{topic},
		i.currentTime().Add(-i.recentWindow),
	)
	if err != nil {
		return brokerRecord{}, RecoveryMetadata{}, dlqInspectionError(ctx, err)
	}
	partitions, found := offsets[topic]
	if !found {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQInspectionFailed
	}
	partitionOffsets, found := partitions[partition]
	if !found {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQInvalidPartition
	}
	if offset < partitionOffsets.Start {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQOffsetExpired
	}
	if offset >= partitionOffsets.End {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQOffsetUnavailable
	}
	records, err := i.backend.ReadRecords(operationContext, []dlqReadRange{{
		Topic: topic, Partition: partition, Start: offset,
		End: partitionOffsets.End, Limit: 1,
	}})
	if err != nil {
		return brokerRecord{}, RecoveryMetadata{}, dlqInspectionError(ctx, err)
	}
	if len(records) != 1 || records[0].Topic != topic ||
		records[0].Partition != partition || records[0].Offset != offset {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQOffsetUnavailable
	}
	metadata, err := decodeDLQRecoveryMetadata(i.prefix, recovery, records[0])
	if err != nil || metadata.ConsumerGroup != recovery.Group || metadata.Tier != 0 {
		return brokerRecord{}, RecoveryMetadata{}, ErrDLQRecordInvalid
	}
	record := records[0]
	record.Key = append([]byte(nil), record.Key...)
	record.Value = append([]byte(nil), record.Value...)
	record.Headers = append([]applicationeventstream.Header(nil), record.Headers...)
	return record, metadata, nil
}

type registeredDLQTopic struct {
	name string
	spec RecoverySpec
}

func registeredDLQTopics(prefix string) ([]registeredDLQTopic, error) {
	result := make([]registeredDLQTopic, 0)
	for _, recovery := range Recoveries() {
		if recovery.Policy != RecoveryRetryTopics {
			continue
		}
		name, err := TopicName(prefix, recovery.DLQTopic)
		if err != nil {
			return nil, err
		}
		result = append(result, registeredDLQTopic{name: name, spec: recovery})
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].name < result[right].name
	})
	return result, nil
}

func diagnoseDLQRecord(
	prefix string,
	recovery RecoverySpec,
	record brokerRecord,
) (DLQRecordDiagnostic, error) {
	metadata, err := decodeDLQRecoveryMetadata(prefix, recovery, record)
	if err != nil || metadata.ConsumerGroup != recovery.Group || metadata.Tier != 0 {
		return DLQRecordDiagnostic{}, ErrDLQRecordInvalid
	}
	keyHash := sha256.Sum256(record.Key)
	jsonFields, jsonValid := boundedJSONDiagnostics(record.Value)
	contentType := "application/octet-stream"
	if jsonValid {
		contentType = "application/json"
	}
	return DLQRecordDiagnostic{
		Topic: record.Topic, Partition: record.Partition, Offset: record.Offset,
		Timestamp:   record.Timestamp.UTC(),
		SourceTopic: metadata.SourceTopic, SourcePartition: metadata.SourcePartition,
		SourceOffset: metadata.SourceOffset, ConsumerGroup: metadata.ConsumerGroup,
		EventID: metadata.EventID, ReplayID: metadata.ReplayID,
		SchemaVersion: metadata.SchemaVersion, FailureClass: metadata.FailureClass,
		Attempt: metadata.Attempt, FirstFailureAt: metadata.FirstFailureAt,
		LatestFailureAt: metadata.LatestFailureAt, NotBefore: metadata.NotBefore,
		ConsumedTopic:     metadata.ConsumedTopic,
		ConsumedPartition: metadata.ConsumedPartition,
		ConsumedOffset:    metadata.ConsumedOffset,
		MetadataCode:      metadata.MetadataCode,
		Replayable:        !metadata.NonReplayable,
		KeyBytes:          len(record.Key), KeySHA256: hex.EncodeToString(keyHash[:]),
		PayloadBytes: len(record.Value), PayloadSHA256: metadata.PayloadSHA256,
		ContentType: contentType, JSONValid: jsonValid, JSONFields: jsonFields,
	}, nil
}

func decodeDLQRecoveryMetadata(
	prefix string,
	recovery RecoverySpec,
	record brokerRecord,
) (RecoveryMetadata, error) {
	metadata, err := DecodeRecoveryHeaders(
		prefix,
		recovery.DLQTopic,
		record.Headers,
		record.Key,
		record.Value,
	)
	if err == nil {
		return metadata, nil
	}
	quarantine, quarantineErr := DecodeRecoveryQuarantineHeaders(
		prefix,
		recovery.DLQTopic,
		record.Headers,
		record.Key,
		record.Value,
	)
	if quarantineErr != nil {
		return RecoveryMetadata{}, ErrDLQRecordInvalid
	}
	return RecoveryMetadata{
		ConsumerGroup:     quarantine.ConsumerGroup,
		FailureClass:      quarantine.FailureClass,
		LatestFailureAt:   quarantine.QuarantinedAt,
		FirstFailureAt:    quarantine.QuarantinedAt,
		NotBefore:         quarantine.QuarantinedAt,
		PayloadSHA256:     quarantine.PayloadSHA256,
		ConsumedTopic:     quarantine.ConsumedTopic,
		ConsumedPartition: quarantine.ConsumedPartition,
		ConsumedOffset:    quarantine.ConsumedOffset,
		KeySHA256:         quarantine.KeySHA256,
		MetadataCode:      quarantine.MetadataCode,
		NonReplayable:     quarantine.NonReplayable,
	}, nil
}

func boundedJSONDiagnostics(value []byte) ([]string, bool) {
	if !json.Valid(value) {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil || object == nil {
		return nil, true
	}
	all := make([]string, 0, len(object))
	for field := range object {
		if len(field) <= MaxDLQJSONFieldLength {
			all = append(all, field)
		}
	}
	sort.Strings(all)
	if len(all) > MaxDLQJSONFields {
		all = all[:MaxDLQJSONFields]
	}
	return all, true
}

func (i *DLQInspector) currentTime() time.Time {
	if i != nil && i.now != nil {
		return i.now().UTC()
	}
	return time.Now().UTC()
}

func (i *DLQInspector) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if i.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, i.timeout)
}

func dlqInspectionError(parent context.Context, err error) error {
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrDLQInspectionFailed
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

type dlqCoordinate struct {
	topic     string
	partition int32
}

func (b *franzDLQInspectionBackend) PartitionOffsets(
	ctx context.Context,
	topics []string,
	recentSince time.Time,
) (map[string]map[int32]dlqPartitionOffsets, error) {
	if b == nil || b.admin == nil {
		return nil, ErrKafkaUnavailable
	}
	starts, err := b.admin.ListStartOffsets(ctx, topics...)
	if err != nil || starts.Error() != nil {
		return nil, errors.Join(err, starts.Error())
	}
	ends, err := b.admin.ListEndOffsets(ctx, topics...)
	if err != nil || ends.Error() != nil {
		return nil, errors.Join(err, ends.Error())
	}
	recent, err := b.admin.ListOffsetsAfterMilli(
		ctx,
		recentSince.UTC().UnixMilli(),
		topics...,
	)
	if err != nil || recent.Error() != nil {
		return nil, errors.Join(err, recent.Error())
	}
	result := make(map[string]map[int32]dlqPartitionOffsets, len(topics))
	for _, topic := range topics {
		startPartitions, exists := starts[topic]
		if !exists || len(startPartitions) == 0 {
			return nil, ErrKafkaUnavailable
		}
		result[topic] = make(map[int32]dlqPartitionOffsets, len(startPartitions))
		for partition, start := range startPartitions {
			end, endFound := ends.Lookup(topic, partition)
			recentOffset, recentFound := recent.Lookup(topic, partition)
			if partition < 0 || start.Err != nil || !endFound || end.Err != nil ||
				!recentFound || recentOffset.Err != nil ||
				start.Offset < 0 || end.Offset < start.Offset {
				return nil, ErrKafkaUnavailable
			}
			recentStart := recentOffset.Offset
			if recentStart < start.Offset {
				recentStart = start.Offset
			}
			if recentStart > end.Offset {
				recentStart = end.Offset
			}
			result[topic][partition] = dlqPartitionOffsets{
				Start: start.Offset, End: end.Offset, RecentStart: recentStart,
			}
		}
	}
	return result, nil
}

func (b *franzDLQInspectionBackend) ReadRecords(
	ctx context.Context,
	ranges []dlqReadRange,
) ([]brokerRecord, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	assignments := make(map[string]map[int32]kgo.Offset)
	states := make(map[dlqCoordinate]*dlqReadState, len(ranges))
	totalLimit := 0
	for _, item := range ranges {
		if item.Topic == "" || item.Partition < 0 || item.Start < 0 ||
			item.End <= item.Start || item.Limit < 1 ||
			item.Limit > MaxDLQReadLimit {
			return nil, ErrDLQInspectionFailed
		}
		coordinate := dlqCoordinate{topic: item.Topic, partition: item.Partition}
		if _, duplicate := states[coordinate]; duplicate {
			return nil, ErrDLQInspectionFailed
		}
		if assignments[item.Topic] == nil {
			assignments[item.Topic] = make(map[int32]kgo.Offset)
		}
		assignments[item.Topic][item.Partition] = kgo.NewOffset().At(item.Start)
		states[coordinate] = &dlqReadState{rangeSpec: item}
		totalLimit += item.Limit
	}
	options, err := clientOptions(b.config)
	if err != nil {
		return nil, err
	}
	options = append(options,
		kgo.ConsumePartitions(assignments),
		kgo.FetchMaxWait(250*time.Millisecond),
		kgo.FetchMaxBytes(8<<20),
		kgo.FetchMaxPartitionBytes(512<<10),
	)
	reader, err := kgo.NewClient(options...)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	result := make([]brokerRecord, 0, totalLimit)
	pending := len(states)
	for pending > 0 {
		fetches := reader.PollFetches(ctx)
		for _, fetchErr := range fetches.Errors() {
			if fetchErr.Err == nil {
				continue
			}
			if errors.Is(fetchErr.Err, kerr.OffsetOutOfRange) {
				return nil, ErrDLQOffsetUnavailable
			}
			return nil, fetchErr.Err
		}
		fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
			if err != nil {
				return
			}
			coordinate := dlqCoordinate{
				topic: partition.Topic, partition: partition.Partition,
			}
			state, exists := states[coordinate]
			if !exists || state.done {
				return
			}
			if partition.Err != nil {
				err = partition.Err
				return
			}
			for _, record := range partition.Records {
				if record.Offset < state.rangeSpec.Start {
					continue
				}
				if record.Offset >= state.rangeSpec.End {
					state.done = true
					break
				}
				result = append(result, brokerRecord{
					Topic: record.Topic, Partition: record.Partition,
					Offset: record.Offset, Timestamp: record.Timestamp.UTC(),
					Key:     append([]byte(nil), record.Key...),
					Value:   append([]byte(nil), record.Value...),
					Headers: recordHeaders(record),
				})
				state.read++
				if state.read >= state.rangeSpec.Limit ||
					record.Offset+1 >= state.rangeSpec.End {
					state.done = true
					break
				}
			}
			if !state.done && len(partition.Records) == 0 &&
				partition.HighWatermark >= state.rangeSpec.End {
				state.done = true
			}
			if state.done {
				pending--
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

type dlqReadState struct {
	rangeSpec dlqReadRange
	read      int
	done      bool
}

func recordHeaders(record *kgo.Record) []applicationeventstream.Header {
	headers := make([]applicationeventstream.Header, 0, len(record.Headers))
	for _, header := range record.Headers {
		headers = append(headers, applicationeventstream.Header{
			Key: header.Key, Value: append([]byte(nil), header.Value...),
		})
	}
	return headers
}
