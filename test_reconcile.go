package main

import (
	"fmt"
	"path/filepath"
)

func inferCameraIDFromPath(path string) string {
	// Expected structure: .../camera_uuid/yyyy-mm-dd/hh/segment.mp4
	dir := filepath.Dir(path)                     // hh
	dir = filepath.Dir(dir)                       // yyyy-mm-dd
	cameraDir := filepath.Base(filepath.Dir(dir)) // camera_uuid

	if cameraDir != "" && cameraDir != "." && cameraDir != string(filepath.Separator) && cameraDir != "\\" {
		return cameraDir
	}
	return "unknown"
}

func main() {
	path := `C:\ProgramData\TechnoSupport\VMS\recordings\tenant-default\site-default\cam-test-01\2026-03-10\11\segment_00001.mp4`
	id := inferCameraIDFromPath(path)
	fmt.Printf("Path: %s\nDetected ID: %s (Expected: cam-test-01)\n", path, id)

	if id == "cam-test-01" {
		fmt.Println("SUCCESS: Camera ID correctly inferred.")
	} else {
		fmt.Println("FAILURE: Camera ID incorrect.")
	}
}
