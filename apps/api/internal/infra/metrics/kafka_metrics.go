package inframetrics

import (
	"strings"
	"sync"
	"time"

	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	KafkaProduceTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_produce_total",
			Help: "Kafka produce attempts by registered topic, producer, and bounded result.",
		},
		[]string{"topic", "producer", "result"},
	)
	KafkaProduceDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux", Name: "kafka_produce_duration_seconds",
			Help:    "Kafka acknowledged produce duration.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"topic", "producer", "result"},
	)
	KafkaConsumedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_consumed_total",
			Help: "Kafka records handled by registered topic, group, and bounded outcome.",
		},
		[]string{"topic", "group", "outcome"},
	)
	KafkaConsumeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux", Name: "kafka_consume_duration_seconds",
			Help:    "Kafka record handling duration.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"topic", "group", "outcome"},
	)
	KafkaCommitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_commit_total",
			Help: "Kafka explicit offset commit outcomes.",
		},
		[]string{"topic", "group", "result"},
	)
	KafkaRebalanceTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_rebalance_total",
			Help: "Kafka consumer group assignment changes.",
		},
		[]string{"group", "result"},
	)
	KafkaConsumerLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_consumer_lag",
			Help: "Kafka consumer lag for a registered consumed topic, owning group, and stage.",
		},
		[]string{"topic", "group", "stage"},
	)
	KafkaConsumerWorkflowLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_consumer_workflow_lag",
			Help: "Aggregate Kafka consumer lag across all observed stages of an owning workflow.",
		},
		[]string{"group"},
	)
	KafkaDeliveryDelay = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux", Name: "kafka_delivery_delay_seconds",
			Help:    "Kafka delivery delay from record timestamp to handling.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 900, 3600},
		},
		[]string{"topic", "group"},
	)
	KafkaContractFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_contract_failures_total",
			Help: "Terminal Kafka contract failures by bounded code.",
		},
		[]string{"topic", "group", "code"},
	)
	KafkaDataLossTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_data_loss_total",
			Help: "Kafka client data-loss notifications recovered by cursor reset.",
		},
		[]string{"topic", "group"},
	)
	KafkaTopologyValidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_topology_validation_total",
			Help: "Kafka registered topic provisioning and validation outcomes.",
		},
		[]string{"topic", "result"},
	)
	KafkaConsumerSessionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_consumer_session_total",
			Help: "Kafka consumer supervisor session lifecycle outcomes.",
		},
		[]string{"group", "stage", "result"},
	)
	KafkaConsumerSessionHealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_consumer_session_healthy",
			Help: "Whether a registered Kafka consumer stage currently has a healthy session.",
		},
		[]string{"group", "stage"},
	)
	KafkaConsumerWorkflowHealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_consumer_workflow_healthy",
			Help: "Whether every observed stage of an owning Kafka workflow is healthy.",
		},
		[]string{"group"},
	)
	KafkaBrokerHealthy = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_broker_healthy",
			Help: "Whether the configured Kafka broker is reachable.",
		},
	)
	KafkaRecoveryPublishTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_recovery_publish_total",
			Help: "Kafka retry and DLQ publications by registered group, destination tier, and result.",
		},
		[]string{"group", "tier", "result"},
	)
	KafkaRecoveryDelay = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux", Name: "kafka_recovery_delay_seconds",
			Help:    "Kafka retry-tier partition pause duration.",
			Buckets: []float64{0.1, 1, 5, 30, 120, 600, 1800},
		},
		[]string{"group", "tier", "result"},
	)
	KafkaLocalRetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_local_retry_total",
			Help: "Bounded local Kafka handler retries by registered group and result.",
		},
		[]string{"group", "result"},
	)
	KafkaRecoveryRetainedOffsetGrowth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_retained_offset_growth",
			Help: "Recent retained DLQ end-offset growth by registered topic.",
		},
		[]string{"topic"},
	)
	KafkaRecoveryRetainedEndOffset = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_retained_end_offset",
			Help: "Aggregate end offset for a registered Kafka DLQ topic.",
		},
		[]string{"group", "topic"},
	)
	KafkaRecoveryRetainedRecords = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_retained_records",
			Help: "Estimated retained records for a registered Kafka DLQ topic.",
		},
		[]string{"group", "topic"},
	)
	KafkaRecoveryOldestRecordAgeSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_oldest_record_age_seconds",
			Help: "Age of the oldest retained Kafka DLQ record by registered topic.",
		},
		[]string{"topic"},
	)
	KafkaRecoveryOldestRecordTimestampSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_oldest_record_timestamp_seconds",
			Help: "Timestamp of the oldest retained record for a registered Kafka DLQ topic.",
		},
		[]string{"group", "topic"},
	)
	KafkaRecoveryRetentionRisk = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_retention_risk",
			Help: "Whether the oldest retained Kafka DLQ record is approaching registered expiry.",
		},
		[]string{"topic", "state"},
	)
	KafkaRecoveryReplayTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_recovery_replay_total",
			Help: "Kafka native single-record replay outcomes by registered group.",
		},
		[]string{"group", "result"},
	)
	KafkaRecoveryProgressTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_recovery_progress_total",
			Help: "Durable Kafka recovery progress by registered group and bounded kind.",
		},
		[]string{"group", "kind"},
	)
	KafkaRecoveryInspectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_recovery_inspection_total",
			Help: "Kafka DLQ inspection outcomes.",
		},
		[]string{"result"},
	)
	KafkaRecoveryCollectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_recovery_collection_total",
			Help: "Periodic Kafka DLQ summary collection outcomes.",
		},
		[]string{"result"},
	)
	KafkaRecoveryMetricsStale = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_recovery_metrics_stale",
			Help: "Whether Kafka recovery summary gauges are stale after a failed periodic collection.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		KafkaProduceTotal,
		KafkaProduceDuration,
		KafkaConsumedTotal,
		KafkaConsumeDuration,
		KafkaCommitTotal,
		KafkaRebalanceTotal,
		KafkaConsumerLag,
		KafkaConsumerWorkflowLag,
		KafkaDeliveryDelay,
		KafkaContractFailuresTotal,
		KafkaDataLossTotal,
		KafkaTopologyValidationTotal,
		KafkaConsumerSessionTotal,
		KafkaConsumerSessionHealthy,
		KafkaConsumerWorkflowHealthy,
		KafkaBrokerHealthy,
		KafkaRecoveryPublishTotal,
		KafkaRecoveryDelay,
		KafkaLocalRetryTotal,
		KafkaRecoveryRetainedOffsetGrowth,
		KafkaRecoveryRetainedEndOffset,
		KafkaRecoveryRetainedRecords,
		KafkaRecoveryOldestRecordAgeSeconds,
		KafkaRecoveryOldestRecordTimestampSeconds,
		KafkaRecoveryRetentionRisk,
		KafkaRecoveryReplayTotal,
		KafkaRecoveryProgressTotal,
		KafkaRecoveryInspectionTotal,
		KafkaRecoveryCollectionTotal,
		KafkaRecoveryMetricsStale,
	)
}

type KafkaFailureRecoveryObserver struct{}

func (KafkaFailureRecoveryObserver) ObserveInspection(result string) {
	KafkaRecoveryInspectionTotal.WithLabelValues(
		boundedKafkaLabel(result, "succeeded", "failed"),
	).Inc()
}

func (KafkaFailureRecoveryObserver) ObserveReplay(group, result string) {
	group = kafkaGroupLabel(infrakafka.ConsumerGroupID(group))
	result = boundedKafkaLabel(
		result,
		"succeeded", "failed", "pending",
		"duplicate_succeeded", "duplicate_failed", "duplicate_pending",
		"reconciled_succeeded", "reconciled_failed",
	)
	KafkaRecoveryReplayTotal.WithLabelValues(
		group,
		result,
	).Inc()
	if result == "succeeded" || result == "reconciled_succeeded" {
		KafkaRecoveryProgressTotal.WithLabelValues(group, "replay").Inc()
	}
}

func (KafkaFailureRecoveryObserver) ObserveCollection(result string) {
	result = boundedKafkaLabel(result, "succeeded", "failed")
	KafkaRecoveryCollectionTotal.WithLabelValues(result).Inc()
	if result == "succeeded" {
		KafkaRecoveryMetricsStale.Set(0)
		return
	}
	KafkaRecoveryMetricsStale.Set(1)
}

func (KafkaFailureRecoveryObserver) ObserveTopicSummary(
	summary domainkafkafailure.TopicSummary,
) {
	topic := kafkaResolvedTopicLabel(summary.Topic)
	group := kafkaGroupLabel(infrakafka.ConsumerGroupID(summary.ConsumerGroup))
	growth := summary.EndOffsetGrowth
	if growth < 0 {
		growth = 0
	}
	age := summary.OldestAge.Seconds()
	if age < 0 {
		age = 0
	}
	KafkaRecoveryRetainedOffsetGrowth.WithLabelValues(topic).Set(float64(growth))
	KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic).
		Set(float64(summary.EndOffset))
	KafkaRecoveryRetainedRecords.WithLabelValues(group, topic).
		Set(float64(summary.RetainedEstimate))
	KafkaRecoveryOldestRecordAgeSeconds.WithLabelValues(topic).Set(age)
	oldestTimestamp := 0.0
	if !summary.OldestRecordAt.IsZero() {
		oldestTimestamp = float64(summary.OldestRecordAt.UTC().Unix())
	}
	KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic).
		Set(oldestTimestamp)
	retentionRisk := summary.Retention > 0 &&
		summary.OldestAge >= summary.Retention*8/10
	setKafkaStateGauge(KafkaRecoveryRetentionRisk, []string{topic}, retentionRisk)
}

func setKafkaStateGauge(
	gauge *prometheus.GaugeVec,
	labels []string,
	active bool,
) {
	activeValue := 0.0
	clearValue := 1.0
	if active {
		activeValue = 1
		clearValue = 0
	}
	detectedLabels := append(append([]string(nil), labels...), "detected")
	clearLabels := append(append([]string(nil), labels...), "clear")
	gauge.WithLabelValues(detectedLabels...).Set(activeValue)
	gauge.WithLabelValues(clearLabels...).Set(clearValue)
}

type KafkaObserver struct{}

var kafkaConsumerWorkflowState = struct {
	sync.Mutex
	lag    map[string]map[string]float64
	health map[string]map[string]float64
}{
	lag:    make(map[string]map[string]float64),
	health: make(map[string]map[string]float64),
}

func (KafkaObserver) ObserveBrokerHealth(healthy bool) {
	if healthy {
		KafkaBrokerHealthy.Set(1)
		return
	}
	KafkaBrokerHealthy.Set(0)
}

func (KafkaObserver) ObserveProduce(
	topic infrakafka.TopicID,
	producer infrakafka.ProducerID,
	result string,
	duration time.Duration,
) {
	labels := []string{kafkaTopicLabel(topic), kafkaProducerLabel(producer), kafkaProduceResultLabel(result)}
	KafkaProduceTotal.WithLabelValues(labels...).Inc()
	KafkaProduceDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (KafkaObserver) ObserveConsume(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
	outcome string,
	duration time.Duration,
	delay time.Duration,
) {
	topicLabel := kafkaTopicLabel(topic)
	groupLabel := kafkaGroupLabel(group)
	outcomeLabel := kafkaConsumeOutcomeLabel(outcome)
	KafkaConsumedTotal.WithLabelValues(topicLabel, groupLabel, outcomeLabel).Inc()
	KafkaConsumeDuration.WithLabelValues(topicLabel, groupLabel, outcomeLabel).Observe(duration.Seconds())
	KafkaDeliveryDelay.WithLabelValues(topicLabel, groupLabel).Observe(delay.Seconds())
}

func (KafkaObserver) ObserveCommit(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
	result string,
) {
	KafkaCommitTotal.WithLabelValues(
		kafkaTopicLabel(topic), kafkaGroupLabel(group), kafkaCommitResultLabel(result),
	).Inc()
}

func (KafkaObserver) ObserveRebalance(group infrakafka.ConsumerGroupID, result string) {
	KafkaRebalanceTotal.WithLabelValues(
		kafkaGroupLabel(group), kafkaRebalanceResultLabel(result),
	).Inc()
}

func (KafkaObserver) ObserveContract(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
	code infrakafka.ContractFailureCode,
) {
	KafkaContractFailuresTotal.WithLabelValues(
		kafkaTopicLabel(topic), kafkaGroupLabel(group), kafkaContractCodeLabel(code),
	).Inc()
}

func (KafkaObserver) ObserveLag(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
	stage infrakafka.ConsumerStage,
	lag int64,
) {
	ObserveKafkaLag(topic, group, stage, lag)
}

func (KafkaObserver) ObserveDataLoss(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
) {
	KafkaDataLossTotal.WithLabelValues(
		kafkaTopicLabel(topic),
		kafkaGroupLabel(group),
	).Inc()
}

func (KafkaObserver) ObserveTopology(topic infrakafka.TopicID, result string) {
	KafkaTopologyValidationTotal.WithLabelValues(
		kafkaTopicLabel(topic), kafkaTopologyResultLabel(result),
	).Inc()
}

func (KafkaObserver) ObserveConsumerSession(
	group infrakafka.ConsumerGroupID,
	stage infrakafka.ConsumerStage,
	result string,
) {
	groupLabel := kafkaGroupLabel(group)
	stageLabel := kafkaConsumerStageLabel(stage)
	resultLabel := kafkaConsumerSessionResultLabel(result)
	KafkaConsumerSessionTotal.WithLabelValues(groupLabel, stageLabel, resultLabel).Inc()
	switch resultLabel {
	case "started":
		KafkaConsumerSessionHealthy.WithLabelValues(groupLabel, stageLabel).Set(1)
		updateKafkaWorkflowHealth(groupLabel, stageLabel, 1)
	case "retryable_failure", "fatal_failure", "rebalance_restart", "stopped":
		KafkaConsumerSessionHealthy.WithLabelValues(groupLabel, stageLabel).Set(0)
		updateKafkaWorkflowHealth(groupLabel, stageLabel, 0)
	}
}

func (KafkaObserver) ObserveRecoveryPublish(
	group infrakafka.ConsumerGroupID,
	destination, result string,
) {
	KafkaRecoveryPublishTotal.WithLabelValues(
		kafkaGroupLabel(group),
		boundedKafkaLabel(destination, "retry_5s", "retry_30s", "retry_2m", "retry_10m", "retry_30m", "dlq"),
		boundedKafkaLabel(result, "acknowledged", "failed", "uncertain"),
	).Inc()
}

func (KafkaObserver) ObserveRecoveryDelay(
	group infrakafka.ConsumerGroupID,
	tier, result string,
	duration time.Duration,
) {
	KafkaRecoveryDelay.WithLabelValues(
		kafkaGroupLabel(group),
		boundedKafkaLabel(tier, "5s", "30s", "2m", "10m", "30m"),
		boundedKafkaLabel(result, "ready", "resumed", "canceled", "revoked", "lost"),
	).Observe(duration.Seconds())
}

func (KafkaObserver) ObserveLocalRetry(group infrakafka.ConsumerGroupID, result string) {
	KafkaLocalRetryTotal.WithLabelValues(
		kafkaGroupLabel(group),
		boundedKafkaLabel(result, "attempted"),
	).Inc()
}

func (KafkaObserver) ObserveRecoveryProgress(
	group infrakafka.ConsumerGroupID,
	kind string,
) {
	KafkaRecoveryProgressTotal.WithLabelValues(
		kafkaGroupLabel(group),
		boundedKafkaLabel(kind, "durable"),
	).Inc()
}

func ObserveKafkaLag(
	topic infrakafka.TopicID,
	group infrakafka.ConsumerGroupID,
	stage infrakafka.ConsumerStage,
	lag int64,
) {
	if lag < 0 {
		lag = 0
	}
	groupLabel := kafkaGroupLabel(group)
	stageLabel := kafkaConsumerStageLabel(stage)
	value := float64(lag)
	KafkaConsumerLag.WithLabelValues(
		kafkaTopicLabel(topic), groupLabel, stageLabel,
	).Set(value)
	updateKafkaWorkflowLag(groupLabel, stageLabel, value)
}

func updateKafkaWorkflowLag(group, stage string, value float64) {
	kafkaConsumerWorkflowState.Lock()
	defer kafkaConsumerWorkflowState.Unlock()
	stages := kafkaConsumerWorkflowState.lag[group]
	if stages == nil {
		stages = make(map[string]float64)
		kafkaConsumerWorkflowState.lag[group] = stages
	}
	stages[stage] = value
	total := 0.0
	for _, current := range stages {
		total += current
	}
	KafkaConsumerWorkflowLag.WithLabelValues(group).Set(total)
}

func updateKafkaWorkflowHealth(group, stage string, value float64) {
	kafkaConsumerWorkflowState.Lock()
	defer kafkaConsumerWorkflowState.Unlock()
	stages := kafkaConsumerWorkflowState.health[group]
	if stages == nil {
		stages = make(map[string]float64)
		kafkaConsumerWorkflowState.health[group] = stages
	}
	stages[stage] = value
	healthy := 1.0
	for _, current := range stages {
		if current == 0 {
			healthy = 0
			break
		}
	}
	KafkaConsumerWorkflowHealthy.WithLabelValues(group).Set(healthy)
}

func kafkaTopicLabel(value infrakafka.TopicID) string {
	for _, spec := range infrakafka.Topics() {
		if spec.ID == value {
			return string(value)
		}
	}
	return "unknown"
}

func kafkaResolvedTopicLabel(value string) string {
	value = strings.TrimSpace(value)
	for _, spec := range infrakafka.Topics() {
		if value == string(spec.ID) || strings.HasSuffix(value, "."+spec.BaseName) ||
			value == spec.BaseName {
			return string(spec.ID)
		}
	}
	return "unknown"
}

func kafkaProducerLabel(value infrakafka.ProducerID) string {
	switch value {
	case infrakafka.ProducerPlatformAPI, infrakafka.ProducerPlatformWorker,
		infrakafka.ProducerInteractionAPI, infrakafka.ProducerExposureWorker,
		infrakafka.ProducerVideoWorker, infrakafka.ProducerMediaAPI:
		return string(value)
	default:
		return "unknown"
	}
}

func kafkaGroupLabel(value infrakafka.ConsumerGroupID) string {
	for _, spec := range infrakafka.ConsumerGroups() {
		if spec.ID == value {
			return string(value)
		}
	}
	return "unknown"
}

func kafkaConsumerStageLabel(value infrakafka.ConsumerStage) string {
	if value == infrakafka.ConsumerStageSource {
		return string(value)
	}
	for _, recovery := range infrakafka.Recoveries() {
		for _, tier := range recovery.RetryTiers {
			if value == infrakafka.ConsumerStage("retry_"+tier.Label) {
				return string(value)
			}
		}
	}
	return "unknown"
}

func kafkaProduceResultLabel(value string) string {
	return boundedKafkaLabel(value, "acknowledged", "failed", "uncertain", "canceled", "contract")
}

func kafkaConsumeOutcomeLabel(value string) string {
	return boundedKafkaLabel(
		value,
		"durable_success", "terminal", "retryable", "terminal_contract",
		"recovery_invalid", "routed_dlq", "routed_retry", "recovery_publish_failed",
		"routed_quarantine", "recovery_quarantine_failed",
	)
}

func kafkaCommitResultLabel(value string) string {
	return boundedKafkaLabel(value, "success", "uncertain")
}

func kafkaRebalanceResultLabel(value string) string {
	return boundedKafkaLabel(value, "assigned", "revoked", "lost")
}

func kafkaContractCodeLabel(value infrakafka.ContractFailureCode) string {
	return boundedKafkaLabel(string(value),
		string(infrakafka.ContractMalformedJSON),
		string(infrakafka.ContractTrailingData),
		string(infrakafka.ContractOversizedRecord),
		string(infrakafka.ContractUnknownEvent),
		string(infrakafka.ContractUnsupportedVersion),
		string(infrakafka.ContractInvalidEnvelope),
		string(infrakafka.ContractInvalidKey),
		string(infrakafka.ContractInvalidPayload),
	)
}

func kafkaTopologyResultLabel(value string) string {
	return boundedKafkaLabel(value, "valid", "provisioned", "missing", "invalid", "provision_failed", "broker_error")
}

func kafkaConsumerSessionResultLabel(value string) string {
	return boundedKafkaLabel(
		value,
		"started", "retryable_failure", "fatal_failure", "rebalance_restart", "stopped",
	)
}

func boundedKafkaLabel(value string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "unknown"
}
