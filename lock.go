package cache

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// blockRetryInterval is how often Block retries acquiring the lock.
const blockRetryInterval = 250 * time.Millisecond

// LockStore is the storage a Lock needs: acquisition via Add, owner-checked
// atomic release via ReleaseLock, and unconditional deletion via Forget.
// ReleaseLock must delete key only while it still holds owner's token, as a
// non-atomic check-then-delete could remove a lock acquired by someone else
// in between. Requiring it at construction time guarantees every acquirable
// lock can also be released safely.
type LockStore interface {
	Add(key string, value any, t time.Duration) bool
	Forget(key string) bool
	ReleaseLock(key, owner string) bool
}

// Lock is a mutual-exclusion lock backed by a LockStore. Every instance
// carries a unique owner token: releasing only succeeds while the lock is
// still held by that owner, so an instance whose lock already expired cannot
// release a competitor's lock.
type Lock struct {
	store    LockStore
	key      string
	owner    string
	ttl      *time.Duration
	acquired bool
}

func NewLock(instance LockStore, key string, t ...time.Duration) *Lock {
	l := &Lock{
		store: instance,
		key:   key,
		owner: newOwner(),
	}
	if len(t) > 0 {
		l.ttl = &t[0]
	}

	return l
}

func newOwner() string {
	b := make([]byte, 16)
	// crypto/rand.Read is documented to always succeed since Go 1.24.
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}

// Block waits up to t for the lock to become available, retrying every
// blockRetryInterval, and reports whether it was acquired. When a callback
// is given, the lock is released after the callback runs.
func (r *Lock) Block(t time.Duration, callback ...func()) bool {
	if r.Get(callback...) {
		return true
	}

	timer := time.NewTimer(t)
	defer timer.Stop()
	ticker := time.NewTicker(blockRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timer.C:
			return r.Get(callback...)
		case <-ticker.C:
			if r.Get(callback...) {
				return true
			}
		}
	}
}

// Get attempts to acquire the lock and reports whether it succeeded. When a
// callback is given and the lock is acquired, the callback runs before the
// lock is released again; Get still returns true when that release fails
// (e.g. the lock expired while the callback ran), since the callback did
// execute.
func (r *Lock) Get(callback ...func()) bool {
	var ok bool
	if r.ttl == nil {
		ok = r.store.Add(r.key, r.owner, NoExpiration)
	} else {
		ok = r.store.Add(r.key, r.owner, *r.ttl)
	}
	if !ok {
		return false
	}

	r.acquired = true

	if len(callback) == 0 {
		return true
	}

	defer r.Release()
	callback[0]()

	return true
}

// Release releases the lock if this instance still holds it. It returns
// false when the lock was never acquired, already released, or has expired
// and may since be held by another owner.
func (r *Lock) Release() bool {
	if !r.acquired {
		return false
	}
	if !r.store.ReleaseLock(r.key, r.owner) {
		return false
	}
	r.acquired = false

	return true
}

// ForceRelease releases the lock regardless of its current owner.
func (r *Lock) ForceRelease() bool {
	return r.store.Forget(r.key)
}
