package infrakafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

var ErrConsumerCutover = errors.New("kafka consumer cutover failed")

type CutoverResult string
type CutoverMode string

const (
	CutoverInitialized CutoverResult = "initialized"
	CutoverPreserved   CutoverResult = "preserved"
	CutoverReset       CutoverResult = "reset"

	CutoverInitializeOnly CutoverMode = "initialize_only"
	CutoverForceReset     CutoverMode = "force_reset"
)

type cutoverBackend interface {
	FetchOffsets(context.Context, string, string) (kadm.OffsetResponses, error)
	OffsetsAfter(context.Context, time.Time, string) (kadm.ListedOffsets, error)
	CommitOffsets(context.Context, string, kadm.Offsets) error
	GroupInactive(context.Context, string) (bool, error)
}

type franzCutoverBackend struct {
	client *kadm.Client
}

type CutoverAdministrator struct {
	backend cutoverBackend
	prefix  string
	timeout time.Duration
}

func NewCutoverAdministrator(client *Client, cfg infraconfig.KafkaConfig) *CutoverAdministrator {
	return &CutoverAdministrator{
		backend: &franzCutoverBackend{client: kadm.NewClient(client.kgoClient)},
		prefix:  cfg.TopicPrefix,
		timeout: client.adminTimeout,
	}
}

func (a *CutoverAdministrator) Apply(
	ctx context.Context,
	groupID ConsumerGroupID,
	boundary string,
	mode CutoverMode,
) (CutoverResult, error) {
	if a == nil || a.backend == nil {
		return "", ErrKafkaUnavailable
	}
	group, err := ConsumerGroup(groupID)
	if err != nil || group.Shadow {
		return "", fmt.Errorf("%w: active group is required", ErrConsumerCutover)
	}
	cutoverAt, err := time.Parse(time.RFC3339, boundary)
	if err != nil {
		return "", fmt.Errorf("%w: invalid boundary", ErrConsumerCutover)
	}
	if mode != CutoverInitializeOnly && mode != CutoverForceReset {
		return "", fmt.Errorf("%w: invalid mode", ErrConsumerCutover)
	}
	topicName, err := TopicName(a.prefix, group.Topic)
	if err != nil {
		return "", err
	}
	groupName, err := ResolvedGroupName(a.prefix, "", groupID)
	if err != nil {
		return "", err
	}
	timeout := a.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	adminContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	committed, err := a.backend.FetchOffsets(adminContext, groupName, topicName)
	if err != nil || committed.Error() != nil {
		return "", fmt.Errorf("%w: fetch committed offsets", ErrConsumerCutover)
	}
	if mode == CutoverInitializeOnly && allPartitionsCommitted(committed, topicName) {
		return CutoverPreserved, nil
	}
	inactive, err := a.backend.GroupInactive(adminContext, groupName)
	if err != nil {
		return "", fmt.Errorf("%w: inspect group state", ErrConsumerCutover)
	}
	if !inactive {
		return "", fmt.Errorf("%w: group must be inactive", ErrConsumerCutover)
	}
	offsets, err := a.backend.OffsetsAfter(adminContext, cutoverAt.UTC(), topicName)
	if err != nil || offsets.Error() != nil || len(offsets[topicName]) == 0 {
		return "", fmt.Errorf("%w: resolve boundary offsets", ErrConsumerCutover)
	}
	if mode == CutoverInitializeOnly {
		committed, err = a.backend.FetchOffsets(adminContext, groupName, topicName)
		if err != nil || committed.Error() != nil {
			return "", fmt.Errorf("%w: recheck committed offsets", ErrConsumerCutover)
		}
		if allPartitionsCommitted(committed, topicName) {
			return CutoverPreserved, nil
		}
	}
	metadata := "frux-cutover:" + cutoverAt.UTC().Format(time.RFC3339)
	targets := cutoverTargets(offsets, committed, topicName, metadata, mode)
	if len(targets[topicName]) == 0 {
		return CutoverPreserved, nil
	}
	inactive, err = a.backend.GroupInactive(adminContext, groupName)
	if err != nil {
		return "", fmt.Errorf("%w: recheck group state", ErrConsumerCutover)
	}
	if !inactive {
		return "", fmt.Errorf("%w: group became active", ErrConsumerCutover)
	}
	if err := a.backend.CommitOffsets(adminContext, groupName, targets); err != nil {
		return "", fmt.Errorf("%w: commit boundary offsets", ErrConsumerCutover)
	}
	if mode == CutoverForceReset {
		return CutoverReset, nil
	}
	return CutoverInitialized, nil
}

func allPartitionsCommitted(offsets kadm.OffsetResponses, topic string) bool {
	if len(offsets[topic]) == 0 {
		return false
	}
	for _, offset := range offsets[topic] {
		if offset.Err != nil || offset.At < 0 {
			return false
		}
	}
	return true
}

func cutoverTargets(
	listed kadm.ListedOffsets,
	committed kadm.OffsetResponses,
	topic string,
	metadata string,
	mode CutoverMode,
) kadm.Offsets {
	targets := make(kadm.Offsets)
	for partition, listedOffset := range listed[topic] {
		if mode == CutoverInitializeOnly {
			existing, found := committed.Lookup(topic, partition)
			if found && existing.Err == nil && existing.At >= 0 {
				continue
			}
		}
		targets.Add(kadm.Offset{
			Topic: topic, Partition: partition, At: listedOffset.Offset,
			LeaderEpoch: listedOffset.LeaderEpoch, Metadata: metadata,
		})
	}
	return targets
}

func (b *franzCutoverBackend) FetchOffsets(
	ctx context.Context,
	group string,
	topic string,
) (kadm.OffsetResponses, error) {
	return b.client.FetchOffsetsForTopics(ctx, group, topic)
}

func (b *franzCutoverBackend) OffsetsAfter(
	ctx context.Context,
	boundary time.Time,
	topic string,
) (kadm.ListedOffsets, error) {
	return b.client.ListOffsetsAfterMilli(ctx, boundary.UnixMilli(), topic)
}

func (b *franzCutoverBackend) CommitOffsets(
	ctx context.Context,
	group string,
	offsets kadm.Offsets,
) error {
	return b.client.CommitAllOffsets(ctx, group, offsets)
}

func (b *franzCutoverBackend) GroupInactive(ctx context.Context, group string) (bool, error) {
	groups, err := b.client.DescribeGroups(ctx, group)
	if err != nil {
		if errors.Is(err, kerr.GroupIDNotFound) {
			return true, nil
		}
		return false, err
	}
	description, exists := groups[group]
	if !exists || errors.Is(description.Err, kerr.GroupIDNotFound) {
		return true, nil
	}
	if description.Err != nil {
		return false, description.Err
	}
	state := strings.ToLower(strings.TrimSpace(description.State))
	return len(description.Members) == 0 &&
		(state == "" || state == "empty" || state == "dead"), nil
}
