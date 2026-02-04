#pragma once
#include <prometheus/registry.h>
#include <prometheus/exposer.h>
#include <prometheus/gauge.h>
#include <prometheus/counter.h>
#include <memory>
#include <string>

namespace ts::vms::media::utils {

class Metrics {
public:
    static Metrics& Instance();

    void Init(const std::string& addr);

    prometheus::Gauge& pipelines_active();
    prometheus::Counter& stalls_total();
    prometheus::Counter& reconnects_total();
    prometheus::Gauge& ingest_fps_avg();
    prometheus::Gauge& sfu_egress_active(); // SFU Metric
    prometheus::Counter& errors_total(const std::string& type);

    // HLS Metrics
    prometheus::Gauge& hls_sessions_active();
    prometheus::Counter& hls_segments_written_total();
    prometheus::Counter& hls_parts_written_total();
    prometheus::Counter& hls_playlist_writes_total();
    prometheus::Counter& hls_session_restarts_total(const std::string& reason);
    prometheus::Counter& hls_disk_cleanup_bytes_reclaimed_total();
    prometheus::Counter& hls_disk_cleanup_failures_total();
    prometheus::Counter& hls_write_errors_total(const std::string& type);

private:
    Metrics();
    void EnsureMetricsCreated();
    std::shared_ptr<prometheus::Registry> registry_;
    std::unique_ptr<prometheus::Exposer> exposer_;

    prometheus::Gauge* pipelines_active_ = nullptr;
    prometheus::Counter* stalls_total_ = nullptr;
    prometheus::Counter* reconnects_total_ = nullptr;
    prometheus::Gauge* ingest_fps_avg_ = nullptr;
    prometheus::Gauge* sfu_egress_active_ = nullptr;
    prometheus::Family<prometheus::Counter>* errors_family_ = nullptr;
    
    // HLS Storage
    prometheus::Gauge* hls_sessions_active_ = nullptr;
    prometheus::Counter* hls_segments_written_total_ = nullptr;
    prometheus::Counter* hls_parts_written_total_ = nullptr;
    prometheus::Counter* hls_playlist_writes_total_ = nullptr;
    prometheus::Family<prometheus::Counter>* hls_session_restarts_family_ = nullptr;
    prometheus::Counter* hls_disk_cleanup_bytes_reclaimed_total_ = nullptr;
    prometheus::Counter* hls_disk_cleanup_failures_total_ = nullptr;
    prometheus::Family<prometheus::Counter>* hls_write_errors_family_ = nullptr;
};

} // namespace ts::vms::media::utils
