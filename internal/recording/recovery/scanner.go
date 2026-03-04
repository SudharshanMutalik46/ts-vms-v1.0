package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ScannerReport struct {
	TmpDeletedCount         int
	Mp4QuarantinedCount     int
	DbMissingFilesCount     int
	DiskUnindexedFilesCount int
}

type FileFinding struct {
	Path      string
	Kind      string
	SizeBytes int64
	ModTime   time.Time
}

type Scanner struct {
	cfg Config
}

func NewScanner(cfg Config) *Scanner {
	return &Scanner{cfg: cfg}
}

func (s *Scanner) Scan(volumes []string) ([]FileFinding, ScannerReport, error) {
	report := ScannerReport{}
	findings := make([]FileFinding, 0, 128)

	for _, root := range volumes {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".tmp":
				findings = append(findings, FileFinding{Path: path, Kind: "tmp", SizeBytes: info.Size(), ModTime: info.ModTime()})
			case ".mp4", ".mkv":
				kind := "video"
				if info.Size() == 0 {
					kind = "corrupt"
					report.Mp4QuarantinedCount++
				}
				findings = append(findings, FileFinding{Path: path, Kind: kind, SizeBytes: info.Size(), ModTime: info.ModTime()})
			}
			return nil
		})
		if err != nil {
			return nil, report, err
		}
	}
	return findings, report, nil
}
