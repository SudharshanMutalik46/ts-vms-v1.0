package audit

import (
	"fmt"
	"time"
)

const MinRetentionYears = 7
const DaysinYear = 365.25 // Approx for leap years

// RetentionPolicyGuard verifies any purge/cleanup operation
func CheckRetentionPolicy(requestedYears int) error {
	if requestedYears < MinRetentionYears {
		return fmt.Errorf("compliance violation: retention must be minimum %d years (requested: %d)", MinRetentionYears, requestedYears)
	}
	return nil
}

// EnsureSafePurgeDate calculates the SAFEST date that can be purged.
// Any date AFTER this result CANNOT be touched.
func EnsureSafePurgeDate() time.Time {
	// Now - 7 Years (Using 2557 days for strict compliance)
	// 2556.75 rounds to 2557 for safety
	days := 2557
	return time.Now().AddDate(0, 0, -days)
}

// CanPurge checks if a timestamp is eligible for purging
func CanPurge(recordTime time.Time) bool {
	safeDate := EnsureSafePurgeDate()
	return recordTime.Before(safeDate)
}
