package circuit_breaker

import "errors"

// MockDiskProvider allows overriding disk stats for testing
type MockDiskProvider struct {
	Stats map[string]VolumeStats
}

func (m *MockDiskProvider) GetStats(path string) (VolumeStats, error) {
	if s, ok := m.Stats[path]; ok {
		return s, nil
	}
	return VolumeStats{}, errors.New("volume not found")
}
