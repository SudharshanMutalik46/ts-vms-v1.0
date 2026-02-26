package recording

import (
	"log"
	"os"
)

type IRecoveryHooks interface {
	OnStartupScan()
}

type Phase43RecoveryStub struct{}

func (p *Phase43RecoveryStub) OnStartupScan() {
	log.Println("[RECOVERY] Phase 4.3 will handle tmp file cleanup and corruption scanning here.")
}

type RecoveryManager struct {
	PidPath string
	Hooks   IRecoveryHooks
}

func (r *RecoveryManager) CheckAndProtect() {
	if _, err := os.Stat(r.PidPath); err == nil {
		log.Println("[WARNING] PID file found on boot! Previous shutdown was abnormal (Crash/Power Loss).")
		log.Println("[RECOVERY] Initiating recovery routines...")
		r.Hooks.OnStartupScan()
	}

	// Create new PID marker
	_ = os.WriteFile(r.PidPath, []byte("running"), 0644)
}

func (r *RecoveryManager) CleanExit() {
	_ = os.Remove(r.PidPath)
	log.Println("[INFO] Clean exit, PID file removed.")
}
