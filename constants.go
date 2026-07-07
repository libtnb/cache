package cache

import (
	"time"
)

const (
	// NoExpiration indicates that an entry never expires.
	NoExpiration time.Duration = 0

	// DefaultCleanupInterval is how often the janitor started by NewCache
	// sweeps expired entries. Override it with WithCleanupInterval.
	DefaultCleanupInterval = 5 * time.Minute
)
