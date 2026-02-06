package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Phase 3.8: AI Overlay Metrics
// All metrics are low-cardinality (no camera_id/user_id/session_id labels)

var (
	// AIInferenceTotal counts total inference runs
	AIInferenceTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_inference_total",
			Help: "Total inference runs by stream type",
		},
		[]string{"stream", "label"},
	)

	// AIInferenceLatency tracks inference latency
	AIInferenceLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ai_inference_latency_ms",
			Help:    "Inference latency in milliseconds",
			Buckets: []float64{50, 100, 200, 500, 1000, 2000, 5000},
		},
		[]string{"stream"},
	)

	// AIFramesDroppedTotal counts frames dropped due to overload
	AIFramesDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_frames_dropped_total",
			Help: "Total frames dropped due to overload",
		},
		[]string{"stream"},
	)

	// AIOverlayUpdatesTotal counts overlay updates sent to clients
	AIOverlayUpdatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_overlay_updates_total",
			Help: "Total overlay updates sent to clients",
		},
		[]string{"stream"},
	)

	// AIServiceUp is a gauge for service health
	AIServiceUp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ai_service_up",
			Help: "AI service health status (1=up, 0=down)",
		},
	)
)

// Helper functions for metrics recording

func RecordInference(stream, label string) {
	AIInferenceTotal.WithLabelValues(stream, label).Inc()
}

func RecordInferenceLatency(stream string, latencyMs float64) {
	AIInferenceLatency.WithLabelValues(stream).Observe(latencyMs)
}

func RecordFrameDrop(stream string, count int) {
	AIFramesDroppedTotal.WithLabelValues(stream).Add(float64(count))
}

func RecordOverlayUpdate(stream string) {
	AIOverlayUpdatesTotal.WithLabelValues(stream).Inc()
}

func SetServiceUp(up bool) {
	if up {
		AIServiceUp.Set(1)
	} else {
		AIServiceUp.Set(0)
	}
}
