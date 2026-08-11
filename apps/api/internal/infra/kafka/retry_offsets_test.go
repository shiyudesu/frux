package infrakafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

type fakeRetryOffsetBackend struct {
	mu                   sync.Mutex
	committed            kadm.OffsetResponses
	starts               kadm.ListedOffsets
	ends                 kadm.ListedOffsets
	groupState           retryConsumerGroupState
	err                  error
	commitErrors         map[int32][]error
	commitResponseErrors map[int32][]error
	commitCalls          []int32
}

func (f *fakeRetryOffsetBackend) FetchOffsets(
	context.Context,
	string,
	string,
) (kadm.OffsetResponses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneOffsetResponses(f.committed), f.err
}

func (f *fakeRetryOffsetBackend) StartOffsets(
	context.Context,
	string,
) (kadm.ListedOffsets, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.err
}

func (f *fakeRetryOffsetBackend) EndOffsets(
	context.Context,
	string,
) (kadm.ListedOffsets, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ends, f.err
}

func (f *fakeRetryOffsetBackend) CommitOffsets(
	_ context.Context,
	_ string,
	offsets kadm.Offsets,
) (kadm.OffsetResponses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sorted := offsets.Sorted()
	if len(sorted) != 1 {
		return nil, errors.New("test backend expects one partition per commit")
	}
	target := sorted[0]
	f.commitCalls = append(f.commitCalls, target.Partition)
	if failures := f.commitErrors[target.Partition]; len(failures) > 0 {
		err := failures[0]
		f.commitErrors[target.Partition] = failures[1:]
		return nil, err
	}
	responseErr := error(nil)
	if failures := f.commitResponseErrors[target.Partition]; len(failures) > 0 {
		responseErr = failures[0]
		f.commitResponseErrors[target.Partition] = failures[1:]
	}
	responses := kadm.OffsetResponses{}
	responses.Add(kadm.OffsetResponse{Offset: target, Err: responseErr})
	if responseErr == nil {
		if f.committed == nil {
			f.committed = make(kadm.OffsetResponses)
		}
		f.committed.Add(kadm.OffsetResponse{Offset: target})
		f.groupState = retryConsumerGroupState{Exists: true, Inactive: true}
	}
	return responses, nil
}

func (f *fakeRetryOffsetBackend) GroupState(
	context.Context,
	string,
) (retryConsumerGroupState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.groupState, f.err
}

func (f *fakeRetryOffsetBackend) deleteCommitted(topic string, partition int32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.committed[topic], partition)
	f.groupState = retryConsumerGroupState{
		Exists: true, Inactive: true, Dead: true,
	}
}

type memoryRetryOffsetStore struct {
	mu     sync.Mutex
	states map[string]RetryOffsetInitializationState
}

type memoryRetryOffsetLease struct {
	store       *memoryRetryOffsetStore
	fingerprint string
	closed      bool
}

func newMemoryRetryOffsetStore() *memoryRetryOffsetStore {
	return &memoryRetryOffsetStore{
		states: make(map[string]RetryOffsetInitializationState),
	}
}

func (s *memoryRetryOffsetStore) Lock(
	_ context.Context,
	identity RetryOffsetInitializationIdentity,
) (RetryOffsetInitializationLease, error) {
	s.mu.Lock()
	return &memoryRetryOffsetLease{
		store: s, fingerprint: identity.Fingerprint(),
	}, nil
}

func (l *memoryRetryOffsetLease) Load(
	context.Context,
) (RetryOffsetInitializationState, error) {
	return cloneRetryInitializationState(l.store.states[l.fingerprint]), nil
}

func (l *memoryRetryOffsetLease) Ensure(
	_ context.Context,
	partitions []RetryOffsetInitializationPartition,
) (RetryOffsetInitializationState, error) {
	state := cloneRetryInitializationState(l.store.states[l.fingerprint])
	if !state.Exists {
		state.Exists = true
		state.Partitions = make(map[int32]RetryOffsetInitializationPartition)
	}
	for _, partition := range partitions {
		existing, found := state.Partitions[partition.Partition]
		if found && existing.InitialOffset != partition.InitialOffset {
			return RetryOffsetInitializationState{}, errors.New("plan changed")
		}
		if found && existing.Committed {
			partition.Committed = true
		}
		if !found || partition.Committed {
			state.Partitions[partition.Partition] = partition
		}
		if !found {
			state.Complete = false
		}
	}
	l.store.states[l.fingerprint] = cloneRetryInitializationState(state)
	return cloneRetryInitializationState(state), nil
}

func (l *memoryRetryOffsetLease) MarkCommitted(
	_ context.Context,
	partition int32,
) error {
	state := cloneRetryInitializationState(l.store.states[l.fingerprint])
	item, found := state.Partitions[partition]
	if !found {
		return errors.New("partition missing")
	}
	item.Committed = true
	state.Partitions[partition] = item
	l.store.states[l.fingerprint] = state
	return nil
}

func (l *memoryRetryOffsetLease) Complete(context.Context) error {
	state := cloneRetryInitializationState(l.store.states[l.fingerprint])
	for _, partition := range state.Partitions {
		if !partition.Committed {
			return errors.New("incomplete")
		}
	}
	state.Complete = true
	l.store.states[l.fingerprint] = state
	return nil
}

func (l *memoryRetryOffsetLease) Close() error {
	if l == nil || l.closed {
		return nil
	}
	l.closed = true
	l.store.mu.Unlock()
	return nil
}

func (s *memoryRetryOffsetStore) state(
	identity RetryOffsetInitializationIdentity,
) RetryOffsetInitializationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRetryInitializationState(s.states[identity.Fingerprint()])
}

func TestRetryOffsetsInitializeBrandNewGroupAtRetainedStarts(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7, 12),
		ends:       listedOffsets(topic, 20, 30),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	state := store.state(identity)
	if !state.Complete || len(state.Partitions) != 2 ||
		state.Partitions[0].InitialOffset != 7 ||
		state.Partitions[1].InitialOffset != 12 ||
		!state.Partitions[0].Committed || !state.Partitions[1].Committed {
		t.Fatalf("state=%+v", state)
	}
	if !equalPartitions(backend.commitCalls, []int32{0, 1}) {
		t.Fatalf("commit order=%v", backend.commitCalls)
	}
}

func TestRetryOffsetsPreserveDurableGroupAcrossRestart(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7, 12),
		ends:       listedOffsets(topic, 20, 30),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	backend.commitCalls = nil
	backend.committed = committedOffsets(topic, 11, 19)
	backend.groupState = retryConsumerGroupState{Exists: true, Inactive: true}
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	if len(backend.commitCalls) != 0 || !store.state(identity).Complete {
		t.Fatalf("commits=%v state=%+v", backend.commitCalls, store.state(identity))
	}
}

func TestRetryOffsetsResumeOnlyMissingPartitionAfterPartialCommit(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7, 12),
		ends:       listedOffsets(topic, 20, 30),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
		commitResponseErrors: map[int32][]error{
			1: {kerr.RequestTimedOut, nil},
		},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	if !equalPartitions(backend.commitCalls, []int32{0, 1, 1}) {
		t.Fatalf("commit order=%v", backend.commitCalls)
	}
	state := store.state(identity)
	if !state.Complete || !state.Partitions[0].Committed ||
		!state.Partitions[1].Committed {
		t.Fatalf("state=%+v", state)
	}
}

func TestRetryOffsetsConcurrentInitializationCommitsEachPartitionOnce(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7, 12, 25),
		ends:       listedOffsets(topic, 20, 30, 40),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
	}
	first, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	second, _ := testRetryOffsetAdministrator(t, backend, store, topic)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, admin := range []*retryOffsetAdministrator{first, second} {
		admin := admin
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- admin.Initialize(
				context.Background(),
				identity.ConsumerGroup,
				topic,
			)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !equalPartitions(backend.commitCalls, []int32{0, 1, 2}) {
		t.Fatalf("commit calls=%v", backend.commitCalls)
	}
}

func TestRetryOffsetsInitializeOnlyNewTrailingPartitions(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7, 12),
		ends:       listedOffsets(topic, 20, 30),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	backend.commitCalls = nil
	backend.starts = listedOffsets(topic, 7, 12, 25)
	backend.ends = listedOffsets(topic, 20, 30, 25)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	if !equalPartitions(backend.commitCalls, []int32{2}) {
		t.Fatalf("commit calls=%v", backend.commitCalls)
	}
	state := store.state(identity)
	if !state.Complete || len(state.Partitions) != 3 ||
		state.Partitions[2].InitialOffset != 25 {
		t.Fatalf("state=%+v", state)
	}
}

func TestRetryOffsetsRejectMissingOffsetWithCompleteMarker(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7),
		ends:       listedOffsets(topic, 20),
		groupState: retryConsumerGroupState{Inactive: true, Dead: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	if err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic); err != nil {
		t.Fatal(err)
	}
	backend.deleteCommitted(topic, 0)
	err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic)
	if !errors.Is(err, ErrConsumerDataLoss) {
		t.Fatalf("error=%v", err)
	}
}

func TestRetryOffsetsRejectCommittedOffsetOutsideRetention(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		committed:  committedOffsets(topic, 6),
		starts:     listedOffsets(topic, 7),
		ends:       listedOffsets(topic, 20),
		groupState: retryConsumerGroupState{Exists: true, Inactive: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic)
	if !errors.Is(err, ErrConsumerDataLoss) {
		t.Fatalf("error=%v", err)
	}
}

func TestRetryOffsetsRejectExistingActiveGroupWithoutOffsets(t *testing.T) {
	topic := "frux.feed.video-published.retry-5s.v1"
	store := newMemoryRetryOffsetStore()
	backend := &fakeRetryOffsetBackend{
		starts:     listedOffsets(topic, 7),
		ends:       listedOffsets(topic, 20),
		groupState: retryConsumerGroupState{Exists: true},
	}
	admin, identity := testRetryOffsetAdministrator(t, backend, store, topic)
	err := admin.Initialize(context.Background(), identity.ConsumerGroup, topic)
	if !errors.Is(err, ErrConsumerDataLoss) || len(backend.commitCalls) != 0 {
		t.Fatalf("error=%v commits=%v", err, backend.commitCalls)
	}
}

func testRetryOffsetAdministrator(
	t *testing.T,
	backend retryOffsetBackend,
	store RetryOffsetInitializationStore,
	topic string,
) (*retryOffsetAdministrator, RetryOffsetInitializationIdentity) {
	t.Helper()
	identity, err := NewRetryOffsetInitializationIdentity(
		"test",
		"frux",
		"frux.feed.retry.tier-1.v1",
		topic,
	)
	if err != nil {
		t.Fatal(err)
	}
	return &retryOffsetAdministrator{
		backend: backend, store: store, identity: identity, timeout: time.Second,
	}, identity
}

func listedOffsets(topic string, offsets ...int64) kadm.ListedOffsets {
	result := kadm.ListedOffsets{topic: make(map[int32]kadm.ListedOffset, len(offsets))}
	for partition, offset := range offsets {
		result[topic][int32(partition)] = kadm.ListedOffset{
			Topic: topic, Partition: int32(partition),
			Offset: offset, LeaderEpoch: int32(partition + 1),
		}
	}
	return result
}

func committedOffsets(topic string, offsets ...int64) kadm.OffsetResponses {
	result := kadm.OffsetResponses{topic: make(map[int32]kadm.OffsetResponse, len(offsets))}
	for partition, offset := range offsets {
		if offset < 0 {
			continue
		}
		result[topic][int32(partition)] = kadm.OffsetResponse{
			Offset: kadm.Offset{
				Topic: topic, Partition: int32(partition), At: offset,
				Metadata: retryOffsetMetadata,
			},
		}
	}
	return result
}

func cloneOffsetResponses(source kadm.OffsetResponses) kadm.OffsetResponses {
	result := make(kadm.OffsetResponses, len(source))
	for topic, partitions := range source {
		result[topic] = make(map[int32]kadm.OffsetResponse, len(partitions))
		for partition, offset := range partitions {
			result[topic][partition] = offset
		}
	}
	return result
}

func cloneRetryInitializationState(
	source RetryOffsetInitializationState,
) RetryOffsetInitializationState {
	result := source
	result.Partitions = make(
		map[int32]RetryOffsetInitializationPartition,
		len(source.Partitions),
	)
	for partition, item := range source.Partitions {
		result.Partitions[partition] = item
	}
	return result
}

func equalPartitions(left, right []int32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
