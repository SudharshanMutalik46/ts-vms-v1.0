package retention

import (
	"log"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

type ISpaceVerifier interface {
	GetFreeSpace(path string) (uint64, error)
	VerifyReclamation(path string, expectedBytes int64, beforeBytes uint64)
}

type SpaceVerifier struct{}

func NewSpaceVerifier() *SpaceVerifier {
	return &SpaceVerifier{}
}

func (s *SpaceVerifier) GetFreeSpace(path string) (uint64, error) {
	if runtime.GOOS == "windows" {
		return s.getFreeSpaceWindows(path)
	}
	// Fallback/stub for Linux
	return s.getFreeSpaceLinux(path)
}

func (s *SpaceVerifier) getFreeSpaceWindows(path string) (uint64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	root := filepath.VolumeName(path)
	if root == "" {
		root = `C:\`
	} else {
		root += `\`
	}

	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}

	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		return 0, err
	}
	return freeBytesAvailable, nil
}

func (s *SpaceVerifier) getFreeSpaceLinux(path string) (uint64, error) {
	// Stub for Linux since focus is Windows
	return 0, nil
}

func (s *SpaceVerifier) VerifyReclamation(path string, expectedBytes int64, beforeBytes uint64) {
	if expectedBytes <= 0 {
		return
	}

	afterBytes, err := s.GetFreeSpace(path)
	if err != nil {
		log.Printf("[WARNING] retention.space_verification_failed volume=%s err=%v", path, err)
		return
	}

	freed := int64(afterBytes) - int64(beforeBytes)

	// Define a tolerance: at least 50% of expected bytes should reflect immediately on NTFS
	if freed < expectedBytes/2 {
		log.Printf("[WARNING] retention.space_verification_failed volume=%s expected=%d verified=%d", path, expectedBytes, freed)
	} else {
		log.Printf("[INFO] retention.space_verified volume=%s freed_bytes=%d", path, freed)
	}
}
