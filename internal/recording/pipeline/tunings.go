package pipeline

import (
	"fmt"
	"os"
)

// PipelineTuning applies Phase 4.11 performance parameters to the GStreamer string
type PipelineTuning struct {
	QueueMaxTimeNs     int64
	FragmentDurationMs int
	RtspLatencyMs      int
}

// DefaultHighPerfTuning returns the heavily optimized 128-cam scaling defaults
func DefaultHighPerfTuning() PipelineTuning {
	// Silence GStreamer logs (Hotspot Fix #1)
	if os.Getenv("GST_DEBUG") == "" {
		os.Setenv("GST_DEBUG", "1")
	}

	return PipelineTuning{
		QueueMaxTimeNs:     2000000000, // 2 seconds max memory buffer
		FragmentDurationMs: 1000,       // 1 second fragments (<2s Live-to-File latency)
		RtspLatencyMs:      200,        // Low ingest latency
	}
}

// BuildOptimizedPipeline constructs a zero-copy, highly scalable recording pipeline
func BuildOptimizedPipeline(camID string, rtspUrl string, t PipelineTuning) string {
	// 1. rtspsrc with low latency
	// 2. queue with strict time bounds (Memory bounds Fix)
	// 3. parse without decoding (CPU Fix)
	// 4. mp4mux with fragmented output (Latency & CPU Reallocation Fix)

	return fmt.Sprintf(`
		rtspsrc location="%s" latency=%d drop-on-latency=true ! 
		rtph265depay ! 
		h265parse ! 
		queue max-size-time=%d max-size-bytes=0 max-size-buffers=0 leaky=downstream ! 
		mp4mux fragment-duration=%d ! 
		appsink name=sink_%s sync=false async=false max-buffers=10 drop=true
	`,
		rtspUrl,
		t.RtspLatencyMs,
		t.QueueMaxTimeNs,
		t.FragmentDurationMs,
		camID)
}
