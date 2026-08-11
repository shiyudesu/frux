package applicationkafkafailure

import (
	"context"
	"time"

	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
)

const (
	defaultCollectionInterval = 15 * time.Second
	defaultCollectionTimeout  = 5 * time.Second
	minCollectionInterval     = 10 * time.Millisecond
	maxCollectionInterval     = 5 * time.Minute
	minCollectionTimeout      = 10 * time.Millisecond
	maxCollectionTimeout      = 30 * time.Second
)

type SummaryLister interface {
	List(ctx context.Context) ([]domainkafkafailure.TopicSummary, error)
}

type CollectionObserver interface {
	ObserveCollection(result string)
	ObserveTopicSummary(summary domainkafkafailure.TopicSummary)
}

type Collector struct {
	lister   SummaryLister
	observer CollectionObserver
	interval time.Duration
	timeout  time.Duration
	onError  func(error)
}

type CollectorOption func(*Collector)

func NewCollector(
	lister SummaryLister,
	observer CollectionObserver,
	interval time.Duration,
	timeout time.Duration,
	options ...CollectorOption,
) *Collector {
	collector := &Collector{
		lister: lister, observer: observer,
		interval: boundedDuration(
			interval, defaultCollectionInterval,
			minCollectionInterval, maxCollectionInterval,
		),
		timeout: boundedDuration(
			timeout, defaultCollectionTimeout,
			minCollectionTimeout, maxCollectionTimeout,
		),
	}
	for _, option := range options {
		if option != nil {
			option(collector)
		}
	}
	return collector
}

func WithCollectionErrorHandler(handler func(error)) CollectorOption {
	return func(collector *Collector) {
		collector.onError = handler
	}
}

func (c *Collector) Run(ctx context.Context) {
	if c == nil || c.lister == nil {
		return
	}
	c.collect(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	collectionContext, cancel := context.WithTimeout(ctx, c.timeout)
	summaries, err := c.lister.List(collectionContext)
	cancel()
	if err != nil {
		if c.observer != nil {
			c.observer.ObserveCollection("failed")
		}
		if c.onError != nil && ctx.Err() == nil {
			c.onError(err)
		}
		return
	}
	if c.observer != nil {
		for _, summary := range summaries {
			c.observer.ObserveTopicSummary(summary)
		}
		c.observer.ObserveCollection("succeeded")
	}
}

func boundedDuration(
	value time.Duration,
	fallback time.Duration,
	minimum time.Duration,
	maximum time.Duration,
) time.Duration {
	if value <= 0 {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
