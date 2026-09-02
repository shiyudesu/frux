package main

import (
	"strings"
	"testing"
	"time"

	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
)

func TestExpectedIndexEntriesForAllPushAndHybrid(t *testing.T) {
	normal := authorTopology{followers: 5419, routingCount: 5419}
	allPushBig := authorTopology{followers: 11999, routingCount: domainfeed.BigCreatorFollowerThreshold - 1}
	hybridBig := authorTopology{followers: 11999, routingCount: domainfeed.BigCreatorFollowerThreshold + 1999}

	inbox, outbox := expectedIndexEntries(80, 20, normal, allPushBig)
	if inbox != 673500 || outbox != 0 {
		t.Fatalf("all-push entries: inbox=%d outbox=%d", inbox, outbox)
	}
	inbox, outbox = expectedIndexEntries(80, 20, normal, hybridBig)
	if inbox != 433520 || outbox != 20 {
		t.Fatalf("hybrid entries: inbox=%d outbox=%d", inbox, outbox)
	}
}

func TestSummarizeLatenciesUsesNearestRankQuantiles(t *testing.T) {
	values := make([]time.Duration, 100)
	for index := range values {
		values[index] = time.Duration(index+1) * time.Millisecond
	}
	summary := summarizeLatencies(values)
	if summary.Count != 100 || summary.P50MS != 50 || summary.P95MS != 95 || summary.P99MS != 99 || summary.MaxMS != 100 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestParseWorkerSnapshotAndDelta(t *testing.T) {
	beforeText := `
frux_worker_jobs_total{job="video_fanout",result="success"} 10
frux_worker_job_duration_seconds_bucket{job="video_fanout",result="success",le="0.1"} 4
frux_worker_job_duration_seconds_bucket{job="video_fanout",result="success",le="0.5"} 10
frux_worker_job_duration_seconds_count{job="video_fanout",result="success"} 10
frux_worker_job_duration_seconds_sum{job="video_fanout",result="success"} 2
`
	afterText := `
frux_worker_jobs_total{job="video_fanout",result="success"} 12
frux_worker_job_duration_seconds_bucket{job="video_fanout",result="success",le="0.1"} 5
frux_worker_job_duration_seconds_bucket{job="video_fanout",result="success",le="0.5"} 12
frux_worker_job_duration_seconds_count{job="video_fanout",result="success"} 12
frux_worker_job_duration_seconds_sum{job="video_fanout",result="success"} 2.6
`
	before, err := parseWorkerSnapshot(strings.NewReader(beforeText))
	if err != nil {
		t.Fatal(err)
	}
	after, err := parseWorkerSnapshot(strings.NewReader(afterText))
	if err != nil {
		t.Fatal(err)
	}
	delta := after.subtract(before)
	if delta.Success != 2 || delta.Failure != 0 || delta.AverageMS < 299 || delta.AverageMS > 301 || delta.P95UpperMS != 500 {
		t.Fatalf("delta=%+v", delta)
	}
}

func TestRedisSnapshotDeltaCountsFollowingWriteCommands(t *testing.T) {
	before := redisSnapshot{zadd: 10, zremrangebyrank: 20, expire: 30, totalCommands: 100, usedMemory: 1000}
	after := redisSnapshot{zadd: 12, zremrangebyrank: 23, expire: 34, totalCommands: 120, usedMemory: 1500}
	delta := after.subtract(before)
	if delta.ZAdd != 2 || delta.ZRemRangeByRank != 3 || delta.Expire != 4 || delta.FollowingIndexWrites != 9 || delta.UsedMemoryDeltaBytes != 500 {
		t.Fatalf("delta=%+v", delta)
	}
}
