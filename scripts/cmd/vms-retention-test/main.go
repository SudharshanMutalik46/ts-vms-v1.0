package main

import (
    "fmt"
    "os"
    "strconv"
    "time"

    "github.com/technosupport/ts-vms/internal/retention"
)

type MockSpaceVerifier struct{}
func (m *MockSpaceVerifier) GetFreeSpace(p string) (uint64, error) { return 1000000000, nil }
func (m *MockSpaceVerifier) VerifyReclamation(path string, expectedBytes int64, beforeBytes uint64) {}

type MockProtector struct{}
func (m MockProtector) IsProtected(cam, file string) bool { return file == "cam02_protected.mp4" }

func main() {
    fastForwardDays := 0
    if len(os.Args) > 1 {
        ff, _ := strconv.Atoi(os.Args[1])
        fastForwardDays = ff
    }

    var cfg retention.Config
    cfg.Defaults.DaysToKeep = 5
    cfg.Defaults.MaxStorageGB = 0
    cfg.Defaults.DryRun = false
    
    cfg.Safety.IncludeSidecars = true
    cfg.Safety.NeverDeleteNewerThanMinutes = 0
    
    cfg.Protection.ProtectIfEventLinked = true

    // Set the 90-day override for cam-01
    cfg.Scopes.Cameras = []retention.CameraConfig{
        {
            CameraID: "cam-01",
            ScopeConfig: retention.ScopeConfig{
                DaysToKeep: 90,
            },
        },
    }

    idx := retention.NewFileSystemEnumerator()
    prot := MockProtector{}
    aud := retention.NewJSONAuditWriter("retention_audit.jsonl")
    ver := &MockSpaceVerifier{}

    engine := retention.NewRetentionEngine(cfg, prot, idx, ver, aud)

    // Run Engine
    runTime := time.Now().Add(time.Duration(fastForwardDays) * 24 * time.Hour)
    status := engine.RunOnce(runTime)
    
    fmt.Printf("DONE: Deleted: %d, Skipped Protected: %d, Errors: %d\n", status.DeletedCount, status.SkippedProtected, status.Errors)
}
