package domaingovernance

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Key string

const (
	FeedPreloadEnabled          Key = "feed.preload.enabled"
	RateLimitDistributedEnabled Key = "rate_limit.distributed.enabled"
	RateLimitEmergencyEnabled   Key = "rate_limit.emergency.enabled"
)

type Process string

const (
	ProcessAPI    Process = "api"
	ProcessWorker Process = "worker"
)

type ValueType string

const ValueTypeBoolean ValueType = "boolean"

type Value struct {
	valueType ValueType
	boolean   bool
}

func BooleanValue(value bool) Value {
	return Value{valueType: ValueTypeBoolean, boolean: value}
}

func RestoreValue(valueType ValueType, boolean bool) (Value, error) {
	if valueType != ValueTypeBoolean {
		return Value{}, ErrInvalidControlValue
	}
	return BooleanValue(boolean), nil
}

func (v Value) Type() ValueType { return v.valueType }

func (v Value) Boolean() (bool, bool) {
	return v.boolean, v.valueType == ValueTypeBoolean
}

func (v Value) Validate(expected ValueType) error {
	if v.valueType != expected || expected != ValueTypeBoolean {
		return ErrInvalidControlValue
	}
	return nil
}

type Definition struct {
	Key            Key
	Owner          string
	Description    string
	ValueType      ValueType
	Default        Value
	FailureDefault Value
	Processes      []Process
	MaxStaleness   time.Duration
}

func (d Definition) Supports(process Process) bool {
	for _, candidate := range d.Processes {
		if candidate == process {
			return true
		}
	}
	return false
}

type Registry struct {
	definitions map[Key]Definition
	keys        []Key
}

func DefaultRegistry() *Registry {
	registry, err := NewRegistry([]Definition{
		{
			Key:            FeedPreloadEnabled,
			Owner:          "feed",
			Description:    "Allow noncritical API preload suggestions and worker feed-cache preheating.",
			ValueType:      ValueTypeBoolean,
			Default:        BooleanValue(true),
			FailureDefault: BooleanValue(false),
			Processes:      []Process{ProcessAPI, ProcessWorker},
			MaxStaleness:   2 * time.Minute,
		},
		{
			Key:            RateLimitDistributedEnabled,
			Owner:          "platform",
			Description:    "Enable Redis coordination for registered distributed rate-limit policies.",
			ValueType:      ValueTypeBoolean,
			Default:        BooleanValue(true),
			FailureDefault: BooleanValue(false),
			Processes:      []Process{ProcessAPI},
			MaxStaleness:   2 * time.Minute,
		},
		{
			Key:            RateLimitEmergencyEnabled,
			Owner:          "platform",
			Description:    "Apply code-registered emergency rate-limit profiles.",
			ValueType:      ValueTypeBoolean,
			Default:        BooleanValue(false),
			FailureDefault: BooleanValue(false),
			Processes:      []Process{ProcessAPI},
			MaxStaleness:   2 * time.Minute,
		},
	})
	if err != nil {
		panic(err)
	}
	return registry
}

func NewRegistry(definitions []Definition) (*Registry, error) {
	registry := &Registry{
		definitions: make(map[Key]Definition, len(definitions)),
		keys:        make([]Key, 0, len(definitions)),
	}
	for _, definition := range definitions {
		definition.Key = Key(strings.TrimSpace(string(definition.Key)))
		definition.Owner = strings.TrimSpace(definition.Owner)
		definition.Description = strings.TrimSpace(definition.Description)
		if definition.Key == "" || definition.Owner == "" || definition.Description == "" ||
			definition.MaxStaleness <= 0 || len(definition.Processes) == 0 {
			return nil, ErrInvalidControlValue
		}
		if _, exists := registry.definitions[definition.Key]; exists {
			return nil, fmt.Errorf("%w: duplicate key %s", ErrInvalidControlValue, definition.Key)
		}
		if err := definition.Default.Validate(definition.ValueType); err != nil {
			return nil, err
		}
		if err := definition.FailureDefault.Validate(definition.ValueType); err != nil {
			return nil, err
		}
		processes := make([]Process, 0, len(definition.Processes))
		seen := make(map[Process]struct{}, len(definition.Processes))
		for _, process := range definition.Processes {
			if process != ProcessAPI && process != ProcessWorker {
				return nil, ErrUnsupportedProcess
			}
			if _, exists := seen[process]; exists {
				continue
			}
			seen[process] = struct{}{}
			processes = append(processes, process)
		}
		definition.Processes = processes
		registry.definitions[definition.Key] = cloneDefinition(definition)
		registry.keys = append(registry.keys, definition.Key)
	}
	sort.Slice(registry.keys, func(i, j int) bool { return registry.keys[i] < registry.keys[j] })
	return registry, nil
}

func (r *Registry) Definition(key Key) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	definition, ok := r.definitions[Key(strings.TrimSpace(string(key)))]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(definition), true
}

func (r *Registry) Require(key Key) (Definition, error) {
	definition, ok := r.Definition(key)
	if !ok {
		return Definition{}, ErrUnknownControl
	}
	return definition, nil
}

func (r *Registry) Definitions() []Definition {
	if r == nil {
		return nil
	}
	result := make([]Definition, 0, len(r.keys))
	for _, key := range r.keys {
		result = append(result, cloneDefinition(r.definitions[key]))
	}
	return result
}

func cloneDefinition(definition Definition) Definition {
	definition.Processes = append([]Process(nil), definition.Processes...)
	return definition
}
