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
	TopicBackboneProbe TopicID = "backbone_probe"

	TopicClassEvent   TopicClass = "event"
	TopicClassCommand TopicClass = "command"

	CleanupDelete  CleanupPolicy = "delete"
	CleanupCompact CleanupPolicy = "compact"

	KeyKindProbeID KeyKind = "probe_id"

	ProducerPlatformAPI    ProducerID = "platform_api"
	ProducerPlatformWorker ProducerID = "platform_worker"

	GroupBackboneProbeActive ConsumerGroupID = "backbone_probe_active"
	GroupBackboneProbeShadow ConsumerGroupID = "backbone_probe_shadow"

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
}

var consumerGroups = [...]ConsumerGroupSpec{
	{ID: GroupBackboneProbeActive, BaseName: "frux.platform.backbone_probe.active.v1", Topic: TopicBackboneProbe},
	{ID: GroupBackboneProbeShadow, BaseName: "frux.platform.backbone_probe.shadow.v1", Topic: TopicBackboneProbe, Shadow: true},
}

var migrations = [...]MigrationSpec{
	{Responsibility: ResponsibilityActionChanged, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
	{Responsibility: ResponsibilityVideoPublished, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
	{Responsibility: ResponsibilityVideoEmbedding, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
	{Responsibility: ResponsibilityViewEventRecorded, DefaultProducer: ProducerModeRabbit, DefaultConsumer: ConsumerModeRabbit},
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
