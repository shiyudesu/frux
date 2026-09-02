package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infradatabase "github.com/shiyudesu/frux/internal/infra/database"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
	infravideostream "github.com/shiyudesu/frux/internal/infra/videostream"

	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const visibilityPollInterval = 200 * time.Millisecond

type latencySummary struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type redisCommandDelta struct {
	ZAdd                 int64 `json:"zadd"`
	ZRemRangeByRank      int64 `json:"zremrangebyrank"`
	Expire               int64 `json:"expire"`
	FollowingIndexWrites int64 `json:"following_index_write_commands"`
	TotalCommands        int64 `json:"total_commands"`
	NetInputBytes        int64 `json:"net_input_bytes"`
	NetOutputBytes       int64 `json:"net_output_bytes"`
	UsedMemoryDeltaBytes int64 `json:"used_memory_delta_bytes"`
}

type followingIndexSummary struct {
	InboxKeys      int   `json:"inbox_keys"`
	InboxEntries   int64 `json:"inbox_entries"`
	OutboxKeys     int   `json:"outbox_keys"`
	OutboxEntries  int64 `json:"outbox_entries"`
	TotalEntries   int64 `json:"total_entries"`
	ExpectedInbox  int64 `json:"expected_inbox_entries"`
	ExpectedOutbox int64 `json:"expected_outbox_entries"`
	ExpectedTotal  int64 `json:"expected_total_entries"`
	Exact          bool  `json:"exact"`
}

type workerDelta struct {
	Success       int64   `json:"success"`
	Failure       int64   `json:"failure"`
	AverageMS     float64 `json:"average_ms"`
	P50UpperMS    float64 `json:"p50_upper_bound_ms"`
	P95UpperMS    float64 `json:"p95_upper_bound_ms"`
	P99UpperMS    float64 `json:"p99_upper_bound_ms"`
	HistogramNote string  `json:"histogram_note"`
}

type result struct {
	Source                   string                `json:"source"`
	Strategy                 string                `json:"strategy"`
	Events                   int                   `json:"events"`
	NormalEvents             int                   `json:"normal_events"`
	BigEvents                int                   `json:"big_events"`
	NormalFollowers          int64                 `json:"normal_followers"`
	BigFollowers             int64                 `json:"big_followers"`
	NormalRoutingFollowers   int64                 `json:"normal_routing_followers"`
	BigRoutingFollowers      int64                 `json:"big_routing_followers"`
	DurationMS               float64               `json:"duration_ms"`
	EventsPerSecond          float64               `json:"events_per_second"`
	ProducerAcknowledgement  latencySummary        `json:"producer_acknowledgement"`
	MaxConsumerLag           int64                 `json:"max_consumer_lag"`
	TimeToDrain50PercentMS   float64               `json:"time_to_drain_50_percent_ms"`
	TimeToDrain95PercentMS   float64               `json:"time_to_drain_95_percent_ms"`
	RecoveryMS               float64               `json:"recovery_ms"`
	TotalPublishToRecoveryMS float64               `json:"total_publish_to_recovery_ms"`
	IndexVisibility          latencySummary        `json:"index_visibility"`
	MissingVisibility        int                   `json:"missing_visibility"`
	RedisCommands            redisCommandDelta     `json:"redis_commands"`
	FollowingIndex           followingIndexSummary `json:"following_index"`
	FanoutWorker             workerDelta           `json:"fanout_worker"`
}

type lagSample struct {
	afterPublish time.Duration
	lag          int64
}

type lagResult struct {
	maxLag                 int64
	recovery               time.Duration
	totalPublishToRecovery time.Duration
	timeToDrain50Percent   time.Duration
	timeToDrain95Percent   time.Duration
}

type authorTopology struct {
	authorID      int64
	followers     int64
	routingCount  int64
	witnessUserID int64
}

type visibilityProbe struct {
	key       string
	member    string
	startedAt time.Time
}

type visibilityResult struct {
	latencies []time.Duration
	missing   int
}

type redisSnapshot struct {
	zadd            int64
	zremrangebyrank int64
	expire          int64
	totalCommands   int64
	netInputBytes   int64
	netOutputBytes  int64
	usedMemory      int64
}

type workerSnapshot struct {
	success int64
	failure int64
	count   float64
	sum     float64
	buckets map[float64]float64
}

func main() {
	configPath := flag.String("config", "./configs/config.yaml", "benchmark API config path")
	eventCount := flag.Int("events", 100, "number of video-published events")
	bigEvery := flag.Int("big-every", 5, "emit one big-creator event every N events")
	normalAuthor := flag.Int64("normal-author", 3, "normal creator account ID")
	bigAuthor := flag.Int64("big-author", 2, "big creator account ID")
	authorCount := flag.Int64("author-count", 500, "seeded author count used to resolve video IDs")
	workerMetricsURL := flag.String("worker-metrics-url", "http://worker:9091/metrics", "worker Prometheus metrics URL")
	flag.Parse()

	if *eventCount <= 0 || *bigEvery <= 0 || *normalAuthor <= 0 || *bigAuthor <= 0 || *authorCount <= 0 {
		log.Fatal("invalid benchmark publication arguments")
	}
	cfg, err := infraconfig.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := infradatabase.New(cfg.Database)
	if err != nil {
		log.Fatalf("connect benchmark database: %v", err)
	}
	defer db.Close()
	normalTopology, err := loadAuthorTopology(ctx, db, *normalAuthor)
	if err != nil {
		log.Fatalf("load normal author topology: %v", err)
	}
	bigTopology, err := loadAuthorTopology(ctx, db, *bigAuthor)
	if err != nil {
		log.Fatalf("load big author topology: %v", err)
	}
	strategy := fanoutStrategy(normalTopology, bigTopology)

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect benchmark redis: %v", err)
	}
	redisBefore, err := loadRedisSnapshot(ctx, redisClient)
	if err != nil {
		log.Fatalf("read redis metrics before fanout: %v", err)
	}
	workerBefore, err := loadWorkerSnapshot(ctx, *workerMetricsURL)
	if err != nil {
		log.Printf("worker metrics before fanout unavailable: %v", err)
	}

	backbone, err := infrakafka.Start(ctx, cfg.Kafka, nil, nil)
	if err != nil {
		log.Fatalf("start kafka backbone: %v", err)
	}
	defer func() { _ = backbone.Close(context.Background()) }()
	publisher, err := infravideostream.NewVideoPublisher(backbone.Publisher(), nil)
	if err != nil {
		log.Fatalf("create video publisher: %v", err)
	}

	started := time.Now()
	groupName, err := infrakafka.GroupName(cfg.Kafka.TopicPrefix, infrakafka.GroupFeedVideoPublishedActive)
	if err != nil {
		log.Fatalf("resolve feed consumer group: %v", err)
	}
	publishingDone := make(chan struct{})
	lagResultChannel := make(chan lagResult, 1)
	go monitorLag(ctx, cfg.Kafka.Brokers, groupName, started, publishingDone, lagResultChannel)

	normalEvents := 0
	bigEvents := 0
	producerLatencies := make([]time.Duration, 0, *eventCount)
	visibilityProbes := make([]visibilityProbe, 0, *eventCount)
	runID := started.UTC().Format("20060102T150405.000000000")
	for index := 0; index < *eventCount; index++ {
		topology := normalTopology
		authorSequence := normalEvents
		if (index+1)%*bigEvery == 0 {
			topology = bigTopology
			authorSequence = bigEvents
			bigEvents++
		} else {
			normalEvents++
		}
		videoID := topology.authorID + int64(authorSequence%100)*(*authorCount)
		occurredAt := time.Now().UTC()
		event := &applicationvideo.PublishedEvent{
			EventID:      fmt.Sprintf("benchmark-publish:%s:%d", runID, index+1),
			VideoID:      videoID,
			AuthorID:     topology.authorID,
			Title:        fmt.Sprintf("Benchmark publication %d", index+1),
			Description:  "isolated Kafka fanout benchmark",
			MediaURL:     fmt.Sprintf("https://benchmark.invalid/video/%d.mp4", videoID),
			CoverURL:     fmt.Sprintf("https://benchmark.invalid/cover/%d.jpg", videoID),
			VideoVersion: 1,
			PublishedAt:  occurredAt,
			OccurredAt:   occurredAt,
		}
		publishStarted := time.Now()
		if err := publisher.PublishVideoPublished(ctx, event); err != nil {
			log.Fatalf("publish event %d: %v", index+1, err)
		}
		producerLatencies = append(producerLatencies, time.Since(publishStarted))
		visibilityProbes = append(visibilityProbes, newVisibilityProbe(topology, event, publishStarted))
	}
	duration := time.Since(started)
	close(publishingDone)
	visibilityChannel := make(chan visibilityResult, 1)
	go watchVisibility(ctx, redisClient, visibilityProbes, visibilityChannel)
	consumerLag := <-lagResultChannel
	visibility := <-visibilityChannel

	redisAfter, err := loadRedisSnapshot(ctx, redisClient)
	if err != nil {
		log.Fatalf("read redis metrics after fanout: %v", err)
	}
	indexSummary, err := loadFollowingIndexSummary(ctx, redisClient)
	if err != nil {
		log.Fatalf("read following index summary: %v", err)
	}
	expectedInbox, expectedOutbox := expectedIndexEntries(normalEvents, bigEvents, normalTopology, bigTopology)
	indexSummary.ExpectedInbox = expectedInbox
	indexSummary.ExpectedOutbox = expectedOutbox
	indexSummary.ExpectedTotal = expectedInbox + expectedOutbox
	indexSummary.Exact = indexSummary.InboxEntries == expectedInbox &&
		indexSummary.OutboxEntries == expectedOutbox &&
		indexSummary.TotalEntries == indexSummary.ExpectedTotal

	workerAfter, workerAfterErr := loadWorkerSnapshot(ctx, *workerMetricsURL)
	if workerAfterErr != nil {
		log.Printf("worker metrics after fanout unavailable: %v", workerAfterErr)
	}
	output := result{
		Source:                   "direct_kafka_video_published",
		Strategy:                 strategy,
		Events:                   *eventCount,
		NormalEvents:             normalEvents,
		BigEvents:                bigEvents,
		NormalFollowers:          normalTopology.followers,
		BigFollowers:             bigTopology.followers,
		NormalRoutingFollowers:   normalTopology.routingCount,
		BigRoutingFollowers:      bigTopology.routingCount,
		DurationMS:               milliseconds(duration),
		EventsPerSecond:          float64(*eventCount) / duration.Seconds(),
		ProducerAcknowledgement:  summarizeLatencies(producerLatencies),
		MaxConsumerLag:           consumerLag.maxLag,
		TimeToDrain50PercentMS:   milliseconds(consumerLag.timeToDrain50Percent),
		TimeToDrain95PercentMS:   milliseconds(consumerLag.timeToDrain95Percent),
		RecoveryMS:               milliseconds(consumerLag.recovery),
		TotalPublishToRecoveryMS: milliseconds(consumerLag.totalPublishToRecovery),
		IndexVisibility:          summarizeLatencies(visibility.latencies),
		MissingVisibility:        visibility.missing,
		RedisCommands:            redisAfter.subtract(redisBefore),
		FollowingIndex:           indexSummary,
		FanoutWorker:             workerAfter.subtract(workerBefore),
	}
	if err := json.NewEncoder(log.Writer()).Encode(output); err != nil {
		log.Fatalf("encode result: %v", err)
	}
	if output.MissingVisibility != 0 || !output.FollowingIndex.Exact || output.FanoutWorker.Failure != 0 {
		log.Fatalf(
			"fanout correctness failed: missing_visibility=%d exact_index=%t worker_failures=%d",
			output.MissingVisibility, output.FollowingIndex.Exact, output.FanoutWorker.Failure,
		)
	}
}

func loadAuthorTopology(ctx context.Context, db *sql.DB, authorID int64) (authorTopology, error) {
	value := authorTopology{authorID: authorID}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(user_id), 0)
		FROM user_follow
		WHERE target_user_id = $1 AND status = 1
	`, authorID).Scan(&value.followers, &value.witnessUserID); err != nil {
		return authorTopology{}, err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT follower_count
		FROM user_relation_stat
		WHERE user_id = $1
	`, authorID).Scan(&value.routingCount); err != nil {
		return authorTopology{}, err
	}
	if value.followers <= 0 || value.witnessUserID <= 0 || value.routingCount < 0 {
		return authorTopology{}, fmt.Errorf("invalid author topology: author=%d", authorID)
	}
	return value, nil
}

func fanoutStrategy(normal, big authorTopology) string {
	if normal.routingCount < domainfeed.BigCreatorFollowerThreshold && big.routingCount >= domainfeed.BigCreatorFollowerThreshold {
		return "hybrid"
	}
	if normal.routingCount < domainfeed.BigCreatorFollowerThreshold && big.routingCount < domainfeed.BigCreatorFollowerThreshold {
		return "all_push"
	}
	return "custom"
}

func expectedIndexEntries(normalEvents, bigEvents int, normal, big authorTopology) (int64, int64) {
	var inbox int64
	var outbox int64
	appendExpected := func(events int, topology authorTopology) {
		if topology.routingCount >= domainfeed.BigCreatorFollowerThreshold {
			outbox += int64(events)
			return
		}
		inbox += int64(events) * topology.followers
	}
	appendExpected(normalEvents, normal)
	appendExpected(bigEvents, big)
	return inbox, outbox
}

func newVisibilityProbe(topology authorTopology, event *applicationvideo.PublishedEvent, startedAt time.Time) visibilityProbe {
	key := fmt.Sprintf("feed:following:inbox:v2:%d", topology.witnessUserID)
	if topology.routingCount >= domainfeed.BigCreatorFollowerThreshold {
		key = fmt.Sprintf("feed:following:author:v2:%d", topology.authorID)
	}
	return visibilityProbe{
		key:       key,
		member:    fmt.Sprintf("%020d:%020d:%s", event.VideoID, event.AuthorID, event.PublishedAt.UTC().Format(time.RFC3339Nano)),
		startedAt: startedAt,
	}
}

func watchVisibility(ctx context.Context, client *redis.Client, probes []visibilityProbe, resultChannel chan<- visibilityResult) {
	pending := make(map[int]struct{}, len(probes))
	for index := range probes {
		pending[index] = struct{}{}
	}
	result := visibilityResult{latencies: make([]time.Duration, 0, len(probes))}
	ticker := time.NewTicker(visibilityPollInterval)
	defer ticker.Stop()
	check := func() {
		pipe := client.Pipeline()
		commands := make(map[int]*redis.FloatCmd, len(pending))
		for index := range pending {
			probe := probes[index]
			commands[index] = pipe.ZScore(ctx, probe.key, probe.member)
		}
		_, _ = pipe.Exec(ctx)
		now := time.Now()
		for index, command := range commands {
			if _, err := command.Result(); err == nil {
				result.latencies = append(result.latencies, now.Sub(probes[index].startedAt))
				delete(pending, index)
			}
		}
	}
	for len(pending) > 0 {
		check()
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			result.missing = len(pending)
			resultChannel <- result
			return
		case <-ticker.C:
		}
	}
	resultChannel <- result
}

func monitorLag(ctx context.Context, brokers []string, groupName string, startedAt time.Time, publishingDone <-chan struct{}, resultChannel chan<- lagResult) {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ClientID("frux-benchmark-lag-monitor"))
	if err != nil {
		resultChannel <- lagResult{}
		return
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(90 * time.Second)
	defer timeout.Stop()

	var value lagResult
	done := false
	zeroSamples := 0
	doneAt := time.Time{}
	doneChannel := publishingDone
	postPublishSamples := make([]lagSample, 0, 256)
	for {
		select {
		case <-ctx.Done():
			resultChannel <- finalizeLagResult(value, postPublishSamples)
			return
		case <-timeout.C:
			if done && !doneAt.IsZero() {
				value.recovery = time.Since(doneAt)
				value.totalPublishToRecovery = time.Since(startedAt)
			}
			resultChannel <- finalizeLagResult(value, postPublishSamples)
			return
		case <-doneChannel:
			done = true
			doneAt = time.Now()
			doneChannel = nil
		case <-ticker.C:
			lag, lagErr := readLag(ctx, admin, groupName)
			if lagErr != nil {
				continue
			}
			if lag > value.maxLag {
				value.maxLag = lag
			}
			if !done {
				continue
			}
			postPublishSamples = append(postPublishSamples, lagSample{afterPublish: time.Since(doneAt), lag: lag})
			if lag == 0 {
				zeroSamples++
				if zeroSamples >= 3 {
					value.recovery = time.Since(doneAt)
					value.totalPublishToRecovery = time.Since(startedAt)
					resultChannel <- finalizeLagResult(value, postPublishSamples)
					return
				}
			} else {
				zeroSamples = 0
			}
		}
	}
}

func finalizeLagResult(value lagResult, samples []lagSample) lagResult {
	if value.maxLag <= 0 {
		return value
	}
	halfThreshold := int64(math.Ceil(float64(value.maxLag) * 0.50))
	fivePercentThreshold := int64(math.Ceil(float64(value.maxLag) * 0.05))
	for _, sample := range samples {
		if value.timeToDrain50Percent == 0 && sample.lag <= halfThreshold {
			value.timeToDrain50Percent = sample.afterPublish
		}
		if value.timeToDrain95Percent == 0 && sample.lag <= fivePercentThreshold {
			value.timeToDrain95Percent = sample.afterPublish
		}
	}
	return value
}

func readLag(ctx context.Context, admin *kadm.Client, groupName string) (int64, error) {
	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	lags, err := admin.Lag(requestContext, groupName)
	if err != nil {
		return 0, err
	}
	group, ok := lags[groupName]
	if !ok {
		return 0, infrakafka.ErrKafkaUnavailable
	}
	if err := group.Error(); err != nil {
		return 0, err
	}
	return group.Lag.Total(), nil
}

func summarizeLatencies(values []time.Duration) latencySummary {
	if len(values) == 0 {
		return latencySummary{}
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return latencySummary{
		Count: len(ordered),
		P50MS: milliseconds(durationQuantile(ordered, 0.50)),
		P95MS: milliseconds(durationQuantile(ordered, 0.95)),
		P99MS: milliseconds(durationQuantile(ordered, 0.99)),
		MaxMS: milliseconds(ordered[len(ordered)-1]),
	}
}

func durationQuantile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func milliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func loadRedisSnapshot(ctx context.Context, client *redis.Client) (redisSnapshot, error) {
	content, err := client.Info(ctx, "commandstats", "stats", "memory").Result()
	if err != nil {
		return redisSnapshot{}, err
	}
	return redisSnapshot{
		zadd:            redisCommandCalls(content, "zadd"),
		zremrangebyrank: redisCommandCalls(content, "zremrangebyrank"),
		expire:          redisCommandCalls(content, "expire"),
		totalCommands:   redisInfoInt(content, "total_commands_processed"),
		netInputBytes:   redisInfoInt(content, "total_net_input_bytes"),
		netOutputBytes:  redisInfoInt(content, "total_net_output_bytes"),
		usedMemory:      redisInfoInt(content, "used_memory"),
	}, nil
}

func redisCommandCalls(content, command string) int64 {
	prefix := "cmdstat_" + command + ":"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, part := range strings.Split(strings.TrimPrefix(line, prefix), ",") {
			if strings.HasPrefix(part, "calls=") {
				value, _ := strconv.ParseInt(strings.TrimPrefix(part, "calls="), 10, 64)
				return value
			}
		}
	}
	return 0
}

func redisInfoInt(content, key string) int64 {
	prefix := key + ":"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			value, _ := strconv.ParseInt(strings.TrimPrefix(line, prefix), 10, 64)
			return value
		}
	}
	return 0
}

func (after redisSnapshot) subtract(before redisSnapshot) redisCommandDelta {
	zadd := after.zadd - before.zadd
	zrem := after.zremrangebyrank - before.zremrangebyrank
	expire := after.expire - before.expire
	return redisCommandDelta{
		ZAdd:                 zadd,
		ZRemRangeByRank:      zrem,
		Expire:               expire,
		FollowingIndexWrites: zadd + zrem + expire,
		TotalCommands:        after.totalCommands - before.totalCommands,
		NetInputBytes:        after.netInputBytes - before.netInputBytes,
		NetOutputBytes:       after.netOutputBytes - before.netOutputBytes,
		UsedMemoryDeltaBytes: after.usedMemory - before.usedMemory,
	}
}

func loadFollowingIndexSummary(ctx context.Context, client *redis.Client) (followingIndexSummary, error) {
	var summary followingIndexSummary
	if err := accumulateIndexPattern(ctx, client, "feed:following:inbox:v2:*", &summary.InboxKeys, &summary.InboxEntries); err != nil {
		return followingIndexSummary{}, err
	}
	if err := accumulateIndexPattern(ctx, client, "feed:following:author:v2:*", &summary.OutboxKeys, &summary.OutboxEntries); err != nil {
		return followingIndexSummary{}, err
	}
	summary.TotalEntries = summary.InboxEntries + summary.OutboxEntries
	return summary, nil
}

func accumulateIndexPattern(ctx context.Context, client *redis.Client, pattern string, keyCount *int, entries *int64) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, pattern, 1000).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			pipe := client.Pipeline()
			commands := make([]*redis.IntCmd, 0, len(keys))
			for _, key := range keys {
				commands = append(commands, pipe.ZCard(ctx, key))
			}
			if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
				return err
			}
			for _, command := range commands {
				count, err := command.Result()
				if err != nil {
					return err
				}
				*entries += count
			}
			*keyCount += len(keys)
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func loadWorkerSnapshot(ctx context.Context, endpoint string) (workerSnapshot, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return workerSnapshot{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return workerSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return workerSnapshot{}, fmt.Errorf("worker metrics status %d", response.StatusCode)
	}
	return parseWorkerSnapshot(response.Body)
}

func parseWorkerSnapshot(reader io.Reader) (workerSnapshot, error) {
	snapshot := workerSnapshot{buckets: map[float64]float64{}}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := parsePrometheusSample(line)
		if !ok || labels["job"] != "video_fanout" {
			continue
		}
		switch name {
		case "frux_worker_jobs_total":
			if labels["result"] == "success" {
				snapshot.success += int64(value)
			} else {
				snapshot.failure += int64(value)
			}
		case "frux_worker_job_duration_seconds_count":
			if labels["result"] == "success" {
				snapshot.count = value
			}
		case "frux_worker_job_duration_seconds_sum":
			if labels["result"] == "success" {
				snapshot.sum = value
			}
		case "frux_worker_job_duration_seconds_bucket":
			if labels["result"] != "success" {
				continue
			}
			upper, parseErr := strconv.ParseFloat(labels["le"], 64)
			if parseErr == nil {
				snapshot.buckets[upper] = value
			}
		}
	}
	return snapshot, scanner.Err()
}

func parsePrometheusSample(line string) (string, map[string]string, float64, bool) {
	space := strings.LastIndexByte(line, ' ')
	if space <= 0 {
		return "", nil, 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(line[space+1:]), 64)
	if err != nil {
		return "", nil, 0, false
	}
	metric := line[:space]
	labels := map[string]string{}
	open := strings.IndexByte(metric, '{')
	if open < 0 {
		return metric, labels, value, true
	}
	close := strings.LastIndexByte(metric, '}')
	if close <= open {
		return "", nil, 0, false
	}
	for _, item := range strings.Split(metric[open+1:close], ",") {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			labels[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
		}
	}
	return metric[:open], labels, value, true
}

func (after workerSnapshot) subtract(before workerSnapshot) workerDelta {
	count := after.count - before.count
	sum := after.sum - before.sum
	buckets := make(map[float64]float64, len(after.buckets))
	for upper, value := range after.buckets {
		buckets[upper] = value - before.buckets[upper]
	}
	averageMS := 0.0
	if count > 0 {
		averageMS = sum / count * 1000
	}
	return workerDelta{
		Success:       after.success - before.success,
		Failure:       after.failure - before.failure,
		AverageMS:     averageMS,
		P50UpperMS:    histogramQuantileUpperBound(buckets, count, 0.50) * 1000,
		P95UpperMS:    histogramQuantileUpperBound(buckets, count, 0.95) * 1000,
		P99UpperMS:    histogramQuantileUpperBound(buckets, count, 0.99) * 1000,
		HistogramNote: "upper bounds from Prometheus histogram buckets",
	}
}

func histogramQuantileUpperBound(buckets map[float64]float64, count, quantile float64) float64 {
	if count <= 0 || len(buckets) == 0 {
		return 0
	}
	upperBounds := make([]float64, 0, len(buckets))
	for upper := range buckets {
		upperBounds = append(upperBounds, upper)
	}
	sort.Float64s(upperBounds)
	target := count * quantile
	for _, upper := range upperBounds {
		if buckets[upper] >= target {
			return upper
		}
	}
	return upperBounds[len(upperBounds)-1]
}
