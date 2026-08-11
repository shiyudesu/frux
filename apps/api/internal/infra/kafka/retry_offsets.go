package infrakafka

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

var (
	ErrRetryOffsetInitialization = errors.New("kafka retry offset initialization failed")
	retryOffsetLocks             sync.Map
)

const (
	retryOffsetMetadata              = "frux-retry-retained-start:v1"
	retryOffsetInitializationVersion = "v1"
)

type RetryOffsetInitializationIdentity struct {
	Environment   string
	TopicPrefix   string
	ConsumerGroup string
	Topic         string
	Version       string
}

type RetryOffsetInitializationPartition struct {
	Partition     int32
	InitialOffset int64
	Committed     bool
}

type RetryOffsetInitializationState struct {
	Exists     bool
	Complete   bool
	Partitions map[int32]RetryOffsetInitializationPartition
}

type RetryOffsetInitializationLease interface {
	Load(context.Context) (RetryOffsetInitializationState, error)
	Ensure(
		context.Context,
		[]RetryOffsetInitializationPartition,
	) (RetryOffsetInitializationState, error)
	MarkCommitted(context.Context, int32) error
	Complete(context.Context) error
	Close() error
}

type RetryOffsetInitializationStore interface {
	Lock(
		context.Context,
		RetryOffsetInitializationIdentity,
	) (RetryOffsetInitializationLease, error)
}

func NewRetryOffsetInitializationIdentity(
	environment string,
	topicPrefix string,
	consumerGroup string,
	topic string,
) (RetryOffsetInitializationIdentity, error) {
	identity := RetryOffsetInitializationIdentity{
		Environment:   strings.TrimSpace(environment),
		TopicPrefix:   strings.TrimSpace(topicPrefix),
		ConsumerGroup: strings.TrimSpace(consumerGroup),
		Topic:         strings.TrimSpace(topic),
		Version:       retryOffsetInitializationVersion,
	}
	if identity.ConsumerGroup == "" || identity.Topic == "" {
		return RetryOffsetInitializationIdentity{}, fmt.Errorf(
			"%w: group and topic are required",
			ErrRetryOffsetInitialization,
		)
	}
	return identity, nil
}

func (i RetryOffsetInitializationIdentity) Fingerprint() string {
	value := strings.Join([]string{
		i.Version,
		i.Environment,
		i.TopicPrefix,
		i.ConsumerGroup,
		i.Topic,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type retryOffsetBackend interface {
	FetchOffsets(context.Context, string, string) (kadm.OffsetResponses, error)
	StartOffsets(context.Context, string) (kadm.ListedOffsets, error)
	EndOffsets(context.Context, string) (kadm.ListedOffsets, error)
	CommitOffsets(context.Context, string, kadm.Offsets) (kadm.OffsetResponses, error)
	GroupState(context.Context, string) (retryConsumerGroupState, error)
}

type franzRetryOffsetBackend struct {
	client *kadm.Client
}

type retryConsumerGroupState struct {
	Exists   bool
	Inactive bool
	Dead     bool
}

type retryOffsetAdministrator struct {
	backend  retryOffsetBackend
	store    RetryOffsetInitializationStore
	identity RetryOffsetInitializationIdentity
	timeout  time.Duration
}

type retryOffsetSnapshot struct {
	starts    kadm.ListedOffsets
	ends      kadm.ListedOffsets
	committed kadm.OffsetResponses
}

type retryOffsetRange struct {
	partition   int32
	start       int64
	end         int64
	leaderEpoch int32
}

func (a *retryOffsetAdministrator) Initialize(
	ctx context.Context,
	group string,
	topic string,
) error {
	if a == nil || a.backend == nil || a.store == nil {
		return ErrKafkaUnavailable
	}
	group = strings.TrimSpace(group)
	topic = strings.TrimSpace(topic)
	if group == "" || topic == "" ||
		a.identity.ConsumerGroup != group || a.identity.Topic != topic {
		return fmt.Errorf(
			"%w: durable identity does not match group and topic",
			ErrRetryOffsetInitialization,
		)
	}
	lock := retryOffsetLock(a.identity.Fingerprint())
	lock.Lock()
	defer lock.Unlock()

	timeout := a.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	adminContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lease, err := a.store.Lock(adminContext, a.identity)
	if err != nil {
		return fmt.Errorf(
			"%w: lock durable initialization marker: %w",
			ErrRetryOffsetInitialization,
			err,
		)
	}
	defer func() {
		_ = lease.Close()
	}()

	for {
		err = a.initializeOnce(adminContext, lease, group, topic)
		if err == nil || !retryableRetryOffsetInitialization(err) {
			return err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-adminContext.Done():
			timer.Stop()
			return errors.Join(
				ErrRetryOffsetInitialization,
				adminContext.Err(),
			)
		case <-timer.C:
		}
	}
}

func (a *retryOffsetAdministrator) initializeOnce(
	ctx context.Context,
	lease RetryOffsetInitializationLease,
	group string,
	topic string,
) error {
	snapshot, err := a.snapshot(ctx, group, topic)
	if err != nil {
		return err
	}
	ranges, err := retryOffsetRanges(snapshot, topic)
	if err != nil {
		return err
	}
	state, err := lease.Load(ctx)
	if err != nil {
		return retryOffsetStoreError("load durable initialization marker", err)
	}
	groupState, err := a.backend.GroupState(ctx, group)
	if err != nil {
		return fmt.Errorf(
			"%w: inspect group state: %w",
			ErrRetryOffsetInitialization,
			err,
		)
	}
	if state.Exists &&
		(state.Complete || retryOffsetStateHasCommitted(state)) &&
		groupState.Dead {
		return retryOffsetDataLoss("durably initialized retry group is dead")
	}

	plan, err := retryOffsetPlan(snapshot, ranges, state, groupState, topic)
	if err != nil {
		return err
	}
	if len(plan) > 0 {
		if !groupState.Inactive {
			return fmt.Errorf(
				"%w: group must be inactive",
				ErrRetryOffsetInitialization,
			)
		}
		state, err = lease.Ensure(ctx, plan)
		if err != nil {
			return retryOffsetStoreError("persist initialization plan", err)
		}
	}

	for _, partition := range sortedRetryInitializationPartitions(state) {
		if partition.Committed {
			continue
		}
		groupState, err = a.backend.GroupState(ctx, group)
		if err != nil {
			return fmt.Errorf(
				"%w: inspect group state: %w",
				ErrRetryOffsetInitialization,
				err,
			)
		}
		if !groupState.Inactive {
			return fmt.Errorf(
				"%w: group must remain inactive",
				ErrRetryOffsetInitialization,
			)
		}
		currentRange := ranges[partition.Partition]
		targets := retryOffsetTarget(topic, currentRange, partition.InitialOffset)
		responses, commitErr := a.backend.CommitOffsets(ctx, group, targets)
		if err := validateRetryOffsetCommitResponse(
			responses,
			commitErr,
			topic,
			partition.Partition,
			partition.InitialOffset,
		); err != nil {
			return err
		}
		if err := lease.MarkCommitted(ctx, partition.Partition); err != nil {
			return retryOffsetStoreError("record committed initialization offset", err)
		}
		state.Partitions[partition.Partition] = RetryOffsetInitializationPartition{
			Partition:     partition.Partition,
			InitialOffset: partition.InitialOffset,
			Committed:     true,
		}
	}
	if !state.Complete {
		groupState, err = a.backend.GroupState(ctx, group)
		if err != nil {
			return fmt.Errorf(
				"%w: inspect group state: %w",
				ErrRetryOffsetInitialization,
				err,
			)
		}
		if !groupState.Inactive {
			return fmt.Errorf(
				"%w: group became active before initialization completed",
				ErrRetryOffsetInitialization,
			)
		}
	}

	finalSnapshot, err := a.snapshot(ctx, group, topic)
	if err != nil {
		return err
	}
	if err := verifyRetryOffsetInitialization(finalSnapshot, state, topic); err != nil {
		return err
	}
	if err := lease.Complete(ctx); err != nil {
		return retryOffsetStoreError("complete durable initialization marker", err)
	}
	return nil
}

func (a *retryOffsetAdministrator) snapshot(
	ctx context.Context,
	group string,
	topic string,
) (retryOffsetSnapshot, error) {
	starts, err := a.backend.StartOffsets(ctx, topic)
	if err != nil || starts.Error() != nil {
		return retryOffsetSnapshot{}, fmt.Errorf(
			"%w: fetch retained start offsets: %w",
			ErrRetryOffsetInitialization,
			errors.Join(err, starts.Error()),
		)
	}
	ends, err := a.backend.EndOffsets(ctx, topic)
	if err != nil || ends.Error() != nil {
		return retryOffsetSnapshot{}, fmt.Errorf(
			"%w: fetch end offsets: %w",
			ErrRetryOffsetInitialization,
			errors.Join(err, ends.Error()),
		)
	}
	committed, err := a.backend.FetchOffsets(ctx, group, topic)
	if retryGroupMissing(err) ||
		(committed != nil && retryGroupMissing(committed.Error())) {
		committed = make(kadm.OffsetResponses)
		err = nil
	}
	if err != nil || committed.Error() != nil {
		return retryOffsetSnapshot{}, fmt.Errorf(
			"%w: fetch committed offsets: %w",
			ErrRetryOffsetInitialization,
			errors.Join(err, committed.Error()),
		)
	}
	return retryOffsetSnapshot{
		starts: starts, ends: ends, committed: committed,
	}, nil
}

func retryOffsetRanges(
	snapshot retryOffsetSnapshot,
	topic string,
) (map[int32]retryOffsetRange, error) {
	partitions := make([]int, 0, len(snapshot.starts[topic]))
	for partition := range snapshot.starts[topic] {
		partitions = append(partitions, int(partition))
	}
	sort.Ints(partitions)
	if len(partitions) == 0 {
		return nil, fmt.Errorf(
			"%w: topic has no retained offset ranges",
			ErrRetryOffsetInitialization,
		)
	}
	ranges := make(map[int32]retryOffsetRange, len(partitions))
	for index, rawPartition := range partitions {
		partition := int32(rawPartition)
		if partition != int32(index) {
			return nil, fmt.Errorf(
				"%w: non-contiguous topic partitions",
				ErrRetryOffsetInitialization,
			)
		}
		start, startFound := snapshot.starts.Lookup(topic, partition)
		end, endFound := snapshot.ends.Lookup(topic, partition)
		if !startFound || !endFound || start.Err != nil || end.Err != nil ||
			start.Offset < 0 || end.Offset < start.Offset {
			return nil, fmt.Errorf(
				"%w: invalid retained offset range",
				ErrRetryOffsetInitialization,
			)
		}
		ranges[partition] = retryOffsetRange{
			partition: partition, start: start.Offset, end: end.Offset,
			leaderEpoch: start.LeaderEpoch,
		}
	}
	if len(snapshot.ends[topic]) != len(partitions) {
		return nil, fmt.Errorf(
			"%w: offset range partition mismatch",
			ErrRetryOffsetInitialization,
		)
	}
	return ranges, nil
}

func retryOffsetPlan(
	snapshot retryOffsetSnapshot,
	ranges map[int32]retryOffsetRange,
	state RetryOffsetInitializationState,
	groupState retryConsumerGroupState,
	topic string,
) ([]RetryOffsetInitializationPartition, error) {
	if state.Exists {
		if len(state.Partitions) == 0 {
			return nil, retryOffsetDataLoss("durable retry initialization marker has no partitions")
		}
		if err := validateStoredRetryPartitions(state, ranges); err != nil {
			return nil, err
		}
	}

	plan := make([]RetryOffsetInitializationPartition, 0, len(ranges))
	validPartitions := make([]int32, 0, len(ranges))
	missingPartitions := make([]int32, 0, len(ranges))
	for partition := int32(0); partition < int32(len(ranges)); partition++ {
		currentRange := ranges[partition]
		committed, found, err := retryCommittedOffset(
			snapshot.committed,
			topic,
			currentRange,
		)
		if err != nil {
			return nil, err
		}
		stored, storedFound := state.Partitions[partition]
		if storedFound {
			if found {
				validPartitions = append(validPartitions, partition)
				if !stored.Committed {
					if committed.At != stored.InitialOffset ||
						committed.Metadata != retryOffsetMetadata {
						return nil, retryOffsetDataLoss(
							"durable initialization found an unexpected offset",
						)
					}
					plan = append(plan, RetryOffsetInitializationPartition{
						Partition: partition, InitialOffset: stored.InitialOffset,
						Committed: true,
					})
				}
				continue
			}
			missingPartitions = append(missingPartitions, partition)
			if state.Complete || stored.Committed {
				return nil, retryOffsetDataLoss(
					"durably established retry offset is missing",
				)
			}
			if currentRange.start != stored.InitialOffset ||
				stored.InitialOffset > currentRange.end {
				return nil, retryOffsetDataLoss(
					"pending retry initialization offset is no longer retained",
				)
			}
			continue
		}

		if found {
			validPartitions = append(validPartitions, partition)
			plan = append(plan, RetryOffsetInitializationPartition{
				Partition: partition, InitialOffset: committed.At, Committed: true,
			})
			continue
		}
		missingPartitions = append(missingPartitions, partition)
		plan = append(plan, RetryOffsetInitializationPartition{
			Partition: partition, InitialOffset: currentRange.start,
		})
	}

	if !state.Exists {
		if err := validateRetryOffsetGaps(
			missingPartitions,
			validPartitions,
			groupState.Exists,
		); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func validateStoredRetryPartitions(
	state RetryOffsetInitializationState,
	ranges map[int32]retryOffsetRange,
) error {
	for partition := int32(0); partition < int32(len(state.Partitions)); partition++ {
		stored, found := state.Partitions[partition]
		if !found || stored.Partition != partition {
			return retryOffsetDataLoss(
				"durable retry initialization partitions are not contiguous",
			)
		}
		currentRange, retained := ranges[partition]
		if !retained || stored.InitialOffset < 0 {
			return retryOffsetDataLoss(
				"durable retry initialization offset is outside the retained range",
			)
		}
		if !state.Complete && !stored.Committed &&
			(stored.InitialOffset < currentRange.start ||
				stored.InitialOffset > currentRange.end) {
			return retryOffsetDataLoss(
				"pending retry initialization offset is outside the retained range",
			)
		}
	}
	if len(state.Partitions) > len(ranges) {
		return retryOffsetDataLoss(
			"durable retry initialization references removed partitions",
		)
	}
	return nil
}

func retryCommittedOffset(
	committed kadm.OffsetResponses,
	topic string,
	currentRange retryOffsetRange,
) (kadm.Offset, bool, error) {
	offset, found := committed.Lookup(topic, currentRange.partition)
	if !found || offset.At < 0 {
		return kadm.Offset{}, false, nil
	}
	if offset.Err != nil {
		return kadm.Offset{}, false, fmt.Errorf(
			"%w: invalid committed offset: %w",
			ErrRetryOffsetInitialization,
			offset.Err,
		)
	}
	if offset.At < currentRange.start || offset.At > currentRange.end {
		return kadm.Offset{}, false, retryOffsetDataLoss(
			"committed offset is outside retained range",
		)
	}
	return offset.Offset, true, nil
}

func validateRetryOffsetGaps(missing, valid []int32, groupExists bool) error {
	if len(missing) == 0 {
		return nil
	}
	if len(valid) == 0 {
		if groupExists {
			return retryOffsetDataLoss("established group has no committed retry offsets")
		}
		return nil
	}
	firstMissing := missing[0]
	for _, partition := range valid {
		if partition > firstMissing {
			return retryOffsetDataLoss("established group has a missing committed retry offset")
		}
	}
	for index, partition := range missing {
		if partition != firstMissing+int32(index) {
			return retryOffsetDataLoss("missing retry offsets are not new trailing partitions")
		}
	}
	return nil
}

func retryOffsetTarget(
	topic string,
	currentRange retryOffsetRange,
	offset int64,
) kadm.Offsets {
	targets := make(kadm.Offsets)
	targets.Add(kadm.Offset{
		Topic: topic, Partition: currentRange.partition, At: offset,
		LeaderEpoch: currentRange.leaderEpoch, Metadata: retryOffsetMetadata,
	})
	return targets
}

func validateRetryOffsetCommitResponse(
	responses kadm.OffsetResponses,
	requestErr error,
	topic string,
	partition int32,
	offset int64,
) error {
	response, found := responses.Lookup(topic, partition)
	if requestErr != nil {
		return fmt.Errorf(
			"%w: commit retained start offset for partition %d: %w",
			ErrRetryOffsetInitialization,
			partition,
			requestErr,
		)
	}
	if !found {
		return fmt.Errorf(
			"%w: missing commit response for partition %d",
			ErrRetryOffsetInitialization,
			partition,
		)
	}
	if response.Err != nil {
		return fmt.Errorf(
			"%w: commit retained start offset for partition %d: %w",
			ErrRetryOffsetInitialization,
			partition,
			response.Err,
		)
	}
	if response.Topic != topic || response.Partition != partition ||
		response.At != offset {
		return fmt.Errorf(
			"%w: mismatched commit response for partition %d",
			ErrRetryOffsetInitialization,
			partition,
		)
	}
	return nil
}

func verifyRetryOffsetInitialization(
	snapshot retryOffsetSnapshot,
	state RetryOffsetInitializationState,
	topic string,
) error {
	ranges, err := retryOffsetRanges(snapshot, topic)
	if err != nil {
		return err
	}
	if len(state.Partitions) != len(ranges) {
		return fmt.Errorf(
			"%w: initialization partition count changed",
			ErrRetryOffsetInitialization,
		)
	}
	for partition := int32(0); partition < int32(len(ranges)); partition++ {
		stored, found := state.Partitions[partition]
		if !found || !stored.Committed {
			return fmt.Errorf(
				"%w: initialization partition %d is incomplete",
				ErrRetryOffsetInitialization,
				partition,
			)
		}
		committed, found, err := retryCommittedOffset(
			snapshot.committed,
			topic,
			ranges[partition],
		)
		if err != nil {
			return err
		}
		if !found {
			return retryOffsetDataLoss(
				"committed initialization offset disappeared before completion",
			)
		}
		if !state.Complete && committed.At < stored.InitialOffset {
			return retryOffsetDataLoss("initialization offset moved backwards")
		}
	}
	return nil
}

func sortedRetryInitializationPartitions(
	state RetryOffsetInitializationState,
) []RetryOffsetInitializationPartition {
	partitions := make([]RetryOffsetInitializationPartition, 0, len(state.Partitions))
	for _, partition := range state.Partitions {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(left, right int) bool {
		return partitions[left].Partition < partitions[right].Partition
	})
	return partitions
}

func retryOffsetStateHasCommitted(state RetryOffsetInitializationState) bool {
	for _, partition := range state.Partitions {
		if partition.Committed {
			return true
		}
	}
	return false
}

func retryOffsetLock(identity string) *sync.Mutex {
	value, _ := retryOffsetLocks.LoadOrStore(identity, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func retryOffsetStoreError(action string, err error) error {
	return fmt.Errorf(
		"%w: %s: %w",
		ErrRetryOffsetInitialization,
		action,
		err,
	)
}

func retryOffsetDataLoss(reason string) error {
	return errors.Join(
		ErrRetryOffsetInitialization,
		ErrConsumerDataLoss,
		errors.New(reason),
	)
}

func retryGroupMissing(err error) bool {
	return errors.Is(err, kerr.GroupIDNotFound)
}

func retryableRetryOffsetInitialization(err error) bool {
	return errors.Is(err, kerr.UnknownTopicOrPartition) ||
		errors.Is(err, kerr.LeaderNotAvailable) ||
		errors.Is(err, kerr.NotLeaderForPartition) ||
		errors.Is(err, kerr.CoordinatorLoadInProgress) ||
		errors.Is(err, kerr.CoordinatorNotAvailable) ||
		errors.Is(err, kerr.NotCoordinator) ||
		errors.Is(err, kerr.RequestTimedOut)
}

func (b *franzRetryOffsetBackend) FetchOffsets(
	ctx context.Context,
	group string,
	topic string,
) (kadm.OffsetResponses, error) {
	return b.client.FetchOffsetsForTopics(ctx, group, topic)
}

func (b *franzRetryOffsetBackend) StartOffsets(
	ctx context.Context,
	topic string,
) (kadm.ListedOffsets, error) {
	return b.client.ListStartOffsets(ctx, topic)
}

func (b *franzRetryOffsetBackend) EndOffsets(
	ctx context.Context,
	topic string,
) (kadm.ListedOffsets, error) {
	return b.client.ListEndOffsets(ctx, topic)
}

func (b *franzRetryOffsetBackend) CommitOffsets(
	ctx context.Context,
	group string,
	offsets kadm.Offsets,
) (kadm.OffsetResponses, error) {
	return b.client.CommitOffsets(ctx, group, offsets)
}

func (b *franzRetryOffsetBackend) GroupState(
	ctx context.Context,
	group string,
) (retryConsumerGroupState, error) {
	groups, err := b.client.DescribeGroups(ctx, group)
	if retryGroupMissing(err) {
		return retryConsumerGroupState{Inactive: true, Dead: true}, nil
	}
	if err != nil {
		return retryConsumerGroupState{}, err
	}
	description, exists := groups[group]
	if !exists || retryGroupMissing(description.Err) {
		return retryConsumerGroupState{Inactive: true, Dead: true}, nil
	}
	if description.Err != nil {
		return retryConsumerGroupState{}, description.Err
	}
	state := strings.ToLower(strings.TrimSpace(description.State))
	if state == "dead" {
		return retryConsumerGroupState{
			Inactive: true, Dead: true,
		}, nil
	}
	inactive := len(description.Members) == 0 &&
		(state == "" || state == "empty")
	return retryConsumerGroupState{Exists: true, Inactive: inactive}, nil
}
