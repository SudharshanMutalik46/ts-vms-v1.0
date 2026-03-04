package main

import (
	"flag"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/technosupport/ts-vms/internal/recording/health"
)

func main() {
	dropFlag := flag.Bool("simulate-drops", false, "Simulate >5% frame drops")
	slowDiskFlag := flag.Bool("simulate-slow-disk", false, "Simulate <0.5MBps write rate")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg := health.Config{}
	cfg.Enabled = true
	cfg.SampleIntervalSec = 2 // Fast for testing
	cfg.FrameDrop.Enabled = true
	cfg.FrameDrop.WarnDropRatePct = 1.0
	cfg.FrameDrop.CritDropRatePct = 5.0
	cfg.FrameDrop.WindowSec = 10
	cfg.DiskWriteRate.Enabled = true
	cfg.DiskWriteRate.WindowSec = 10
	cfg.DiskWriteRate.WarnMinMBps = 2.0
	cfg.DiskWriteRate.CritMinMBps = 0.5
	cfg.Alerts.CooldownSec = 5
	cfg.Alerts.SustainedWindowsForCritical = 2

	mgr := health.NewManager(cfg)
	mgr.Start()

	camID := "cam-test-01"
	mgr.SetState(camID, health.StateActive, nil)

	// Simulation loop
	go func() {
		for {
			time.Sleep(100 * time.Millisecond) // 10 FPS

			// Normal: 3 MB/s total -> ~314KB per frame
			bytesPerFrame := uint64(300 * 1024)
			if *slowDiskFlag {
				bytesPerFrame = uint64(20 * 1024) // Drops to ~0.2 MB/s
			}

			dropped := uint64(0)
			valid := uint64(1)

			if *dropFlag {
				// 20% drop rate to ensure we hit >5% critical threshold quickly
				if rand.Float32() < 0.20 {
					dropped = 1
					valid = 0
				}
			}

			mgr.AddTelemetry(camID, bytesPerFrame, valid, dropped, time.Now())
		}
	}()

	http.HandleFunc("/status", health.StatusHandler(mgr))
	slog.Info("Test harness running on :8089")
	if err := http.ListenAndServe(":8089", nil); err != nil {
		slog.Error("Failed to start server", "err", err)
	}
}
