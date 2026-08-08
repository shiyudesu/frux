package inframetrics

import (
	"strings"
	"time"

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
			Help: "Kafka consumer lag for registered topic and group.",
		},
		[]string{"topic", "group"},
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
	KafkaTopologyValidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "kafka_topology_validation_total",
			Help: "Kafka registered topic provisioning and validation outcomes.",
		},
		[]string{"topic", "result"},
	)
	KafkaBrokerHealthy = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "kafka_broker_healthy",
			Help: "Whether the configured Kafka broker is reachable.",
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
		KafkaDeliveryDelay,
		KafkaContractFailuresTotal,
		KafkaTopologyValidationTotal,
		KafkaBrokerHealthy,
	)
}

type KafkaObserver struct{}

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
	lag int64,
) {
	ObserveKafkaLag(topic, group, lag)
}

func (KafkaObserver) ObserveTopology(topic infrakafka.TopicID, result string) {
	KafkaTopologyValidationTotal.WithLabelValues(
		kafkaTopicLabel(topic), kafkaTopologyResultLabel(result),
	).Inc()
}

func ObserveKafkaLag(topic infrakafka.TopicID, group infrakafka.ConsumerGroupID, lag int64) {
	if lag < 0 {
		lag = 0
	}
	KafkaConsumerLag.WithLabelValues(kafkaTopicLabel(topic), kafkaGroupLabel(group)).Set(float64(lag))
}

func kafkaTopicLabel(value infrakafka.TopicID) string {
	for _, spec := range infrakafka.Topics() {
		if spec.ID == value {
			return string(value)
		}
	}
	return "unknown"
}

func kafkaProducerLabel(value infrakafka.ProducerID) string {
	switch value {
	case infrakafka.ProducerPlatformAPI, infrakafka.ProducerPlatformWorker:
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

func kafkaProduceResultLabel(value string) string {
	return boundedKafkaLabel(value, "acknowledged", "failed", "uncertain", "canceled", "contract")
}

func kafkaConsumeOutcomeLabel(value string) string {
	return boundedKafkaLabel(value, "durable_success", "terminal", "retryable", "terminal_contract")
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

func boundedKafkaLabel(value string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "unknown"
}
