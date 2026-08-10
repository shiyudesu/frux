package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var VideoEmbeddingVectorsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "video_embedding_vectors_total",
		Help:      "Video embedding outcomes by fixed model and source.",
	},
	[]string{"model", "source", "outcome"},
)

func init() {
	prometheus.MustRegister(VideoEmbeddingVectorsTotal)
}

func ObserveHashVector(outcome string) {
	switch outcome {
	case "generated", "skipped", "failed":
	default:
		outcome = "failed"
	}
	VideoEmbeddingVectorsTotal.WithLabelValues("hash", "event", outcome).Inc()
}
