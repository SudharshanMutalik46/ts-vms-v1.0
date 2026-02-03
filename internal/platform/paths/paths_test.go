package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveRoots(t *testing.T) {
	// 1. resolves default InstallRoot/DataRoot correctly on Windows
	os.Unsetenv("VMS_INSTALL_ROOT")
	os.Unsetenv("VMS_DATA_ROOT")
	assert.Equal(t, DefaultInstallRoot, ResolveInstallRoot())
	assert.Equal(t, DefaultDataRoot, ResolveDataRoot())

	os.Setenv("VMS_INSTALL_ROOT", `C:\Custom\Install`)
	os.Setenv("VMS_DATA_ROOT", `C:\Custom\Data`)
	assert.Equal(t, `C:\Custom\Install`, ResolveInstallRoot())
	assert.Equal(t, `C:\Custom\Data`, ResolveDataRoot())
}

func TestSafeJoin(t *testing.T) {
	base := `C:\VMS\Data`

	// 2. rejects path traversal attempts
	cases := []struct {
		name     string
		elements []string
		valid    bool
	}{
		{"normal", []string{"logs", "app.log"}, true},
		{"parent", []string{"..", "other"}, false},
		{"nested_parent", []string{"logs", "..", "..", "secrets"}, false},
		{"absolute", []string{`C:\Windows\System32`}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := SafeJoin(base, tc.elements...)
			if tc.valid {
				assert.NoError(t, err)
				assert.Contains(t, res, base)
			} else {
				if assert.Error(t, err) {
					assert.Contains(t, err.Error(), "traversal")
				}
			}
		})
	}
}

func TestEnsureDirs(t *testing.T) {
	tmpRoot := filepath.Join(os.TempDir(), "vms_test_data")
	os.Setenv("VMS_DATA_ROOT", tmpRoot)
	defer os.RemoveAll(tmpRoot)

	// 3. creates required DataRoot subdirectories
	err := EnsureDirs()
	assert.NoError(t, err)

	subdirs := []string{"config", "logs", "db", "tmp"}
	for _, sub := range subdirs {
		_, err := os.Stat(filepath.Join(tmpRoot, sub))
		assert.NoError(t, err, "subdirectory %s should exist", sub)
	}
}
