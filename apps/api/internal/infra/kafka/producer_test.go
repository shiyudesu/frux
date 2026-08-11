package infrakafka

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeSyncProducer struct {
	calls   int
	results kgo.ProduceResults
	wait    <-chan struct{}
	record  *kgo.Record
}

func (f *fakeSyncProducer) ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
	f.calls++
	f.record = records[0]
	if f.wait != nil {
		select {
		case <-f.wait:
		case <-ctx.Done():
			return kgo.ProduceResults{{Record: records[0], Err: ctx.Err()}}
		}
	}
	if f.results == nil {
		records[0].Partition = 2
		records[0].Offset = 42
		return kgo.ProduceResults{{Record: records[0]}}
	}
	for index := range f.results {
		if f.results[index].Record == nil {
			f.results[index].Record = records[0]
		}
	}
	return f.results
}

func TestPublisherReportsAcknowledgedRecord(t *testing.T) {
	fake := &fakeSyncProducer{}
	publisher := &Publisher{producer: fake, timeout: time.Second}
	result, err := publisher.Publish(context.Background(), TopicBackboneProbe, []byte("probe:one"), validProbeMetadata(), BackboneProbePayload{ProbeID: "one", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Partition != 2 || result.Offset != 42 || fake.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, fake.calls)
	}
	if !fake.record.Timestamp.IsZero() {
		t.Fatalf("producer set record timestamp from its own clock: %s", fake.record.Timestamp)
	}
}

func TestPublisherClassifiesFailureCancellationAndUncertainty(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() context.Context
		fake *fakeSyncProducer
		want error
	}{
		{name: "broker error", ctx: context.Background, fake: &fakeSyncProducer{
			results: kgo.ProduceResults{{Err: kerr.RequestTimedOut}},
		}, want: ErrProduceUncertain},
		{name: "definite broker rejection", ctx: context.Background, fake: &fakeSyncProducer{
			results: kgo.ProduceResults{{Err: kerr.TopicAuthorizationFailed}},
		}, want: ErrProduceFailed},
		{name: "missing result", ctx: context.Background, fake: &fakeSyncProducer{
			results: kgo.ProduceResults{},
		}, want: ErrProduceUncertain},
		{name: "canceled before produce", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, fake: &fakeSyncProducer{}, want: ErrProduceCanceled},
		{name: "deadline while waiting", ctx: context.Background, fake: &fakeSyncProducer{
			wait: make(chan struct{}),
		}, want: ErrProduceUncertain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timeout := time.Second
			if test.name == "deadline while waiting" {
				timeout = time.Millisecond
			}
			publisher := &Publisher{producer: test.fake, timeout: timeout}
			_, err := publisher.Publish(test.ctx(), TopicBackboneProbe, []byte("probe:one"), validProbeMetadata(), BackboneProbePayload{ProbeID: "one", Source: "test"})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPublisherLeavesDuplicateSafeRetriesToIdempotentClient(t *testing.T) {
	fake := &fakeSyncProducer{results: kgo.ProduceResults{{Err: errors.New("retry exhausted")}}}
	publisher := &Publisher{producer: fake, timeout: time.Second}
	_, _ = publisher.Publish(context.Background(), TopicBackboneProbe, []byte("probe:one"), validProbeMetadata(), BackboneProbePayload{ProbeID: "one", Source: "test"})
	if fake.calls != 1 {
		t.Fatalf("application retried produce %d times; franz-go owns idempotent retries", fake.calls)
	}
}

func TestRecoveryPublisherEnforcesRegisteredDestinationRecordSize(t *testing.T) {
	source, err := Topic(TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := Topic(TopicFeedVideoPublishedRetry5s)
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRecordBoundary(t, source, topic)
}

func TestRecoveryRecordCapacityScalesForSmallerSourceTopics(t *testing.T) {
	source, err := Topic(TopicMediaProcessingRequested)
	if err != nil {
		t.Fatal(err)
	}
	topic := TopicSpec{
		ID:             TopicID("test_media_recovery"),
		MaxRecordBytes: recoveryMaxRecordBytes(source),
		RecoverySource: source.ID,
	}
	assertRecoveryRecordBoundary(t, source, topic)
}

func assertRecoveryRecordBoundary(t *testing.T, source, destination TopicSpec) {
	t.Helper()
	key := []byte("source:key")
	headers := []kgo.RecordHeader{{
		Key: "h",
		Value: bytes.Repeat(
			[]byte("h"),
			MaxRecoveryTotalHeaderBytes-len("h"),
		),
	}}
	value := bytes.Repeat(
		[]byte("x"),
		brokerMaxMessageBytes(source)-len(key),
	)
	record := &kgo.Record{Key: key, Value: value, Headers: headers}
	if err := validateTopicRecordSizeForSpec(destination, record); err != nil {
		t.Fatalf("source broker boundary rejected: %v", err)
	}
	record.Value = append(record.Value, 'x')
	if err := validateTopicRecordSizeForSpec(destination, record); !errors.Is(err, ErrContractFailure) {
		t.Fatalf("above source broker boundary error=%v", err)
	}
	record.Value = record.Value[:len(record.Value)-1]
	record.Headers[0].Value = append(record.Headers[0].Value, 'h')
	if err := validateTopicRecordSizeForSpec(destination, record); !errors.Is(err, ErrContractFailure) {
		t.Fatalf("above recovery header boundary error=%v", err)
	}
}

func TestUncertainProduceErrorMayHaveAcknowledged(t *testing.T) {
	err := &UncertainProduceError{cause: errors.New("deadline")}
	if !errors.Is(err, ErrProduceUncertain) || !err.MayHaveAcknowledged() {
		t.Fatalf("uncertain error = %v", err)
	}

}

func TestProduceResultAcknowledgementClassification(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{err: kerr.RequestTimedOut, want: true},
		{err: kerr.NotEnoughReplicasAfterAppend, want: true},
		{err: kerr.TopicAuthorizationFailed, want: false},
		{err: kerr.InvalidRecord, want: false},
		{err: errors.New("connection reset"), want: true},
	} {
		if got := produceResultMayHaveAcknowledged(test.err); got != test.want {
			t.Fatalf("error=%v got=%v want=%v", test.err, got, test.want)
		}
	}
}

func validProbeMetadata() EventMetadata {
	now := time.Now().UTC()
	return EventMetadata{
		EventID: "event-one", Type: EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: now.Add(-time.Second), ProducedAt: now,
		Producer: ProducerPlatformWorker,
	}
}
