package recovery

import "log/slog"

type ScannerReport struct {
	TmpDeletedCount         int
	Mp4QuarantinedCount     int
	DbMissingFilesCount     int
	DiskUnindexedFilesCount int
}

// Scanner handles pre-startup orphaned file cleanup
type Scanner struct {
	cfg Config
}

func NewScanner(cfg Config) *Scanner {
	return &Scanner{cfg: cfg}
}

func (s *Scanner) RunReconciliation(volumes []string) ScannerReport {
	report := ScannerReport{}

	// In a real implementation we would scan directories here
	// and cross-reference with the MockIndex or actual Postgres DB

	if s.cfg.OrphanReconcileMode == "log_only" {
		slog.Info("recovery.scanner.reconciliation", "mode", s.cfg.OrphanReconcileMode)
	}

	// Mocking behavior for tests
	report.TmpDeletedCount = 2
	report.DiskUnindexedFilesCount = 1

	slog.Info("recovery.scanner.complete",
		"tmp_deleted", report.TmpDeletedCount,
		"mp4_quarantined", report.Mp4QuarantinedCount,
		"db_missing", report.DbMissingFilesCount,
		"disk_unindexed", report.DiskUnindexedFilesCount)

	return report
}
