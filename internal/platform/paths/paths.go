package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultInstallRoot = `C:\Program Files\TechnoSupport\VMS`
	DefaultDataRoot    = `C:\ProgramData\TechnoSupport\VMS`
)

// getExecutableDir returns the directory of the current executable.
func getExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// ResolveInstallRoot returns the absolute path to the VMS installation directory.
func ResolveInstallRoot() string {
	root := os.Getenv("VMS_INSTALL_ROOT")
	if root == "" {
		// Check if default exists, else fallback to exe dir
		if _, err := os.Stat(DefaultInstallRoot); err == nil {
			root = DefaultInstallRoot
		} else {
			root = getExecutableDir()
		}
	}
	return root
}

// ResolveDataRoot returns the absolute path to the VMS data directory.
func ResolveDataRoot() string {
	root := os.Getenv("VMS_DATA_ROOT")
	if root == "" {
		// Check if default exists, else fallback to a data folder in the exe dir
		if _, err := os.Stat(DefaultDataRoot); err == nil {
			root = DefaultDataRoot
		} else {
			exeDir := getExecutableDir()
			// If we are in 'bin', data's peer is '../data'
			if strings.HasSuffix(strings.ToLower(exeDir), "bin") {
				root = filepath.Join(filepath.Dir(exeDir), "data")
			} else {
				root = filepath.Join(exeDir, "data")
			}
		}
	}
	return root
}

// ResolveConfigPath returns the absolute path to the default configuration file.
func ResolveConfigPath(customPath string) string {
	if customPath != "" {
		return customPath
	}
	return filepath.Join(ResolveDataRoot(), "config", "default.yaml")
}

// EnsureDirs creates the standard VMS data subdirectories if they don't exist.
func EnsureDirs() error {
	dataRoot := ResolveDataRoot()
	subdirs := []string{
		"config",
		"logs",
		"db",
		"tmp",
	}

	for _, sub := range subdirs {
		path := filepath.Join(dataRoot, sub)
		if err := os.MkdirAll(path, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}
	return nil
}

// SafeJoin joins path elements and ensures the result is within the base directory (no traversal).
func SafeJoin(base string, elements ...string) (string, error) {
	for _, el := range elements {
		if filepath.IsAbs(el) || strings.HasPrefix(el, `\\`) {
			return "", fmt.Errorf("path traversal attempt detected: absolute path or UNC not allowed in elements: %s", el)
		}
	}
	joined := filepath.Join(append([]string{base}, elements...)...)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absJoined, absBase) {
		return "", fmt.Errorf("path traversal attempt detected: %s is outside %s", absJoined, absBase)
	}

	return absJoined, nil
}
