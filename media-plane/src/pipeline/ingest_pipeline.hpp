#pragma once
#include <gst/gst.h>
#include <string>
#include <memory>
#include <mutex>
#include <chrono>
#include <atomic>
#include "pipeline_fsm.hpp"

namespace ts::vms::media::pipeline {

struct PipelineConfig {
    std::string camera_id;
    std::string rtsp_url;
    bool prefer_tcp = false;
};

class IngestPipeline {
public:
    IngestPipeline(const PipelineConfig& config);
    ~IngestPipeline();

    bool Start();
    void Stop();

    State GetState() const;
    double GetFps() const;
    int64_t GetLastFrameTimeMs() const;

    // HLS (Phase 3.2+)
    struct HlsConfig {
        bool enabled = true;
        std::string root_dir = "C:\\ProgramData\\TechnoSupport\\VMS\\hls";
        uint32_t segment_duration_sec = 1;
        uint32_t playlist_length = 10;
        double partial_duration_sec = 0.2;
    };

    struct HlsState {
        std::string session_id;
        std::string dir_path;
        bool degraded = false; 
        std::string last_error;
    };

    HlsState GetHlsState() const;
    void SetHlsDegraded(bool degraded, const std::string& error = "");

    // Snapshot (Phase 2.x/3.x)
    std::optional<std::vector<uint8_t>> CaptureSnapshot();

    // SFU Egress (Phase 3.4)
    struct SfuConfig {
        std::string dst_ip;
        int dst_port;
        uint32_t ssrc;
        uint32_t pt;
    };
    bool StartSfuRtpEgress(const SfuConfig& config);
    void StopSfuRtpEgress();
    bool IsSfuEgressRunning() const;
    
    // Metrics (Phase 3.5)
    struct Metrics {
        int64_t ingest_latency_ms = 0;
        int64_t frames_processed = 0;
        int64_t frames_dropped = 0;
        int64_t bitrate_bps = 0;
        uint64_t bytes_in_total = 0;
        uint32_t pipeline_restarts_total = 0;
        uint64_t last_frame_ts_ms = 0;
    };
    Metrics GetMetrics() const;

private:
    static void OnPadAdded(GstElement* src, GstPad* pad, gpointer data);
    static GstFlowReturn OnNewSample(GstElement* sink, gpointer data);
    static gboolean OnBusMessage(GstBus* bus, GstMessage* msg, gpointer data);

    void HandleStall();
    void SetupPipeline();
    void CleanupPipeline();

    PipelineConfig config_;
    PipelineFSM fsm_;

    GstElement* pipeline_ = nullptr;
    GstElement* source_ = nullptr;
    GstElement* depay_ = nullptr;
    GstElement* parse_ = nullptr;
    GstElement* tee_ = nullptr;
    GstElement* appsink_ = nullptr;
    
    std::mutex data_mutex_;
    std::chrono::steady_clock::time_point last_frame_ts_;
    uint64_t frame_count_ = 0;
    double fps_ = 0.0;
    std::chrono::steady_clock::time_point last_fps_calc_ts_;
    uint64_t last_fps_frame_count_ = 0;

    guint bus_watch_id_ = 0;

    // HLS Elements
    GstElement* hls_sink_ = nullptr;
    GstElement* hls_queue_ = nullptr;
    HlsConfig hls_config_;
    HlsState hls_state_;

    // SFU Elements (Phase 3.4)
    GstElement* sfu_queue_ = nullptr;
    GstElement* sfu_pay_ = nullptr;
    GstElement* sfu_sink_ = nullptr;
    SfuConfig sfu_config_;
    bool sfu_egress_running_ = false;

    // HLS Helpers
    void SetupHlsBranch();
    void CreateHlsSession();
    void UpdateMetaJson();

    enum class CodecType { UNKNOWN, H264, H265 };
    CodecType codec_type_ = CodecType::UNKNOWN;

    // Metrics Counters (Atomic)
    std::atomic<int64_t> metrics_frames_processed_{0};
    std::atomic<int64_t> metrics_frames_dropped_{0};
    std::atomic<int64_t> metrics_bitrate_bps_{0}; 
    std::atomic<uint64_t> metrics_bytes_in_total_{0};
    std::atomic<uint32_t> metrics_restarts_total_{0};
    std::atomic<uint64_t> metrics_last_frame_unix_ms_{0};
    std::atomic<int64_t> metrics_ingest_latency_ms_{0};
};

} // namespace ts::vms::media::pipeline
