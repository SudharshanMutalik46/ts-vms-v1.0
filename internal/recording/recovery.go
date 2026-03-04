package recording

import (
	"context"
	"log"
	"os"
)

type RecoveryManager struct {
	PidPath string
	Run     func(context.Context) error
}

func (r *RecoveryManager) CheckAndProtect(ctx context.Context) error {
	if _, err := os.Stat(r.PidPath); err == nil {
		log.Println("[WARNING] abnormal previous shutdown detected, running startup recovery")
		if r.Run != nil {
			if err := r.Run(ctx); err != nil {
				return err
			}
		}
	}
	return os.WriteFile(r.PidPath, []byte("running"), 0o644)
}

func (r *RecoveryManager) CleanExit() {
	_ = os.Remove(r.PidPath)
}
