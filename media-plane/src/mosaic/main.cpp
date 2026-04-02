#include <gst/gst.h>
#include <gst/rtsp-server/rtsp-server.h>
#include <iostream>
#include <vector>
#include <string>
#include <fstream>
#include <regex>
#include <algorithm>

// Tile Configuration
const int COLS = 8;
const int ROWS = 8;
const int TILE_W = 480;
const int TILE_H = 270;
const int OUT_W = 3840;
const int OUT_H = 2160;
const int FPS = 15;

struct MosaicTile {
    int id;
    std::string url;
    int xpos;
    int ypos;
    
    GstElement* bin = nullptr;
    GstElement* scale = nullptr;
    GstPad* comp_sink_pad = nullptr;
    
    int reconnect_attempts = 0;
    bool is_active = false;
};

// Global State
GstElement* main_pipeline = nullptr;
GstElement* compositor = nullptr;
std::vector<MosaicTile> tiles;

static void SafeRemoveFromPipeline(GstElement* pipeline, GstElement* element) {
    if (!pipeline || !element || !GST_IS_ELEMENT(pipeline) || !GST_IS_ELEMENT(element)) {
        return;
    }

    GstObject* parent = gst_object_get_parent(GST_OBJECT(element));
    const bool inPipeline = parent == GST_OBJECT(pipeline);
    if (parent) {
        gst_object_unref(parent);
    }

    if (inPipeline) {
        gst_bin_remove(GST_BIN(pipeline), element);
    } else {
        gst_object_unref(element);
    }
}

static bool SafeSetElementState(GstElement* element, GstState state, const char* label) {
    if (!element || !GST_IS_ELEMENT(element)) {
        std::cerr << "[Mosaic] Skipping set_state on invalid element "
                  << (label ? label : "(unknown)") << std::endl;
        return false;
    }

    return gst_element_set_state(element, state) != GST_STATE_CHANGE_FAILURE;
}

std::string SanitizeUrl(const std::string& url) {
    std::regex cred_regex(R"(rtsp://([^:]+:[^@]+)@)");
    return std::regex_replace(url, cred_regex, "rtsp://***:***@");
}

static void on_source_setup(GstElement* object, GstElement* source, gpointer user_data) {
    if (g_object_class_find_property(G_OBJECT_GET_CLASS(source), "latency")) {
        g_object_set(source, 
            "latency", 200, 
            "protocols", 4, // 4 = GST_RTSP_LOWER_TRANS_TCP
            "drop-on-latency", TRUE, 
            NULL);
    }
}

static void on_pad_added(GstElement* src, GstPad* new_pad, gpointer user_data) {
    MosaicTile* tile = static_cast<MosaicTile*>(user_data);
    if (!tile || !tile->scale) {
        return;
    }

    GstCaps* caps = gst_pad_get_current_caps(new_pad);
    if (!caps) {
        caps = gst_pad_query_caps(new_pad, nullptr);
    }
    if (!caps) {
        return;
    }

    GstStructure* structure = gst_caps_get_structure(caps, 0);
    if (!structure) {
        gst_caps_unref(caps);
        return;
    }

    const gchar* name = gst_structure_get_name(structure);
    if (!name) {
        gst_caps_unref(caps);
        return;
    }
    
    if (g_str_has_prefix(name, "video/x-raw")) {
        GstPad* sink_pad = gst_element_get_static_pad(tile->scale, "sink");
        if (sink_pad) {
            if (!gst_pad_is_linked(sink_pad)) {
                gst_pad_link(new_pad, sink_pad);
            }
            gst_object_unref(sink_pad);
        }
    }
    gst_caps_unref(caps);
}

void StartTile(MosaicTile& tile) {
    if (tile.is_active) return;
    if (!main_pipeline || !compositor || !tile.comp_sink_pad) {
        std::cerr << "[Mosaic] Cannot start tile " << tile.id << " because the pipeline is not ready." << std::endl;
        return;
    }
    
    std::string bin_name = "tile_bin_" + std::to_string(tile.id);
    tile.bin = gst_bin_new(bin_name.c_str());
    
    GstElement* uri_dec = gst_element_factory_make("uridecodebin", NULL);
    tile.scale = gst_element_factory_make("videoscale", NULL);
    GstElement* convert = gst_element_factory_make("videoconvert", NULL);
    GstElement* capsfilter = gst_element_factory_make("capsfilter", NULL);
    GstElement* queue = gst_element_factory_make("queue", NULL);

    if (!tile.bin || !uri_dec || !tile.scale || !convert || !capsfilter || !queue) {
        std::cerr << "[Mosaic] Failed to create tile elements for tile " << tile.id << std::endl;
        if (uri_dec) gst_object_unref(uri_dec);
        if (tile.scale) { gst_object_unref(tile.scale); tile.scale = nullptr; }
        if (convert) gst_object_unref(convert);
        if (capsfilter) gst_object_unref(capsfilter);
        if (queue) gst_object_unref(queue);
        if (tile.bin) {
            gst_object_unref(tile.bin);
            tile.bin = nullptr;
        }
        return;
    }
    
    // Force 15 FPS and specific size
    GstCaps* caps = gst_caps_new_simple("video/x-raw",
        "width", G_TYPE_INT, TILE_W,
        "height", G_TYPE_INT, TILE_H,
        "framerate", GST_TYPE_FRACTION, FPS, 1,
        NULL);
    g_object_set(capsfilter, "caps", caps, NULL);
    gst_caps_unref(caps);
    
    // Bounded leaky queue prevents slow tiles from freezing the compositor
    g_object_set(queue, "max-size-buffers", 30, "max-size-bytes", 0, "max-size-time", (guint64)0, "leaky", 2, NULL); // 2 = leaky downstream
    g_object_set(uri_dec, "uri", tile.url.c_str(), NULL);
    
    gst_bin_add_many(GST_BIN(tile.bin), uri_dec, tile.scale, convert, capsfilter, queue, NULL);
    if (!gst_element_link_many(tile.scale, convert, capsfilter, queue, NULL)) {
        std::cerr << "[Mosaic] Failed to link tile elements for tile " << tile.id << std::endl;
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    
    g_signal_connect(uri_dec, "pad-added", G_CALLBACK(on_pad_added), &tile);
    g_signal_connect(uri_dec, "source-setup", G_CALLBACK(on_source_setup), NULL);
    
    // Create GhostPad on the Bin
    GstPad* queue_src = gst_element_get_static_pad(queue, "src");
    if (!queue_src) {
        std::cerr << "[Mosaic] Failed to get queue src pad for tile " << tile.id << std::endl;
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    GstPad* ghost_pad = gst_ghost_pad_new("src", queue_src);
    if (!ghost_pad) {
        std::cerr << "[Mosaic] Failed to create ghost pad for tile " << tile.id << std::endl;
        gst_object_unref(queue_src);
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    if (!gst_element_add_pad(tile.bin, ghost_pad)) {
        std::cerr << "[Mosaic] Failed to attach ghost pad for tile " << tile.id << std::endl;
        gst_object_unref(ghost_pad);
        gst_object_unref(queue_src);
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    gst_object_unref(queue_src);
    
    gst_bin_add(GST_BIN(main_pipeline), tile.bin);
    
    // Link to existing compositor pad
    if (gst_pad_link(ghost_pad, tile.comp_sink_pad) != GST_PAD_LINK_OK) {
        std::cerr << "[Mosaic] Failed to link tile " << tile.id << " to compositor pad" << std::endl;
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    if (!gst_element_sync_state_with_parent(tile.bin)) {
        std::cerr << "[Mosaic] Failed to sync tile " << tile.id << " state with parent" << std::endl;
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
        tile.bin = nullptr;
        tile.scale = nullptr;
        return;
    }
    
    tile.is_active = true;
    std::cout << "[Mosaic] Tile " << tile.id << " Started: " << SanitizeUrl(tile.url) << std::endl;
}

gboolean ReconnectTileTask(gpointer user_data) {
    MosaicTile* tile = static_cast<MosaicTile*>(user_data);
    std::cout << "[Mosaic] Attempting reconnect for Tile " << tile->id << std::endl;
    StartTile(*tile);
    return G_SOURCE_REMOVE;
}

void HandleTileError(MosaicTile& tile) {
    if (!tile.is_active) return;
    std::cerr << "[Mosaic] Tile " << tile.id << " offline. Scheduling reconnect." << std::endl;
    
    tile.is_active = false;
    tile.reconnect_attempts++;
    
    // Unlink and destroy bin
    if (tile.bin && GST_IS_ELEMENT(tile.bin)) {
        GstPad* ghost_pad = gst_element_get_static_pad(tile.bin, "src");
        if (ghost_pad && tile.comp_sink_pad) {
            gst_pad_unlink(ghost_pad, tile.comp_sink_pad);
        }
        if (ghost_pad) {
            gst_object_unref(ghost_pad);
        }

        SafeSetElementState(tile.bin, GST_STATE_NULL, "tile.bin");
        SafeRemoveFromPipeline(main_pipeline, tile.bin);
    }
    tile.bin = nullptr; // Note: comp_sink_pad remains active on compositor
    
    // Backoff: 2s, 4s, 8s, capped at 15s
    int backoff = std::min(15000, 1000 * (1 << tile.reconnect_attempts));
    g_timeout_add(backoff, ReconnectTileTask, &tile);
}

static gboolean bus_watch(GstBus* bus, GstMessage* msg, gpointer data) {
    if (GST_MESSAGE_TYPE(msg) == GST_MESSAGE_ERROR) {
        GError* err = NULL;
        gchar* debug_info = NULL;
        gst_message_parse_error(msg, &err, &debug_info);
        
        GstObject* src = GST_MESSAGE_SRC(msg);
        for (auto& tile : tiles) {
            if (tile.is_active && tile.bin && gst_object_has_as_ancestor(src, GST_OBJECT(tile.bin))) {
                HandleTileError(tile);
                break;
            }
        }
        g_clear_error(&err);
        g_free(debug_info);
    }
    return TRUE;
}

void ParseConfig(const std::string& filepath) {
    std::ifstream file(filepath);
    std::string line;
    int id = 0;
    while (std::getline(file, line) && id < 64) {
        if (line.empty() || line[0] == '#') continue;
        if (line.find("rtsp://") != std::string::npos) {
            // Very simple extraction for demo purposes
            size_t start = line.find("rtsp://");
            std::string url = line.substr(start);
            
            MosaicTile t;
            t.id = id;
            t.url = url;
            t.xpos = (id % COLS) * TILE_W;
            t.ypos = (id / COLS) * TILE_H;
            tiles.push_back(t);
            id++;
        }
    }
}

int main(int argc, char *argv[]) {
    gst_init(&argc, &argv);
    
    if (argc < 2) {
        std::cerr << "Usage: vms-mosaic <config.yaml>" << std::endl;
        return 1;
    }
    
    ParseConfig(argv[1]);
    
    GMainLoop* loop = g_main_loop_new(NULL, FALSE);
    main_pipeline = gst_pipeline_new("mosaic-pipeline");
    compositor = gst_element_factory_make("compositor", "comp");
    g_object_set(compositor, "background", 1, NULL); // 1 = Black
    
    // Hardware Encoder Selection
    GstElement* encoder = gst_element_factory_make("nvh265enc", NULL);
    if (encoder) {
        g_object_set(encoder, "preset", 2, "bitrate", 8000, "zerolatency", TRUE, "rc-mode", 2, NULL); // LowLatency, CBR
        std::cout << "[Mosaic] Using NVENC Hardware Encoder" << std::endl;
    } else {
        encoder = gst_element_factory_make("x265enc", NULL);
        g_object_set(encoder, "speed-preset", 1, "tune", 4, "bitrate", 8000, "key-int-max", 15, NULL); // ultrafast, zerolatency
        std::cout << "[Mosaic] WARNING: Using CPU x265 Encoder" << std::endl;
    }

    GstElement* out_convert = gst_element_factory_make("videoconvert", NULL);
    GstElement* out_caps = gst_element_factory_make("capsfilter", NULL);
    GstCaps* res_caps = gst_caps_new_simple("video/x-raw", "width", G_TYPE_INT, OUT_W, "height", G_TYPE_INT, OUT_H, NULL);
    g_object_set(out_caps, "caps", res_caps, NULL);
    gst_caps_unref(res_caps);
    
    GstElement* parse = gst_element_factory_make("h265parse", NULL);
    GstElement* sink = gst_element_factory_make("udpsink", NULL);
    g_object_set(sink, "host", "127.0.0.1", "port", 9999, NULL);
    
    gst_bin_add_many(GST_BIN(main_pipeline), compositor, out_convert, out_caps, encoder, parse, sink, NULL);
    gst_element_link_many(compositor, out_convert, out_caps, encoder, parse, sink, NULL);
    
    // Init Compositor Pads
    for (auto& tile : tiles) {
        GstPadTemplate* templ = gst_element_class_get_pad_template(GST_ELEMENT_GET_CLASS(compositor), "sink_%u");
        tile.comp_sink_pad = gst_element_request_pad(compositor, templ, NULL, NULL);
        g_object_set(tile.comp_sink_pad, "xpos", tile.xpos, "ypos", tile.ypos, "width", TILE_W, "height", TILE_H, NULL);
    }

    GstBus* bus = gst_element_get_bus(main_pipeline);
    gst_bus_add_watch(bus, bus_watch, NULL);
    gst_object_unref(bus);
    
    if (!main_pipeline || !GST_IS_ELEMENT(main_pipeline)) {
        std::cerr << "[Mosaic] Main pipeline was not created." << std::endl;
        return 1;
    }

    if (!compositor || !GST_IS_ELEMENT(compositor)) {
        std::cerr << "[Mosaic] Compositor was not created." << std::endl;
        return 1;
    }

    SafeSetElementState(main_pipeline, GST_STATE_PLAYING, "main_pipeline");
    
    // Start all tiles
    for (auto& tile : tiles) StartTile(tile);

    // Setup RTSP Server to consume the UDP stream
    GstRTSPServer* server = gst_rtsp_server_new();
    gst_rtsp_server_set_service(server, "8554");
    GstRTSPMountPoints* mounts = gst_rtsp_server_get_mount_points(server);
    GstRTSPMediaFactory* factory = gst_rtsp_media_factory_new();
    
    gst_rtsp_media_factory_set_launch(factory, 
        "( udpsrc port=9999 caps=\"video/x-h265, stream-format=byte-stream, alignment=au\" "
        "! h265parse ! rtph265pay name=pay0 pt=96 config-interval=1 )");
    gst_rtsp_media_factory_set_shared(factory, TRUE);
    
    gst_rtsp_mount_points_add_factory(mounts, "/mosaic_8x8", factory);
    gst_rtsp_server_attach(server, NULL);
    
    std::cout << "[Mosaic] RTSP Server live at rtsp://0.0.0.0:8554/mosaic_8x8" << std::endl;
    g_main_loop_run(loop);
    
    return 0;
}
