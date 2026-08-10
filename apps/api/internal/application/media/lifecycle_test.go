package applicationmedia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

type lifecycleRepositoryStub struct {
	task      *domainmedia.VideoLifecycleTask
	available time.Time
	leased    bool
}

func (r *lifecycleRepositoryStub) ClaimVideoLifecycleTasks(
	_ context.Context,
	owner string,
	now, leaseUntil time.Time,
	_ int,
) ([]*domainmedia.VideoLifecycleTask, error) {
	if r.task == nil || r.leased ||
		(r.task.State != domainmedia.JobStatePending &&
			r.task.State != domainmedia.JobStateRetryable) ||
		now.Before(r.available) {
		return nil, nil
	}
	r.leased = true
	r.task.State = domainmedia.JobStateProcessing
	r.task.Attempts++
	r.task.LeaseOwner = owner
	r.task.LeaseUntil = &leaseUntil
	copyTask := *r.task
	return []*domainmedia.VideoLifecycleTask{&copyTask}, nil
}

func (r *lifecycleRepositoryStub) UpdateVideoLifecycleTaskOwned(
	_ context.Context,
	task *domainmedia.VideoLifecycleTask,
	_ string,
) error {
	r.leased = false
	r.task.State = task.State
	r.task.Attempts = task.Attempts
	r.task.ErrorCode = task.ErrorCode
	r.task.NextAttemptAt = task.NextAttemptAt
	r.task.CompletedAt = task.CompletedAt
	r.available = task.NextAttemptAt
	return nil
}

func (r *lifecycleRepositoryStub) VideoLifecycleBacklog(
	context.Context,
) (int64, *time.Time, error) {
	if r.task == nil || r.task.State == domainmedia.JobStateCompleted ||
		r.task.State == domainmedia.JobStateFailed {
		return 0, nil, nil
	}
	oldest := r.task.CreatedAt
	return 1, &oldest, nil
}

type lifecycleVideoReaderStub struct {
	state VideoLifecycleState
	err   error
}

func (r lifecycleVideoReaderStub) ReadVideoLifecycleState(
	context.Context,
	int64,
) (VideoLifecycleState, error) {
	return r.state, r.err
}

type lifecycleProtectorStub struct {
	calls        int
	restoreCalls int
	err          error
	restoreErr   error
}

func (s *lifecycleProtectorStub) ProtectVideo(
	context.Context,
	int64,
	int64,
	int64,
) error {
	s.calls++
	return s.err
}

func (s *lifecycleProtectorStub) RestoreVideo(
	context.Context,
	int64,
	int64,
	int64,
) error {
	s.restoreCalls++
	return s.restoreErr
}

type lifecycleCleanupStub struct {
	calls int
	err   error
}

func (s *lifecycleCleanupStub) ScheduleMediaCleanup(
	context.Context,
	int64,
	int64,
) error {
	s.calls++
	return s.err
}

func TestVideoLifecyclePrivateAndDeleteIntentsCompleteIdempotently(t *testing.T) {
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		action       string
		required     VideoLifecycleState
		cleanupCalls int
	}{
		{
			name: "private", action: domainmedia.LifecycleActionProtect,
			required: VideoLifecycleState{Exists: true, Status: 3, Visibility: "private"},
		},
		{
			name: "delete", action: domainmedia.LifecycleActionDelete,
			required:     VideoLifecycleState{Exists: true, Status: 5, Visibility: "public"},
			cleanupCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := &domainmedia.VideoLifecycleTask{
				ID: 1, VideoID: 7, MediaAssetID: 11, CoverAssetID: 12,
				Action: test.action, State: domainmedia.JobStatePending,
				MaxAttempts: 3, NextAttemptAt: now,
			}
			if test.action == domainmedia.LifecycleActionProtect {
				task.RequiredVisibility = "private"
			} else {
				task.RequiredStatus = 5
			}
			repo := &lifecycleRepositoryStub{task: task, available: now}
			protector := &lifecycleProtectorStub{}
			cleanup := &lifecycleCleanupStub{}
			service := NewVideoLifecycleService(
				repo, lifecycleVideoReaderStub{state: test.required},
				protector, cleanup,
			)
			service.now = func() time.Time { return now }
			completed, err := service.RunOnce(
				context.Background(), "worker", 10, time.Minute,
			)
			if err != nil || completed != 1 {
				t.Fatalf("completed=%d err=%v", completed, err)
			}
			if protector.calls != 1 || cleanup.calls != test.cleanupCalls ||
				task.State != domainmedia.JobStateCompleted {
				t.Fatalf(
					"protect=%d cleanup=%d task=%+v",
					protector.calls, cleanup.calls, task,
				)
			}
			completed, err = service.RunOnce(
				context.Background(), "worker", 10, time.Minute,
			)
			if err != nil || completed != 0 ||
				protector.calls != 1 || cleanup.calls != test.cleanupCalls {
				t.Fatalf("duplicate execution completed=%d err=%v", completed, err)
			}
		})
	}
}

func TestVideoLifecycleIntentRetriesAndSupersededPrivateDoesNotDemotePublic(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	task := &domainmedia.VideoLifecycleTask{
		ID: 2, VideoID: 8, MediaAssetID: 21,
		Action:             domainmedia.LifecycleActionProtect,
		RequiredVisibility: "private", State: domainmedia.JobStatePending,
		MaxAttempts: 3, NextAttemptAt: now,
	}
	repo := &lifecycleRepositoryStub{task: task, available: now}
	protector := &lifecycleProtectorStub{err: errors.New("object store unavailable")}
	service := NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{
			state: VideoLifecycleState{Exists: true, Status: 3, Visibility: "private"},
		},
		protector,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err := service.RunOnce(context.Background(), "worker", 1, time.Minute)
	if err == nil || completed != 0 || task.State != domainmedia.JobStateRetryable {
		t.Fatalf("first attempt completed=%d state=%s err=%v", completed, task.State, err)
	}
	now = task.NextAttemptAt
	protector.err = nil
	completed, err = service.RunOnce(context.Background(), "worker", 1, time.Minute)
	if err != nil || completed != 1 || protector.calls != 2 {
		t.Fatalf("retry completed=%d calls=%d err=%v", completed, protector.calls, err)
	}

	superseded := &domainmedia.VideoLifecycleTask{
		ID: 3, VideoID: 9, MediaAssetID: 31,
		Action:             domainmedia.LifecycleActionProtect,
		RequiredVisibility: "private", State: domainmedia.JobStatePending,
		MaxAttempts: 3, NextAttemptAt: now,
	}
	repo = &lifecycleRepositoryStub{task: superseded, available: now}
	publicProtector := &lifecycleProtectorStub{}
	service = NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{
			state: VideoLifecycleState{
				Exists: true, Status: 2, Visibility: "public",
				PublicEligible: true,
			},
		},
		publicProtector,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err = service.RunOnce(context.Background(), "worker", 1, time.Minute)
	if err != nil || completed != 1 || publicProtector.calls != 0 ||
		publicProtector.restoreCalls != 1 ||
		superseded.ErrorCode != "superseded" {
		t.Fatalf(
			"superseded completed=%d protect=%d restore=%d task=%+v err=%v",
			completed, publicProtector.calls, publicProtector.restoreCalls, superseded, err,
		)
	}
}

func TestVideoLifecycleInfrastructureFailureKeepsRetryingAfterMaxAttempts(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	task := &domainmedia.VideoLifecycleTask{
		ID: 7, VideoID: 13, MediaAssetID: 71,
		Action:             domainmedia.LifecycleActionProtect,
		RequiredVisibility: "private", State: domainmedia.JobStatePending,
		MaxAttempts: 1, NextAttemptAt: now,
	}
	repo := &lifecycleRepositoryStub{task: task, available: now}
	delivery := &lifecycleProtectorStub{err: errors.New("object store unavailable")}
	service := NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{state: VideoLifecycleState{
			Exists: true, Status: 2, Visibility: "private",
		}},
		delivery,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err := service.RunOnce(
		context.Background(), "worker", 1, time.Minute,
	)
	if err == nil || completed != 0 ||
		task.State != domainmedia.JobStateRetryable ||
		task.ErrorCode != "attempts_exhausted" {
		t.Fatalf("first attempt completed=%d task=%+v err=%v", completed, task, err)
	}

	now = task.NextAttemptAt
	delivery.err = nil
	completed, err = service.RunOnce(
		context.Background(), "worker", 1, time.Minute,
	)
	if err != nil || completed != 1 || task.State != domainmedia.JobStateCompleted {
		t.Fatalf("recovered attempt completed=%d task=%+v err=%v", completed, task, err)
	}
}

type lifecycleSequenceVideoReaderStub struct {
	states []VideoLifecycleState
	index  int
}

func (r *lifecycleSequenceVideoReaderStub) ReadVideoLifecycleState(
	context.Context,
	int64,
) (VideoLifecycleState, error) {
	if len(r.states) == 0 {
		return VideoLifecycleState{}, nil
	}
	index := r.index
	if index >= len(r.states) {
		index = len(r.states) - 1
	}
	r.index++
	return r.states[index], nil
}

func TestVideoLifecyclePrivateIntentRestoresConcurrentRepublication(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	task := &domainmedia.VideoLifecycleTask{
		ID: 4, VideoID: 10, MediaAssetID: 41, CoverAssetID: 42,
		Action:             domainmedia.LifecycleActionProtect,
		RequiredVisibility: "private", State: domainmedia.JobStatePending,
		MaxAttempts: 3, NextAttemptAt: now,
	}
	repo := &lifecycleRepositoryStub{task: task, available: now}
	delivery := &lifecycleProtectorStub{}
	service := NewVideoLifecycleService(
		repo,
		&lifecycleSequenceVideoReaderStub{states: []VideoLifecycleState{
			{Exists: true, Status: 2, Visibility: "private"},
			{
				Exists: true, Status: 2, Visibility: "public",
				PublicEligible: true,
			},
		}},
		delivery,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err := service.RunOnce(
		context.Background(), "worker", 1, time.Minute,
	)
	if err != nil || completed != 1 || delivery.calls != 1 ||
		delivery.restoreCalls != 1 || task.ErrorCode != "superseded" {
		t.Fatalf(
			"completed=%d protect=%d restore=%d task=%+v err=%v",
			completed, delivery.calls, delivery.restoreCalls, task, err,
		)
	}
}

func TestVideoLifecycleRetryRestoresAlreadyPublicSupersededIntent(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 30, 0, 0, time.UTC)
	task := &domainmedia.VideoLifecycleTask{
		ID: 5, VideoID: 11, MediaAssetID: 51, CoverAssetID: 52,
		Action:             domainmedia.LifecycleActionProtect,
		RequiredVisibility: "private", State: domainmedia.JobStatePending,
		MaxAttempts: 3, NextAttemptAt: now,
	}
	repo := &lifecycleRepositoryStub{task: task, available: now}
	delivery := &lifecycleProtectorStub{}
	service := NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{state: VideoLifecycleState{
			Exists: true, Status: 2, Visibility: "public",
			PublicEligible: true,
		}},
		delivery,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err := service.RunOnce(
		context.Background(), "worker", 1, time.Minute,
	)
	if err != nil || completed != 1 || delivery.calls != 0 ||
		delivery.restoreCalls != 1 || task.ErrorCode != "superseded" {
		t.Fatalf(
			"completed=%d protect=%d restore=%d task=%+v err=%v",
			completed, delivery.calls, delivery.restoreCalls, task, err,
		)
	}
}

func TestVideoLifecycleSupersededIntentStillProtectsNonPublicState(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 45, 0, 0, time.UTC)
	task := &domainmedia.VideoLifecycleTask{
		ID: 6, VideoID: 12, MediaAssetID: 61, CoverAssetID: 62,
		Action:         domainmedia.LifecycleActionProtect,
		RequiredStatus: 3, State: domainmedia.JobStatePending,
		MaxAttempts: 3, NextAttemptAt: now,
	}
	repo := &lifecycleRepositoryStub{task: task, available: now}
	delivery := &lifecycleProtectorStub{}
	service := NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{state: VideoLifecycleState{
			Exists: true, Status: 2, Visibility: "private",
		}},
		delivery,
		&lifecycleCleanupStub{},
	)
	service.now = func() time.Time { return now }
	completed, err := service.RunOnce(
		context.Background(), "worker", 1, time.Minute,
	)
	if err != nil || completed != 1 || delivery.calls != 1 ||
		delivery.restoreCalls != 0 || task.ErrorCode != "superseded" {
		t.Fatalf(
			"completed=%d protect=%d restore=%d task=%+v err=%v",
			completed, delivery.calls, delivery.restoreCalls, task, err,
		)
	}
}

type blockingLifecycleRepository struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (r *blockingLifecycleRepository) ClaimVideoLifecycleTasks(
	context.Context,
	string,
	time.Time,
	time.Time,
	int,
) ([]*domainmedia.VideoLifecycleTask, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return nil, nil
}

func (*blockingLifecycleRepository) UpdateVideoLifecycleTaskOwned(
	context.Context,
	*domainmedia.VideoLifecycleTask,
	string,
) error {
	return nil
}

func (*blockingLifecycleRepository) VideoLifecycleBacklog(
	context.Context,
) (int64, *time.Time, error) {
	return 0, nil, nil
}

func TestVideoLifecycleWorkerStartsInitialPollAsynchronously(t *testing.T) {
	repo := &blockingLifecycleRepository{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewVideoLifecycleService(
		repo,
		lifecycleVideoReaderStub{},
		&lifecycleProtectorStub{},
		&lifecycleCleanupStub{},
	)
	worker := NewVideoLifecycleWorker(service, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	returned := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("worker startup blocked on the initial database poll")
	}
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("initial lifecycle poll did not start")
	}
	close(repo.release)
}
