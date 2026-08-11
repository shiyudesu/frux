package infrakafka

import (
	"fmt"
	"strings"
	"time"
)

type RecoveryPolicy string
type FailureClass string
type ReplayDestination string
type ConsumerStage string

const (
	RecoveryBlockAndRetry RecoveryPolicy = "block-and-retry"
	RecoveryRetryTopics   RecoveryPolicy = "retry-topics"
	RecoveryDurableJob    RecoveryPolicy = "durable-job"

	FailureRetryableInfrastructure FailureClass = "retryable_infrastructure"
	FailureTerminalContract        FailureClass = "terminal_contract"
	FailureTerminalDomain          FailureClass = "terminal_domain"
	FailureLocalRetryExhausted     FailureClass = "local_retry_exhausted"
	FailureRecoveryMetadataInvalid FailureClass = "recovery_metadata_invalid"

	ReplayToSource     ReplayDestination = "source"
	ReplayToFirstRetry ReplayDestination = "first-retry"

	ConsumerStageSource ConsumerStage = "source"

	TopicFeedVideoPublishedRetry5s  TopicID = "feed_video_published_retry_5s"
	TopicFeedVideoPublishedRetry30s TopicID = "feed_video_published_retry_30s"
	TopicFeedVideoPublishedRetry2m  TopicID = "feed_video_published_retry_2m"
	TopicFeedVideoPublishedRetry10m TopicID = "feed_video_published_retry_10m"
	TopicFeedVideoPublishedRetry30m TopicID = "feed_video_published_retry_30m"
	TopicFeedVideoPublishedDLQ      TopicID = "feed_video_published_dlq"
	TopicEmbeddingVideoRetry5s      TopicID = "embedding_video_published_retry_5s"
	TopicEmbeddingVideoRetry30s     TopicID = "embedding_video_published_retry_30s"
	TopicEmbeddingVideoRetry2m      TopicID = "embedding_video_published_retry_2m"
	TopicEmbeddingVideoRetry10m     TopicID = "embedding_video_published_retry_10m"
	TopicEmbeddingVideoRetry30m     TopicID = "embedding_video_published_retry_30m"
	TopicEmbeddingVideoPublishedDLQ TopicID = "embedding_video_published_dlq"
)

const (
	recoveryRetryRetention = 7 * 24 * time.Hour
	recoveryDLQRetention   = 30 * 24 * time.Hour
)

type LocalRetrySpec struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	MaxTotalDelay time.Duration
}

type RetryTierSpec struct {
	Tier  int
	Label string
	Delay time.Duration
	Topic TopicID
}

type RecoverySpec struct {
	Group             ConsumerGroupID
	SourceTopic       TopicID
	Policy            RecoveryPolicy
	LocalRetry        LocalRetrySpec
	RetryTiers        []RetryTierSpec
	DLQTopic          TopicID
	DLQRetention      time.Duration
	FailureClasses    []FailureClass
	ReplayDestination ReplayDestination
}

var standardLocalRetry = LocalRetrySpec{
	MaxAttempts: 3, InitialDelay: 100 * time.Millisecond,
	MaxDelay: 500 * time.Millisecond, MaxTotalDelay: time.Second,
}

var retryFailureClasses = []FailureClass{
	FailureRetryableInfrastructure,
	FailureTerminalContract,
	FailureTerminalDomain,
	FailureLocalRetryExhausted,
}

var recoveries = [...]RecoverySpec{
	{
		Group: GroupBackboneProbeActive, SourceTopic: TopicBackboneProbe,
		Policy: RecoveryBlockAndRetry, LocalRetry: standardLocalRetry,
		FailureClasses: []FailureClass{FailureRetryableInfrastructure, FailureLocalRetryExhausted},
	},
	{
		Group: GroupPersistActionActive, SourceTopic: TopicActionChanged,
		Policy: RecoveryBlockAndRetry, LocalRetry: standardLocalRetry,
		FailureClasses: []FailureClass{FailureRetryableInfrastructure, FailureLocalRetryExhausted},
	},
	{
		Group: GroupConsumeViewActive, SourceTopic: TopicViewEventRecorded,
		Policy: RecoveryBlockAndRetry, LocalRetry: standardLocalRetry,
		FailureClasses: []FailureClass{FailureRetryableInfrastructure, FailureLocalRetryExhausted},
	},
	{
		Group: GroupFeedVideoPublishedActive, SourceTopic: TopicVideoPublished,
		Policy: RecoveryRetryTopics, LocalRetry: standardLocalRetry,
		RetryTiers: retryTiers(
			TopicFeedVideoPublishedRetry5s,
			TopicFeedVideoPublishedRetry30s,
			TopicFeedVideoPublishedRetry2m,
			TopicFeedVideoPublishedRetry10m,
			TopicFeedVideoPublishedRetry30m,
		),
		DLQTopic: TopicFeedVideoPublishedDLQ, DLQRetention: recoveryDLQRetention,
		FailureClasses: retryFailureClasses, ReplayDestination: ReplayToFirstRetry,
	},
	{
		Group: GroupEmbeddingVideoPublishedActive, SourceTopic: TopicVideoPublished,
		Policy: RecoveryRetryTopics, LocalRetry: standardLocalRetry,
		RetryTiers: retryTiers(
			TopicEmbeddingVideoRetry5s,
			TopicEmbeddingVideoRetry30s,
			TopicEmbeddingVideoRetry2m,
			TopicEmbeddingVideoRetry10m,
			TopicEmbeddingVideoRetry30m,
		),
		DLQTopic: TopicEmbeddingVideoPublishedDLQ, DLQRetention: recoveryDLQRetention,
		FailureClasses: retryFailureClasses, ReplayDestination: ReplayToFirstRetry,
	},
	{
		Group: GroupMediaProcessingActive, SourceTopic: TopicMediaProcessingRequested,
		Policy: RecoveryDurableJob, LocalRetry: standardLocalRetry,
		FailureClasses: []FailureClass{FailureRetryableInfrastructure, FailureLocalRetryExhausted},
	},
}

var recoveryTopics = buildRecoveryTopics()

func retryTiers(topic5s, topic30s, topic2m, topic10m, topic30m TopicID) []RetryTierSpec {
	return []RetryTierSpec{
		{Tier: 1, Label: "5s", Delay: 5 * time.Second, Topic: topic5s},
		{Tier: 2, Label: "30s", Delay: 30 * time.Second, Topic: topic30s},
		{Tier: 3, Label: "2m", Delay: 2 * time.Minute, Topic: topic2m},
		{Tier: 4, Label: "10m", Delay: 10 * time.Minute, Topic: topic10m},
		{Tier: 5, Label: "30m", Delay: 30 * time.Minute, Topic: topic30m},
	}
}

func buildRecoveryTopics() []TopicSpec {
	definitions := []struct {
		group ConsumerGroupID
		base  string
		tiers []TopicID
		dlq   TopicID
	}{
		{
			group: GroupFeedVideoPublishedActive, base: "frux.feed.video-published",
			tiers: []TopicID{
				TopicFeedVideoPublishedRetry5s, TopicFeedVideoPublishedRetry30s,
				TopicFeedVideoPublishedRetry2m, TopicFeedVideoPublishedRetry10m,
				TopicFeedVideoPublishedRetry30m,
			},
			dlq: TopicFeedVideoPublishedDLQ,
		},
		{
			group: GroupEmbeddingVideoPublishedActive, base: "frux.embedding.video-published",
			tiers: []TopicID{
				TopicEmbeddingVideoRetry5s, TopicEmbeddingVideoRetry30s,
				TopicEmbeddingVideoRetry2m, TopicEmbeddingVideoRetry10m,
				TopicEmbeddingVideoRetry30m,
			},
			dlq: TopicEmbeddingVideoPublishedDLQ,
		},
	}
	labels := []string{"5s", "30s", "2m", "10m", "30m"}
	result := make([]TopicSpec, 0, len(definitions)*6)
	for _, definition := range definitions {
		source, ok := businessTopic(TopicVideoPublished)
		if !ok {
			panic("video publication topic is not registered")
		}
		for index, topicID := range definition.tiers {
			result = append(result, recoveryTopicSpec(
				topicID,
				definition.base+".retry-"+labels[index]+".v1",
				definition.group,
				source,
				recoveryRetryRetention,
				false,
			))
		}
		result = append(result, recoveryTopicSpec(
			definition.dlq,
			definition.base+".dlq.v1",
			definition.group,
			source,
			recoveryDLQRetention,
			true,
		))
	}
	return result
}

func recoveryTopicSpec(
	id TopicID,
	baseName string,
	group ConsumerGroupID,
	source TopicSpec,
	retention time.Duration,
	replayAllowed bool,
) TopicSpec {
	return TopicSpec{
		ID: id, BaseName: baseName, Version: 1,
		Class: TopicClassEvent, KeyKind: KeyKindVideoID,
		LocalPartitions: 12, Retention: retention, CleanupPolicy: CleanupDelete,
		MessageTimestamp: MessageTimestampLogAppendTime,
		MaxRecordBytes:   recoveryMaxRecordBytes(source),
		AllowedGroups:    []ConsumerGroupID{group},
		ReplayAllowed:    replayAllowed,
		RecoverySource:   source.ID,
	}
}

func RecoveryConsumerStage(group ConsumerGroupID, tier int) (ConsumerStage, error) {
	if tier == 0 {
		if _, err := ConsumerGroup(group); err != nil {
			return "", err
		}
		return ConsumerStageSource, nil
	}
	recovery, err := Recovery(group)
	if err != nil || recovery.Policy != RecoveryRetryTopics {
		return "", fmt.Errorf("%w: recovery consumer stage", ErrUnknownRegistryValue)
	}
	registered, ok := recovery.RetryTier(tier)
	if !ok {
		return "", fmt.Errorf("%w: recovery consumer stage", ErrUnknownRegistryValue)
	}
	return ConsumerStage("retry_" + registered.Label), nil
}

func Recoveries() []RecoverySpec {
	result := make([]RecoverySpec, 0, len(recoveries))
	for _, spec := range recoveries {
		result = append(result, cloneRecoverySpec(spec))
	}
	return result
}

func Recovery(group ConsumerGroupID) (RecoverySpec, error) {
	for _, spec := range recoveries {
		if spec.Group == group {
			return cloneRecoverySpec(spec), nil
		}
	}
	return RecoverySpec{}, fmt.Errorf("%w: recovery group %q", ErrUnknownRegistryValue, group)
}

func cloneRecoverySpec(spec RecoverySpec) RecoverySpec {
	spec.RetryTiers = append([]RetryTierSpec(nil), spec.RetryTiers...)
	spec.FailureClasses = append([]FailureClass(nil), spec.FailureClasses...)
	return spec
}

func (s RecoverySpec) AllowsFailure(class FailureClass) bool {
	for _, allowed := range s.FailureClasses {
		if allowed == class {
			return true
		}
	}
	return false
}

func (s RecoverySpec) RetryTier(tier int) (RetryTierSpec, bool) {
	if tier < 1 || tier > len(s.RetryTiers) {
		return RetryTierSpec{}, false
	}
	return s.RetryTiers[tier-1], true
}

func RecoveryTopic(group ConsumerGroupID, topic TopicID) (RetryTierSpec, bool, error) {
	spec, err := Recovery(group)
	if err != nil {
		return RetryTierSpec{}, false, err
	}
	for _, tier := range spec.RetryTiers {
		if tier.Topic == topic {
			return tier, false, nil
		}
	}
	if spec.DLQTopic == topic && topic != "" {
		return RetryTierSpec{}, true, nil
	}
	return RetryTierSpec{}, false, fmt.Errorf("%w: recovery topic %q", ErrUnknownRegistryValue, topic)
}

func RecoveryConsumerGroupName(prefix string, group ConsumerGroupID, tier int) (string, error) {
	spec, err := Recovery(group)
	if err != nil || spec.Policy != RecoveryRetryTopics {
		return "", fmt.Errorf("%w: recovery consumer group", ErrUnknownRegistryValue)
	}
	retry, ok := spec.RetryTier(tier)
	if !ok {
		return "", fmt.Errorf("%w: recovery tier", ErrUnknownRegistryValue)
	}
	base, err := GroupName(prefix, group)
	if err != nil {
		return "", err
	}
	return base + ".recovery." + retry.Label, nil
}

func DLQTopicAllowed(prefix, name string) (RecoverySpec, error) {
	name = strings.TrimSpace(name)
	for _, spec := range recoveries {
		if spec.Policy != RecoveryRetryTopics {
			continue
		}
		allowed, err := TopicName(prefix, spec.DLQTopic)
		if err != nil {
			return RecoverySpec{}, err
		}
		if name == allowed {
			return cloneRecoverySpec(spec), nil
		}
	}
	return RecoverySpec{}, fmt.Errorf("%w: DLQ topic", ErrUnknownRegistryValue)
}
