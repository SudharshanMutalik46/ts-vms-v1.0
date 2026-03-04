//go:build ignore

package main

import (
	"log"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/control"
	"github.com/technosupport/ts-vms/internal/recording"
)

func main() {
	serviceKey := "super-secret-internal-key-47"

	// --- 1. Boot Recording Service (Internal 18082) ---
	cfg := &recording.Config{}
	cfg.Cameras = []recording.CameraConfig{{ID: "cam-01", Enabled: true}, {ID: "cam-02", Enabled: true}}

	sched := recording.NewScheduleEngine(nil)
	lic := recording.NewLicenseGate(1) // Max 1 camera for quota testing!

	dbMock := &recording.PostgresMetadataDB{DB: nil}
	baseSup := recording.NewSupervisor(cfg, sched, lic, dbMock)
	supExt := recording.NewSupervisorExt(baseSup)

	internalSrv := &recording.InternalAPI{ServiceKey: serviceKey, Supervisor: supExt}
	go func() {
		log.Println("Internal API running on :18082")
		// Log fatally if the port is in use
		if err := http.ListenAndServe(":18082", internalSrv.ServeMux()); err != nil {
			log.Fatalf("Internal API failed to start: %v", err)
		}
	}()

	// --- 2. Boot Control Service (Public 18080) ---
	publicSrv := &control.PublicRecordingAPI{
		InternalBaseURL: "http://127.0.0.1:18082",
		ServiceKey:      serviceKey,
		DB:              dbMock,
		ExportPipeline:  &recording.ExportService{DB: dbMock, Stitcher: &recording.Stitcher{}},
	}
	go func() {
		log.Println("Public API running on :18080")
		// Log fatally if the port is in use
		if err := http.ListenAndServe(":18080", publicSrv.ServeMux()); err != nil {
			log.Fatalf("Public API failed to start: %v", err)
		}
	}()

	// Keep alive for verification script
	time.Sleep(1 * time.Hour)
}
