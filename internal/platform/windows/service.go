package windows

import (
	"golang.org/x/sys/windows/svc"
)

// ServiceRunner handles the Windows Service lifecycle for Go binaries
type ServiceRunner struct {
	StopChan chan<- struct{}
}

// Execute implements svc.Handler
func (m *ServiceRunner) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue
	changes <- svc.Status{State: svc.StartPending}

	// Signal start of actual workload if needed
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			// Signal target to stop
			if m.StopChan != nil {
				close(m.StopChan)
			}
			break
		case svc.Pause:
			changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
		case svc.Continue:
			changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
		default:
			// Ignore
		}
	}

	changes <- svc.Status{State: svc.StopPending}
	return
}

// RunAsService enters the service loop if running in service context
func RunAsService(name string, stopChan chan<- struct{}) error {
	return svc.Run(name, &ServiceRunner{StopChan: stopChan})
}

// IsWindowsService checks if the process is running as a Windows Service
func IsWindowsService() bool {
	isService, _ := svc.IsWindowsService()
	return isService
}
