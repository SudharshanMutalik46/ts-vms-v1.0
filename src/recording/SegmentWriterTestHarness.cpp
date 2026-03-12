#include "SegmentWriter.h"
#include "StartupScanner.h"

#include <filesystem>
#include <gst/gst.h>
#include <iostream>
#include <thread>

using namespace ts::vms::recording;
namespace fs = std::filesystem;

int main(int argc, char* argv[]) {
    gst_init(&argc, &argv);

    if (argc < 2) {
        std::cout << "Usage: vms-segment-harness <scan|record> [out_dir]\n";
        return 1;
    }

    const std::string mode = argv[1];
    const std::string out_dir = (argc > 2) ? argv[2] : "./test_segments";

    if (mode == "scan") {
        std::cout << "--- Running Startup Scanner ---\n";
        // StartupScanner::ScanAndClean(path, retention_days)
        auto report = StartupScanner::ScanAndClean(out_dir, 1);
        std::cout << "TMP Deleted: " << report.tmp_deleted << "\n";
        std::cout << "Video Quarantined: " << report.video_quarantined << "\n";
        for (const auto& p : report.affected_paths) {
            std::cout << "  " << p << "\n";
        }
        return 0;
    }

    if (mode == "record") {
        std::cout << "--- Running Segment Writer (MKV archive mode) ---\n";

        SegmentWriterOptions opts;
        opts.base_path = out_dir;
        opts.segment_duration_sec = 5;
        opts.final_ext = ".mkv";
        opts.enable_checksum = true;

        SegmentWriter writer("cam_test", opts);

        writer.SetCallbacks(
            [](const FinalizedSegmentInfo& info) {
                std::cout << ">> Finalized callback: " << info.final_path
                          << " | size=" << info.size_bytes
                          << " | checksum=" << info.checksum_sha256 << "\n";
            },
            [](const FinalizedSegmentInfo& info) {
                std::cout << ">> ArchiveIndex accepted: " << info.final_path
                          << " | container=" << info.container
                          << " | checksum=" << info.checksum_sha256 << "\n";
                return true;
            },
            [](const std::string& err) {
                std::cerr << ">> Pipeline Error: " << err << "\n";
            }
        );

        // We need a pipeline for splitmuxsink
        std::string pipeline_str = 
            "videotestsrc is-live=true ! openh264enc ! h264parse ! "
            "splitmuxsink name=sink muxer=matroskamux";
        
        GError* err = nullptr;
        GstElement* pipeline = gst_parse_launch(pipeline_str.c_str(), &err);
        if (err) {
            std::cerr << "Pipeline Parse Error: " << err->message << "\n";
            g_error_free(err);
            return 2;
        }

        if (!writer.Start(pipeline)) {
            std::cerr << "Failed to start segment writer.\n";
            gst_object_unref(pipeline);
            return 2;
        }

        gst_element_set_state(pipeline, GST_STATE_PLAYING);

        std::cout << "Recording for 12 seconds...\n";
        std::this_thread::sleep_for(std::chrono::seconds(60));

        writer.Stop();
        gst_element_set_state(pipeline, GST_STATE_NULL);
        gst_object_unref(pipeline);

        std::cout << "--- Files in output directory ---\n";
        if (fs::exists(out_dir)) {
            for (const auto& entry : fs::directory_iterator(out_dir)) {
                std::cout << "  " << entry.path().filename().string() << "\n";
            }
        }

        return 0;
    }

    std::cerr << "Unknown mode: " << mode << "\n";
    return 1;
}
