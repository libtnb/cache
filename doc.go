// Package cache provides a concurrency-safe in-memory cache with TTL
// expiration, atomic counters, and cache-backed mutual-exclusion locks.
//
// The API mirrors the cache module of goravel/framework (itself modeled on
// Laravel's cache): Put/Get/Add/Forever/Remember plus typed getters. Entries
// expire lazily on access; caches built with NewCache additionally run a
// background janitor that sweeps expired entries and stops on its own when
// the cache is garbage collected.
//
//	c := cache.NewCache()
//	_ = c.Put("token", "abc123", time.Minute)
//	token := c.GetString("token")
//
//	value, err := c.Remember("user:1", time.Hour, func() (any, error) {
//		return loadUser(1)
//	})
package cache
