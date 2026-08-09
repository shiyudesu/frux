package infrakafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
)

type fakeCutoverBackend struct {
	fetches      []kadm.OffsetResponses
	offsets      kadm.ListedOffsets
	inactive     bool
	err          error
	fetchCalls   int
	offsetCalls  int
	commitCalls  int
	committed    kadm.Offsets
	commitGroup  string
	inactiveCall int
}

func (f *fakeCutoverBackend) FetchOffsets(
	context.Context,
	string,
	string,
) (kadm.OffsetResponses, error) {
	index := f.fetchCalls
	f.fetchCalls++
	if len(f.fetches) == 0 {
		return kadm.OffsetResponses{}, f.err
	}
	if index >= len(f.fetches) {
		index = len(f.fetches) - 1
	}
	return f.fetches[index], f.err
}

func (f *fakeCutoverBackend) OffsetsAfter(
	context.Context,
	time.Time,
	string,
) (kadm.ListedOffsets, error) {
	f.offsetCalls++
	return f.offsets, f.err
}

func (f *fakeCutoverBackend) CommitOffsets(
	_ context.Context,
	group string,
	offsets kadm.Offsets,
) error {
	f.commitCalls++
	f.commitGroup = group
	f.committed = offsets
	return f.err
}

func (f *fakeCutoverBackend) GroupInactive(context.Context, string) (bool, error) {
	f.inactiveCall++
	return f.inactive, f.err
}

func TestCutoverInitializesTimestampOffsetsExactlyOnce(t *testing.T) {
	topic := "frux.interaction.action-changed.v1"
	backend := &fakeCutoverBackend{
		inactive: true,
		fetches: []kadm.OffsetResponses{
			{},
			{},
		},
		offsets: kadm.ListedOffsets{
			topic: {
				0: {Topic: topic, Partition: 0, Offset: 12, LeaderEpoch: 3},
				1: {Topic: topic, Partition: 1, Offset: 21, LeaderEpoch: 4},
			},
		},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	result, err := admin.Apply(
		context.Background(),
		GroupPersistActionActive,
		"2026-08-09T01:00:00Z",
		CutoverInitializeOnly,
	)
	if err != nil || result != CutoverInitialized {
		t.Fatalf("result=%s error=%v", result, err)
	}
	if backend.commitCalls != 1 ||
		backend.commitGroup != "frux.interaction.persist-action.v1" ||
		backend.committed[topic][0].At != 12 ||
		backend.committed[topic][1].At != 21 ||
		backend.committed[topic][0].Metadata != "frux-cutover:2026-08-09T01:00:00Z" {
		t.Fatalf("group=%q committed=%+v", backend.commitGroup, backend.committed)
	}
}

func TestCutoverPreservesExistingGroupCommitsAcrossRestarts(t *testing.T) {
	topic := "frux.exposure.view-event-recorded.v1"
	backend := &fakeCutoverBackend{
		fetches: []kadm.OffsetResponses{
			{
				topic: {
					0: {Offset: kadm.Offset{Topic: topic, Partition: 0, At: 99}},
				},
			},
		},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	result, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		"2026-08-09T00:00:00Z",
		CutoverInitializeOnly,
	)
	if err != nil || result != CutoverPreserved {
		t.Fatalf("result=%s error=%v", result, err)
	}
	if backend.offsetCalls != 0 || backend.commitCalls != 0 || backend.inactiveCall != 0 {
		t.Fatalf("offsets=%d commits=%d inactive=%d", backend.offsetCalls, backend.commitCalls, backend.inactiveCall)
	}
}

func TestCutoverRecheckPreservesConcurrentInitialization(t *testing.T) {
	topic := "frux.exposure.view-event-recorded.v1"
	backend := &fakeCutoverBackend{
		inactive: true,
		fetches: []kadm.OffsetResponses{
			{},
			{
				topic: {
					0: {Offset: kadm.Offset{Topic: topic, Partition: 0, At: 44}},
				},
			},
		},
		offsets: kadm.ListedOffsets{
			topic: {0: {Topic: topic, Partition: 0, Offset: 10}},
		},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	result, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		"2026-08-09T00:00:00Z",
		CutoverInitializeOnly,
	)
	if err != nil || result != CutoverPreserved || backend.commitCalls != 0 {
		t.Fatalf("result=%s error=%v commits=%d", result, err, backend.commitCalls)
	}
}

func TestCutoverInitializesOnlyMissingPartitions(t *testing.T) {
	topic := "frux.exposure.view-event-recorded.v1"
	partial := kadm.OffsetResponses{
		topic: {
			0: {Offset: kadm.Offset{Topic: topic, Partition: 0, At: 44}},
			1: {Offset: kadm.Offset{Topic: topic, Partition: 1, At: -1}},
		},
	}
	backend := &fakeCutoverBackend{
		inactive: true,
		fetches:  []kadm.OffsetResponses{partial, partial},
		offsets: kadm.ListedOffsets{
			topic: {
				0: {Topic: topic, Partition: 0, Offset: 10},
				1: {Topic: topic, Partition: 1, Offset: 20},
			},
		},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	result, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		"2026-08-09T00:00:00Z",
		CutoverInitializeOnly,
	)
	if err != nil || result != CutoverInitialized {
		t.Fatalf("result=%s error=%v", result, err)
	}
	if _, exists := backend.committed[topic][0]; exists ||
		backend.committed[topic][1].At != 20 {
		t.Fatalf("committed=%+v", backend.committed)
	}
}

func TestCutoverResetRequiresInactiveGroup(t *testing.T) {
	topic := "frux.exposure.view-event-recorded.v1"
	backend := &fakeCutoverBackend{
		inactive: false,
		fetches: []kadm.OffsetResponses{{
			topic: {0: {Offset: kadm.Offset{Topic: topic, Partition: 0, At: 99}}},
		}},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	_, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		"2026-08-09T00:00:00Z",
		CutoverForceReset,
	)
	if !errors.Is(err, ErrConsumerCutover) || backend.commitCalls != 0 {
		t.Fatalf("error=%v commits=%d", err, backend.commitCalls)
	}
}

func TestCutoverMetadataPreservesMillisecondBoundary(t *testing.T) {
	topic := "frux.exposure.view-event-recorded.v1"
	backend := &fakeCutoverBackend{
		inactive: true,
		fetches:  []kadm.OffsetResponses{{}, {}},
		offsets: kadm.ListedOffsets{
			topic: {
				0: {Topic: topic, Partition: 0, Offset: 7},
			},
		},
	}
	admin := &CutoverAdministrator{backend: backend, timeout: time.Second}
	_, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		"2026-08-09T01:00:00.123Z",
		CutoverInitializeOnly,
	)
	if err != nil {
		t.Fatal(err)
	}
	offset := backend.committed[topic][0]
	if offset.Metadata != "frux-cutover:2026-08-09T01:00:00.123Z" {
		t.Fatalf("metadata = %q", offset.Metadata)
	}
}

func TestCutoverRejectsFutureBoundary(t *testing.T) {
	now := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	admin := &CutoverAdministrator{
		backend: &fakeCutoverBackend{},
		timeout: time.Second,
		now:     func() time.Time { return now },
	}
	_, err := admin.Apply(
		context.Background(),
		GroupConsumeViewActive,
		now.Add(time.Millisecond).Format(time.RFC3339Nano),
		CutoverInitializeOnly,
	)
	if !errors.Is(err, ErrConsumerCutover) {
		t.Fatalf("error = %v, want ErrConsumerCutover", err)
	}
}
