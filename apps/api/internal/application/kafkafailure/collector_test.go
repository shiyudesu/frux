package applicationkafkafailure

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
)

type collectorLister struct {
	mu      sync.Mutex
	calls   int
	err     error
	block   bool
	called  chan struct{}
	stopped chan struct{}
	items   []domainkafkafailure.TopicSummary
}

func (l *collectorLister) List(
	ctx context.Context,
) ([]domainkafkafailure.TopicSummary, error) {
	l.mu.Lock()
	l.calls++
	called := l.called
	block := l.block
	err := l.err
	items := append([]domainkafkafailure.TopicSummary(nil), l.items...)
	l.mu.Unlock()
	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}
	if block {
		<-ctx.Done()
		if l.stopped != nil {
			select {
			case l.stopped <- struct{}{}:
			default:
			}
		}
		return nil, ctx.Err()
	}
	return items, err
}

type collectorObserver struct {
	mu      sync.Mutex
	results []string
	topics  []string
}

func (o *collectorObserver) ObserveCollection(result string) {
	o.mu.Lock()
	o.results = append(o.results, result)
	o.mu.Unlock()
}

func (o *collectorObserver) ObserveTopicSummary(
	summary domainkafkafailure.TopicSummary,
) {
	o.mu.Lock()
	o.topics = append(o.topics, summary.Topic)
	o.mu.Unlock()
}

func TestCollectorRunsImmediatelyRepeatsAndStops(t *testing.T) {
	lister := &collectorLister{
		called: make(chan struct{}, 4),
		items: []domainkafkafailure.TopicSummary{{
			Topic: "frux.feed.video-published.dlq.v1",
		}},
	}
	observer := &collectorObserver{}
	collector := NewCollector(lister, observer, 10*time.Millisecond, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		collector.Run(ctx)
		close(done)
	}()
	for range 2 {
		select {
		case <-lister.called:
		case <-time.After(time.Second):
			t.Fatal("collector did not run")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
	lister.mu.Lock()
	calls := lister.calls
	lister.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	lister.mu.Lock()
	defer lister.mu.Unlock()
	if lister.calls != calls {
		t.Fatalf("collector ran after shutdown: before=%d after=%d", calls, lister.calls)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.topics) < 2 {
		t.Fatalf("summary refreshes=%v", observer.topics)
	}
}

func TestCollectorBoundsTimeoutAndReportsErrors(t *testing.T) {
	lister := &collectorLister{
		block: true, called: make(chan struct{}, 1), stopped: make(chan struct{}, 1),
	}
	observer := &collectorObserver{}
	errorsSeen := make(chan error, 1)
	collector := NewCollector(
		lister,
		observer,
		time.Hour,
		time.Nanosecond,
		WithCollectionErrorHandler(func(err error) {
			errorsSeen <- err
		}),
	)
	if collector.interval != maxCollectionInterval ||
		collector.timeout != minCollectionTimeout {
		t.Fatalf(
			"interval=%s timeout=%s",
			collector.interval, collector.timeout,
		)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		collector.Run(ctx)
		close(done)
	}()
	select {
	case <-lister.stopped:
	case <-time.After(time.Second):
		t.Fatal("collection timeout did not cancel the broker call")
	}
	select {
	case err := <-errorsSeen:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collection error was not reported")
	}
	cancel()
	<-done
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.results) != 1 || observer.results[0] != "failed" {
		t.Fatalf("results=%v", observer.results)
	}
}
