package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	SpoolDir           = "C:\\ProgramData\\TechnoSupport\\VMS\\audit_spool"
	MaxSpoolSize int64 = 1024 * 1024 * 1024 // 1GB
	errStopWalk        = errors.New("stop-walk")
	errNilInfo         = errors.New("audit-spool: nil file info")
	spoolMu      sync.Mutex
)

func ConfigureFailover(dir string, maxMB int64) {
	if dir != "" {
		SpoolDir = dir
	}
	if maxMB > 0 {
		MaxSpoolSize = maxMB * 1024 * 1024
	}
	if err := os.MkdirAll(SpoolDir, 0o755); err != nil {
		log.Printf("audit spool mkdir failed: %v", err)
	}
}

// SpoolEvent writes to a local file (hourly rotated) when DB audit write fails.
func SpoolEvent(evt AuditEvent) error {
	// Hardening: Ensure directory exists
	if err := os.MkdirAll(SpoolDir, 0o755); err != nil {
		return fmt.Errorf("failed to create spool directory: %v", err)
	}

	now := time.Now() // Use a single timestamp
	payload := FailoverEvent{
		EventID:   evt.EventID.String(),
		TenantID:  evt.TenantID.String(),
		Payload:   evt,
		Timestamp: now,
	}

	line, err := json.Marshal(payload) // Marshal before lock
	if err != nil {
		return err
	}

	// Prevent interleaved/corrupted logs and maintain consistent capacity check.
	spoolMu.Lock()
	defer spoolMu.Unlock()

	// Check Bounds
	if isSpoolFull() {
		return fmt.Errorf("audit spool is full")
	}

	// Simple rotation: hourly file
	filename := filepath.Join(SpoolDir, now.Format("20060102_15")+"_audit_spool.log")

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Buffered write reduces syscalls while staying simple.
	bw := bufio.NewWriterSize(f, 64*1024)
	if _, err := bw.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}

	return nil
}

func isSpoolFull() bool {
	st, err := os.Stat(SpoolDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false // missing dir => not full
		}
		log.Printf("audit spool stat error: %v", err)
		return true // fail-closed
	}

	if !st.IsDir() {
		log.Printf("audit spool path is not a directory: %s", SpoolDir)
		return true // fail-closed
	}

	var size int64
	err = filepath.Walk(SpoolDir, func(_ string, info fs.FileInfo, walkErr error) error {
		// IMPORTANT: on error, info can be nil -> must not touch info.*
		if walkErr != nil {
			// file disappeared during walk => ignore
			if os.IsNotExist(walkErr) {
				return nil
			}
			// any other error => fail-closed
			return walkErr
		}
		if info == nil {
			return errNilInfo
		}
		if info.IsDir() {
			return nil
		}

		size += info.Size()
		if size >= MaxSpoolSize {
			return errStopWalk // early stop
		}
		return nil
	})

	// Treat any non-sentinel error as fail-closed.
	if err != nil {
		if errors.Is(err, errStopWalk) || errors.Is(err, errNilInfo) {
			return true
		}
		log.Printf("audit spool walk error: %v", err)
		return true
	}

	return size >= MaxSpoolSize
}

// StartReplayer (Background Worker)
func (s *Service) StartReplayer(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ReplaySpool(ctx)
			}
		}
	}()
}

var replayLock sync.Mutex

func (s *Service) ReplaySpool(ctx context.Context) {
	replayLock.Lock()
	defer replayLock.Unlock()

	entries, err := os.ReadDir(SpoolDir)
	if err != nil {
		return
	}

	cur := time.Now().Format("20060102_15") + "_audit_spool.log"

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == cur || !strings.HasSuffix(name, "_audit_spool.log") {
			continue
		}
		files = append(files, filepath.Join(SpoolDir, name))
	}
	sort.Strings(files)

	if len(files) == 0 {
		return
	}
	target := files[0]

	// “Claim” the file atomically to avoid races at the hour boundary.
	replayFile := filepath.Join(SpoolDir, fmt.Sprintf("replay_%d.log", time.Now().UnixNano()))
	spoolMu.Lock()
	err = os.Rename(target, replayFile)
	spoolMu.Unlock()
	if err != nil {
		return // Likely another process claimed it or it was deleted.
	}

	f, err := os.Open(replayFile)
	if err != nil {
		_ = os.Remove(replayFile)
		return
	}
	defer f.Close()

	// Use bufio.Reader instead of Scanner to handle unlimited line lengths.
	reader := bufio.NewReader(f)
	var succeeded, failed int

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var fe FailoverEvent
			if marshalErr := json.Unmarshal(line, &fe); marshalErr != nil {
				log.Printf("audit replay unmarshal error: %v", marshalErr)
				failed++
			} else {
				if dbErr := s.WriteEvent(ctx, fe.Payload); dbErr != nil {
					failed++
				} else {
					succeeded++
				}
			}
		}
		if err != nil {
			break
		}
	}
	f.Close()

	if succeeded > 0 {
		log.Printf("Audit Replay: %d events flushed", succeeded)
	}

	// No-data-loss policy: delete only if completely successful.
	if failed > 0 {
		failedPath := replayFile + ".failed"
		_ = os.Rename(replayFile, failedPath)
		log.Printf("Audit Replay: %d events failed. Log preserved at %s", failed, failedPath)
	} else {
		_ = os.Remove(replayFile)
	}
}
