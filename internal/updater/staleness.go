package updater

import (
	"time"
)

// StalenessResult describes whether a source needs updating.
type StalenessResult struct {
	SourceID      string
	IsStale       bool
	LastChecked   time.Time
	CheckInterval time.Duration
	TimeSince     time.Duration
}

// CheckStaleness determines if a source is stale based on last_checked and check_interval.
func CheckStaleness(lastChecked time.Time, checkInterval time.Duration) bool {
	if lastChecked.IsZero() {
		return true
	}
	return time.Since(lastChecked) > checkInterval
}

// ParseInterval parses a duration string like "24h", "12h", "7d".
func ParseInterval(s string) (time.Duration, error) {
	// Handle day notation
	if len(s) > 0 && s[len(s)-1] == 'd' {
		days, err := time.ParseDuration(s[:len(s)-1] + "h")
		if err != nil {
			return 0, err
		}
		return days * 24, nil
	}
	return time.ParseDuration(s)
}
