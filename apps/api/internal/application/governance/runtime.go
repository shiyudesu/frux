package applicationgovernance

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
)

var ErrInvalidSnapshot = errors.New("invalid degradation control snapshot")

type SnapshotSource interface {
	ListActive(ctx context.Context) ([]*domaingovernance.Revision, error)
}

type RuntimeObserver interface {
	ObservePoll(process domaingovernance.Process, result string)
	ObserveApplied(process domaingovernance.Process, key domaingovernance.Key, revision int64)
	ObserveSnapshotAge(process domaingovernance.Process, key domaingovernance.Key, age time.Duration)
	ObserveInvalid(process domaingovernance.Process, reason string)
	ObserveFallback(process domaingovernance.Process, key domaingovernance.Key, reason string)
}

type snapshot struct {
	loadedAt time.Time
	values   map[domaingovernance.Key]*domaingovernance.Revision
}

type Runtime struct {
	registry *domaingovernance.Registry
	process  domaingovernance.Process
	source   SnapshotSource
	observer RuntimeObserver
	now      func() time.Time
	current  atomic.Pointer[snapshot]
}

type RuntimeOption func(*Runtime)

func NewRuntime(
	registry *domaingovernance.Registry,
	process domaingovernance.Process,
	source SnapshotSource,
	options ...RuntimeOption,
) *Runtime {
	runtime := &Runtime{
		registry: registry, process: process, source: source,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

func WithRuntimeClock(now func() time.Time) RuntimeOption {
	return func(runtime *Runtime) {
		if now != nil {
			runtime.now = now
		}
	}
}

func WithRuntimeObserver(observer RuntimeObserver) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.observer = observer
	}
}

func (r *Runtime) Refresh(ctx context.Context) error {
	if r == nil || r.registry == nil || r.source == nil {
		r.observePoll("failure")
		return ErrInvalidSnapshot
	}
	revisions, err := r.source.ListActive(ctx)
	if err != nil {
		if errors.Is(err, domaingovernance.ErrUnknownControl) ||
			errors.Is(err, domaingovernance.ErrInvalidControlValue) ||
			errors.Is(err, domaingovernance.ErrInvalidRevision) ||
			errors.Is(err, domaingovernance.ErrInvalidActorID) ||
			errors.Is(err, domaingovernance.ErrInvalidReason) ||
			errors.Is(err, domaingovernance.ErrReasonTooLong) ||
			errors.Is(err, domaingovernance.ErrInvalidExpiry) ||
			errors.Is(err, domaingovernance.ErrInvalidCreatedAt) {
			r.observeInvalid("invalid_source")
			r.observePoll("invalid")
		} else {
			r.observePoll("failure")
		}
		r.observeSnapshotAges()
		return err
	}
	values := make(map[domaingovernance.Key]*domaingovernance.Revision, len(revisions))
	for _, revision := range revisions {
		if revision == nil {
			r.observeInvalid("nil_revision")
			r.observePoll("invalid")
			return ErrInvalidSnapshot
		}
		definition, err := r.registry.Require(revision.Key())
		if err != nil {
			r.observeInvalid("unknown_key")
			r.observePoll("invalid")
			return ErrInvalidSnapshot
		}
		if err := revision.Value().Validate(definition.ValueType); err != nil {
			r.observeInvalid("invalid_value")
			r.observePoll("invalid")
			return ErrInvalidSnapshot
		}
		if _, exists := values[revision.Key()]; exists {
			r.observeInvalid("duplicate_key")
			r.observePoll("invalid")
			return ErrInvalidSnapshot
		}
		values[revision.Key()] = revision
	}
	loadedAt := r.now().UTC()
	r.current.Store(&snapshot{loadedAt: loadedAt, values: values})
	r.observePoll("success")
	for _, definition := range r.registry.Definitions() {
		if !definition.Supports(r.process) {
			continue
		}
		revision := values[definition.Key]
		applied := int64(0)
		if revision != nil {
			applied = revision.Number()
		}
		if r.observer != nil {
			r.observer.ObserveApplied(r.process, definition.Key, applied)
			r.observer.ObserveSnapshotAge(r.process, definition.Key, 0)
		}
	}
	return nil
}

func (r *Runtime) Bool(key domaingovernance.Key) bool {
	definition, ok := r.registry.Definition(key)
	if !ok {
		r.observeInvalid("unknown_key")
		return false
	}
	failureDefault, _ := definition.FailureDefault.Boolean()
	if !definition.Supports(r.process) {
		r.observeInvalid("unsupported_process")
		r.observeFallback(key, "unsupported_process")
		return failureDefault
	}
	current := r.current.Load()
	if current == nil {
		r.observeFallback(key, "not_loaded")
		return failureDefault
	}
	now := r.now().UTC()
	age := now.Sub(current.loadedAt)
	if age < 0 {
		age = 0
	}
	if r.observer != nil {
		r.observer.ObserveSnapshotAge(r.process, key, age)
	}
	if age > definition.MaxStaleness {
		r.observeFallback(key, "stale")
		return failureDefault
	}
	revision := current.values[key]
	if revision == nil || revision.Expired(now) {
		value, _ := definition.Default.Boolean()
		if revision == nil {
			r.observeFallback(key, "missing")
		} else {
			r.observeFallback(key, "expired")
		}
		return value
	}
	value, ok := revision.Value().Boolean()
	if !ok {
		r.observeInvalid("invalid_value")
		r.observeFallback(key, "invalid")
		return failureDefault
	}
	return value
}

func (r *Runtime) Run(ctx context.Context, interval, timeout time.Duration) error {
	if interval <= 0 {
		return domaingovernance.ErrInvalidPollInterval
	}
	if timeout <= 0 || timeout > interval {
		return domaingovernance.ErrInvalidPollTimeout
	}
	r.refreshWithTimeout(ctx, timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.refreshWithTimeout(ctx, timeout)
		}
	}
}

func (r *Runtime) refreshWithTimeout(parent context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	_ = r.Refresh(ctx)
}

func (r *Runtime) observePoll(result string) {
	if r.observer != nil {
		r.observer.ObservePoll(r.process, result)
	}
}

func (r *Runtime) observeInvalid(reason string) {
	if r.observer != nil {
		r.observer.ObserveInvalid(r.process, reason)
	}
}

func (r *Runtime) observeFallback(key domaingovernance.Key, reason string) {
	if r.observer != nil {
		r.observer.ObserveFallback(r.process, key, reason)
	}
}

func (r *Runtime) observeSnapshotAges() {
	current := r.current.Load()
	if current == nil || r.observer == nil {
		return
	}
	age := r.now().UTC().Sub(current.loadedAt)
	if age < 0 {
		age = 0
	}
	for _, definition := range r.registry.Definitions() {
		if definition.Supports(r.process) {
			r.observer.ObserveSnapshotAge(r.process, definition.Key, age)
		}
	}
}
