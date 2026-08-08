package infrakafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeAdminBackend struct {
	states  map[string]TopicState
	created []TopicState
	err     error
}

func (f *fakeAdminBackend) TopicStates(context.Context, []string) (map[string]TopicState, error) {
	return f.states, f.err
}

func (f *fakeAdminBackend) CreateTopic(_ context.Context, state TopicState) error {
	f.created = append(f.created, state)
	return f.err
}

func TestAdministratorProvisionsOnlyLocalTopics(t *testing.T) {
	backend := &fakeAdminBackend{states: map[string]TopicState{}}
	admin := &Administrator{
		backend: backend, environment: "local", localProvisioning: true,
		replicationFactor: 1, minInSyncReplicas: 1, timeout: time.Second,
	}
	results, err := admin.EnsureTopics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(backend.created) != 1 ||
		backend.created[0].Name != "frux.platform.backbone_probe.v1" {
		t.Fatalf("results=%+v created=%+v", results, backend.created)
	}
}

func TestAdministratorRejectsProductionMutationAndUnsafeTopology(t *testing.T) {
	admin := &Administrator{
		backend:     &fakeAdminBackend{states: map[string]TopicState{}},
		environment: "production", replicationFactor: 3, minInSyncReplicas: 2,
		timeout: time.Second,
	}
	if _, err := admin.EnsureTopics(context.Background()); !errors.Is(err, ErrTopicTopologyInvalid) {
		t.Fatalf("missing production topic error = %v", err)
	}
	spec, _ := Topic(TopicBackboneProbe)
	name, _ := TopicName("", spec.ID)
	admin.backend = &fakeAdminBackend{states: map[string]TopicState{
		name: {
			Name: name, Partitions: 3, ReplicationFactor: 1, MinInSyncReplicas: 1,
			Retention: spec.Retention, CleanupPolicy: spec.CleanupPolicy,
			MaxRecordBytes: spec.MaxRecordBytes,
		},
	}}
	if _, err := admin.EnsureTopics(context.Background()); !errors.Is(err, ErrTopicTopologyInvalid) {
		t.Fatalf("unsafe topology error = %v", err)
	}
}

func TestValidateTopicStateRejectsImpossibleMinimumISR(t *testing.T) {
	spec, err := Topic(TopicBackboneProbe)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredTopicState("frux.platform.backbone_probe.v1", spec, 1, 1)
	state.MinInSyncReplicas = 2
	if err := validateTopicState(state, spec, true, 1, 1); !errors.Is(err, ErrTopicTopologyInvalid) {
		t.Fatalf("error = %v, want ErrTopicTopologyInvalid", err)
	}
}
