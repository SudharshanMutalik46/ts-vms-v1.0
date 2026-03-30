#include "pipeline/ingest_pipeline.hpp"
#include "utils/logger.hpp"
#include "utils/metrics.hpp"
#include <gst/app/gstappsink.h>
#include <gst/app/gstappsrc.h>
#include <gst/video/video.h>
#include <spdlog/spdlog.h>
#include <nlohmann/json.hpp>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <sstream>
#include <random>

namespace fs = std::filesystem;
using json = nlohmann::json;

namespace ts::vms::media::pipeline {

namespace {

void LogPadCaps(const std::string& camera_id, GstElement* element, const char* pad_name, const char* label) {
    if (!element || !pad_name || !label) return;

    GstPad* pad = gst_element_get_static_pad(element, pad_name);
    if (!pad) {
        spdlog::warn("[{}] [SFU-Bridge] {}: no '{}' pad", camera_id, label, pad_name);
        return;
    }

    GstCaps* caps = gst_pad_get_current_caps(pad);
    if (!caps) {
        caps = gst_pad_query_caps(pad, nullptr);
    }

    if (caps) {
        gchar* caps_str = gst_caps_to_string(caps);
        spdlog::info("[{}] [SFU-Bridge] {} caps: {}", camera_id, label, caps_str ? caps_str : "(null)");
        g_free(caps_str);
        gst_caps_unref(caps);
    } else {
        spdlog::warn("[{}] [SFU-Bridge] {} caps: unavailable", camera_id, label);
    }

    gst_object_unref(pad);
}

void LogBridgeCaps(const std::string& camera_id,
                   GstElement* encoder,
                   GstElement* capsfilter,
                   GstElement* parser,
                   GstElement* parser_capsfilter,
                   GstElement* payloader) {
    LogPadCaps(camera_id, encoder, "src", "encoder.src");
    LogPadCaps(camera_id, capsfilter, "sink", "capsfilter.sink");
    LogPadCaps(camera_id, capsfilter, "src", "capsfilter.src");
    LogPadCaps(camera_id, parser, "sink", "h264parse.sink");
    LogPadCaps(camera_id, parser, "src", "h264parse.src");
    LogPadCaps(camera_id, parser_capsfilter, "sink", "h264parse_caps.sink");
    LogPadCaps(camera_id, parser_capsfilter, "src", "h264parse_caps.src");
    LogPadCaps(camera_id, payloader, "src", "rtph264pay.src");
}

void LogFirstRtpPacket(const std::string& camera_id, GstElement* payloader) {
    if (!payloader) return;

    GstPad* pad = gst_element_get_static_pad(payloader, "src");
    if (!pad) return;

    gst_pad_add_probe(
        pad,
        GST_PAD_PROBE_TYPE_BUFFER,
        [](GstPad*, GstPadProbeInfo* info, gpointer data) -> GstPadProbeReturn {
            auto* camera_id_ptr = static_cast<const std::string*>(data);
            if (!info || !info->data) return GST_PAD_PROBE_REMOVE;

            GstBuffer* buffer = GST_PAD_PROBE_INFO_BUFFER(info);
            if (!buffer) return GST_PAD_PROBE_REMOVE;

            GstMapInfo map{};
            if (!gst_buffer_map(buffer, &map, GST_MAP_READ) || map.size < 12) {
                spdlog::warn("[{}] [SFU-Bridge] first RTP packet: map failed", *camera_id_ptr);
                return GST_PAD_PROBE_REMOVE;
            }

            const guint8* rtp_data = map.data;
            const guint version = (rtp_data[0] >> 6) & 0x03;
            const guint padding = (rtp_data[0] >> 5) & 0x01;
            const guint extension = (rtp_data[0] >> 4) & 0x01;
            const guint csrc_count = rtp_data[0] & 0x0f;
            const guint marker = (rtp_data[1] >> 7) & 0x01;
            const guint pt = rtp_data[1] & 0x7f;
            const guint seq = (static_cast<guint>(rtp_data[2]) << 8) | rtp_data[3];
            const guint32 ts = (static_cast<guint32>(rtp_data[4]) << 24) | (static_cast<guint32>(rtp_data[5]) << 16) |
                               (static_cast<guint32>(rtp_data[6]) << 8) | rtp_data[7];

            guint header_len = 12 + csrc_count * 4;
            if (extension && map.size >= header_len + 4) {
                const guint ext_words = (static_cast<guint>(rtp_data[header_len + 2]) << 8) | rtp_data[header_len + 3];
                header_len += 4 + ext_words * 4;
            }

            const guint payload_len = map.size > header_len ? static_cast<guint>(map.size - header_len) : 0;
            const guint8* payload = map.size > header_len ? rtp_data + header_len : nullptr;
            guint8 b0 = payload_len > 0 ? payload[0] : 0;
            guint8 b1 = payload_len > 1 ? payload[1] : 0;
            guint8 b2 = payload_len > 2 ? payload[2] : 0;
            guint8 b3 = payload_len > 3 ? payload[3] : 0;

            spdlog::info(
                "[{}] [SFU-Bridge] first RTP packet: v={} pt={} seq={} ts={} marker={} padding={} ext={} csrc={} payload_len={} bytes={:02x} {:02x} {:02x} {:02x}",
                *camera_id_ptr, version, pt, seq, ts, marker, padding, extension, csrc_count, payload_len, b0, b1, b2, b3);

            gst_buffer_unmap(buffer, &map);
            return GST_PAD_PROBE_REMOVE;
        },
        new std::string(camera_id),
        [](gpointer p) { delete static_cast<std::string*>(p); });

    gst_object_unref(pad);
}

}  // namespace

IngestPipeline::IngestPipeline(const PipelineConfig& config) : config_(config) {
    last_frame_ts_ = std::chrono::steady_clock::now();
    last_fps_calc_ts_ = last_frame_ts_;
}

IngestPipeline::~IngestPipeline() {
    Stop();
}

bool IngestPipeline::Start() {
    std::lock_guard<std::mutex> lock(data_mutex_);
    if (fsm_.GetCurrentState() != State::STOPPED && fsm_.GetCurrentState() != State::RECONNECTING) {
        return true;
    }

    fsm_.TransitionTo(State::STARTING);
    spdlog::info("[{}] Starting ingestion from {}", config_.camera_id, config_.rtsp_url);

    sfu_egress_running_ = false;
    sfu_appsrc_caps_set_ = false;
    sfu_appsrc_push_count_ = 0;

    SetupPipeline();

    if (gst_element_set_state(pipeline_, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) {
        spdlog::error("[{}] Failed to set pipeline to PLAYING", config_.camera_id);
        fsm_.TransitionTo(State::STOPPED);
        return false;
    }

    return true;
}

void IngestPipeline::Stop() {
    std::lock_guard<std::mutex> lock(data_mutex_);
    if (fsm_.GetCurrentState() == State::STOPPED) {
        return;
    }

    spdlog::info("[{}] Stopping ingestion", config_.camera_id);
    fsm_.TransitionTo(State::STOPPED);
    CleanupPipeline();
}

State IngestPipeline::GetState() const {
    return fsm_.GetCurrentState();
}

double IngestPipeline::GetFps() const {
    return fps_;
}

int64_t IngestPipeline::GetLastFrameTimeMs() const {
    auto now = std::chrono::steady_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::milliseconds>(now - last_frame_ts_);
    return duration.count();
}

std::string IngestPipeline::GetCodecString() const {
    switch (codec_type_) {
        case CodecType::H264: return "H264";
        case CodecType::H265: return "H265";
        default:              return "UNKNOWN";
    }
}

IngestPipeline::Metrics IngestPipeline::GetMetrics() const {
    Metrics m;
    m.ingest_latency_ms = metrics_ingest_latency_ms_.load(std::memory_order_relaxed);
    m.frames_processed = metrics_frames_processed_.load(std::memory_order_relaxed);
    m.frames_dropped = metrics_frames_dropped_.load(std::memory_order_relaxed);
    m.bitrate_bps = metrics_bitrate_bps_.load(std::memory_order_relaxed);
    m.bytes_in_total = metrics_bytes_in_total_.load(std::memory_order_relaxed);
    m.pipeline_restarts_total = metrics_restarts_total_.load(std::memory_order_relaxed);
    m.last_frame_ts_ms = metrics_last_frame_unix_ms_.load(std::memory_order_relaxed);
    return m;
}

void IngestPipeline::SetupPipeline() {
    pipeline_ = gst_pipeline_new((config_.camera_id + "_pipeline").c_str());
    codec_type_ = CodecType::UNKNOWN;
    
    bool is_mock = config_.rtsp_url.find("mock://") == 0;

    tee_ = gst_element_factory_make("tee", "tee");
    
    // Branch A: Queue -> Appsink
    GstElement* q_sink = gst_element_factory_make("queue", "q_sink");
    g_object_set(q_sink, "leaky", 2, "max-size-buffers", 5, NULL);
    appsink_ = gst_element_factory_make("appsink", "sink");
 
    // Branch B: Queue -> Fakesink
    GstElement* q_fake = gst_element_factory_make("queue", "q_fake");
    g_object_set(q_fake, "leaky", 2, "max-size-buffers", 1, NULL);
    GstElement* fakesink = gst_element_factory_make("fakesink", "fakesink");

    // Elements check
    if (!pipeline_ || !tee_ || !q_sink || !appsink_ || !q_fake || !fakesink) {
        spdlog::error("[{}] Failed to create common GStreamer elements", config_.camera_id);
        return;
    }

    if (is_mock) {
        source_ = gst_element_factory_make("videotestsrc", "src");
        GstElement* encoder = gst_element_factory_make("openh264enc", "encoder");
        parse_ = gst_element_factory_make("h264parse", "parse");
        codec_type_ = CodecType::H264;
        
        if (!source_ || !encoder || !parse_) return;

        g_object_set(source_, "is-live", TRUE, NULL);
        g_object_set(encoder, "usage-type", 0, "bitrate", 1000000, NULL); 

        gst_bin_add_many(GST_BIN(pipeline_), source_, encoder, parse_, tee_, q_sink, appsink_, q_fake, fakesink, NULL);
        gst_element_link_many(source_, encoder, parse_, tee_, NULL);
    } else {
        source_ = gst_element_factory_make("rtspsrc", "src");
        if (!source_) return;

        g_object_set(source_, "location", config_.rtsp_url.c_str(), NULL);
        g_object_set(source_, "latency", 200, NULL);
        if (config_.prefer_tcp) {
            g_object_set(source_, "protocols", 4, NULL); // TCP
        } else {
            g_object_set(source_, "protocols", 7, NULL); // UDP + TCP
        }

        gst_bin_add_many(GST_BIN(pipeline_), source_, tee_, q_sink, appsink_, q_fake, fakesink, NULL);
        g_signal_connect(source_, "pad-added", G_CALLBACK(OnPadAdded), this);
    }

    gst_element_link(q_sink, appsink_);
    gst_element_link(q_fake, fakesink);

    GstPad *tee_src_pad_sink = gst_element_request_pad_simple(tee_, "src_%u");
    GstPad *q_sink_pad = gst_element_get_static_pad(q_sink, "sink");
    gst_pad_link(tee_src_pad_sink, q_sink_pad);
    gst_object_unref(tee_src_pad_sink);
    gst_object_unref(q_sink_pad);

    GstPad* tee_sink_pad = gst_element_get_static_pad(tee_, "sink");
    if (tee_sink_pad) {
        gst_pad_add_probe(tee_sink_pad, GST_PAD_PROBE_TYPE_BUFFER, OnMainPathPadProbe, this, nullptr);
        gst_object_unref(tee_sink_pad);
    }

    GstPad *tee_src_pad_fake = gst_element_request_pad_simple(tee_, "src_%u");
    GstPad *q_fake_pad = gst_element_get_static_pad(q_fake, "sink");
    gst_pad_link(tee_src_pad_fake, q_fake_pad);
    gst_object_unref(tee_src_pad_fake);
    gst_object_unref(q_fake_pad);

    // HLS is now initialized lazily from MonitorLoop after first frame
    // to avoid GStreamer lock contention during OnPadAdded.

    g_object_set(appsink_, "emit-signals", TRUE, "sync", FALSE, NULL);
    g_signal_connect(appsink_, "new-sample", G_CALLBACK(OnNewSample), this);

    GstBus* bus = gst_pipeline_get_bus(GST_PIPELINE(pipeline_));
    if (bus) {
        bus_watch_id_ = gst_bus_add_watch(bus, OnBusMessage, this);
        gst_object_unref(bus);
    }
}

void IngestPipeline::CleanupPipeline() {
    DisableHlsBranch("");
    StopSfuRtpEgress();
    if (pipeline_) {
        gst_element_set_state(pipeline_, GST_STATE_NULL);
        if (bus_watch_id_ > 0) {
            g_source_remove(bus_watch_id_);
            bus_watch_id_ = 0;
        }
        gst_object_unref(pipeline_);
        pipeline_ = nullptr;
    }
    
    if (!hls_state_.dir_path.empty()) {
        if (!hls_state_.degraded) {
            utils::Metrics::Instance().hls_sessions_active().Decrement();
        }
        hls_state_ = HlsState{};
    }
}

void IngestPipeline::OnPadAdded(GstElement* /*src*/, GstPad* pad, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    if (self->depay_) return;

    GstCaps* new_pad_caps = gst_pad_get_current_caps(pad);
    GstStructure* new_pad_struct = gst_caps_get_structure(new_pad_caps, 0);
    const gchar* media = gst_structure_get_string(new_pad_struct, "media");
    const gchar* encoding = gst_structure_get_string(new_pad_struct, "encoding-name");

    if (media && g_strcmp0(media, "video") == 0) {
        if (g_strcmp0(encoding, "H264") == 0) {
            self->codec_type_ = CodecType::H264;
            self->depay_ = gst_element_factory_make("rtph264depay", "depay");
            self->parse_ = gst_element_factory_make("h264parse", "parse");
        } else if (g_strcmp0(encoding, "H265") == 0) {
            self->codec_type_ = CodecType::H265;
            self->hls_state_.degraded = true;
            self->hls_state_.last_error = "H265 stream — HLS not supported";
            spdlog::warn("[{}] H265 stream detected — HLS skipped", self->config_.camera_id);
            self->depay_ = gst_element_factory_make("rtph265depay", "depay");
            self->parse_ = gst_element_factory_make("h265parse", "parse");
            if (self->parse_) g_object_set(self->parse_, "config-interval", -1, NULL);
        }

        if (self->depay_ && self->parse_) {
            gst_bin_add_many(GST_BIN(self->pipeline_), self->depay_, self->parse_, NULL);

            // Step 1: link the internal chain first (depay -> parse -> tee)
            if (!gst_element_link(self->depay_, self->parse_)) {
                spdlog::error("[{}] OnPadAdded: failed to link depay->parse", self->config_.camera_id);
                gst_bin_remove_many(GST_BIN(self->pipeline_), self->depay_, self->parse_, NULL);
                self->depay_ = nullptr; self->parse_ = nullptr;
                if (new_pad_caps) gst_caps_unref(new_pad_caps);
                return;
            }
            if (!gst_element_link(self->parse_, self->tee_)) {
                spdlog::error("[{}] OnPadAdded: failed to link parse->tee", self->config_.camera_id);
                gst_bin_remove_many(GST_BIN(self->pipeline_), self->depay_, self->parse_, NULL);
                self->depay_ = nullptr; self->parse_ = nullptr;
                if (new_pad_caps) gst_caps_unref(new_pad_caps);
                return;
            }

            // Step 2: link rtspsrc pad to depay input
            GstPad* sinkpad = gst_element_get_static_pad(self->depay_, "sink");
            if (gst_pad_link(pad, sinkpad) != GST_PAD_LINK_OK) {
                spdlog::error("[{}] OnPadAdded: failed to link rtspsrc_pad->depay", self->config_.camera_id);
                gst_object_unref(sinkpad);
                if (new_pad_caps) gst_caps_unref(new_pad_caps);
                return;
            }
            gst_object_unref(sinkpad);

            // Step 3: sync state only AFTER all pads are linked so caps can flow
            gst_element_sync_state_with_parent(self->parse_);
            gst_element_sync_state_with_parent(self->depay_);

            spdlog::info("[{}] OnPadAdded: {} stream linked and PLAYING", self->config_.camera_id, encoding ? encoding : "unknown");
        }
    }
    if (new_pad_caps) gst_caps_unref(new_pad_caps);
}

GstFlowReturn IngestPipeline::OnNewSample(GstElement* sink, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    GstSample* sample = gst_app_sink_pull_sample(GST_APP_SINK(sink));
    
    if (sample) {
        std::lock_guard<std::mutex> lock(self->data_mutex_);
        self->frame_count_++;
        self->metrics_frames_processed_.fetch_add(1, std::memory_order_relaxed);

        if (self->fsm_.GetCurrentState() == State::STARTING) {
            self->fsm_.TransitionTo(State::RUNNING);
            spdlog::info("[{}] First frame received, pipeline RUNNING", self->config_.camera_id);
            // Signal MonitorLoop to set up HLS branch (safe: not on streaming thread)
            if (self->codec_type_ == CodecType::H264) {
                self->hls_branch_pending_ = true;
            }
        }

        // Forward to SFU bridge if active (H265 only — H264 uses tee directly)
        if (self->sfu_appsrc_ && self->sfu_egress_running_) {
            GstBuffer* buf = gst_sample_get_buffer(sample);
            if (buf) {
                // Do not reuse the original buffer across pipelines.
                // Copy it and let appsrc timestamp it for the bridge pipeline.
                GstBuffer* out = gst_buffer_copy_deep(buf);
                if (out) {
                    // Propagate caps to the separate bridge pipeline exactly once
                    // before pushing the first buffer to ensure caps event arrives before segment event.
                    if (!self->sfu_appsrc_caps_set_) {
                        GstCaps* caps = gst_sample_get_caps(sample);
                        if (caps) {
                            g_object_set(self->sfu_appsrc_, "caps", caps, NULL);
                            self->sfu_appsrc_caps_set_ = true;
                            spdlog::info("[{}] SFU bridge: caps set from sample", self->config_.camera_id);
                        }
                    }

                    GstFlowReturn ret =
                        gst_app_src_push_buffer(GST_APP_SRC(self->sfu_appsrc_), out);
                    if (ret == GST_FLOW_ERROR) {
                        spdlog::error("[{}] SFU bridge appsrc fatal error — disabling egress",
                            self->config_.camera_id);
                        self->sfu_egress_running_ = false;
                    } else if (ret != GST_FLOW_OK && ret != GST_FLOW_FLUSHING) {
                        spdlog::warn("[{}] SFU appsrc push returned {}", self->config_.camera_id, (int)ret);
                    } else {
                        self->sfu_appsrc_push_count_++;
                        if (self->sfu_appsrc_push_count_ % 30 == 0) {
                            spdlog::info("[{}] SFU bridge: pushed {} frames",
                                self->config_.camera_id, self->sfu_appsrc_push_count_);
                        }
                    }
                }
            }
        }

        auto now = std::chrono::steady_clock::now();
        auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - self->last_fps_calc_ts_);
        if (elapsed.count() >= 1) {
            uint64_t frames_since_last = self->frame_count_ - self->last_fps_frame_count_;
            self->fps_ = static_cast<double>(frames_since_last) / elapsed.count();
            self->last_fps_calc_ts_ = now;
            self->last_fps_frame_count_ = self->frame_count_;
        }
        gst_sample_unref(sample);
    }
    return GST_FLOW_OK;
}

GstPadProbeReturn IngestPipeline::OnMainPathPadProbe(GstPad* /*pad*/, GstPadProbeInfo* /*info*/, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    std::lock_guard<std::mutex> lock(self->data_mutex_);
    self->last_frame_ts_ = std::chrono::steady_clock::now();
    return GST_PAD_PROBE_OK;
}

gboolean IngestPipeline::OnBusMessage(GstBus* /*bus*/, GstMessage* msg, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    switch (GST_MESSAGE_TYPE(msg)) {
        case GST_MESSAGE_ERROR: {
            GError* err;
            gchar* debug_info;
            gst_message_parse_error(msg, &err, &debug_info);
            spdlog::error("[{}] GStreamer error: {}", self->config_.camera_id, err->message);
            g_clear_error(&err);
            g_free(debug_info);
            self->fsm_.TransitionTo(State::RECONNECTING);
            break;
        }
        case GST_MESSAGE_EOS:
            self->fsm_.TransitionTo(State::RECONNECTING);
            break;
        default: break;
    }
    return TRUE;
}

bool IngestPipeline::IsSfuEgressRunning() const {
    return sfu_egress_running_;
}

bool IngestPipeline::StartSfuRtpEgress(const SfuConfig& config) {
    if (sfu_egress_running_) return true;

    if (codec_type_ == CodecType::UNKNOWN) {
        spdlog::warn("[{}] StartSfuRtpEgress: codec not yet detected — deferring", config_.camera_id);
        return false;
    }

    const uint32_t sfu_ssrc = (config.ssrc != 0u) ? config.ssrc : 11111111u;
    const uint32_t sfu_pt   = (config.pt   != 0u) ? config.pt   : 96u;
    const int local_rtp_port  = (int)config.dst_port + 100;
    const int local_rtcp_port = local_rtp_port + 1;

    spdlog::info(
        "[{}] Starting SFU egress to {}:{} (local port {}) codec={} pt={} ssrc={}",
        config_.camera_id,
        config.dst_ip,
        config.dst_port,
        local_rtp_port,
        config.codec,
        sfu_pt,
        sfu_ssrc);

    if (codec_type_ == CodecType::H265) {
        // H265: use appsrc bridge — DO NOT touch the main pipeline tee.
        // Touching the tee sends a RECONFIGURE event that can stall the rtspsrc.
        return StartSfuRtpEgressH265Bridge(config);
    }

    // H264 direct path — lightweight, just rtph264pay on the tee
    sfu_queue_ = gst_element_factory_make("queue", "sfu_queue");
    g_object_set(sfu_queue_, "leaky", 2, "max-size-buffers", 30, "max-size-time", (gint64)2000000000, NULL);
    sfu_parse_ = gst_element_factory_make("h264parse", "sfu_parse");
    sfu_pay_ = gst_element_factory_make("rtph264pay", "sfu_pay");
    sfu_sink_ = gst_element_factory_make("udpsink", "sfu_sink");

    if (!sfu_queue_ || !sfu_parse_ || !sfu_pay_ || !sfu_sink_) {
        spdlog::error("[{}] Failed to create H264 SFU elements", config_.camera_id);
        return false;
    }

    g_object_set(sfu_sink_, "host", config.dst_ip.c_str(), "port", config.dst_port,
        "bind-port", local_rtp_port, "sync", FALSE, "async", FALSE, NULL);
    
    // config-interval=-1: tells h264parse to repeat SPS/PPS before every IDR frame.
    g_object_set(sfu_parse_, "config-interval", (gint)-1, NULL);
    
    // rtph264pay config: 
    // - config-interval=-1: also ensures payloader includes SPS/PPS in-band
    // - aggregate-mode=0: none (RFC 3984) — most robust for WebView2/Chromium
    g_object_set(sfu_pay_, "config-interval", (gint)-1, "ssrc", (guint)sfu_ssrc, "pt", (guint)sfu_pt, "aggregate-mode", (gint)0, NULL);

    gst_bin_add_many(GST_BIN(pipeline_), sfu_queue_, sfu_parse_, sfu_pay_, sfu_sink_, NULL);
    gst_element_link_many(sfu_queue_, sfu_parse_, sfu_pay_, sfu_sink_, NULL);

    // Link to tee BEFORE syncing states
    GstPad *tee_src = gst_element_request_pad_simple(tee_, "src_%u");
    GstPad *q_sink = gst_element_get_static_pad(sfu_queue_, "sink");
    if (gst_pad_link(tee_src, q_sink) != GST_PAD_LINK_OK) {
        spdlog::error("[{}] Failed to link tee -> sfu_queue", config_.camera_id);
        gst_object_unref(tee_src);
        gst_object_unref(q_sink);
        return false;
    }
    gst_object_unref(tee_src);
    gst_object_unref(q_sink);

    gst_element_sync_state_with_parent(sfu_sink_);
    gst_element_sync_state_with_parent(sfu_pay_);
    gst_element_sync_state_with_parent(sfu_parse_);
    gst_element_sync_state_with_parent(sfu_queue_);

    // RTCP listener for PLIs (H264 direct path)
    sfu_rtcp_src_ = gst_element_factory_make("udpsrc", "sfu_rtcp_src");
    if (sfu_rtcp_src_) {
        g_object_set(sfu_rtcp_src_, "address", "127.0.0.1", "port", local_rtcp_port, NULL);
        GstPad* rtcp_src_pad = gst_element_get_static_pad(sfu_rtcp_src_, "src");
        if (rtcp_src_pad) {
            gst_pad_add_probe(rtcp_src_pad, GST_PAD_PROBE_TYPE_BUFFER, OnSfuRtcpProbe, this, nullptr);
            gst_object_unref(rtcp_src_pad);
        }
        gst_bin_add(GST_BIN(pipeline_), sfu_rtcp_src_);
        if (gst_element_set_state(sfu_rtcp_src_, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) {
            spdlog::warn("[{}] SFU RTCP listener failed to bind port {} — PLI recovery disabled", 
                config_.camera_id, local_rtcp_port);
            gst_bin_remove(GST_BIN(pipeline_), sfu_rtcp_src_);
            sfu_rtcp_src_ = nullptr;
        } else {
            spdlog::info("[{}] SFU RTCP listener started on port {}", config_.camera_id, local_rtcp_port);
        }
    }

    sfu_egress_running_ = true;
    return true;
}

bool IngestPipeline::StartSfuRtpEgressH265Bridge(const SfuConfig& config) {
    const uint32_t sfu_ssrc = (config.ssrc != 0u) ? config.ssrc : 11111111u;
    const uint32_t sfu_pt   = (config.pt   != 0u) ? config.pt   : 96u;
    const int local_rtp_port  = (int)config.dst_port + 100;
    const int local_rtcp_port = local_rtp_port + 1;

    // Create a separate GStreamer pipeline fed by appsrc.
    // This avoids any modification to the main ingest pipeline's tee,
    // which would cause a permanent RECONFIGURE stall.
    sfu_pipeline_ = gst_pipeline_new("sfu_egress");

    sfu_appsrc_ = gst_element_factory_make("appsrc", "sfu_appsrc");
    g_object_set(sfu_appsrc_, "format", GST_FORMAT_TIME, "is-live", TRUE, "do-timestamp", TRUE, NULL);
    GstElement* appsrc_q = gst_element_factory_make("queue", "sfu_aq");
    
    // Transcode path selection for H265 → H264 SFU bridge.
    //
    // Priority 1 — All-NVIDIA CUDA path (no system-memory copy):
    //   nvh265dec → nvvideoconvert → nvh264enc
    //
    // Priority 2 — All-D3D11 path:
    //   d3d11h265dec → d3d11videoconvert → mfh264enc
    //
    // Priority 3 — Mixed: NVIDIA decode + CPU encode
    //   nvh265dec → cudadownload → videoconvert → nvh264enc
    //   (last resort; only if neither full-GPU path works)
    //
    // Priority 4 — Pure software (requires gst-libav + gst-plugins-ugly):
    //   avdec_h265 → videoconvert → x264enc
    //
    // Do NOT mix GPU-memory decoders with CPU videoconvert without a
    // download element — caps negotiation silently fails at data-flow time.

    sfu_download_  = nullptr;
    sfu_decoder_   = nullptr;
    sfu_converter_ = nullptr;
    sfu_encoder_   = nullptr;

    enum class BridgePath { NVIDIA, D3D11, MIXED, MF, SW, NONE } bridge_path = BridgePath::NONE;
    // The pure D3D11 transcode path has produced RTP that reaches WebRTC consumers
    // but never yields decoded frames in Chromium/WebView2 on affected Windows systems.
    // Prefer mixed/system-memory paths unless this is explicitly re-enabled.
    constexpr bool kEnablePureD3D11BridgePath = false;

    auto make_any = [&](const std::vector<const char*>& names, const char* label) -> GstElement* {
        for (const char* name : names) {
            GstElement* e = gst_element_factory_make(name, label);
            if (e) return e;
        }
        spdlog::warn("[SFU-Bridge] Failed to find ANY elements for {}", label);
        return nullptr;
    };

    // --- Try Priority 1 — True NVIDIA HW path (GPU-only):
    // nvh265dec -> cudaconvert(scale) -> nvh264enc
    {
        GstElement* dec  = make_any({"nvh265dec"}, "sfu_dec");
        GstElement* conv = make_any({"cudaconvertscale", "cudaconvert", "nvvideoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"nvh264enc"}, "sfu_enc");
        if (dec && conv && enc) {
            sfu_decoder_ = dec; sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path  = BridgePath::NVIDIA;
        } else {
            if (dec)  gst_object_unref(dec);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }

    // --- Try Priority 2 — True D3D11 HW path (GPU-only):
    // d3d11h265dec -> d3d11convert(scale) -> mfh264enc (or nvd3d11h264enc)
    if (bridge_path == BridgePath::NONE && kEnablePureD3D11BridgePath) {
        GstElement* dec  = make_any({"d3d11h265dec"}, "sfu_dec");
        GstElement* conv = make_any({"d3d11convert", "d3d11videoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"nvd3d11h264enc", "mfh264enc"}, "sfu_enc");
        if (dec && conv && enc) {
            sfu_decoder_ = dec; sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path  = BridgePath::D3D11;
        } else {
            if (dec)  gst_object_unref(dec);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }
    if (bridge_path == BridgePath::NONE && !kEnablePureD3D11BridgePath) {
        spdlog::info("[{}] Skipping pure D3D11 SFU bridge path; preferring mixed/system-memory transcode", config_.camera_id);
    }

    // --- Try Priority 3a: NVIDIA decode + cudadownload + CPU encode ---
    if (bridge_path == BridgePath::NONE) {
        GstElement* dec  = make_any({"nvh265dec"}, "sfu_dec");
        GstElement* dl   = make_any({"cudadownload"}, "sfu_dl");
        GstElement* conv = make_any({"videoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"x264enc", "openh264enc", "mfh264enc"}, "sfu_enc");
        if (dec && dl && conv && enc) {
            sfu_decoder_   = dec; sfu_download_ = dl;
            sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path    = BridgePath::MIXED;
        } else {
            if (dec)  gst_object_unref(dec);
            if (dl)   gst_object_unref(dl);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }

    // --- Try Priority 3b: D3D11 decode + d3d11download + CPU encode ---
    if (bridge_path == BridgePath::NONE) {
        GstElement* dec  = make_any({"d3d11h265dec"}, "sfu_dec");
        GstElement* dl   = make_any({"d3d11download"}, "sfu_dl");
        GstElement* conv = make_any({"videoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"x264enc", "openh264enc", "mfh264enc"}, "sfu_enc");
        if (dec && dl && conv && enc) {
            sfu_decoder_   = dec; sfu_download_ = dl;
            sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path    = BridgePath::MIXED;
        } else {
            if (dec)  gst_object_unref(dec);
            if (dl)   gst_object_unref(dl);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }

    // --- Try Priority 3c: MF decode + CPU encode ---
    if (bridge_path == BridgePath::NONE) {
        GstElement* dec  = make_any({"mfh265dec"}, "sfu_dec");
        GstElement* conv = make_any({"videoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"x264enc", "openh264enc", "mfh264enc"}, "sfu_enc");
        if (dec && conv && enc) {
            sfu_decoder_ = dec; sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path  = BridgePath::MF;
        } else {
            if (dec)  gst_object_unref(dec);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }

    // --- Try Priority 4: pure software ---
    if (bridge_path == BridgePath::NONE) {
        GstElement* dec  = make_any({"avdec_h265"}, "sfu_dec");
        GstElement* conv = make_any({"videoconvert"}, "sfu_conv");
        GstElement* enc  = make_any({"x264enc", "openh264enc"}, "sfu_enc");
        if (dec && conv && enc) {
            sfu_decoder_ = dec; sfu_converter_ = conv; sfu_encoder_ = enc;
            bridge_path  = BridgePath::SW;
        } else {
            if (dec)  gst_object_unref(dec);
            if (conv) gst_object_unref(conv);
            if (enc)  gst_object_unref(enc);
        }
    }

    if (bridge_path == BridgePath::NONE) {
        spdlog::error("[{}] No H265→H264 transcode path available "
            "(need nvh265dec+nvvideoconvert+nvh264enc, d3d11h265dec+d3d11videoconvert+mfh264enc, "
            "or avdec_h265+x264enc)", config_.camera_id);
        gst_object_unref(sfu_pipeline_); sfu_pipeline_ = nullptr;
        return false;
    }

    const char* path_name = "UNKNOWN";
    switch (bridge_path) {
        case BridgePath::NVIDIA: path_name = "NVIDIA"; break;
        case BridgePath::D3D11:  path_name = "D3D11"; break;
        case BridgePath::MIXED:  path_name = "MIXED"; break;
        case BridgePath::MF:     path_name = "MF"; break;
        case BridgePath::SW:     path_name = "SW"; break;
        case BridgePath::NONE:   path_name = "NONE"; break;
        default: break;
    }
    spdlog::info("[{}] SFU bridge using {} transcode path", config_.camera_id, path_name);
    
    sfu_parse_ = gst_element_factory_make("h264parse", "sfu_parse");
    GstElement* sfu_caps = gst_element_factory_make("capsfilter", "sfu_caps");
    GstElement* sfu_parse_caps = gst_element_factory_make("capsfilter", "sfu_parse_caps");
    sfu_capsfilter_ = sfu_caps;
    sfu_parse_capsfilter_ = sfu_parse_caps;
    GstElement* sfu_scale = nullptr;
    GstElement* sfu_raw_caps = nullptr;
    GstElement* sfu_transcode_q = nullptr;
    GstElement* sfu_post_conv = nullptr;
    sfu_pay_ = gst_element_factory_make("rtph264pay", "sfu_pay");
    sfu_sink_ = gst_element_factory_make("udpsink", "sfu_sink");

    if (!sfu_appsrc_ || !appsrc_q || !sfu_caps || !sfu_parse_ || !sfu_parse_caps || !sfu_pay_ || !sfu_sink_) {
        spdlog::error("[{}] Failed to create H265 SFU bridge common elements", config_.camera_id);
        gst_object_unref(sfu_pipeline_); sfu_pipeline_ = nullptr;
        return false;
    }

    // Configure appsrc with generic H265 byte-stream caps.
    // Do NOT propagate the current-caps from the main pipeline's h265parse src pad:
    // those caps may include codec_data / specific stream-format that the bridge's
    // own h265parse (added below) needs to re-negotiate with the decoder.
    // Keeping caps as ANY here lets the bridge h265parse do all the negotiation.
    g_object_set(
        sfu_appsrc_,
        "caps", static_cast<GstCaps*>(nullptr),
        "is-live", TRUE,
        "do-timestamp", TRUE,
        "format", GST_FORMAT_TIME,
        "stream-type", 0, // GST_APP_STREAM_TYPE_STREAM
        "max-bytes", (guint64)16777216,
        "block", FALSE,
        NULL);

    // Large queue so the hardware decoder has time to initialize before frames
    // overflow and get dropped.  leaky=1 (upstream) drops *new* incoming frames
    // when full, preserving the IDR frame that was already enqueued — critical for
    // decoder startup.  max-size-time acts as a ceiling to avoid unbounded memory.
    g_object_set(appsrc_q, "leaky", 1, "max-size-buffers", 300,
        "max-size-time", (guint64)(10 * GST_SECOND), "max-size-bytes", 0, NULL);

    bool is_x264enc = false;
    bool is_openh264 = false;
    bool is_mf = false;
    const gchar* encoder_name = nullptr;
    {
        GstElementFactory* f = gst_element_get_factory(sfu_encoder_);
        const gchar* n = f ? gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(f)) : nullptr;
        encoder_name = n;
        is_x264enc = (n && g_strcmp0(n, "x264enc") == 0);
        is_openh264 = (n && g_strcmp0(n, "openh264enc") == 0);
        is_mf = (n && g_strcmp0(n, "mfh264enc") == 0);
    }

    bool is_sysmem_convert = false;
    bool use_sw_block = false;
    {
        GstElementFactory* f = gst_element_get_factory(sfu_converter_);
        const gchar* n = f ? gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(f)) : nullptr;
        is_sysmem_convert = (n && g_strcmp0(n, "videoconvert") == 0);
    }

    // WebRTC Level 3.1 trap: clamp to <= 1280x720 and force I420 for all bridge paths.
    // This ensures hardware decoders/encoders don't exceed browser limits.
    sfu_transcode_q = gst_element_factory_make("queue", "sfu_transcode_q");
    sfu_scale = gst_element_factory_make("videoscale", "sfu_scale");
    sfu_raw_caps = gst_element_factory_make("capsfilter", "sfu_raw_caps");
    sfu_post_conv = gst_element_factory_make("videoconvert", "sfu_post_conv");

    if (bridge_path == BridgePath::NVIDIA || bridge_path == BridgePath::D3D11) {
        spdlog::info("[{}] Using GPU-based scaling and conversion — software transcode block bypassed", config_.camera_id);
    } else {
        if (!sfu_transcode_q || !sfu_scale || !sfu_raw_caps || !sfu_post_conv) {
            spdlog::warn("[{}] transcode queue/videoscale/raw capsfilter/post_conv not available; continuing without 720p/I420 clamp", config_.camera_id);
        } else {
            g_object_set(
                G_OBJECT(sfu_transcode_q),
                "max-size-buffers", 3,
                "max-size-bytes", 0,
                "max-size-time", (guint64)0,
                "leaky", 2, // downstream (drop oldest)
                NULL);

            GstCaps* raw_caps = gst_caps_from_string("video/x-raw, format=I420, width=(int)[1,1280], height=(int)[1,720]");
            if (raw_caps) {
                g_object_set(G_OBJECT(sfu_raw_caps), "caps", raw_caps, NULL);
                gst_caps_unref(raw_caps);
            }
            use_sw_block = true;
        }
    }

    // Encoder tuning (Bitrate, GOP, Zero-latency)
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "bitrate")) {
        guint64 bitrate = is_x264enc ? 2048u : 2000u; // kbps baseline
        // x264enc uses kbps. mfh264enc and openh264enc use bps.
        if (is_mf || is_openh264) bitrate *= 1000u;
        g_object_set(sfu_encoder_, "bitrate", (guint)bitrate, NULL);
    }

    // Force regular IDRs (15 frames = ~0.5 sec at 30fps)
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "gop-size"))
        g_object_set(sfu_encoder_, "gop-size", 15, NULL);
    else if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "key-int-max"))
        g_object_set(sfu_encoder_, "key-int-max", 15, NULL);

    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "idrinterval"))
        g_object_set(sfu_encoder_, "idrinterval", 15, NULL);

    // mfh264enc specific tuning
    if (is_mf) {
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "periodic-key-frames"))
            g_object_set(sfu_encoder_, "periodic-key-frames", 30, NULL);
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "quality-vs-speed"))
            g_object_set(sfu_encoder_, "quality-vs-speed", 18, NULL);
    }

    // openh264enc specific tuning
    if (is_openh264) {
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "usage-type"))
            g_object_set(sfu_encoder_, "usage-type", 0 /* camera */, NULL);
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "rate-control"))
            g_object_set(sfu_encoder_, "rate-control", 1 /* bitrate */, NULL);
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "complexity"))
            g_object_set(sfu_encoder_, "complexity", 0 /* low */, NULL);
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "slice-mode"))
            g_object_set(sfu_encoder_, "slice-mode", 1 /* n-slices */, NULL);
        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "num-slices"))
            g_object_set(sfu_encoder_, "num-slices", 1u, NULL);
    }

    // Enable zero-latency mode where supported
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "zerolatency"))
        g_object_set(sfu_encoder_, "zerolatency", TRUE, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "tune"))
        g_object_set(sfu_encoder_, "tune", 4 /* zerolatency for x264enc */, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "speed-preset"))
        g_object_set(sfu_encoder_, "speed-preset", 1 /* ultrafast for x264enc */, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "low-latency"))
        g_object_set(sfu_encoder_, "low-latency", TRUE, NULL);

    // Profile and B-frames
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "profile")) {
        if (is_mf) {
            g_object_set(sfu_encoder_, "profile", 66 /* Baseline */, NULL);
            if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "level"))
                g_object_set(sfu_encoder_, "level", 31 /* 3.1 */, NULL);
        } else {
            g_object_set(sfu_encoder_, "profile", 0 /* baseline for x264/openh264 */, NULL);
        }
    }
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "rc-mode"))
        g_object_set(sfu_encoder_, "rc-mode", 0 /* cbr for mfh264enc */, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "bframes"))
        g_object_set(sfu_encoder_, "bframes", 0, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "max-bframes"))
        g_object_set(sfu_encoder_, "max-bframes", 0, NULL);

    // WebRTC browsers are strict about H264 profiles and framing.
    // Force a byte-stream, access-unit aligned constrained-baseline stream
    // before packetization so the RTP payload matches the negotiated SDP.
    {
        GstCaps* enc_caps = gst_caps_from_string(
            "video/x-h264, stream-format=byte-stream, alignment=au, profile=constrained-baseline");
        if (enc_caps) {
            g_object_set(G_OBJECT(sfu_caps), "caps", enc_caps, NULL);
            gst_caps_unref(enc_caps);
        }
    }

    // Force codec headers to be repeated aggressively so a late-joining consumer
    // gets SPS/PPS with the next IDR.
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "repeat-sequence-header"))
        g_object_set(sfu_encoder_, "repeat-sequence-header", TRUE, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "insert-sps-pps"))
        g_object_set(sfu_encoder_, "insert-sps-pps", TRUE, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_encoder_), "aud"))
        g_object_set(sfu_encoder_, "aud", TRUE, NULL);

    // Inject SPS/PPS into the stream before every IDR frame
    g_object_set(sfu_parse_, "config-interval", (gint)-1, NULL);
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(sfu_parse_), "disable-passthrough"))
        g_object_set(sfu_parse_, "disable-passthrough", TRUE, NULL);

    {
        GstCaps* parse_caps = gst_caps_from_string(
            "video/x-h264, parsed=true, stream-format=byte-stream, alignment=au, profile=constrained-baseline");
        if (parse_caps) {
            g_object_set(G_OBJECT(sfu_parse_caps), "caps", parse_caps, NULL);
            gst_caps_unref(parse_caps);
        }
    }

    g_object_set(sfu_pay_, "config-interval", (gint)-1, "ssrc", (guint)sfu_ssrc, "pt", (guint)sfu_pt, "aggregate-mode", (gint)0, "mtu", (guint)1200, NULL);
    g_object_set(sfu_sink_, "host", config.dst_ip.c_str(), "port", (gint)config.dst_port,
        "bind-port", (gint)local_rtp_port, "sync", (gboolean)FALSE, "async", (gboolean)FALSE, NULL);

    // RTCP listener for PLIs (H265 bridge path).
    sfu_rtcp_src_ = gst_element_factory_make("udpsrc", "sfu_rtcp_src");
    if (sfu_rtcp_src_) {
        g_object_set(sfu_rtcp_src_, "address", "127.0.0.1", "port", local_rtcp_port, NULL);
        GstPad* rtcp_src_pad = gst_element_get_static_pad(sfu_rtcp_src_, "src");
        if (rtcp_src_pad) {
            gst_pad_add_probe(rtcp_src_pad, GST_PAD_PROBE_TYPE_BUFFER, OnSfuRtcpProbe, this, nullptr);
            gst_object_unref(rtcp_src_pad);
        }
        gst_bin_add(GST_BIN(sfu_pipeline_), sfu_rtcp_src_);
        if (gst_element_set_state(sfu_rtcp_src_, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) {
            spdlog::warn("[{}] SFU bridge RTCP listener failed to bind port {} — PLI recovery disabled", 
                config_.camera_id, local_rtcp_port);
            gst_bin_remove(GST_BIN(sfu_pipeline_), sfu_rtcp_src_);
            sfu_rtcp_src_ = nullptr;
        }
    }

    // h265parse at the bridge INPUT normalises stream-format / alignment so the
    // decoder gets exactly what it needs (hvc1 codec_data for d3d11h265dec, or
    // byte-stream for avdec_h265).  Without it, d3d11h265dec silently discards
    // frames because the caps lack codec_data.
    GstElement* h265_in = gst_element_factory_make("h265parse", "sfu_h265in");
    if (!h265_in) {
        spdlog::error("[{}] h265parse not available — bridge cannot start", config_.camera_id);
        gst_object_unref(sfu_pipeline_); sfu_pipeline_ = nullptr;
        return false;
    }
    // config-interval=-1: insert VPS/SPS/PPS with every IDR so the decoder always
    // has a complete parameter set even if it misses the first IDR.
    g_object_set(h265_in, "config-interval", -1, NULL);

    // Add elements to the bin safely (GST_BIN_ADD_MANY is NULL-sensitive and would crash)
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_appsrc_);
    gst_bin_add(GST_BIN(sfu_pipeline_), appsrc_q);
    gst_bin_add(GST_BIN(sfu_pipeline_), h265_in);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_decoder_);
    if (sfu_download_) gst_bin_add(GST_BIN(sfu_pipeline_), sfu_download_);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_converter_);
    
    if (sfu_transcode_q) gst_bin_add(GST_BIN(sfu_pipeline_), sfu_transcode_q);
    if (sfu_scale)       gst_bin_add(GST_BIN(sfu_pipeline_), sfu_scale);
    if (sfu_raw_caps)    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_raw_caps);
    if (sfu_post_conv)   gst_bin_add(GST_BIN(sfu_pipeline_), sfu_post_conv);

    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_encoder_);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_caps);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_parse_);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_parse_caps);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_pay_);
    gst_bin_add(GST_BIN(sfu_pipeline_), sfu_sink_);

    // Link step-by-step for robustness
    bool link_ok = true;
    link_ok = link_ok && (gst_element_link_many(sfu_appsrc_, appsrc_q, h265_in, sfu_decoder_, NULL) == TRUE);
    
    if (sfu_download_) {
        link_ok = link_ok && (gst_element_link(sfu_decoder_, sfu_download_) == TRUE);
        link_ok = link_ok && (gst_element_link(sfu_download_, sfu_converter_) == TRUE);
    } else {
        link_ok = link_ok && (gst_element_link(sfu_decoder_, sfu_converter_) == TRUE);
    }

    if (use_sw_block) {
        link_ok = link_ok && (gst_element_link_many(sfu_converter_, sfu_transcode_q, sfu_scale, sfu_raw_caps, sfu_post_conv, sfu_encoder_, NULL) == TRUE);
    } else {
        // Pure GPU Path: Use caps between converter and encoder to trigger HW scaling
        GstCaps* hw_caps = (bridge_path == BridgePath::NVIDIA) 
            ? gst_caps_from_string("video/x-raw(memory:CUDAMemory), format=NV12, width=1280, height=720, pixel-aspect-ratio=1/1")
            : gst_caps_from_string("video/x-raw(memory:D3D11Memory), format=NV12, width=1280, height=720, pixel-aspect-ratio=1/1");
        
        if (hw_caps) {
            link_ok = link_ok && (gst_element_link_filtered(sfu_converter_, sfu_encoder_, hw_caps) == TRUE);
            gst_caps_unref(hw_caps);
        } else {
            link_ok = link_ok && (gst_element_link(sfu_converter_, sfu_encoder_) == TRUE);
        }
    }

    link_ok = link_ok && (gst_element_link_many(sfu_encoder_, sfu_caps, sfu_parse_, sfu_parse_caps, sfu_pay_, sfu_sink_, NULL) == TRUE);

    if (!link_ok) {
        spdlog::error("[{}] Failed to link bridge pipeline elements — memory mismatch or incompatible caps", config_.camera_id);
        gst_object_unref(sfu_pipeline_); sfu_pipeline_ = nullptr;
        sfu_appsrc_ = nullptr; sfu_download_ = nullptr;
        return false;
    }

    // Diagnostic probes to locate where the data stops if RTP is still absent
    auto make_probe = [](const char*) {
        return [](GstPad*, GstPadProbeInfo*, gpointer d) -> GstPadProbeReturn {
            spdlog::info("[SFU-Bridge] first buffer at: {}", static_cast<const char*>(d));
            return GST_PAD_PROBE_REMOVE;
        };
    };
    auto add_one_shot = [&](GstElement* el, const char* pad_name, const char* label) {
        GstPad* p = gst_element_get_static_pad(el, pad_name);
        if (p) {
            gst_pad_add_probe(p, GST_PAD_PROBE_TYPE_BUFFER, make_probe(label),
                (gpointer)label, nullptr);
            gst_object_unref(p);
        }
    };
    add_one_shot(h265_in,     "src",  "h265parse_in.src  (→ decoder)");
    add_one_shot(sfu_decoder_,"src",  "decoder.src       (→ download/conv)");
    add_one_shot(sfu_encoder_,"src",  "encoder.src       (→ h264parse)");
    add_one_shot(sfu_pay_,    "src",  "rtph264pay.src    (→ udpsink = RTP sent)");
    LogFirstRtpPacket(config_.camera_id, sfu_pay_);

    spdlog::info("[{}] SFU bridge pipeline linking OK (dec={} enc={} factory={})", config_.camera_id,
        GST_ELEMENT_NAME(sfu_decoder_), GST_ELEMENT_NAME(sfu_encoder_),
        encoder_name ? encoder_name : "unknown");

    // Enable frame pushing BEFORE set_state(PLAYING) so the very first IDR frame
    // that arrives after the pipeline starts is captured in the queue.
    // Previously this was set AFTER a 2-second poll, causing ~50 frames (including
    // the IDR) to be silently discarded before the flag was ever set.
    sfu_egress_running_ = true;

    gst_element_set_state(sfu_pipeline_, GST_STATE_PLAYING);

    // Wait up to 2s for hardware elements (nvh265dec, mfh264enc, d3d11h265dec) to
    // complete their async initialisation.  The previous 100ms window was too short
    // and caused the failure to be silently swallowed, leaving a broken pipeline.
    GstStateChangeReturn sc = gst_element_get_state(sfu_pipeline_, nullptr, nullptr, 2000 * GST_MSECOND);
    if (sc == GST_STATE_CHANGE_FAILURE || sc == GST_STATE_CHANGE_ASYNC) {
        // Drain the bus for the first error message so we can log it.
        GstBus* errBus = gst_pipeline_get_bus(GST_PIPELINE(sfu_pipeline_));
        if (errBus) {
            GstMessage* errMsg = gst_bus_timed_pop_filtered(errBus, 0,
                static_cast<GstMessageType>(GST_MESSAGE_ERROR));
            if (errMsg) {
                GError* err; gchar* dbg;
                gst_message_parse_error(errMsg, &err, &dbg);
                spdlog::error("[{}] SFU bridge hw init failed: {} ({})",
                    config_.camera_id, err->message, dbg ? dbg : "");
                g_clear_error(&err); g_free(dbg);
                gst_message_unref(errMsg);
            } else {
                spdlog::error("[{}] SFU bridge hw init timed out (>2s) — aborting",
                    config_.camera_id);
            }
            gst_object_unref(errBus);
        }
        sfu_egress_running_ = false;
        gst_element_set_state(sfu_pipeline_, GST_STATE_NULL);
        gst_object_unref(sfu_pipeline_); sfu_pipeline_ = nullptr;
        sfu_appsrc_ = nullptr; sfu_download_ = nullptr;
        return false;
    }

    // Pipeline reached PLAYING. Attach a persistent bus watch so that errors
    // occurring mid-stream (GPU fault, driver crash, encoder stall) are caught
    // and egress is disabled rather than silently sending nothing.
    GstBus* sfuBus = gst_pipeline_get_bus(GST_PIPELINE(sfu_pipeline_));
    if (sfuBus) {
        sfu_bus_watch_id_ = gst_bus_add_watch(sfuBus, OnSfuBusMessage, this);
        gst_object_unref(sfuBus);
    }

    // Kick the bridge encoder for a few startup IDRs even before RTCP PLIs arrive.
    // Some encoder paths do not emit a browser-decodable first IDR soon enough on
    // their own, which leaves WebRTC receiving RTP packets but zero complete frames.
    auto requestStartupKeyframe = [](gpointer data) -> gboolean {
        auto* self = static_cast<IngestPipeline*>(data);
        if (!self || !self->sfu_encoder_) return G_SOURCE_REMOVE;

        spdlog::info("[{}] [SFU-Bridge] Requesting startup keyframe", self->config_.camera_id);
        static guint key_unit_count = 0;
        GstEvent* key_unit = gst_video_event_new_downstream_force_key_unit(
            GST_CLOCK_TIME_NONE,
            GST_CLOCK_TIME_NONE,
            GST_CLOCK_TIME_NONE,
            TRUE,
            key_unit_count++);
        if (key_unit) {
            gst_element_send_event(self->sfu_encoder_, key_unit);
        }
        return G_SOURCE_REMOVE;
    };
    g_timeout_add(0, requestStartupKeyframe, this);
    g_timeout_add(750, requestStartupKeyframe, this);
    g_timeout_add(2000, requestStartupKeyframe, this);
    LogBridgeCaps(config_.camera_id, sfu_encoder_, sfu_capsfilter_, sfu_parse_, sfu_parse_capsfilter_, sfu_pay_);

    spdlog::info("[{}] SFU bridge pipeline started (H265->H264)", config_.camera_id);
    if (sfu_rtcp_src_) {
        spdlog::info("[{}] SFU bridge RTCP listener started on port {}", config_.camera_id, config.dst_port + 1);
    }
    return true;
}

gboolean IngestPipeline::OnSfuBusMessage(GstBus* /*bus*/, GstMessage* msg, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    switch (GST_MESSAGE_TYPE(msg)) {
        case GST_MESSAGE_ERROR: {
            GError* err; gchar* dbg;
            gst_message_parse_error(msg, &err, &dbg);
            spdlog::error("[{}] SFU bridge async error: {} ({})",
                self->config_.camera_id, err->message, dbg ? dbg : "");
            g_clear_error(&err); g_free(dbg);
            // Stop feeding the dead pipeline; cleanup happens at next StopSfuRtpEgress.
            self->sfu_egress_running_ = false;
            break;
        }
        case GST_MESSAGE_EOS:
            spdlog::warn("[{}] SFU bridge received EOS — disabling egress", self->config_.camera_id);
            self->sfu_egress_running_ = false;
            break;
        default: break;
    }
    return TRUE;
}

void IngestPipeline::StopSfuRtpEgress() {
    // Also handle the case where the bus watch has already cleared sfu_egress_running_
    // but sfu_pipeline_ still needs to be torn down.
    if (!sfu_egress_running_ && !sfu_pipeline_) return;
    spdlog::info("[{}] Stopping SFU egress", config_.camera_id);

    sfu_egress_running_ = false;

    // Remove the bridge bus watch before destroying the pipeline to prevent
    // the callback from firing on a partially-destroyed object.
    if (sfu_bus_watch_id_ > 0) {
        g_source_remove(sfu_bus_watch_id_);
        sfu_bus_watch_id_ = 0;
    }

    if (sfu_rtcp_src_) {
        gst_element_set_state(sfu_rtcp_src_, GST_STATE_NULL);
        // If sfu_pipeline_ exists, it's already there; if not, it might be in pipeline_
        if (sfu_pipeline_) gst_bin_remove(GST_BIN(sfu_pipeline_), sfu_rtcp_src_);
        else if (pipeline_) gst_bin_remove(GST_BIN(pipeline_), sfu_rtcp_src_);
        sfu_rtcp_src_ = nullptr;
    }

    // Stop and cleanup bridge pipeline (H265)
    if (sfu_pipeline_) {
        gst_element_set_state(sfu_pipeline_, GST_STATE_NULL);
        gst_object_unref(sfu_pipeline_);
        sfu_pipeline_ = nullptr;
        sfu_appsrc_ = nullptr;    // owned by sfu_pipeline_
        sfu_download_ = nullptr;  // owned by sfu_pipeline_
        sfu_capsfilter_ = nullptr;
        sfu_parse_capsfilter_ = nullptr;
    }

    // Unlink and cleanup tee branch (H264)
    if (sfu_queue_) {
        GstPad* q_sink_pad = gst_element_get_static_pad(sfu_queue_, "sink");
        GstPad* tee_src_pad = gst_pad_get_peer(q_sink_pad);
        if (tee_src_pad) {
            gst_pad_unlink(tee_src_pad, q_sink_pad);
            gst_element_release_request_pad(tee_, tee_src_pad);
            gst_object_unref(tee_src_pad);
        }
        gst_object_unref(q_sink_pad);
        gst_element_set_state(sfu_queue_, GST_STATE_NULL);
        
        gst_bin_remove(GST_BIN(pipeline_), sfu_queue_);
        sfu_queue_ = nullptr;
    }

    // Cleanup common elements (used by both paths or specifically H.264)
    if (sfu_decoder_) { gst_element_set_state(sfu_decoder_, GST_STATE_NULL); if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_decoder")) gst_bin_remove(GST_BIN(pipeline_), sfu_decoder_); sfu_decoder_ = nullptr; }
    if (sfu_converter_) { gst_element_set_state(sfu_converter_, GST_STATE_NULL); if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_conv")) gst_bin_remove(GST_BIN(pipeline_), sfu_converter_); sfu_converter_ = nullptr; }
    if (sfu_encoder_) { gst_element_set_state(sfu_encoder_, GST_STATE_NULL); if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_encoder")) gst_bin_remove(GST_BIN(pipeline_), sfu_encoder_); sfu_encoder_ = nullptr; }
    if (sfu_parse_) { gst_element_set_state(sfu_parse_, GST_STATE_NULL); if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_parse")) gst_bin_remove(GST_BIN(pipeline_), sfu_parse_); sfu_parse_ = nullptr; }
    
    if (sfu_pay_) {
        gst_element_set_state(sfu_pay_, GST_STATE_NULL);
        if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_pay")) gst_bin_remove(GST_BIN(pipeline_), sfu_pay_);
        sfu_pay_ = nullptr;
    }
    if (sfu_sink_) {
        gst_element_set_state(sfu_sink_, GST_STATE_NULL);
        if (GST_IS_BIN(pipeline_) && gst_bin_get_by_name(GST_BIN(pipeline_), "sfu_sink")) gst_bin_remove(GST_BIN(pipeline_), sfu_sink_);
        sfu_sink_ = nullptr;
    }
}

void IngestPipeline::SetupHlsBranch() {
    if (!hls_config_.enabled) return;
    CreateHlsSession();
    hls_queue_ = gst_element_factory_make("queue", "hls_queue");
    hls_sink_ = gst_element_factory_make("splitmuxsink", "hls_sink");
    g_object_set(hls_sink_, "muxer-factory", "mp4mux", "location", (fs::path(hls_state_.dir_path) / "seg_%05d.mp4").string().c_str(), "max-size-time", (guint64)2000000000, "async-finalize", TRUE, NULL);
    gst_bin_add_many(GST_BIN(pipeline_), hls_queue_, hls_sink_, NULL);
    gst_element_link(hls_queue_, hls_sink_);
    GstPad *tee_src = gst_element_request_pad_simple(tee_, "src_%u");
    GstPad *q_sink = gst_element_get_static_pad(hls_queue_, "sink");
    gst_pad_link(tee_src, q_sink);
    hls_tee_pad_ = tee_src;
    gst_object_unref(q_sink);
}

void IngestPipeline::CreateHlsSession() {
    hls_state_.session_id = "hls_" + std::to_string(std::chrono::system_clock::now().time_since_epoch().count());
    hls_state_.dir_path = (fs::path(hls_config_.root_dir) / config_.camera_id / hls_state_.session_id).string();
    fs::create_directories(hls_state_.dir_path);
}

void IngestPipeline::DisableHlsBranch(const std::string& reason) {
    if (!hls_sink_) return;
    gst_element_set_state(hls_sink_, GST_STATE_NULL);
    spdlog::warn("[{}] HLS disabled: {}", config_.camera_id, reason);
}

IngestPipeline::HlsState IngestPipeline::GetHlsState() const { return hls_state_; }
bool IngestPipeline::GetHlsBranchPending() const { return hls_branch_pending_; }
void IngestPipeline::ClearHlsBranchPending() { hls_branch_pending_ = false; }
void IngestPipeline::SetHlsDegraded(bool d, const std::string& e) { hls_state_.degraded = d; hls_state_.last_error = e; }
std::optional<std::vector<uint8_t>> IngestPipeline::CaptureSnapshot() { return std::nullopt; }

void IngestPipeline::HandleStall() {
    spdlog::warn("[{}] Ingest stall detected", config_.camera_id);
    fsm_.TransitionTo(State::RECONNECTING);
}

void IngestPipeline::UpdateMetaJson() {
    if (hls_state_.dir_path.empty()) return;
    try {
        json j;
        j["session_id"] = hls_state_.session_id;
        j["camera_id"] = config_.camera_id;
        j["start_time_unix"] = std::chrono::system_clock::now().time_since_epoch().count();
        j["codec"] = GetCodecString();
        
        std::ofstream o(fs::path(hls_state_.dir_path) / "meta.json");
        o << std::setw(4) << j << std::endl;
    } catch (const std::exception& e) {
        spdlog::error("[{}] Failed to update HLS meta.json: {}", config_.camera_id, e.what());
    }
}

GstPadProbeReturn IngestPipeline::OnSfuRtcpProbe(GstPad* /*pad*/, GstPadProbeInfo* /*info*/, gpointer data) {
    IngestPipeline* self = static_cast<IngestPipeline*>(data);
    
    // SFU sends RTCP PLI/FIR to request a keyframe.
    // Send a GstForceKeyUnitEvent upstream through the SFU bridge pipeline
    // so it reaches the encoder.  sfu_pay_ is the downstream-most element
    // in the bridge; the event propagates upstream through h264parse and
    // capsfilter to openh264enc / mfh264enc which then emits an IDR.
    // NOTE: g_signal_emit_by_name("force-key-unit") is NOT supported by
    // openh264enc and was previously sending the event to self->source_
    // (the RTSP ingest source) which is in a different pipeline entirely.
    GstElement* fku_target = self->sfu_pay_ ? self->sfu_pay_ : self->sfu_encoder_;
    if (fku_target) {
        spdlog::info("[{}] [SFU-Bridge] RTCP feedback received, forcing keyframe on bridge encoder", self->config_.camera_id);
        GstEvent* event = gst_video_event_new_upstream_force_key_unit(GST_CLOCK_TIME_NONE, TRUE, 0);
        if (!gst_element_send_event(fku_target, event)) {
            spdlog::warn("[{}] [SFU-Bridge] force-key-unit send_event failed — IDR may be delayed", self->config_.camera_id);
        }
    }
    
    return GST_PAD_PROBE_OK;
}

} // namespace ts::vms::media::pipeline
