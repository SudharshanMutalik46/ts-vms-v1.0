package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/technosupport/ts-vms/internal/recording/circuit_breaker"
	"github.com/technosupport/ts-vms/internal/recording/health"
	"github.com/technosupport/ts-vms/internal/recording/recovery"
)

func main() {
	simulateCrashFlag := flag.Bool("simulate-crash", false, "Force the harness to exit with code 1")
	simulateDiskFullFlag := flag.Bool("simulate-disk-full", false, "Mock volume stats to < CritFreeGB")
	simulateNoDbFlag := flag.Bool("simulate-no-db", false, "Simulate unreachable DB")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if *simulateCrashFlag {
		slog.Error("FATAL: Unhandled Exception. Access Violation 0x00000000.")
		os.Exit(1)
	}

	// 1. Storage Circuit Breaker
	cbCfg := circuit_breaker.Config{
		Enabled:          true,
		WarnFreeGB:       20,
		CritFreeGB:       10,
		WarnUsagePercent: 80,
		CritUsagePercent: 95,
		CheckIntervalSec: 2,
		CooldownSec:      2,
	}

	mockDisk := &circuit_breaker.MockDiskProvider{
		Stats: map[string]circuit_breaker.VolumeStats{
			"C:\\": {
				Path:         "C:\\",
				TotalBytes:   1000 * 1024 * 1024 * 1024,
				FreeBytes:    50 * 1024 * 1024 * 1024,
				UsagePercent: 95.0,
			},
		},
	}

	if *simulateDiskFullFlag {
		slog.Warn("Simulating Disk Full on C:\\")
		mockDisk.Stats["C:\\"] = circuit_breaker.VolumeStats{
			FreeBytes:    5 * 1024 * 1024 * 1024, // 5GB (Critical)
			UsagePercent: 99.5,
		}
	} else {
		// Ensure non-critical defaults explicitly (e.g. usage > 95 is critical, so we drop it)
		mockDisk.Stats["C:\\"] = circuit_breaker.VolumeStats{
			FreeBytes:    50 * 1024 * 1024 * 1024,
			UsagePercent: 50.0,
		}
	}

	cb := circuit_breaker.NewManager(cbCfg, mockDisk, []string{"C:\\"})
	cb.Start()
	defer cb.Stop()

	// 2. Recovery DB
	mockIndex := &recovery.MockIndex{
		Data: map[string]recovery.SegmentMeta{
			"cam-01": {Path: "cam-01_123456.mp4", EndTS: time.Now().Add(-10 * time.Minute), IsCorrupt: false},
			"cam-02": {Path: "cam-02_corrupt.mp4", EndTS: time.Now().Add(-5 * time.Minute), IsCorrupt: true},
		},
	}

	recCfg := recovery.Config{
		Enabled:             true,
		RestartBackoffSec:   5,
		DBRequiredForReady:  true,
		OrphanReconcileMode: "log_only",
	}

	recMgr := recovery.NewManager(recCfg, mockIndex)
	recScanner := recovery.NewScanner(recCfg)

	// 3. Perform pre-startup recovery tasks
	slog.Info("Starting VMS Recording Startup Sequence...")
	recScanner.RunReconciliation([]string{"C:\\"})

	plan1 := recMgr.EvaluateResume("cam-01") // Normal resume
	plan2 := recMgr.EvaluateResume("cam-02") // Corrupt segment resume
	plan3 := recMgr.EvaluateResume("cam-03") // No history baseline

	slog.Info("Computed plans", "cam-01", plan1.StartFresh, "cam-02", plan2.StartFresh, "cam-03", plan3.StartFresh)

	// 4. Expose Health Endpoints
	healthState := health.ReadinessState{
		IsDBConnected: func() bool {
			return !*simulateNoDbFlag // Reverse flag to make logic clear
		},
		IsBreakerEngaged: func() bool {
			return cb.IsEngaged()
		},
		DBRequiredForReady: recCfg.DBRequiredForReady,
	}

	http.HandleFunc("/healthz", health.LivenessHandler())
	http.HandleFunc("/readyz", health.ReadinessHandler(healthState))

	// Allow normal manager instantiation for empty /status so compilation passes
	healthCfg := health.Config{}
	hm := health.NewManager(healthCfg)
	http.HandleFunc("/status", health.StatusHandler(hm))

	slog.Info("Test harness running on :8082")
	http.ListenAndServe(":8082", nil)
}
