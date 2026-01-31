package windows

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows/svc/eventlog"
)

// EventLogger handles writing to Windows Event Log with a fallback to standard log
type EventLogger struct {
	source string
	elog   *eventlog.Log
}

// NewEventLogger creates a new logger for the specified source.
// It assumes the source has been registered by the installer.
func NewEventLogger(source string) *EventLogger {
	l, err := eventlog.Open(source)
	if err != nil {
		log.Printf("Warning: Could not open Windows Event Log source '%s': %v. Falling back to stdout.", source, err)
		return &EventLogger{source: source}
	}
	return &EventLogger{source: source, elog: l}
}

// Info logs an informational event.
func (l *EventLogger) Info(eid uint32, msg string) {
	if l.elog != nil {
		l.elog.Info(eid, msg)
	}
	log.Printf("[INFO] %s: %s", l.source, msg)
}

// Warning logs a warning event.
func (l *EventLogger) Warning(eid uint32, msg string) {
	if l.elog != nil {
		l.elog.Warning(eid, msg)
	}
	log.Printf("[WARN] %s: %s", l.source, msg)
}

// Error logs an error event. No secrets should be passed here.
func (l *EventLogger) Error(eid uint32, msg string) {
	if l.elog != nil {
		l.elog.Error(eid, msg)
	}
	fmt.Fprintf(os.Stderr, "[ERROR] %s: %s\n", l.source, msg)
}

// Close releases the event log handle.
func (l *EventLogger) Close() {
	if l.elog != nil {
		l.elog.Close()
	}
}
