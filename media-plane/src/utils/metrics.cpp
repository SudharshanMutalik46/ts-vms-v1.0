#include "metrics.hpp"

namespace ts::vms::media::utils {

Metrics& Metrics::Instance() {
    static Metrics instance;
    instance.EnsureMetricsCreated();
    return instance;
}

Metrics::Metrics() : registry_(std::make_shared<prometheus::Registry>()) {}

void Metrics::Init(const std::string& addr) {
    if (exposer_) return;
    exposer_ = std::make_unique<prometheus::Exposer>(addr);
    exposer_->RegisterCollectable(registry_);
}

void Metrics::EnsureMetricsCreated() {
    if (pipelines_active_) return;
    if (!registry_) registry_ = std::make_shared<prometheus::Registry>();

    pipelines_active_ = &prometheus::BuildGauge()
                             .Name("media_pipelines_active")
                             .Help("Number of active ingestion pipelines")
                             .Register(*registry_)
                             .Add({});

    stalls_total_ = &prometheus::BuildCounter()
                         .Name("media_pipeline_stalls_total")
                         .Help("Total number of pipeline stalls detected")
                         .Register(*registry_)
                         .Add({});

    reconnects_total_ = &prometheus::BuildCounter()
                             .Name("media_pipeline_reconnects_total")
                             .Help("Total number of pipeline reconnections triggered")
                             .Register(*registry_)
                             .Add({});

    ingest_fps_avg_ = &prometheus::BuildGauge()
                           .Name("media_ingest_fps_avg")
                           .Help("Average FPS across all active pipelines")
                           .Register(*registry_)
                           .Add({});
    
    // SFU Metrics
    sfu_egress_active_ = &prometheus::BuildGauge()
                              .Name("media_sfu_egress_active")
                              .Help("Number of active SFU RTP egress sessions")
                              .Register(*registry_)
                              .Add({});

    errors_family_ = &prometheus::BuildCounter()
                          .Name("media_errors_total")
                          .Help("Total number of errors by type")
                          .Register(*registry_);

    // HLS Metrics Initialization
    hls_sessions_active_ = &prometheus::BuildGauge()
                                .Name("hls_sessions_active")
                                .Help("Number of active HLS sessions")
                                .Register(*registry_)
                                .Add({});

    hls_segments_written_total_ = &prometheus::BuildCounter()
                                       .Name("hls_segments_written_total")
                                       .Help("Total number of HLS segments written")
                                       .Register(*registry_)
                                       .Add({});

    hls_parts_written_total_ = &prometheus::BuildCounter()
                                    .Name("hls_parts_written_total")
                                    .Help("Total number of HLS partial segments written")
                                    .Register(*registry_)
                                    .Add({});

    hls_playlist_writes_total_ = &prometheus::BuildCounter()
                                      .Name("hls_playlist_writes_total")
                                      .Help("Total number of HLS playlist updates")
                                      .Register(*registry_)
                                      .Add({});

    hls_session_restarts_family_ = &prometheus::BuildCounter()
                                        .Name("hls_session_restarts_total")
                                        .Help("Total number of session restarts")
                                        .Register(*registry_);

    hls_disk_cleanup_bytes_reclaimed_total_ = &prometheus::BuildCounter()
                                                   .Name("hls_disk_cleanup_bytes_reclaimed_total")
                                                   .Help("Total bytes reclaimed by disk cleanup")
                                                   .Register(*registry_)
                                                   .Add({});

    hls_disk_cleanup_failures_total_ = &prometheus::BuildCounter()
                                            .Name("hls_disk_cleanup_failures_total")
                                            .Help("Total number of disk cleanup failures")
                                            .Register(*registry_)
                                            .Add({});

    hls_write_errors_family_ = &prometheus::BuildCounter()
                                    .Name("hls_write_errors_total")
                                    .Help("Total number of HLS write errors")
                                    .Register(*registry_);
}

prometheus::Gauge& Metrics::pipelines_active() { return *pipelines_active_; }
prometheus::Counter& Metrics::stalls_total() { return *stalls_total_; }
prometheus::Counter& Metrics::reconnects_total() { return *reconnects_total_; }
prometheus::Gauge& Metrics::ingest_fps_avg() { return *ingest_fps_avg_; }
prometheus::Gauge& Metrics::sfu_egress_active() { return *sfu_egress_active_; }

prometheus::Counter& Metrics::errors_total(const std::string& type) {
    return errors_family_->Add({{"type", type}});
}

prometheus::Gauge& Metrics::hls_sessions_active() { return *hls_sessions_active_; }
prometheus::Counter& Metrics::hls_segments_written_total() { return *hls_segments_written_total_; }
prometheus::Counter& Metrics::hls_parts_written_total() { return *hls_parts_written_total_; }
prometheus::Counter& Metrics::hls_playlist_writes_total() { return *hls_playlist_writes_total_; }

prometheus::Counter& Metrics::hls_session_restarts_total(const std::string& reason) {
    return hls_session_restarts_family_->Add({{"reason", reason}});
}

prometheus::Counter& Metrics::hls_disk_cleanup_bytes_reclaimed_total() { return *hls_disk_cleanup_bytes_reclaimed_total_; }
prometheus::Counter& Metrics::hls_disk_cleanup_failures_total() { return *hls_disk_cleanup_failures_total_; }

prometheus::Counter& Metrics::hls_write_errors_total(const std::string& type) {
    return hls_write_errors_family_->Add({{"type", type}});
}

} // namespace ts::vms::media::utils
