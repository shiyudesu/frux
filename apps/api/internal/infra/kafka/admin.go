package infrakafka

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

var (
	ErrTopicTopologyInvalid = errors.New("kafka topic topology invalid")
	ErrTopicProvisionFailed = errors.New("kafka topic provision failed")
)

type TopicState struct {
	Name              string
	Partitions        int
	ReplicationFactor int
	MinInSyncReplicas int
	Retention         time.Duration
	CleanupPolicy     CleanupPolicy
	MaxRecordBytes    int
}

type TopicValidation struct {
	Topic  TopicID
	Result string
}

type adminBackend interface {
	TopicStates(ctx context.Context, names []string) (map[string]TopicState, error)
	CreateTopic(ctx context.Context, state TopicState) error
}

type franzAdminBackend struct {
	client *kadm.Client
}

type Administrator struct {
	backend           adminBackend
	prefix            string
	environment       string
	localProvisioning bool
	replicationFactor int
	minInSyncReplicas int
	timeout           time.Duration
	observer          TopologyObserver
}

type TopologyObserver interface {
	ObserveTopology(topic TopicID, result string)
}

func NewAdministrator(client *Client, cfg infraconfig.KafkaConfig, observer TopologyObserver) *Administrator {
	return &Administrator{
		backend: &franzAdminBackend{client: kadm.NewClient(client.kgoClient)},
		prefix:  cfg.TopicPrefix, environment: cfg.Environment,
		localProvisioning: cfg.AllowLocalProvisioning,
		replicationFactor: cfg.ProductionValidation.ReplicationFactor,
		minInSyncReplicas: cfg.ProductionValidation.MinInSyncReplicas,
		timeout:           client.adminTimeout, observer: observer,
	}
}

func (a *Administrator) EnsureTopics(ctx context.Context) ([]TopicValidation, error) {
	if a == nil || a.backend == nil {
		return nil, ErrKafkaUnavailable
	}
	names := make([]string, 0, len(topics))
	specByName := make(map[string]TopicSpec, len(topics))
	for _, spec := range topics {
		name, err := TopicName(a.prefix, spec.ID)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		specByName[name] = spec
	}
	adminContext, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	states, err := a.backend.TopicStates(adminContext, names)
	if err != nil {
		a.observeAll("broker_error")
		return nil, fmt.Errorf("%w: inspect topics", ErrTopicTopologyInvalid)
	}
	local := a.environment == "local" || a.environment == "test"
	results := make([]TopicValidation, 0, len(names))
	for _, name := range names {
		spec := specByName[name]
		state, exists := states[name]
		if !exists {
			if !local || !a.localProvisioning {
				a.observe(spec.ID, "missing")
				return results, fmt.Errorf("%w: registered topic is missing", ErrTopicTopologyInvalid)
			}
			desired := desiredTopicState(name, spec, a.replicationFactor, a.minInSyncReplicas)
			if err := a.backend.CreateTopic(adminContext, desired); err != nil {
				a.observe(spec.ID, "provision_failed")
				return results, fmt.Errorf("%w: registered topic", ErrTopicProvisionFailed)
			}
			a.observe(spec.ID, "provisioned")
			results = append(results, TopicValidation{Topic: spec.ID, Result: "provisioned"})
			continue
		}
		if err := validateTopicState(state, spec, local, a.replicationFactor, a.minInSyncReplicas); err != nil {
			a.observe(spec.ID, "invalid")
			return results, err
		}
		a.observe(spec.ID, "valid")
		results = append(results, TopicValidation{Topic: spec.ID, Result: "valid"})
	}
	return results, nil
}

func desiredTopicState(
	name string,
	spec TopicSpec,
	replicationFactor int,
	minInSyncReplicas int,
) TopicState {
	return TopicState{
		Name: name, Partitions: spec.LocalPartitions,
		ReplicationFactor: replicationFactor,
		MinInSyncReplicas: minInSyncReplicas,
		Retention:         spec.Retention, CleanupPolicy: spec.CleanupPolicy,
		MaxRecordBytes: brokerMaxMessageBytes(spec),
	}
}

func validateTopicState(
	state TopicState,
	spec TopicSpec,
	local bool,
	requiredReplication int,
	requiredISR int,
) error {
	partitionValid := state.Partitions >= spec.LocalPartitions
	if local {
		partitionValid = state.Partitions == spec.LocalPartitions
	}
	if !partitionValid || state.ReplicationFactor < requiredReplication ||
		state.MinInSyncReplicas < requiredISR ||
		state.MinInSyncReplicas > state.ReplicationFactor ||
		state.CleanupPolicy != spec.CleanupPolicy ||
		state.Retention != spec.Retention ||
		state.MaxRecordBytes < brokerMaxMessageBytes(spec) {
		return fmt.Errorf("%w: registered policy mismatch", ErrTopicTopologyInvalid)
	}
	return nil
}

func brokerMaxMessageBytes(spec TopicSpec) int {
	return spec.MaxRecordBytes + 64<<10
}

func (a *Administrator) observe(topic TopicID, result string) {
	if a.observer != nil {
		a.observer.ObserveTopology(topic, result)
	}
}

func (a *Administrator) observeAll(result string) {
	for _, spec := range topics {
		a.observe(spec.ID, result)
	}
}

func (b *franzAdminBackend) TopicStates(
	ctx context.Context,
	names []string,
) (map[string]TopicState, error) {
	details, err := b.client.ListTopics(ctx, names...)
	if err != nil {
		return nil, err
	}
	configs, err := b.client.DescribeTopicConfigs(ctx, names...)
	if err != nil {
		return nil, err
	}
	configByName := make(map[string]map[string]string, len(configs))
	for _, resource := range configs {
		if resource.Err != nil {
			continue
		}
		values := make(map[string]string, len(resource.Configs))
		for _, config := range resource.Configs {
			values[config.Key] = config.MaybeValue()
		}
		configByName[resource.Name] = values
	}
	states := make(map[string]TopicState, len(names))
	for _, name := range names {
		detail, exists := details[name]
		if !exists || detail.Err != nil || len(detail.Partitions) == 0 {
			continue
		}
		replication := 0
		for _, partition := range detail.Partitions {
			if partition.Err != nil {
				return nil, partition.Err
			}
			if replication == 0 || len(partition.Replicas) < replication {
				replication = len(partition.Replicas)
			}
		}
		values := configByName[name]
		retentionMillis, err := strconv.ParseInt(values["retention.ms"], 10, 64)
		if err != nil {
			return nil, err
		}
		minISR, err := strconv.Atoi(values["min.insync.replicas"])
		if err != nil {
			return nil, err
		}
		maxBytes, err := strconv.Atoi(values["max.message.bytes"])
		if err != nil {
			return nil, err
		}
		states[name] = TopicState{
			Name: name, Partitions: len(detail.Partitions),
			ReplicationFactor: replication, MinInSyncReplicas: minISR,
			Retention:      time.Duration(retentionMillis) * time.Millisecond,
			CleanupPolicy:  CleanupPolicy(values["cleanup.policy"]),
			MaxRecordBytes: maxBytes,
		}
	}
	return states, nil
}

func (b *franzAdminBackend) CreateTopic(ctx context.Context, state TopicState) error {
	retention := strconv.FormatInt(state.Retention.Milliseconds(), 10)
	cleanup := string(state.CleanupPolicy)
	maxBytes := strconv.Itoa(state.MaxRecordBytes)
	minISR := strconv.Itoa(state.MinInSyncReplicas)
	responses, err := b.client.CreateTopics(
		ctx, int32(state.Partitions), int16(state.ReplicationFactor),
		map[string]*string{
			"retention.ms": &retention, "cleanup.policy": &cleanup,
			"max.message.bytes": &maxBytes, "min.insync.replicas": &minISR,
		},
		state.Name,
	)
	if err != nil {
		return err
	}
	response, exists := responses[state.Name]
	if !exists {
		return ErrTopicProvisionFailed
	}
	if response.Err != nil && !errors.Is(response.Err, kerr.TopicAlreadyExists) {
		return response.Err
	}
	return nil
}
