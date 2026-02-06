package metrics

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/technosupport/ts-vms/gen/go/media/v1"
)

// Config holds dependencies for the collector
type Config struct {
	MediaClient pb.MediaServiceClient
	SfuURL      string
	SfuSecret   string
	MaxCameras  int
	PerCamera   bool
}

// Collector manages metric aggregation and exposure
type Collector struct {
	config   Config
	registry *prometheus.Registry

	mu           sync.RWMutex
	lastSnapshot time.Time

	// Metrics
	up          *prometheus.GaugeVec
	snapshotAge *prometheus.GaugeVec

	// Media Plane
	mediaIngestLatency   *prometheus.GaugeVec // per camera (if enabled)
	mediaFramesProcessed *prometheus.GaugeVec
	mediaFramesDropped   *prometheus.GaugeVec
	mediaBitrate         *prometheus.GaugeVec
	mediaBytesIn         *prometheus.GaugeVec
	mediaRestarts        *prometheus.GaugeVec
	mediaActive          prometheus.Gauge

	// SFU
	sfuRooms      prometheus.Gauge
	sfuViewers    prometheus.Gauge
	sfuProducers  prometheus.Gauge
	sfuConsumers  prometheus.Gauge
	sfuTransports prometheus.Gauge
	sfuBytesIn    prometheus.Gauge
	sfuBytesOut   prometheus.Gauge
}

func NewCollector(cfg Config) *Collector {
	reg := prometheus.NewRegistry()

	c := &Collector{
		config:   cfg,
		registry: reg,
	}

	// Meta
	c.up = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_metrics_up",
		Help: "Status of backend components (1=up, 0=down)",
	}, []string{"component"})
	reg.MustRegister(c.up)

	c.snapshotAge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_metrics_snapshot_age_seconds",
		Help: "Age of the last successful scrape loop",
	}, []string{"component"})
	reg.MustRegister(c.snapshotAge)

	// Media Plane Global
	c.mediaActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "vms_media_pipelines_active",
		Help: "Total number of active ingest pipelines",
	})
	reg.MustRegister(c.mediaActive)

	c.mediaBytesIn = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_bytes_in_total",
		Help: "Total bytes received by Media Plane",
	}, []string{"camera_id"})
	// Register conditionally if PerCamera?
	// The requirement is to control cardinality.
	// If PerCamera is FALSE, we shouldn't use camera_id label or only use "total".
	// Implementation: Always register Vec, but use "aggregated" label if disabled?
	// Or use separate metrics.
	// For simplicity, we use the Vec but only populate distinct labels if enabled.
	reg.MustRegister(c.mediaBytesIn)

	// Standard metrics
	c.mediaIngestLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_ingest_latency_ms",
		Help: "Approximate ingest latency in ms",
	}, []string{"camera_id"})
	reg.MustRegister(c.mediaIngestLatency)

	// ... (Other metrics registration)
	c.mediaFramesDropped = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_frames_dropped_total",
		Help: "Total frames dropped",
	}, []string{"camera_id"})
	reg.MustRegister(c.mediaFramesDropped)

	c.mediaFramesProcessed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_frames_processed_total",
		Help: "Total frames processed",
	}, []string{"camera_id"})
	reg.MustRegister(c.mediaFramesProcessed)

	c.mediaRestarts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_restarts_total",
		Help: "Pipeline restart count",
	}, []string{"camera_id"})
	reg.MustRegister(c.mediaRestarts)

	c.mediaBitrate = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vms_media_bitrate_bps",
		Help: "Ingest bitrate in bps",
	}, []string{"camera_id"})
	reg.MustRegister(c.mediaBitrate)

	// SFU
	c.sfuRooms = prometheus.NewGauge(prometheus.GaugeOpts{Name: "vms_sfu_rooms", Help: "Total SFU rooms"})
	reg.MustRegister(c.sfuRooms)
	c.sfuViewers = prometheus.NewGauge(prometheus.GaugeOpts{Name: "vms_sfu_viewers", Help: "Total SFU viewers"})
	reg.MustRegister(c.sfuViewers)

	// Register others...
	c.sfuBytesIn = prometheus.NewGauge(prometheus.GaugeOpts{Name: "vms_sfu_bytes_in_total", Help: "Total SFU bytes in"})
	reg.MustRegister(c.sfuBytesIn)

	c.sfuBytesOut = prometheus.NewGauge(prometheus.GaugeOpts{Name: "vms_sfu_bytes_out_total", Help: "Total SFU bytes out"})
	reg.MustRegister(c.sfuBytesOut)

	return c
}

func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{})
}

func (c *Collector) collect() {
	var wg sync.WaitGroup
	wg.Add(2)

	go c.collectMedia(&wg)
	go c.collectSFU(&wg)

	wg.Wait()

	c.mu.Lock()
	c.lastSnapshot = time.Now()
	c.mu.Unlock()
}

func (c *Collector) collectMedia(wg *sync.WaitGroup) {
	defer wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.config.MediaClient.ListIngests(ctx, &pb.ListIngestsRequest{})
	if err != nil {
		c.up.WithLabelValues("media").Set(0)
		return
	}
	c.up.WithLabelValues("media").Set(1)
	c.snapshotAge.WithLabelValues("media").Set(0) // Reset age

	// Update metrics
	c.mediaActive.Set(float64(len(resp.Ingests)))

	for _, ingest := range resp.Ingests {
		label := ingest.CameraId
		if !c.config.PerCamera {
			continue
		}

		c.mediaIngestLatency.WithLabelValues(label).Set(float64(ingest.IngestLatencyMs))
		c.mediaBytesIn.WithLabelValues(label).Set(float64(ingest.BytesInTotal))
		c.mediaFramesProcessed.WithLabelValues(label).Set(float64(ingest.FramesProcessed))
		c.mediaFramesDropped.WithLabelValues(label).Set(float64(ingest.FramesDropped))
		c.mediaRestarts.WithLabelValues(label).Set(float64(ingest.PipelineRestartsTotal))
		c.mediaBitrate.WithLabelValues(label).Set(float64(ingest.BitrateBps))
	}
}

func (c *Collector) collectSFU(wg *sync.WaitGroup) {
	defer wg.Done()

	client := http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("GET", c.config.SfuURL+"/stats", nil)
	req.Header.Set("X-Internal-Auth", c.config.SfuSecret)

	resp, err := client.Do(req)
	if err != nil {
		c.up.WithLabelValues("sfu").Set(0)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.up.WithLabelValues("sfu").Set(0)
		return
	}

	// Parse JSON
	var stats struct {
		Totals struct {
			Rooms    int   `json:"rooms"`
			Viewers  int   `json:"viewers"`
			BytesIn  int64 `json:"bytes_in"`
			BytesOut int64 `json:"bytes_out"`
		} `json:"totals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err == nil {
		c.up.WithLabelValues("sfu").Set(1)
		c.sfuRooms.Set(float64(stats.Totals.Rooms))
		c.sfuViewers.Set(float64(stats.Totals.Viewers))
		c.sfuBytesIn.Set(float64(stats.Totals.BytesIn))
		c.sfuBytesOut.Set(float64(stats.Totals.BytesOut))
	} else {
		log.Printf("Failed to decode SFU stats: %v", err)
	}
}
