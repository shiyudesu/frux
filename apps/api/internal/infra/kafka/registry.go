package infrakafka

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type TopicID string
type TopicClass string
type CleanupPolicy string
type KeyKind string
type ProducerID string
type ConsumerGroupID string
type ResponsibilityID string
type ProducerMode string
type ConsumerMode string

const (
	TopicBackboneProbe     TopicID = "backbone_probe"
	TopicActionChanged     TopicID = "action_changed"
	TopicViewEventRecorded TopicID = "view_event_recorded"

	TopicClassEvent   TopicClass = "event"
	TopicClassCommand TopicClass = "command"

	CleanupDelete  CleanupPolicy = "delete"
	CleanupCompact CleanupPolicy = "compact"

	KeyKindProbeID     KeyKind = "probe_id"
	KeyKindActionState KeyKind = "action_state"
	KeyKindUserID      KeyKind = "user_id"

	ProducerPlatformAPI    ProducerID = "platform_api"
	ProducerPlatformWorker ProducerID = "platform_worker"
	ProducerInteractionAPI ProducerID = "interaction_api"
	ProducerExposureWorker ProducerID = "exposure_worker"

	GroupBackboneProbeActive ConsumerGroupID = "backbone_probe_active"
	GroupBackboneProbeShadow ConsumerGroupID = "backbone_probe_shadow"
	GroupPersistActionActive ConsumerGroupID = "persist_action_active"
	GroupPersistActionShadow ConsumerGroupID = "persist_action_shadow"
	GroupConsumeViewActive   ConsumerGroupID = "consume_view_active"
	GroupConsumeViewShadow   ConsumerGroupID = "consume_view_shadow"

	ResponsibilityActionChanged     ResponsibilityID = "action_changed"
	ResponsibilityVideoPublished    ResponsibilityID = "video_published"
	ResponsibilityVideoEmbedding    ResponsibilityID = "video_embedding"
	ResponsibilityViewEventRecorded ResponsibilityID = "view_event_recorded"
	ResponsibilityMediaProcessing   ResponsibilityID = "media_processing"

	ProducerModeRabbit                ProducerMode = "rabbit"
	ProducerModeRabbitWithKafkaMirror ProducerMode = "rabbit_with_kafka_mirror"
	ProducerModeKafkaWithRabbitMirror ProducerMode = "kafka_with_rabbit_mirror"
	ProducerModeKafka                 ProducerMode = "kafka"

	ConsumerModeRabbit      ConsumerMode = "rabbit"
	ConsumerModeKafkaShadow ConsumerMode = "kafka_shadow"
	ConsumerModeKafka       ConsumerMode = "kafka"
)

var ErrUnknownRegistryValue = errors.New("unknown kafka registry value")

type TopicSpec struct {
	ID                 TopicID
	BaseName           string
	Version            int
	Class              TopicClass
	KeyKind            KeyKind
	LocalPartitions    int
	Retention          time.Duration
	CleanupPolicy      CleanupPolicy
	MaxRecordBytes     int
	AllowedProducers   []ProducerID
	AllowedGroups      []ConsumerGroupID
	ReplayAllowed      bool
	RetryTopicsAllowed bool
}

type ConsumerGroupSpec struct {
	ID       ConsumerGroupID
	BaseName string
	Topic    TopicID
	Shadow   bool
}

type MigrationSpec struct {
	Responsibility         ResponsibilityID
	DefaultProducer        ProducerMode
	DefaultConsumer        ConsumerMode
	KafkaProducerAvailable bool
	KafkaConsumerAvailable bool
}

var topics = [...]TopicSpec{
	{
		ID: TopicBackboneProbe, BaseName: "frux.platform.backbone_probe.v1",
		Version: 1, Class: TopicClassEvent, KeyKind: KeyKindProbeID,
		LocalPartitions: 3, Retention: time.Hour, CleanupPolicy: CleanupDelete,
		MaxRecordBytes:   900 << 10,
		AllowedProducers: []ProducerID{ProducerPlatformAPI, ProducerPlatformWorker},
		AllowedGroups:    []ConsumerGroupID{GroupBackboneProbeActive, GroupBackboneProbeShadow},
	},
	{
		ID: TopicActionChanged, BaseName: "frux.interaction.action-changed.v1",
		Version: 1, Class: TopicClassEvent, KeyKind: KeyKindActionState,
		LocalPartitions: 12, Retention: 7 * 24 * time.Hour, CleanupPolicy: CleanupDelete,
		MaxRecordBytes:   256 << 10,
		AllowedProducers: []ProducerID{ProducerInteractionAPI},
		AllowedGroups:    []ConsumerGroupID{GroupPersistActionActive, GroupPersistActionShadow},
	},
	{
		ID: TopicViewEventRecorded, BaseName: "frux.exposure.view-event-recorded.v1",
		Version: 1, Class: TopicClassEvent, KeyKind: KeyKindUserID,
		LocalPartitions: 12, Retention: 7 * 24 * time.Hour, CleanupPolicy: CleanupDelete,
		MaxRecordBytes:   256 << 10,
		AllowedProducers: []ProducerID{ProducerExposureWorker},
		AllowedGroups:    []ConsumerGroupID{GroupConsumeViewActive, GroupConsumeViewShadow},
	},
}

var consumerGroups = [...]ConsumerGroupSpec{
	{ID: GroupBackboneProbeActive, BaseName: "frux.platform.backbone_probe.active.v1", Topic: TopicBackboneProbe},
	{ID: GroupBackboneProbeShadow, BaseName: "frux.platform.backbone_probe.active.v1", Topic: TopicBackboneProbe, Shadow: true},
	{ID: GroupPersistActionActive, BaseName: "frux.interaction.persist-action.v1", Topic: TopicActionChanged},
	{ID: GroupPersistActionShadow, BaseName: "frux.interaction.persist-action.v1", Topic: TopicActionChanged, Shadow: true},
	{ID: GroupConsumeViewActive, BaseName: "frux.recommendation.consume-view.v1", Topic: TopicViewEventRecorded},
	{ID: GroupConsumeViewShadow, BaseName: "frux.recommendation.consume-view.v1", Topic: TopicViewEventRecorded, Shadow: true},
}

var migrations = [...]MigrationSpec{
	{
		Responsibility: ResponsibilityActionChanged, DefaultProducer: ProducerModeRabbit,
		DefaultConsumer: ConsumerModeRabbit, KafkaProducerAvailable: true, KafkaConsumerAvailable: true,
	},
	{Responsibility: ResponsibilityVideoPublished, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
	{Responsibility: ResponsibilityVideoEmbedding, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
	{
		Responsibility: ResponsibilityViewEventRecorded, DefaultProducer: ProducerModeRabbit,
		DefaultConsumer: ConsumerModeRabbit, KafkaProducerAvailable: true, KafkaConsumerAvailable: true,
	},
	{Responsibility: ResponsibilityMediaProcessing, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
}

var topicPrefixPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)

func Topics() []TopicSpec {
	return append([]TopicSpec(nil), topics[:]...)
}

func ConsumerGroups() []ConsumerGroupSpec {
	return append([]ConsumerGroupSpec(nil), consumerGroups[:]...)
}

func Migrations() []MigrationSpec {
	return append([]MigrationSpec(nil), migrations[:]...)
}

func Topic(id TopicID) (TopicSpec, error) {
	for _, spec := range topics {
		if spec.ID == id {
			return spec, nil
		}
	}
	return TopicSpec{}, fmt.Errorf("%w: topic %q", ErrUnknownRegistryValue, id)
}

func ConsumerGroup(id ConsumerGroupID) (ConsumerGroupSpec, error) {
	for _, spec := range consumerGroups {
		if spec.ID == id {
			return spec, nil
		}
	}
	return ConsumerGroupSpec{}, fmt.Errorf("%w: consumer group %q", ErrUnknownRegistryValue, id)
}

func TopicName(prefix string, id TopicID) (string, error) {
	spec, err := Topic(id)
	if err != nil {
		return "", err
	}
	prefix = strings.TrimSpace(strings.TrimSuffix(prefix, "."))
	if prefix == "" {
		return spec.BaseName, nil
	}
	if !topicPrefixPattern.MatchString(prefix) {
		return "", fmt.Errorf("%w: topic prefix", ErrUnknownRegistryValue)
	}
	return prefix + "." + spec.BaseName, nil
}

func GroupName(prefix string, id ConsumerGroupID) (string, error) {
	spec, err := ConsumerGroup(id)
	if err != nil {
		return "", err
	}
	prefix = strings.TrimSpace(strings.TrimSuffix(prefix, "."))
	if prefix == "" {
		return spec.BaseName, nil
	}
	if !topicPrefixPattern.MatchString(prefix) {
		return "", fmt.Errorf("%w: group prefix", ErrUnknownRegistryValue)
	}
	return prefix + "." + spec.BaseName, nil
}

func ResolvedGroupName(prefix, shadowDeployment string, id ConsumerGroupID) (string, error) {
	spec, err := ConsumerGroup(id)
	if err != nil {
		return "", err
	}
	name, err := GroupName(prefix, id)
	if err != nil {
		return "", err
	}
	if !spec.Shadow {
		return name, nil
	}
	shadowDeployment = strings.TrimSpace(shadowDeployment)
	if !topicPrefixPattern.MatchString(shadowDeployment) {
		return "", fmt.Errorf("%w: shadow deployment", ErrUnknownRegistryValue)
	}
	return name + ".shadow." + shadowDeployment, nil
}

func ValidProducerMode(mode ProducerMode) bool {
	switch mode {
	case ProducerModeRabbit, ProducerModeRabbitWithKafkaMirror,
		ProducerModeKafkaWithRabbitMirror, ProducerModeKafka:
		return true
	default:
		return false
	}
}

func ValidConsumerMode(mode ConsumerMode) bool {
	switch mode {
	case ConsumerModeRabbit, ConsumerModeKafkaShadow, ConsumerModeKafka:
		return true
	default:
		return false
	}
}
