#include "SegmentWriter.h"
#include "FileSync.h"
#include <iostream>
#include <filesystem>
#include <chrono>
#include <iomanip>
#include <sstream>
#include <ctime>
#include <fstream>

namespace fs = std::filesystem;

namespace ts {
namespace vms {
namespace recording {

static gchar* format_location_cb(GstElement* splitmux, guint fragment_id, gpointer user_data) {
    SegmentWriter* writer = static_cast<SegmentWriter*>(user_data);
    return writer->FormatLocation(fragment_id);
}

static gboolean bus_call(GstBus* bus, GstMessage* msg, gpointer user_data) {
    SegmentWriter* writer = static_cast<SegmentWriter*>(user_data);
    writer->HandleBusMessage(msg);
    return TRUE;
}

SegmentWriter::SegmentWriter() {}
SegmentWriter::~SegmentWriter() { Stop(); }

bool SegmentWriter::Start(const std::string& camera_id, const std::string& rtsp_url, const std::string& out_dir, const WriterOptions& opts) {
    camera_id_ = camera_id;
    out_dir_ = out_dir;
    opts_ = opts;

    fs::create_directories(out_dir_);

    std::string protocols = opts.prefer_tcp ? "tcp" : "udp";
    
    // Phase 4.3 Pipeline: Ingest -> Depay -> Parse -> SplitMuxSink (No Transcode)
    std::ostringstream pipe_str;
    pipe_str << "rtspsrc location=" << rtsp_url 
             << " protocols=" << protocols 
             << " latency=" << opts.latency_ms 
             << " drop-on-latency=true ! "
             << "rtph265depay ! h265parse ! "
             << "splitmuxsink name=smux max-size-time=" 
             << (opts.segment_duration_sec * 1000000000ULL);

    GError* err = nullptr;
    pipeline_ = gst_parse_launch(pipe_str.str().c_str(), &err);
    if (err) {
        if (error_cb_) error_cb_(err->message);
        g_error_free(err);
        return false;
    }

    GstElement* smux = gst_bin_get_by_name(GST_BIN(pipeline_), "smux");
    
    // NEW CODE: Use Matroska for crash-resilient segments
    GstElement* muxer = gst_element_factory_make("matroskamux", "mux");
    // Ensure fragment-duration or streamable properties are set if needed for live-tailing MKV
    g_object_set(G_OBJECT(muxer), "streamable", TRUE, NULL); 
    
    g_object_set(G_OBJECT(smux), "muxer", muxer, NULL);
    g_signal_connect(smux, "format-location", G_CALLBACK(format_location_cb), this);
    gst_object_unref(smux);

    bus_ = gst_element_get_bus(pipeline_);
    bus_watch_id_ = gst_bus_add_watch(bus_, bus_call, this);

    gst_element_set_state(pipeline_, GST_STATE_PLAYING);
    std::cout << "[SegmentWriter] Started for " << camera_id << std::endl;
    return true;
}

void SegmentWriter::Stop() {
    if (pipeline_) {
        // Send EOS to cleanly finalize the currently open segment
        gst_element_send_event(pipeline_, gst_event_new_eos());
        
        // Wait briefly for EOS to propagate and finalize
        GstMessage* msg = gst_bus_timed_pop_filtered(bus_, 2 * GST_SECOND, 
            (GstMessageType)(GST_MESSAGE_EOS | GST_MESSAGE_ERROR));
        if (msg) gst_message_unref(msg);

        gst_element_set_state(pipeline_, GST_STATE_NULL);
        gst_object_unref(pipeline_);
        pipeline_ = nullptr;
    }
    if (bus_watch_id_ > 0) {
        g_source_remove(bus_watch_id_);
        bus_watch_id_ = 0;
    }
    if (bus_) {
        gst_object_unref(bus_);
        bus_ = nullptr;
    }
}

gchar* SegmentWriter::FormatLocation(guint fragment_id) {
    auto now = std::chrono::system_clock::now();
    std::time_t now_c = std::chrono::system_clock::to_time_t(now);
    std::stringstream ss;
    ss << std::put_time(std::gmtime(&now_c), "%Y%m%dT%H%M%SZ");
    
    std::string filename = camera_id_ + "_" + ss.str() + "_" + 
                           std::to_string(opts_.segment_duration_sec) + "_" + 
                           std::to_string(fragment_id) + opts_.tmp_ext;
                           
    fs::path full_path = fs::path(out_dir_) / filename;
    return g_strdup(full_path.string().c_str());
}

void SegmentWriter::HandleBusMessage(GstMessage* msg) {
    switch (GST_MESSAGE_TYPE(msg)) {
        case GST_MESSAGE_ERROR: {
            GError* err = nullptr;
            gchar* debug = nullptr;
            gst_message_parse_error(msg, &err, &debug);
            if (error_cb_) error_cb_(err->message);
            g_error_free(err);
            g_free(debug);
            break;
        }
        case GST_MESSAGE_ELEMENT: {
            const GstStructure* s = gst_message_get_structure(msg);
            if (gst_structure_has_name(s, "splitmuxsink-fragment-closed")) {
                const gchar* location = gst_structure_get_string(s, "location");
                if (location) {
                    FinalizeSegment(location);
                }
            }
            break;
        }
        default:
            break;
    }
}

void SegmentWriter::FinalizeSegment(const std::string& tmp_path) {
    fs::path tmp(tmp_path);
    if (!fs::exists(tmp)) return;

    // 1. Force flush to disk to guarantee data persistence
    if (!FileSync::FlushToDisk(tmp.string())) {
        std::cerr << "[SegmentWriter] CRITICAL: Failed to flush " << tmp.string() << " to disk!" << std::endl;
        // Keep as .tmp, do not rename if we can guarantee flush
        return;
    }

    // 2. Atomic Rename
    fs::path final_path = tmp;
    final_path.replace_extension(opts_.final_ext);
    
    std::error_code ec;
    fs::rename(tmp, final_path, ec);
    if (ec) {
        std::cerr << "[SegmentWriter] Failed to rename segment: " << ec.message() << std::endl;
        return;
    }

    // 3. Optional Checksum
    std::string checksum = "";
    if (opts_.enable_checksum) {
        checksum = FileSync::ComputeChecksum(final_path.string());
        std::ofstream sig_file(final_path.string() + ".sha256");
        sig_file << checksum;
    }

    uint64_t size = fs::file_size(final_path);
    
    std::cout << "[SegmentWriter] Finalized: " << final_path.filename() << " (" << size << " bytes)" << std::endl;
    
    if (segment_cb_) {
        segment_cb_(final_path.string(), size, checksum);
    }
}

}
}
}
