#pragma once
#include "ts/vms/media/v1/media.grpc.pb.h"
#include "service/ingest_manager.hpp"

namespace ts::vms::media::service {

class MediaServiceImpl final : public ts::vms::media::v1::MediaService::Service {
public:
    explicit MediaServiceImpl(std::shared_ptr<IngestManager> manager);

    grpc::Status StartIngest(grpc::ServerContext* context,
                             const ts::vms::media::v1::StartIngestRequest* request,
                             ts::vms::media::v1::StartIngestResponse* response) override;

    grpc::Status StopIngest(grpc::ServerContext* context,
                            const ts::vms::media::v1::StopIngestRequest* request,
                            ts::vms::media::v1::StopIngestResponse* response) override;

    grpc::Status GetIngestStatus(grpc::ServerContext* context,
                                 const ts::vms::media::v1::GetIngestStatusRequest* request,
                                 ts::vms::media::v1::GetIngestStatusResponse* response) override;

    grpc::Status ListIngests(grpc::ServerContext* context,
                             const ts::vms::media::v1::ListIngestsRequest* request,
                             ts::vms::media::v1::ListIngestsResponse* response) override;

    grpc::Status CaptureSnapshot(grpc::ServerContext* context,
                                 const ts::vms::media::v1::CaptureSnapshotRequest* request,
                                 ts::vms::media::v1::CaptureSnapshotResponse* response) override;

    grpc::Status Health(grpc::ServerContext* context,
                        const ts::vms::media::v1::HealthRequest* request,
                        ts::vms::media::v1::HealthResponse* response) override;

    // Phase 3.4 SFU
    grpc::Status StartSfuRtpEgress(grpc::ServerContext* context,
                                   const ts::vms::media::v1::StartSfuRtpEgressRequest* request,
                                   ts::vms::media::v1::StartSfuRtpEgressResponse* response) override;

    grpc::Status StopSfuRtpEgress(grpc::ServerContext* context,
                                  const ts::vms::media::v1::StopSfuRtpEgressRequest* request,
                                  ts::vms::media::v1::StopSfuRtpEgressResponse* response) override;

private:
    std::shared_ptr<IngestManager> manager_;
};

} // namespace ts::vms::media::service
