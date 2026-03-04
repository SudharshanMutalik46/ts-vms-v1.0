package retention

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SegmentMeta struct {
	TenantID  string
	SiteID    string
	CameraID  string
	Path      string
	Filename  string
	StartTime time.Time
	SizeBytes int64
}

type ISegmentEnumerator interface {
	Enumerate(volumeRoot string) ([]SegmentMeta, error)
}

// FileSystemEnumerator walks <volume_root>/<tenant>/<site>/<camera>/<YYYY-MM-DD>/<HH>/*.mp4
type FileSystemEnumerator struct{}

func NewFileSystemEnumerator() *FileSystemEnumerator {
	return &FileSystemEnumerator{}
}

func (e *FileSystemEnumerator) isVideoSegment(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	// Accept new MKV files AND legacy MP4 files
	return ext == ".mkv" || ext == ".mp4"
}

func (e *FileSystemEnumerator) Enumerate(volumeRoot string) ([]SegmentMeta, error) {
	var segments []SegmentMeta

	err := filepath.Walk(volumeRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if !e.isVideoSegment(path) {
			return nil
		}

		// Extract context from path
		// Expected relative: tenant/site/camera/YYYY-MM-DD/HH/file.mp4
		rel, err := filepath.Rel(volumeRoot, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 6 {
			return nil
		}

		tenant := parts[0]
		site := parts[1]
		cam := parts[2]

		// Parse timestamp from filename: cam-01_1708945200000_60_1.mp4
		fname := info.Name()
		nameParts := strings.Split(fname, "_")

		var startTime time.Time
		if len(nameParts) >= 2 {
			tsMs, err := strconv.ParseInt(nameParts[1], 10, 64)
			if err == nil {
				startTime = time.UnixMilli(tsMs)
			} else {
				startTime = info.ModTime() // fallback
			}
		} else {
			startTime = info.ModTime() // fallback
		}

		segments = append(segments, SegmentMeta{
			TenantID:  tenant,
			SiteID:    site,
			CameraID:  cam,
			Path:      path,
			Filename:  fname,
			StartTime: startTime,
			SizeBytes: info.Size(),
		})

		return nil
	})

	// Sort oldest first
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].StartTime.Before(segments[j].StartTime)
	})

	return segments, err
}
