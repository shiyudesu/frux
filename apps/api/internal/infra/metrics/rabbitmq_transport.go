package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RabbitMQTransportHealthy = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "rabbitmq_transport_healthy",
			Help:      "Whether the supervised RabbitMQ transport is connected.",
		},
	)
	RabbitMQReconnectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "rabbitmq_reconnect_total",
			Help:      "Supervised RabbitMQ connection outcomes.",
		},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(RabbitMQTransportHealthy, RabbitMQReconnectTotal)
}

func ObserveRabbitMQTransport(healthy bool) {
	if healthy {
		RabbitMQTransportHealthy.Set(1)
		RabbitMQReconnectTotal.WithLabelValues("success").Inc()
		return
	}
	RabbitMQTransportHealthy.Set(0)
}

func ObserveRabbitMQReconnectFailure() {
	RabbitMQReconnectTotal.WithLabelValues("failure").Inc()
	RabbitMQTransportHealthy.Set(0)
}
