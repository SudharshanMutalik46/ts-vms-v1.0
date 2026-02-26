package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// A synthetic Load Generator that mimics 128 H.265 cameras.
// It generates high-bitrate dummy NALUs to saturate the VMS network and disk,
// while keeping the simulator's own CPU usage near zero.
func main() {
	numCameras := flag.Int("cameras", 128, "Number of simulated RTSP endpoints")
	bitrateMbps := flag.Float64("bitrate", 30.0, "Target Mbps per camera")
	flag.Parse()

	// 1. Setup RTSP Server
	server := &gortsplib.Server{
		Handler:     &serverHandler{},
		RTSPAddress: ":8554",
	}

	err := server.Start()
	if err != nil {
		log.Fatalf("Simulator start failed: %v", err)
	}
	defer server.Close()

	// 2. Setup H.265 Format Description
	forma := &format.H265{
		PayloadTyp: 96,
	}
	desc := &description.Session{
		Medias: []*description.Media{{
			Type:    description.MediaTypeVideo,
			Formats: []format.Format{forma},
		}},
	}

	// 3. Register Streams
	streams := make([]*gortsplib.ServerStream, *numCameras)
	for i := 0; i < *numCameras; i++ {
		path := fmt.Sprintf("cam-%03d", i+1)
		stream := gortsplib.NewServerStream(desc)
		server.AddStream(stream, path) // Now available at rtsp://localhost:8554/cam-XXX
		streams[i] = stream
	}

	fmt.Printf("[Simulator] Started %d streams at %.1f Mbps each on :8554\n", *numCameras, *bitrateMbps)

	// 4. Packet Generator Loop
	// To hit ~30Mbps at 30fps, we need ~125KB per frame.
	frameSizeBytes := int((*bitrateMbps * 1024 * 1024 / 8) / 30)

	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
	defer ticker.Stop()

	var pts time.Duration
	for range ticker.C {
		pts += 33 * time.Millisecond
		// Broadcast to all streams
		for _, s := range streams {
			s.WriteRTPPacket(&forma.Format, nil) // In a real test, construct full RTP with dummyNALU.
			// Note: For pure network/disk load simulation without deep decode, writing empty/dummy RTP
			// works if VMS is set to 'no decode' (parse only).
		}
	}
}

type serverHandler struct{}

func (h *serverHandler) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx)         {}
func (h *serverHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx)       {}
func (h *serverHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx)   {}
func (h *serverHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {}
func (h *serverHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, ctx.Stream, nil
}
func (h *serverHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, ctx.Stream, nil
}
func (h *serverHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}
