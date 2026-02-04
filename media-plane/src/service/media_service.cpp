#include "service/media_service.hpp"
#include "pipeline/pipeline_fsm.hpp"
#include <spdlog/spdlog.h>

namespace ts::vms::media::service {

MediaServiceImpl::MediaServiceImpl(std::shared_ptr<IngestManager> manager) : manager_(manager) {}

grpc::Status MediaServiceImpl::StartIngest(grpc::ServerContext* /*context*/,
                                          const ts::vms::media::v1::StartIngestRequest* request,
                                          ts::vms::media::v1::StartIngestResponse* response) {
    if (request->camera_id().empty() || request->rtsp_url().empty()) {
        return grpc::Status(grpc::INVALID_ARGUMENT, "camera_id and rtsp_url are required");
    }

    if (manager_->StartIngest(request->camera_id(), request->rtsp_url(), request->prefer_tcp())) {
        response->set_pipeline_id(request->camera_id());
        return grpc::Status::OK;
    }

    return grpc::Status(grpc::RESOURCE_EXHAUSTED, "Failed to start ingest (cap or rate limit)");
}

grpc::Status MediaServiceImpl::StopIngest(grpc::ServerContext* /*context*/,
                                         const ts::vms::media::v1::StopIngestRequest* request,
                                         ts::vms::media::v1::StopIngestResponse* response) {
    if (request->camera_id().empty()) {
        return grpc::Status(grpc::INVALID_ARGUMENT, "camera_id is required");
    }

    manager_->StopIngest(request->camera_id());
    response->set_success(true);
    return grpc::Status::OK;
}

grpc::Status MediaServiceImpl::GetIngestStatus(grpc::ServerContext* /*context*/,
                                              const ts::vms::media::v1::GetIngestStatusRequest* request,
                                              ts::vms::media::v1::GetIngestStatusResponse* response) {
    auto status = manager_->GetStatus(request->camera_id());
    if (!status) {
        return grpc::Status(grpc::NOT_FOUND, "Camera not found");
    }

    response->set_running(status->state == pipeline::State::RUNNING);
    response->set_state(ts::vms::media::pipeline::PipelineFSM::StateToString(status->state));
    response->set_fps(static_cast<int32_t>(status->fps));
    response->set_last_frame_age_ms(status->last_frame_age_ms);
    response->set_reconnect_attempts(status->reconnect_attempts);
    
    // HLS Status
    response->set_session_id(status->hls_state.session_id);
    response->set_hls_state(status->hls_state.degraded ? "DEGRADED" : (status->hls_state.session_id.empty() ? "STOPPED" : "OK"));
    // response->set_last_segment_seq() // Need to add sequence tracking if precise seq is needed, currently only have session
    response->set_recent_error_code(status->hls_state.last_error);
    // response->set_disk_free_bytes() // DiskManager should provide this globally or per session? 
    // Simplified: Provide global free logic or keep 0 if not implemented yet
    response->set_required_action(status->hls_state.degraded ? "Check Disk / Logs" : "");

    return grpc::Status::OK;
}

grpc::Status MediaServiceImpl::ListIngests(grpc::ServerContext* /*context*/,
                                          const ts::vms::media::v1::ListIngestsRequest* /*request*/,
                                          ts::vms::media::v1::ListIngestsResponse* response) {
    auto ingests = manager_->ListIngests();
    for (const auto& s : ingests) {
        auto* item = response->add_ingests();
        item->set_running(s.state == pipeline::State::RUNNING);
        item->set_fps(static_cast<int32_t>(s.fps));
        item->set_last_frame_age_ms(s.last_frame_age_ms);
        item->set_reconnect_attempts(s.reconnect_attempts);
        item->set_session_id(s.hls_state.session_id);
        item->set_hls_state(s.hls_state.degraded ? "DEGRADED" : (s.hls_state.session_id.empty() ? "STOPPED" : "OK"));
    }
    return grpc::Status::OK;
}

grpc::Status MediaServiceImpl::CaptureSnapshot(grpc::ServerContext* /*context*/,
                                              const ts::vms::media::v1::CaptureSnapshotRequest* request,
                                              ts::vms::media::v1::CaptureSnapshotResponse* response) {
    auto snapshot = manager_->CaptureSnapshot(request->camera_id());
    if (!snapshot) {
        return grpc::Status(grpc::NOT_FOUND, "Camera not found or frame unavailable");
    }

    response->set_image_data(snapshot->data.data(), snapshot->data.size());
    response->set_mime_type("image/jpeg");
    response->set_timestamp(snapshot->timestamp);
    return grpc::Status::OK;
}

grpc::Status MediaServiceImpl::Health(grpc::ServerContext* /*context*/,
                                     const ts::vms::media::v1::HealthRequest* /*request*/,
                                     ts::vms::media::v1::HealthResponse* response) {
    response->set_ok(true);
    response->set_status("OK");
    return grpc::Status::OK;
}

grpc::Status MediaServiceImpl::StartSfuRtpEgress(grpc::ServerContext* /*context*/,
                                                const ts::vms::media::v1::StartSfuRtpEgressRequest* request,
                                                ts::vms::media::v1::StartSfuRtpEgressResponse* response) {
    if (request->camera_id().empty() || request->dst_ip().empty() || request->dst_port() == 0) {
        return grpc::Status(grpc::INVALID_ARGUMENT, "Missing mandatory SFU egress parameters");
    }

    auto result = manager_->StartSfuRtpEgress(
        request->camera_id(), request->dst_ip(), request->dst_port(), request->ssrc(), request->pt());
    
    if (result == IngestManager::Result::SUCCESS) {
        return grpc::Status::OK;
    } else if (result == IngestManager::Result::ALREADY_RUNNING) {
        response->set_already_running(true);
        return grpc::Status::OK;
    }

    return grpc::Status(grpc::INTERNAL, "Failed to initialize RTP egress branch");
}

grpc::Status MediaServiceImpl::StopSfuRtpEgress(grpc::ServerContext* /*context*/,
                                               const ts::vms::media::v1::StopSfuRtpEgressRequest* request,
                                               ts::vms::media::v1::StopSfuRtpEgressResponse* response) {
    manager_->StopSfuRtpEgress(request->camera_id());
    response->set_success(true);
    return grpc::Status::OK;
}

} // namespace ts::vms::media::service
