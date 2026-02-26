#include "SegmentWriter.h"
#include "StartupScanner.h"
#include <gst/gst.h>
#include <iostream>
#include <thread>
#include <filesystem>

using namespace ts::vms::recording;
namespace fs = std::filesystem;

int main(int argc, char* argv[]) {
    gst_init(&argc, &argv);

    if (argc < 2) {
        std::cout << "Usage: vms-segment-harness <scan|record> [out_dir]\n";
        return 1;
    }

    std::string mode = argv[1];
    std::string out_dir = (argc > 2) ? argv[2] : "./test_segments";

    if (mode == "scan") {
        std::cout << "--- Running Startup Scanner ---\n";
        auto report = StartupScanner::ScanAndClean(out_dir, 1); // 1 minute TTL for fast testing
        std::cout << "TMP Deleted: " << report.tmp_deleted << "\n";
        std::cout << "MP4 Quarantined: " << report.mp4_quarantined << "\n";
        for (const auto& p : report.affected_paths) {
            std::cout << "  " << p << "\n";
        }
    } 
    else if (mode == "record") {
        std::cout << "--- Running Segment Writer ---\n";
        
        WriterOptions opts;
        opts.segment_duration_sec = 5; // Very short for testing
        opts.enable_checksum = true;

        SegmentWriter writer;
        writer.OnSegmentFinalized([](const std::string& path, uint64_t size, const std::string& chk) {
            std::cout << ">> Callback Received: " << path << " | Checksum: " << chk << "\n";
        });
        writer.OnError([](const std::string& err) {
            std::cerr << ">> Pipeline Error: " << err << "\n";
        });

        // Use videotestsrc for reliable testing without network
        std::string mock_rtsp = "videotestsrc is-live=true ! x265enc ! rtph265pay ! rtspclientsink"; 
        // For harness simulation, we intercept and override inside SegmentWriter internally if we wanted, 
        // but passing a real RTSP or letting it fail works for structure tests. 
        // Since we want actual files, we assume the user provides a real RTSP url or we rely on the PS1 script.

        writer.Start("cam_test", "rtsp://127.0.0.1:8554/mosaic_8x8", out_dir, opts);

        // Run for 15 seconds (creates ~3 segments)
        GMainLoop* loop = g_main_loop_new(NULL, FALSE);
        std::thread([&]() {
            std::this_thread::sleep_for(std::chrono::seconds(16));
            g_main_loop_quit(loop);
        }).detach();

        g_main_loop_run(loop);
        writer.Stop();
    }

    return 0;
}
