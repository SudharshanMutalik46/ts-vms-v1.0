package retention

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type IProtector interface {
	IsProtected(cameraID string, filename string) bool
}

// ManifestProtector implements event protection using JSONL manifests
// protected_segments/<cameraID>/<YYYY-MM-DD>.jsonl
type ManifestProtector struct {
	manifestRoot string
}

func NewManifestProtector(root string) *ManifestProtector {
	return &ManifestProtector{manifestRoot: root}
}

func (m *ManifestProtector) IsProtected(cameraID string, filename string) bool {
	// Extract YYYY-MM-DD from the filename assuming cam_<date> format, or just scan all recent manifests.
	// We'll scan all manifests for this camera to be safe, or cache them.
	// For Phase 4.4, simple enumeration of the camera's protected files.

	camDir := filepath.Join(m.manifestRoot, cameraID)
	files, err := os.ReadDir(camDir)
	if err != nil {
		return false // No manifest dir means no protection applied
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}

		fPath := filepath.Join(camDir, file.Name())
		f, err := os.Open(fPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var entry struct {
				Segment string `json:"segment"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
				if entry.Segment == filename {
					f.Close()
					return true
				}
			}
		}
		f.Close()
	}

	return false
}
