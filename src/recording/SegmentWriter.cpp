#include "SegmentWriter.h"
#include "FileSync.h"

#include <iostream>
#include <filesystem>
#include <fstream>
#include <chrono>
#include <iomanip>
#include <sstream>

namespace {
    std::string ToUtcStamp(std::chrono::system_clock::time_point tp) {
        auto t = std::chrono::system_clock::to_time_t(tp);
        std::stringstream ss;
        ss << std::put_time(std::gmtime(&t), "%Y%m%dT%H%M%SZ");
        return ss.str();
    }
}

namespace ts {
namespace vms {
namespace recording {

SegmentWriter::SegmentWriter(const std::string& camera_id, const SegmentWriterOptions& opts)
    : camera_id_(camera_id), opts_(opts) {}

SegmentWriter::~SegmentWriter() {
    Stop();
}

bool SegmentWriter::Start(GstElement* pipeline) {
    if (!pipeline) return false;
    pipeline_ = pipeline;

    GstElement* sink = gst_bin_get_by_name(GST_BIN(pipeline_), "sink");
    if (!sink) return false;

    g_object_set(sink, "location", (opts_.base_path + "/" + camera_id_ + "_%05d.tmp").c_str(), nullptr);
    g_object_set(sink, "max-size-time", (guint64)opts_.segment_duration_sec * GST_SECOND, nullptr);
    
    g_signal_connect(sink, "format-location", G_CALLBACK(OnFormatLocation), this);

    GstBus* bus = gst_element_get_bus(pipeline_);
    bus_watch_id_ = gst_bus_add_watch(bus, [](GstBus*, GstMessage* msg, gpointer data) -> gboolean {
        static_cast<SegmentWriter*>(data)->HandleBusMessage(msg);
        return TRUE;
    }, this);
    gst_object_unref(bus);
    gst_object_unref(sink);

    return true;
}

void SegmentWriter::Stop() {
    if (bus_watch_id_) {
        g_source_remove(bus_watch_id_);
        bus_watch_id_ = 0;
    }
    pipeline_ = nullptr;
}

GstPadProbeReturn SegmentWriter::OnPadProbe(GstPad*, GstPadProbeInfo* info, gpointer user_data) {
    // Legacy support if needed, otherwise splitmuxsink handles fragments
    return GST_PAD_PROBE_OK;
}

gchar* SegmentWriter::OnFormatLocation(GstElement*, guint fragment_id, gpointer user_data) {
    SegmentWriter* self = static_cast<SegmentWriter*>(user_data);
    namespace fs = std::filesystem;

    auto now = std::chrono::system_clock::now();
    fs::path dir(self->opts_.base_path);
    if (!fs::exists(dir)) fs::create_directories(dir);

    std::string filename = self->camera_id_ + "_" + std::to_string(fragment_id) + ".tmp";
    fs::path full_path = dir / filename;

    {
        std::lock_guard<std::mutex> lock(self->pending_mu_);
        self->pending_fragments_[full_path.string()] = PendingFragment{fragment_id, now};
    }
    return g_strdup(full_path.string().c_str());
}

void SegmentWriter::HandleBusMessage(GstMessage* msg) {
    switch (GST_MESSAGE_TYPE(msg)) {
    case GST_MESSAGE_ERROR: {
        GError* err = nullptr;
        gchar* debug = nullptr;
        gst_message_parse_error(msg, &err, &debug);
        if (error_cb_ && err != nullptr) {
            error_cb_(err->message);
        }
        if (err) g_error_free(err);
        if (debug) g_free(debug);
        break;
    }
    case GST_MESSAGE_ELEMENT: {
        const GstStructure* s = gst_message_get_structure(msg);
        if (s != nullptr && gst_structure_has_name(s, "splitmuxsink-fragment-closed")) {
            const gchar* location = gst_structure_get_string(s, "location");
            if (location != nullptr) {
                FinalizeSegment(location);
            }
        }
        break;
    }
    default:
        break;
    }
}

SegmentWriter::PendingFragment SegmentWriter::TakePendingFragment(const std::string& tmp_path) {
    std::lock_guard<std::mutex> lock(pending_mu_);
    auto it = pending_fragments_.find(tmp_path);
    if (it == pending_fragments_.end()) {
        return PendingFragment{};
    }

    PendingFragment value = it->second;
    pending_fragments_.erase(it);
    return value;
}

std::string SegmentWriter::BuildSegmentId(const PendingFragment& pending) const {
    return camera_id_ + ":" + ToUtcStamp(pending.start_time_utc) + ":" + std::to_string(pending.fragment_id);
}

void SegmentWriter::FinalizeSegment(const std::string& tmp_path) {
    namespace fs = std::filesystem;

    const fs::path tmp(tmp_path);
    if (!fs::exists(tmp)) {
        std::cerr << "[SegmentWriter] Finalize skipped; missing tmp file: " << tmp.string() << '\n';
        return;
    }

    PendingFragment pending = TakePendingFragment(tmp_path);

    if (pending.start_time_utc.time_since_epoch().count() == 0) {
        std::cerr << "[SegmentWriter] Missing pending fragment metadata for "
                  << tmp.string() << '\n';
        return;
    }

    if (!FileSync::FlushToDisk(tmp.string())) {
        std::cerr << "[SegmentWriter] FlushToDisk failed for " << tmp.string() << '\n';
        return;
    }


    fs::path final_path = tmp;
    final_path.replace_extension(opts_.final_ext);

    std::error_code rename_error;
    fs::rename(tmp, final_path, rename_error);
    if (rename_error) {
        std::cerr << "[SegmentWriter] Rename failed for " << tmp.string()
                  << " -> " << final_path.string()
                  << " : " << rename_error.message() << '\n';
        return;
    }

    const std::string checksum = opts_.enable_checksum
        ? FileSync::ComputeChecksum(final_path.string())
        : std::string{};

    if (opts_.enable_checksum && checksum.empty()) {
        std::cerr << "[SegmentWriter] SHA-256 checksum failed for " << final_path.string() << '\n';
        return;
    }

    if (!checksum.empty()) {
        std::ofstream sig_file(final_path.string() + ".sha256", std::ios::trunc);
        if (sig_file.is_open()) {
            sig_file << checksum;
        }
    }

    FinalizedSegmentInfo info;
    info.segment_id = BuildSegmentId(pending);
    info.camera_id = camera_id_;
    info.final_path = final_path.string();
    info.container = "mkv";
    info.checksum_sha256 = checksum;
    info.start_time_utc = pending.start_time_utc;
    info.end_time_utc = std::chrono::system_clock::now();
    info.duration_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        info.end_time_utc - info.start_time_utc).count();
    info.size_bytes = fs::exists(final_path) ? static_cast<uint64_t>(fs::file_size(final_path)) : 0ULL;

    if (archive_index_cb_) {
        const bool indexed = archive_index_cb_(info);
        if (!indexed) {
            std::cerr << "[SegmentWriter] Archive index rejected finalized segment "
                      << info.final_path << '\n';
            return;
        }
    }

    std::cout << "[SegmentWriter] Finalized MKV: "
              << final_path.filename().string()
              << " (" << info.size_bytes << " bytes)" << '\n';

    if (segment_cb_) {
        segment_cb_(info);
    }
}

}
}
}
