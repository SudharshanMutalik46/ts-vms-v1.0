package recording

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// FlushToDisk ensures all OS buffers for the file are written to durable storage.
func FlushToDisk(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// ComputeSHA256 calculates the real SHA-256 hash of a file.
func ComputeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// FinalizeSegment performs the strict pipeline: Flush -> Rename -> Checksum.
// It returns the final path and the checksum.
func FinalizeSegment(tmpPath string) (string, string, error) {
	// 1. Flush
	if err := FlushToDisk(tmpPath); err != nil {
		return "", "", fmt.Errorf("flush failed: %w", err)
	}

	// 2. Determine final path
	finalPath := strings.TrimSuffix(tmpPath, ".tmp")
	if !strings.HasSuffix(finalPath, ".mkv") {
		finalPath += ".mkv"
	}

	// 3. Rename
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", "", fmt.Errorf("rename failed: %w", err)
	}

	// 4. Checksum
	checksum, err := ComputeSHA256(finalPath)
	if err != nil {
		return finalPath, "", fmt.Errorf("checksum failed: %w", err)
	}

	return finalPath, checksum, nil
}
