package cache

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/spf13/cast"
	"golang.org/x/sync/singleflight"
)

// ErrNotInteger is returned by Increment and Decrement when the stored value
// is not an integer.
var ErrNotInteger = errors.New("cache: value is not an integer")

// item is a single cache entry with an optional expiration deadline. Entries
// are stored as pointers so concurrent compare-and-swap operations compare
// entry identity, never the (possibly uncomparable) user value.
type item struct {
	value    any
	expireAt int64 // UnixNano deadline; 0 means the entry never expires
}

func (i *item) expired(now int64) bool {
	return i.expireAt > 0 && now >= i.expireAt
}

func newItem(value any, t time.Duration) *item {
	it := &item{value: value}
	if t != NoExpiration {
		it.expireAt = time.Now().Add(t).UnixNano()
	}

	return it
}

// Memory is an in-memory Cache implementation. Expired entries are removed
// lazily on access and, when constructed via NewCache, periodically by a
// background janitor.
//
// The zero value is ready to use but runs no janitor: entries then expire
// only lazily, so a workload that writes TTL keys it never reads back will
// retain them indefinitely. Prefer NewCache, or call DeleteExpired
// periodically when using the zero value.
type Memory struct {
	items sync.Map // string -> *item
	group singleflight.Group
}

// load returns the entry stored under key if present and not expired.
// An expired entry is deleted on access; the CompareAndDelete guarantees a
// concurrently written replacement survives.
func (r *Memory) load(key string) (*item, bool) {
	v, exist := r.items.Load(key)
	if !exist {
		return nil, false
	}

	it := v.(*item)
	if it.expired(time.Now().UnixNano()) {
		r.items.CompareAndDelete(key, v)
		return nil, false
	}

	return it, true
}

// Add an item in the cache if the key does not exist or its current entry
// has expired.
func (r *Memory) Add(key string, value any, t time.Duration) bool {
	it := newItem(value, t)
	for {
		old, loaded := r.items.LoadOrStore(key, it)
		if !loaded {
			return true
		}
		if !old.(*item).expired(time.Now().UnixNano()) {
			return false
		}
		if r.items.CompareAndSwap(key, old, it) {
			return true
		}
	}
}

// Decrement decrements the value of an item in the cache.
func (r *Memory) Decrement(key string, value ...int64) (int64, error) {
	delta := int64(1)
	if len(value) > 0 {
		delta = value[0]
	}

	return r.Increment(key, -delta)
}

// Forever Put an item in the cache indefinitely.
func (r *Memory) Forever(key string, value any) bool {
	return r.Put(key, value, NoExpiration) == nil
}

// Forget Remove an item from the cache.
func (r *Memory) Forget(key string) bool {
	r.items.Delete(key)

	return true
}

// Flush Remove all items from the cache.
func (r *Memory) Flush() bool {
	r.items.Clear()

	return true
}

// Get Retrieve an item from the cache by key.
func (r *Memory) Get(key string, def ...any) any {
	if it, ok := r.load(key); ok {
		return it.value
	}

	return defaultValue(def)
}

func (r *Memory) GetBool(key string, def ...bool) bool {
	var d bool
	if len(def) > 0 {
		d = def[0]
	}

	return cast.ToBool(r.Get(key, d))
}

func (r *Memory) GetInt(key string, def ...int) int {
	var d int
	if len(def) > 0 {
		d = def[0]
	}

	return cast.ToInt(r.Get(key, d))
}

func (r *Memory) GetInt64(key string, def ...int64) int64 {
	var d int64
	if len(def) > 0 {
		d = def[0]
	}

	return cast.ToInt64(r.Get(key, d))
}

func (r *Memory) GetString(key string, def ...string) string {
	var d string
	if len(def) > 0 {
		d = def[0]
	}

	return cast.ToString(r.Get(key, d))
}

// Has Checks an item exists in the cache.
func (r *Memory) Has(key string) bool {
	_, ok := r.load(key)

	return ok
}

// Increment increments the value of an item in the cache. Missing keys start
// at zero without expiration; existing entries keep their expiration and must
// hold an integer value.
func (r *Memory) Increment(key string, value ...int64) (int64, error) {
	delta := int64(1)
	if len(value) > 0 {
		delta = value[0]
	}

	fresh := newItem(delta, NoExpiration)
	for {
		old, loaded := r.items.LoadOrStore(key, fresh)
		if !loaded {
			return delta, nil
		}

		it := old.(*item)
		if it.expired(time.Now().UnixNano()) {
			if r.items.CompareAndSwap(key, old, fresh) {
				return delta, nil
			}
			continue
		}

		current, ok := toInt64(it.value)
		if !ok {
			return 0, fmt.Errorf("%w: key %q holds %T", ErrNotInteger, key, it.value)
		}

		next := current + delta
		if r.items.CompareAndSwap(key, old, &item{value: next, expireAt: it.expireAt}) {
			return next, nil
		}
	}
}

func (r *Memory) Lock(key string, t ...time.Duration) *Lock {
	return NewLock(r, key, t...)
}

// Pull Retrieve an item from the cache and delete it atomically.
func (r *Memory) Pull(key string, def ...any) any {
	if v, ok := r.items.LoadAndDelete(key); ok {
		it := v.(*item)
		if !it.expired(time.Now().UnixNano()) {
			return it.value
		}
	}

	return defaultValue(def)
}

// Put stores an item in the cache for a given time, replacing any previous
// entry and its expiration. NoExpiration (0) means the entry never expires;
// a negative duration stores an entry that is already expired.
func (r *Memory) Put(key string, value any, t time.Duration) error {
	r.items.Store(key, newItem(value, t))

	return nil
}

// Remember Get an item from the cache, or execute the given Closure and
// store the result. Concurrent calls for the same key share a single
// callback execution, and a cached nil value counts as a hit.
func (r *Memory) Remember(key string, ttl time.Duration, callback func() (any, error)) (any, error) {
	if it, ok := r.load(key); ok {
		return it.value, nil
	}

	val, err, _ := r.group.Do(key, func() (any, error) {
		if it, ok := r.load(key); ok {
			return it.value, nil
		}

		val, err := callback()
		if err != nil {
			return nil, err
		}
		if err = r.Put(key, val, ttl); err != nil {
			return nil, err
		}

		return val, nil
	})
	if err != nil {
		return nil, err
	}

	return val, nil
}

// RememberForever Get an item from the cache, or execute the given Closure
// and store the result forever.
func (r *Memory) RememberForever(key string, callback func() (any, error)) (any, error) {
	return r.Remember(key, NoExpiration, callback)
}

// WithContext implements Cache. The in-memory driver performs no I/O, so the
// context is unused and the same instance is returned.
func (r *Memory) WithContext(_ context.Context) Cache {
	return r
}

// DeleteExpired removes all expired entries. It is called periodically by
// the janitor started by NewCache and may be called manually on instances
// constructed without one.
func (r *Memory) DeleteExpired() {
	now := time.Now().UnixNano()
	r.items.Range(func(key, v any) bool {
		if v.(*item).expired(now) {
			r.items.CompareAndDelete(key, v)
		}

		return true
	})
}

// ReleaseLock atomically deletes key while it still holds owner's token,
// implementing LockReleaser.
func (r *Memory) ReleaseLock(key, owner string) bool {
	v, exist := r.items.Load(key)
	if !exist {
		return false
	}

	it := v.(*item)
	if it.expired(time.Now().UnixNano()) || it.value != owner {
		return false
	}

	return r.items.CompareAndDelete(key, v)
}

func defaultValue(def []any) any {
	if len(def) == 0 {
		return nil
	}
	if fn, ok := def[0].(func() any); ok {
		return fn()
	}

	return def[0]
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case uint:
		return int64(n), n <= math.MaxInt64
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), n <= math.MaxInt64
	default:
		return 0, false
	}
}
