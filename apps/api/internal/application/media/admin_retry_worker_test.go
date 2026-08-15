package applicationmedia

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestProcessingRetryNotificationWorkerDeliversProjection(t *testing.T) {
	now := time.Now().UTC()
	repository := &retryNotificationRepositoryStub{
		items: []*domainmedia.RetryNotificationOutboxItem{{
			EventID: "event-1", AssetID: 11, Attempts: 1,
		}},
	}
	notifier := &retryNotifierStub{}
	worker := NewProcessingRetryNotificationWorker(repository, notifier)
	worker.now = func() time.Time { return now }
	processed, err := worker.DispatchOnce(context.Background())
	if err != nil || processed != 1 || notifier.assetID != 11 ||
		repository.delivered != "event-1" {
		t.Fatalf(
			"processed=%d err=%v notifier=%+v repository=%+v",
			processed, err, notifier, repository,
		)
	}
}

func TestProcessingRetryNotificationWorkerRetainsRetryableFailure(t *testing.T) {
	repository := &retryNotificationRepositoryStub{
		items: []*domainmedia.RetryNotificationOutboxItem{{
			EventID: "event-2", AssetID: 12, Attempts: 2,
		}},
	}
	notifier := &retryNotifierStub{err: errors.New("projection unavailable")}
	worker := NewProcessingRetryNotificationWorker(repository, notifier)
	processed, err := worker.DispatchOnce(context.Background())
	if err == nil || processed != 1 || repository.failed != "event-2" ||
		repository.terminal {
		t.Fatalf(
			"processed=%d err=%v repository=%+v", processed, err, repository,
		)
	}
}

type retryNotificationRepositoryStub struct {
	items     []*domainmedia.RetryNotificationOutboxItem
	delivered string
	failed    string
	terminal  bool
}

func (s *retryNotificationRepositoryStub) ClaimProcessingRetryNotifications(
	context.Context, string, int, time.Time, time.Time,
) ([]*domainmedia.RetryNotificationOutboxItem, error) {
	if len(s.items) == 0 {
		return nil, nil
	}
	item := s.items[0]
	s.items = s.items[1:]
	return []*domainmedia.RetryNotificationOutboxItem{item}, nil
}

func (s *retryNotificationRepositoryStub) MarkProcessingRetryNotificationDelivered(
	_ context.Context,
	eventID, _ string,
	_ time.Time,
) error {
	s.delivered = eventID
	return nil
}

func (s *retryNotificationRepositoryStub) MarkProcessingRetryNotificationFailed(
	_ context.Context,
	eventID, _ string,
	_ time.Time,
	_ string,
	terminal bool,
) error {
	s.failed, s.terminal = eventID, terminal
	return nil
}

func (s *retryNotificationRepositoryStub) CountPendingProcessingRetryNotifications(
	context.Context,
) (int64, error) {
	return int64(len(s.items)), nil
}

type retryNotifierStub struct {
	assetID int64
	err     error
}

func (*retryNotifierStub) MediaReady(context.Context, int64) error {
	return nil
}

func (s *retryNotifierStub) MediaRepairing(
	_ context.Context,
	assetID int64,
	_ string,
) error {
	s.assetID = assetID
	return s.err
}

func (*retryNotifierStub) MediaFailed(context.Context, int64, string, string) error {
	return nil
}
