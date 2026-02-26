package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/technosupport/ts-vms/internal/recording"
	"gopkg.in/yaml.v3"
)

func main() {
	log.Println("Starting vms-recording service...")

	// 1. Load Config
	cfgData, err := os.ReadFile("../../config/recording.yaml") // Need to point to correct relative root when running scripts or correct absolute path, using relative fallback here for standalone testing, updated below via explicit path or workingdir assuming config is up two dirs
	if err != nil {
		// Try fallback if running directly from repo root
		cfgData, err = os.ReadFile("config/recording.yaml")
		if err != nil {
			log.Fatalf("Failed to read config: %v", err)
		}
	}
	var cfg recording.Config
	yaml.Unmarshal(cfgData, &cfg)

	// 2. Crash Recovery Hooks
	recMgr := &recording.RecoveryManager{
		PidPath: "vms-recording.pid",
		Hooks:   &recording.Phase43RecoveryStub{},
	}
	recMgr.CheckAndProtect()
	defer recMgr.CleanExit()

	// 3. Initialize Modules
	scheduler := recording.NewScheduleEngine(cfg.Schedules)
	license := recording.NewLicenseGate(cfg.Limits.MaxRecordingCameras)
	supervisor := recording.NewSupervisor(&cfg, scheduler, license)

	health := recording.NewHealthServer(&cfg, supervisor, scheduler)
	health.Start()

	// 4. Start Supervisor
	ctx, cancel := context.WithCancel(context.Background())
	go supervisor.Run(ctx)

	// Wait for interrupt
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	fmt.Println("\nShutting down gracefully...")
	cancel()
}
