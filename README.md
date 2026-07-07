# Cache

[![Go Reference](https://pkg.go.dev/badge/github.com/libtnb/cache.svg)](https://pkg.go.dev/github.com/libtnb/cache)
[![Test](https://github.com/libtnb/cache/actions/workflows/test.yml/badge.svg)](https://github.com/libtnb/cache/actions/workflows/test.yml)

A concurrency-safe in-memory cache for Go with TTL expiration, atomic
counters, singleflight-backed `Remember`, and cache-backed locks. The API
mirrors the cache module of [goravel/framework](https://github.com/goravel/framework)
(itself modeled on Laravel's cache).

## Installation

```bash
go get github.com/libtnb/cache
```

## Quick start

```go
package main

import (
	"fmt"
	"time"

	"github.com/libtnb/cache"
)

func main() {
	c := cache.NewCache()

	_ = c.Put("token", "abc123", time.Minute) // expires after 1 minute
	c.Forever("site", "libtnb")               // never expires

	fmt.Println(c.GetString("token"))     // "abc123"
	fmt.Println(c.GetInt("missing", 42))  // 42 (default)
	fmt.Println(c.Has("site"))            // true
	fmt.Println(c.Pull("token"))          // "abc123", then removed atomically
}
```

### Expiration

- `NoExpiration` (`0`) means an entry never expires.
- A negative duration stores an entry that is already expired.
- Expired entries are removed lazily on access. Caches built with `NewCache`
  additionally run a background janitor (default: every 5 minutes) that
  sweeps expired entries and stops on its own once the cache is garbage
  collected. Tune or disable it with `WithCleanupInterval`:

```go
c := cache.NewCache(cache.WithCleanupInterval(time.Minute)) // custom interval
c = cache.NewCache(cache.WithCleanupInterval(0))            // lazy expiration only
```

Without a janitor (interval `0`, or a zero-value `&cache.Memory{}`), expired
entries are only reclaimed when accessed — write-only workloads should call
`DeleteExpired()` periodically.

### Counters

`Increment`/`Decrement` work on any integer value and are safe under
concurrency. Missing keys start at zero without expiration; existing entries
keep their expiration.

```go
n, _ := c.Increment("visits")     // 1
n, _ = c.Increment("visits", 10)  // 11
n, _ = c.Decrement("visits")      // 10
```

### Remember

`Remember` returns the cached value, or computes and stores it. Concurrent
calls for the same key share a single callback execution.

```go
user, err := c.Remember("user:1", time.Hour, func() (any, error) {
	return loadUser(1)
})
```

### Locks

Every lock carries a unique owner token: a lock whose TTL already expired
cannot release a competitor's lock.

```go
lock := c.Lock("deploy", 10*time.Second)
if lock.Get() {
	defer lock.Release()
	// critical section
}

// Or wait up to 5 seconds for the lock, releasing automatically:
c.Lock("deploy").Block(5*time.Second, func() {
	// critical section
})
```

### Typed access

`GetBool`/`GetInt`/`GetInt64`/`GetString` coerce the stored value. The
generic `GetAs` type-asserts instead, returning the default (or zero value)
on a type mismatch:

```go
user := cache.GetAs[User](c, "user:1")
name := cache.GetAs(c, "name", "default")
```
