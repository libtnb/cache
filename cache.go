package cache

import (
	"context"
	"runtime"
	"time"
)

type Cache interface {
	// Add an item in the cache if the key does not exist.
	Add(key string, value any, t time.Duration) bool
	// Decrement decrements the value of an item in the cache.
	Decrement(key string, value ...int64) (int64, error)
	// Forever add an item in the cache indefinitely.
	Forever(key string, value any) bool
	// Forget removes an item from the cache.
	Forget(key string) bool
	// Flush remove all items from the cache.
	Flush() bool
	// Get retrieve an item from the cache by key.
	// The optional default may be a plain value or a func() any that is
	// invoked lazily when the key is missing; any other function signature
	// is returned as-is.
	Get(key string, def ...any) any
	// GetBool retrieves an item from the cache by key as a boolean.
	GetBool(key string, def ...bool) bool
	// GetInt retrieves an item from the cache by key as an integer.
	GetInt(key string, def ...int) int
	// GetInt64 retrieves an item from the cache by key as a 64-bit integer.
	GetInt64(key string, def ...int64) int64
	// GetString retrieves an item from the cache by key as a string.
	GetString(key string, def ...string) string
	// Has check an item exists in the cache.
	Has(key string) bool
	// Increment increments the value of an item in the cache.
	Increment(key string, value ...int64) (int64, error)
	// Lock get a lock instance.
	Lock(key string, t ...time.Duration) *Lock
	// Put stores an item in the cache for a given time.
	// NoExpiration (0) means the entry never expires; a negative duration
	// stores an entry that is already expired.
	Put(key string, value any, t time.Duration) error
	// Pull retrieve an item from the cache and delete it.
	Pull(key string, def ...any) any
	// Remember gets an item from the cache, or execute the given Closure and store the result.
	Remember(key string, ttl time.Duration, callback func() (any, error)) (any, error)
	// RememberForever get an item from the cache, or execute the given Closure and store the result forever.
	RememberForever(key string, callback func() (any, error)) (any, error)
	// WithContext returns a Cache instance using the given context for
	// operations that support it.
	WithContext(ctx context.Context) Cache
}

// Option configures the Cache returned by NewCache.
type Option func(*options)

type options struct {
	cleanupInterval time.Duration
}

// WithCleanupInterval sets how often the background janitor sweeps expired
// entries. A zero or negative interval disables the janitor entirely, in
// which case expired entries are only removed lazily when accessed.
func WithCleanupInterval(d time.Duration) Option {
	return func(o *options) {
		o.cleanupInterval = d
	}
}

// NewCache returns an in-memory Cache. Unless disabled via
// WithCleanupInterval, a background janitor sweeps expired entries every
// DefaultCleanupInterval and stops automatically once the returned Cache
// becomes unreachable.
func NewCache(opts ...Option) Cache {
	o := options{cleanupInterval: DefaultCleanupInterval}
	for _, opt := range opts {
		opt(&o)
	}

	m := &Memory{}
	if o.cleanupInterval <= 0 {
		return m
	}

	j := &janitor{interval: o.cleanupInterval, stop: make(chan struct{})}
	go j.run(m)

	// The janitor goroutine keeps m alive, so the cleanup must be attached
	// to the wrapper handed to the caller and must not reference it.
	w := &janitorCache{Memory: m}
	runtime.AddCleanup(w, func(stop chan struct{}) { close(stop) }, j.stop)

	return w
}

// janitorCache ties the janitor's lifetime to the instance held by the
// caller: once it is garbage collected, the janitor goroutine is stopped.
type janitorCache struct {
	*Memory
}

// WithContext keeps returning the wrapper so the janitor's lifetime stays
// tied to the instance the caller holds.
func (w *janitorCache) WithContext(_ context.Context) Cache {
	return w
}

type janitor struct {
	interval time.Duration
	stop     chan struct{}
}

func (j *janitor) run(m *Memory) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.DeleteExpired()
		case <-j.stop:
			return
		}
	}
}

// GetAs retrieves the value stored under key and type-asserts it to T. It
// returns def (or T's zero value) when the key is missing or the stored
// value is not a T. Unlike GetBool/GetInt/GetString it performs no
// conversion between types.
func GetAs[T any](c Cache, key string, def ...T) T {
	if v, ok := c.Get(key).(T); ok {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}

	var zero T
	return zero
}
