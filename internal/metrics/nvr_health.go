package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	NVRsOnline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nvr_health_nvrs_online",
		Help: "Current number of NVRs with status online",
	})

	ChannelsUnreachable = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nvr_channel_health_unreachable_total",
		Help: "Current number of channels unreachable due to NVR offline status",
	})

	NVRQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nvr_health_queue_depth",
		Help: "Number of NVRs waiting for health checks",
	})

	ChannelQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nvr_channel_health_queue_depth",
		Help: "Number of channels waiting for health checks",
	})

	NVRChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nvr_health_checks_total",
		Help: "Total number of NVR health checks",
	}, []string{"result", "reason"})

	ChannelChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nvr_channel_health_checks_total",
		Help: "Total number of channel health checks",
	}, []string{"result", "reason"})
)
